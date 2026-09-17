//go:build windows

// Windows implementation.
//
// Pure Go throughout: Win32 through golang.org/x/sys/windows, COM through
// hand-rolled vtable calls on top of go-ole. Wails compiles its Windows
// backend as pure Go, so keeping this file cgo-free is what lets a Windows
// build be cross-compiled from a Mac.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Capabilities describes what the Windows build can do. The gaps are real, not
// unfinished work: Windows has no Quick Look, no purgeable-space accounting,
// and no per-app disk privacy grant.
func Capabilities() Caps {
	return Caps{
		OS:              "windows",
		Preview:         false,
		Properties:      true,
		Snapshots:       false, // VSS shadow copies are a later feature
		Purgeable:       false,
		Helper:          true,
		HelperInstall:   false, // elevated on demand, nothing to install
		FullDiskAccess:  false,
		RevealLabel:     "Show in Explorer",
		TrashLabel:      "Recycle Bin",
		TrashVerb:       "Move to Recycle Bin",
		PropertiesLabel: "Properties",
		ModKey:          "Ctrl+",
	}
}

// SupportDir is where preferences and the scan cache live.
func SupportDir() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		return "", fmt.Errorf("could not locate the application data directory")
	}
	dir := filepath.Join(base, AppDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procGetLogicalDriveStringsW = kernel32.NewProc("GetLogicalDriveStringsW")
	procGetDriveTypeW           = kernel32.NewProc("GetDriveTypeW")
	procGetDiskFreeSpaceExW     = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetVolumeInformationW   = kernel32.NewProc("GetVolumeInformationW")
	procSHObjectProperties      = shell32.NewProc("SHObjectProperties")
)

// Drive types, from GetDriveType.
const (
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDRom     = 5
)

// ListVolumes returns every drive the user can scan.
func ListVolumes(withIcons bool) ([]Volume, error) {
	buf := make([]uint16, 512)
	n, _, err := procGetLogicalDriveStringsW.Call(
		uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if n == 0 {
		return nil, fmt.Errorf("listing drives: %w", err)
	}

	out := []Volume{}
	for _, root := range splitDriveStrings(buf[:n]) {
		v, ok := describeDrive(root, withIcons)
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// splitDriveStrings walks the double-NUL-terminated list GetLogicalDriveStrings
// returns.
func splitDriveStrings(b []uint16) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c != 0 {
			continue
		}
		if i > start {
			out = append(out, windows.UTF16ToString(b[start:i]))
		}
		start = i + 1
	}
	return out
}

func describeDrive(root string, withIcons bool) (Volume, bool) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return Volume{}, false
	}
	kind, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p)))
	switch uint32(kind) {
	case driveRemovable, driveFixed, driveRemote, driveCDRom:
	default:
		// Unknown, no-root-directory or RAM disks: nothing a user scans.
		return Volume{}, false
	}

	// A drive with no media — an empty card reader — answers the type query
	// but not the capacity one, and must not appear as a scannable volume.
	var freeToCaller, total, free uint64
	ok, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)))
	if ok == 0 || total == 0 {
		return Volume{}, false
	}

	name := make([]uint16, 261)
	fsName := make([]uint16, 261)
	var serial, maxComp, flags uint32
	procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComp)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)))

	label := windows.UTF16ToString(name)
	letter := strings.TrimSuffix(root, `\`)
	if label == "" {
		label = "Local Disk"
	}
	const fileReadOnlyVolume = 0x00080000

	return Volume{
		Name: fmt.Sprintf("%s (%s)", label, letter),
		Path: root,
		// The serial is the closest thing Windows has to a stable volume
		// identity, and is what the scan cache is keyed by.
		UUID:      fmt.Sprintf("%08X", serial),
		Total:     int64(total),
		Free:      int64(freeToCaller),
		AvailRaw:  int64(freeToCaller),
		Purgeable: 0, // no Windows equivalent
		Used:      int64(total) - int64(free),
		Removable: uint32(kind) == driveRemovable || uint32(kind) == driveCDRom,
		Internal:  uint32(kind) == driveFixed,
		Network:   uint32(kind) == driveRemote,
		ReadOnly:  flags&fileReadOnlyVolume != 0 || uint32(kind) == driveCDRom,
		// The drive holding Windows is the one most people mean by "this disk".
		IsRoot: strings.EqualFold(root, systemDriveRoot()),
		FS:     windows.UTF16ToString(fsName),
	}, true
}

func systemDriveRoot() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return d + `\`
	}
	return `C:\`
}

var (
	volMu   sync.Mutex
	volHook func()
	volStop chan struct{}
)

// WatchVolumes polls for drives appearing and disappearing.
//
// The tidy answer is a hidden window receiving WM_DEVICECHANGE, but that needs
// a message loop on a thread this package does not own. Polling the drive
// bitmask is one cheap call and misses nothing a user would notice.
func WatchVolumes(fn func()) {
	volMu.Lock()
	defer volMu.Unlock()
	volHook = fn
	if volStop != nil {
		return
	}
	stop := make(chan struct{})
	volStop = stop

	getDrives := kernel32.NewProc("GetLogicalDrives")
	go func() {
		last, _, _ := getDrives.Call()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				now, _, _ := getDrives.Call()
				if now == last {
					continue
				}
				last = now
				volMu.Lock()
				h := volHook
				volMu.Unlock()
				if h != nil {
					go h()
				}
			}
		}
	}()
}

// UnwatchVolumes stops the poll.
func UnwatchVolumes() {
	volMu.Lock()
	defer volMu.Unlock()
	if volStop != nil {
		close(volStop)
		volStop = nil
	}
	volHook = nil
}

// Reveal selects the given paths in Explorer.
//
// Explorer takes one selection per invocation, so several paths mean several
// windows — the same as Finder opening one window per directory.
func Reveal(paths []string) error {
	for _, p := range paths {
		// Deliberately not exec.Command with /select as one argument:
		// explorer requires the comma form with no space, and Go would quote
		// it into something explorer ignores.
		cmd := exec.Command("explorer.exe", "/select,"+filepath.Clean(p))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		// explorer.exe returns a non-zero exit code even when it succeeds.
		_ = cmd.Start()
	}
	return nil
}

// ShowProperties opens the system's file properties window.
func ShowProperties(path string) error {
	p, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return err
	}
	const pageGeneral = 0
	r, _, e := procSHObjectProperties.Call(
		0, // no parent window
		2, // SHOP_FILEPATH
		uintptr(unsafe.Pointer(p)),
		uintptr(pageGeneral))
	if r == 0 {
		return fmt.Errorf("could not open Properties: %w", e)
	}
	return nil
}

// Open hands a path to its default application.
func Open(path string) error {
	return shellExecute("open", filepath.Clean(path), "")
}

// OpenURL opens a URL in the default browser, and is also how the app reaches
// Settings pages.
func OpenURL(url string) error { return shellExecute("open", url, "") }

// OpenAccessSettings has nowhere to go on Windows: there is no per-app disk
// privacy grant. Capabilities().FullDiskAccess is false so nothing offers it.
func OpenAccessSettings() error { return nil }

// OpenHelperApprovalSettings likewise: the helper is elevated on demand.
func OpenHelperApprovalSettings() {}

// PreviewItems has no Windows equivalent. Quick Look is a macOS feature;
// Capabilities().Preview is false, and the interface opens the file instead.
func PreviewItems(paths []string, index int) {}

// ClosePreview is a no-op for the same reason.
func ClosePreview() {}

// PreviewVisible is always false.
func PreviewVisible() bool { return false }

// CheckFullAccess reports whether this process can read everything. Windows has
// no per-app grant, so the question is simply whether it is elevated.
func CheckFullAccess() AccessStatus {
	if isElevated() {
		return AccessStatus{Granted: true, Probe: "process is elevated"}
	}
	return AccessStatus{
		Granted: false,
		Probe:   "process is not elevated",
		Detail:  "Folders belonging to other users and some system folders cannot be read. Scan as administrator to include them.",
	}
}

func isElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// ListLocalSnapshots has no Windows implementation yet. VSS shadow copies are
// the equivalent and are a later feature; Capabilities().Snapshots is false.
func ListLocalSnapshots(mount string) ([]Snapshot, error) { return []Snapshot{}, nil }

// DeleteLocalSnapshot likewise.
func DeleteLocalSnapshot(mount, name string) error {
	return fmt.Errorf("snapshots are not supported on Windows")
}

// IsPackaged reports whether the app was installed rather than run from a build
// directory. Used to decide whether the updater can work.
func IsPackaged() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// An installed build sits under Program Files or the per-user Programs
	// directory; a development build does not.
	lower := strings.ToLower(exe)
	for _, marker := range []string{`\program files`, `\programs\`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// AppID identifies the application to the system.
func AppID() string { return "com.sr3d.diskspacesaver" }

// SigningID is the Authenticode publisher of the running binary, empty when
// unsigned. Filled in with the helper's peer verification.
func SigningID() string { return "" }

// HelperPlistName has no meaning on Windows; kept so callers compile.
const HelperPlistName = ""

// RegisterHelper is a no-op: the Windows helper is elevated per scan rather
// than installed. Capabilities().HelperInstall is false.
func RegisterHelper() error { return nil }

// UnregisterHelper likewise.
func UnregisterHelper() error { return nil }

// HelperRegistration reports the helper as always available on demand.
func HelperRegistration() string { return "on-demand" }
