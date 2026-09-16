//go:build darwin

// Package macos wraps the AppKit and Security calls the app needs. Everything
// that touches AppKit is marshalled onto the main thread inside the ObjC shim,
// because bound API calls arrive on arbitrary goroutines.
package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework Quartz -framework Security -framework Foundation -framework ServiceManagement -framework QuickLookThumbnailing
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Volume describes a mounted filesystem.
type Volume struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	UUID      string `json:"uuid"`
	Total     int64  `json:"total"`
	Free      int64  `json:"free"`     // available for important usage — Finder's number
	AvailRaw  int64  `json:"availRaw"` // available without evicting purgeable data
	Purgeable int64  `json:"purgeable"`
	Used      int64  `json:"used"`
	Removable bool   `json:"removable"`
	Internal  bool   `json:"internal"`
	Network   bool   `json:"network"`
	ReadOnly  bool   `json:"readOnly"`
	IsRoot    bool   `json:"isRoot"`
	FS        string `json:"fs"`
	Icon      string `json:"icon,omitempty"`
}

func goString(c *C.char) string {
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

// errFrom turns a shim's returned error string into a Go error.
func errFrom(c *C.char) error {
	s := goString(c)
	if s == "" {
		return nil
	}
	return errors.New(s)
}

// ListVolumes returns every browsable mounted volume.
func ListVolumes(withIcons bool) ([]Volume, error) {
	raw := goString(C.dss_list_volumes(C.bool(withIcons)))
	var vs []Volume
	if err := json.Unmarshal([]byte(raw), &vs); err != nil {
		return nil, fmt.Errorf("decoding volume list: %w", err)
	}
	return vs, nil
}

var (
	volMu   sync.Mutex
	volHook func()
)

// WatchVolumes installs a callback fired on mount, unmount and rename.
func WatchVolumes(fn func()) {
	volMu.Lock()
	volHook = fn
	volMu.Unlock()
	C.dss_watch_volumes()
}

// UnwatchVolumes stops volume notifications.
func UnwatchVolumes() {
	C.dss_unwatch_volumes()
	volMu.Lock()
	volHook = nil
	volMu.Unlock()
}

func fireVolumesChanged() {
	volMu.Lock()
	fn := volHook
	volMu.Unlock()
	if fn != nil {
		go fn()
	}
}

// Trash moves a path to the user's Trash, returning its new location.
func Trash(path string) (string, error) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	var newPath *C.char
	if err := errFrom(C.dss_trash(cp, &newPath)); err != nil {
		return "", err
	}
	return goString(newPath), nil
}

func cStrings(paths []string) (**C.char, func()) {
	if len(paths) == 0 {
		return nil, func() {}
	}
	arr := C.malloc(C.size_t(len(paths)) * C.size_t(unsafe.Sizeof(uintptr(0))))
	slice := unsafe.Slice((**C.char)(arr), len(paths))
	for i, p := range paths {
		slice[i] = C.CString(p)
	}
	return (**C.char)(arr), func() {
		for i := range slice {
			C.free(unsafe.Pointer(slice[i]))
		}
		C.free(arr)
	}
}

// Reveal selects the given paths in Finder.
func Reveal(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	arr, free := cStrings(paths)
	defer free()
	return errFrom(C.dss_reveal((**C.char)(unsafe.Pointer(arr)), C.int(len(paths))))
}

// GetInfo opens Finder's Get Info window for a path.
func GetInfo(path string) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return errFrom(C.dss_get_info(cp))
}

// Open hands a path to its default application.
func Open(path string) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return errFrom(C.dss_open(cp))
}

// OpenURL opens a URL, including System Settings deep links.
func OpenURL(url string) error {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	return errFrom(C.dss_open_url(cu))
}

// QuickLook toggles the Quick Look panel over the given paths.
func QuickLook(paths []string, index int) {
	if len(paths) == 0 {
		return
	}
	arr, free := cStrings(paths)
	defer free()
	C.dss_quicklook((**C.char)(unsafe.Pointer(arr)), C.int(len(paths)), C.int(index))
}

// CloseQuickLook hides the panel if it is up.
func CloseQuickLook() { C.dss_quicklook_close() }

// QuickLookVisible reports whether the panel is showing.
func QuickLookVisible() bool { return bool(C.dss_quicklook_visible()) }

// SetDockBadge sets the Dock icon badge; empty clears it.
func SetDockBadge(text string) {
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	C.dss_set_dock_badge(ct)
}

// SetDockProgress draws a progress bar on the Dock icon. Negative clears it.
func SetDockProgress(fraction float64) { C.dss_set_dock_progress(C.double(fraction)) }

// IsBundled reports whether the process is running from a .app. Several
// features (the updater, helper registration) only work when it is.
func IsBundled() bool { return bool(C.dss_is_bundled()) }

// BundleID is the running bundle's identifier.
func BundleID() string { return goString(C.dss_bundle_id()) }

// TeamID is the Team ID from the running code's own signature, empty when
// unsigned or ad-hoc signed.
func TeamID() string { return goString(C.dss_team_id()) }

// FDAStatus reports whether the app has Full Disk Access.
type FDAStatus struct {
	Granted bool   `json:"granted"`
	Probe   string `json:"probe"`
	Detail  string `json:"detail,omitempty"`
}

// CheckFullDiskAccess probes a TCC-protected location. Reading the TCC
// database itself is the canonical test: nothing but a Full Disk Access
// grant makes it readable, and the probe has no side effects.
func CheckFullDiskAccess() FDAStatus {
	home, _ := os.UserHomeDir()
	probes := []string{
		filepath.Join(home, "Library/Application Support/com.apple.TCC/TCC.db"),
		filepath.Join(home, "Library/Safari/Bookmarks.plist"),
		filepath.Join(home, "Library/Mail"),
	}
	var lastErr string
	for _, p := range probes {
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				continue // nothing to learn from a path this Mac does not have
			}
			lastErr = err.Error()
			continue
		}
		f, err := os.Open(p)
		if err == nil {
			f.Close()
			return FDAStatus{Granted: true, Probe: p}
		}
		if os.IsPermission(err) {
			return FDAStatus{Granted: false, Probe: p, Detail: err.Error()}
		}
		lastErr = err.Error()
	}
	// Every probe was absent: assume granted rather than nag the user with a
	// prompt we cannot substantiate.
	return FDAStatus{Granted: true, Probe: "none available", Detail: lastErr}
}

// FDASettingsURL deep-links to the Full Disk Access pane.
const FDASettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

// LoginItemsSettingsURL deep-links to Login Items, where a registered daemon
// must be approved.
const LoginItemsSettingsURL = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"

// Snapshot is one APFS local Time Machine snapshot.
type Snapshot struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

// ListLocalSnapshots reads the APFS local snapshots for a mount point. These
// are a common reason a volume reports far less free space than the visible
// files explain.
func ListLocalSnapshots(mount string) ([]Snapshot, error) {
	if mount == "" {
		mount = "/"
	}
	out, err := exec.Command("/usr/bin/tmutil", "listlocalsnapshots", mount).Output()
	if err != nil {
		// tmutil exits non-zero when Time Machine has never run here, which is
		// a legitimate "no snapshots" answer rather than a failure.
		return nil, nil
	}
	var snaps []Snapshot
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "com.apple.TimeMachine.") {
			continue
		}
		name := line
		date := strings.TrimPrefix(name, "com.apple.TimeMachine.")
		date = strings.TrimSuffix(date, ".local")
		snaps = append(snaps, Snapshot{Name: name, Date: date})
	}
	return snaps, nil
}

// DeleteLocalSnapshot removes one snapshot by its date stamp. tmutil accepts
// the date portion, not the full name.
func DeleteLocalSnapshot(mount, name string) error {
	date := strings.TrimPrefix(name, "com.apple.TimeMachine.")
	date = strings.TrimSuffix(date, ".local")
	if date == "" {
		return errors.New("empty snapshot name")
	}
	if mount == "" {
		mount = "/"
	}
	out, err := exec.Command("/usr/bin/tmutil", "deletelocalsnapshots", date).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmutil deletelocalsnapshots %s: %s", date, strings.TrimSpace(string(out)))
	}
	return nil
}

// PurgeableBytes is the gap between what Finder says is available and what is
// actually free right now.
func PurgeableBytes(v Volume) int64 {
	if v.Free > v.AvailRaw {
		return v.Free - v.AvailRaw
	}
	return 0
}

// FormatBytes renders a byte count the way the UI does, for log and menu text.
func FormatBytes(b int64, base2 bool) string {
	unit := int64(1000)
	suffix := []string{"kB", "MB", "GB", "TB", "PB"}
	if base2 {
		unit = 1024
		suffix = []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	}
	if b < unit {
		return strconv.FormatInt(b, 10) + " B"
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit && exp < len(suffix)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), suffix[exp])
}

// Thumbnail renders a preview of a file as a PNG data URI. It returns an empty
// string when the system has no representation for that file, which is the
// normal answer for most things and not an error.
//
// Blocks while the thumbnail is produced, so callers should not hold a lock.
func Thumbnail(path string, maxPoints int, scale float64) string {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	if maxPoints <= 0 {
		maxPoints = 512
	}
	if scale <= 0 {
		scale = 2
	}
	return goString(C.dss_thumbnail(cp, C.int(maxPoints), C.double(scale)))
}

// HelperPlistName is the daemon plist embedded in Contents/Library/LaunchDaemons.
const HelperPlistName = "com.sr3d.diskspacesaver.helper.plist"

// RegisterHelper installs the privileged helper as a launchd daemon. macOS
// shows the user an approval prompt in Login Items the first time.
func RegisterHelper() error {
	cn := C.CString(HelperPlistName)
	defer C.free(unsafe.Pointer(cn))
	return errFrom(C.dss_helper_register(cn))
}

// UnregisterHelper removes the privileged helper.
func UnregisterHelper() error {
	cn := C.CString(HelperPlistName)
	defer C.free(unsafe.Pointer(cn))
	return errFrom(C.dss_helper_unregister(cn))
}

// HelperRegistration reports SMAppService's view of the daemon: one of
// "not-registered", "enabled", "requires-approval", "not-found", "unknown" or
// "unsupported".
func HelperRegistration() string {
	cn := C.CString(HelperPlistName)
	defer C.free(unsafe.Pointer(cn))
	return goString(C.dss_helper_status(cn))
}

// OpenLoginItemsSettings shows the pane where a pending helper is approved.
func OpenLoginItemsSettings() { C.dss_open_login_items_settings() }
