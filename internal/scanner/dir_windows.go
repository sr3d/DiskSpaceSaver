//go:build windows

package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Directory listing on Windows: FindFirstFileEx per directory, plus one
// GetCompressedFileSize per regular file for the allocated size.
//
// No cgo anywhere in this file. Wails compiles its Windows backend as pure Go,
// so keeping this package cgo-free is what lets a Windows build be
// cross-compiled from a Mac; introducing cgo here would tie every build to a
// Windows host.
//
// bulk_windows.go will later add the NtQueryDirectoryFile fast path, which
// returns the allocated size in the enumeration itself and so removes the
// per-file call — the same relationship getattrlistbulk has to fstatat on
// macOS.

// Attributes x/sys/windows does not name.
const (
	fileAttributeRecallOnOpen       = 0x00040000
	fileAttributeRecallOnDataAccess = 0x00400000
)

// Reparse tags worth telling apart. A junction or a mounted volume is a
// boundary the walk stops at; a symlink is reported and never followed.
const (
	reparseTagMountPoint = 0xA0000003
	reparseTagSymlink    = 0xA000000C
	reparseTagOneDrive   = 0x9000701A
)

// Skipped everywhere: bookkeeping trees that are not reclaimable storage and
// that Windows will not let an ordinary process read anyway.
var alwaysSkip = map[string]bool{
	"System Volume Information": true,
	"$Recycle.Bin":              true,
	"$RECYCLE.BIN":              true,
	"$WinREAgent":               true,
}

// skipAbs is empty on Windows: there is no equivalent of /dev or /Volumes, and
// drive roots are scanned directly rather than being reached through a tree.
var skipAbs = map[string]bool{}

// packageExts is empty on Windows. macOS presents .app and friends as single
// documents; Windows has no bundle concept, and treating .exe or a folder as a
// leaf would hide exactly what the user is looking for.
var packageExts = map[string]bool{}

// ResolveScanRoot is the identity on Windows. The macOS version exists to
// redirect "/" to the Data volume around the sealed system snapshot; a drive
// letter has no such indirection.
func ResolveScanRoot(path string) string {
	return filepath.Clean(path)
}

// cloudRootsFor has no path-based providers to name on Windows. OneDrive,
// Dropbox and Google Drive all mark their placeholders with file attributes
// instead, which is both more reliable and provider-agnostic — see cloudFlags.
func cloudRootsFor(home string) (parents, exact []string) {
	return nil, nil
}

// longPath prefixes a path so it escapes the legacy 260-character limit. Deep
// node_modules trees exceed it routinely, and without this a scan simply stops
// partway down with a confusing error.
func longPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if len(p) < 240 {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC` + p[1:]
	}
	return `\\?\` + p
}

// rootVolumeID identifies the volume a scan starts on, so a child on a
// different one can be recognised as a mount point.
func rootVolumeID(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return 0, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p,
		0, // no access needed: metadata only
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	return uint64(info.VolumeSerialNumber), nil
}

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procFindFirstFileExW  = kernel32.NewProc("FindFirstFileExW")
	procGetCompressedSize = kernel32.NewProc("GetCompressedFileSizeW")
)

const (
	findExInfoBasic       = 1
	findExSearchNameMatch = 0
	findFirstExLargeFetch = 2
	invalidFileSize       = 0xFFFFFFFF
)

// allocatedSize is what the chart draws: the space the volume actually gives
// back. GetCompressedFileSize accounts for NTFS compression and sparse ranges,
// which a plain file size does not; on failure the logical size is the honest
// fallback rather than zero.
func allocatedSize(path string, logical int64) int64 {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return logical
	}
	var high uint32
	low, _, _ := procGetCompressedSize.Call(
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == invalidFileSize {
		return logical
	}
	return int64(high)<<32 | int64(uint32(low))
}

// findFirstEx opens an enumeration with the flags that matter for speed:
// FindExInfoBasic skips the 8.3 short name the filesystem would otherwise have
// to look up, and LARGE_FETCH asks for bigger batches from the driver.
func findFirstEx(pattern string, data *windows.Win32finddata) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(pattern)
	if err != nil {
		return windows.InvalidHandle, err
	}
	r, _, e := procFindFirstFileExW.Call(
		uintptr(unsafe.Pointer(p)),
		findExInfoBasic,
		uintptr(unsafe.Pointer(data)),
		findExSearchNameMatch,
		0,
		findFirstExLargeFetch)
	if windows.Handle(r) == windows.InvalidHandle {
		return windows.InvalidHandle, e
	}
	return windows.Handle(r), nil
}

func (s *Scanner) listDirNative(job dirJob, frame *Frame, children *[]dirJob) (int, error) {
	var data windows.Win32finddata
	h, err := findFirstEx(longPath(filepath.Join(job.path, "*")), &data)
	if err != nil {
		return 0, &os.PathError{Op: "readdir", Path: job.path, Err: err}
	}
	defer windows.FindClose(h)

	frame.Entry = frame.Entry[:0]
	if cap(frame.Entry) == 0 {
		frame.Entry = make([]Entry, 0, 64)
	}

	for {
		name := windows.UTF16ToString(data.FileName[:])
		if name != "." && name != ".." && name != "" {
			frame.Entry = append(frame.Entry, s.makeEntry(job, name, &data, children))
		}
		if err := windows.FindNextFile(h, &data); err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			// A partial listing beats none: the directory is still reported
			// with whatever was read before the failure.
			return len(frame.Entry), nil
		}
	}
	return len(frame.Entry), nil
}

// cloudFlags reports whether an entry is a cloud placeholder whose contents are
// not on this disk. Every provider that uses the cloud filter driver — OneDrive,
// Dropbox, Google Drive — sets one of these, so this needs no list of names.
func cloudFlags(attrs, reparseTag uint32) bool {
	if attrs&(fileAttributeRecallOnDataAccess|fileAttributeRecallOnOpen) != 0 {
		return true
	}
	if attrs&windows.FILE_ATTRIBUTE_OFFLINE != 0 {
		return true
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && reparseTag == reparseTagOneDrive
}

// makeEntry turns one enumeration record into an Entry, applying the cloud,
// reparse-point and exclusion rules, and enqueues the directory for walking
// when appropriate.
func (s *Scanner) makeEntry(job dirJob, name string, d *windows.Win32finddata, children *[]dirJob) Entry {
	attrs := d.FileAttributes
	tag := d.Reserved0 // the reparse tag, when the reparse-point attribute is set

	e := Entry{
		Name:    name,
		Logical: int64(d.FileSizeHigh)<<32 | int64(d.FileSizeLow),
		Mtime:   time.Unix(0, d.LastWriteTime.Nanoseconds()).Unix(),
	}
	// Hidden and system both mean "not part of what the user manages"; Explorer
	// hides them together and so does the list.
	if attrs&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0 {
		e.Flags |= FlagHidden
	}

	isReparse := attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	isDir := attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0

	if cloudFlags(attrs, tag) {
		// Not on this disk: it occupies no local space, and reading it would
		// pull it over the network.
		e.Flags |= FlagCloud
		e.Kind = KindFile
		if isDir {
			e.Kind = KindDir
		}
		e.Alloc, e.Logical = 0, 0
		s.files.Add(1)
		return e
	}

	switch {
	case isReparse && tag == reparseTagSymlink:
		// Never followed: a symlink occupies only its own record, and
		// following would double-count and risk cycles.
		e.Kind = KindSymlink
		s.files.Add(1)
		return e
	case isDir:
		e.Kind = KindDir
	default:
		e.Kind = KindFile
	}

	if e.Kind == KindFile {
		full := filepath.Join(job.path, name)
		e.Alloc = allocatedSize(full, e.Logical)
		s.files.Add(1)
		s.bytes.Add(e.Alloc)
		return e
	}

	full := filepath.Join(job.path, name)
	switch {
	case isReparse && tag == reparseTagMountPoint:
		// A junction or a volume mounted into this tree. Descending would
		// either leave the volume or walk the same bytes twice.
		e.Flags |= FlagMountPt
	case alwaysSkip[name] || skipAbs[full]:
		e.Flags |= FlagExcluded
	case s.excluded(full):
		e.Flags |= FlagExcluded
	default:
		e.DirID = s.nextID.Add(1)
		*children = append(*children, dirJob{path: full, id: e.DirID, dev: job.dev})
	}
	return e
}

// AllocatedSize is what one already-stat'd file occupies on disk, in the same
// terms the walk records. It is exported for the monitor, which re-measures a
// watched path directly rather than through a scan, and must not report a
// different number for the same file than the chart does.
func AllocatedSize(path string, fi os.FileInfo) int64 {
	return allocatedSize(path, fi.Size())
}
