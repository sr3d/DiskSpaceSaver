//go:build darwin

package scanner

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Directory listing on macOS: readdir plus one fstatat per entry. This is the
// portable-POSIX path; bulk_darwin.go has the getattrlistbulk fast path that
// replaces roughly N syscalls with N/128.

// Always skipped: synthetic or bookkeeping trees that either are not real
// storage or that macOS refuses to let anyone read.
// Always skipped: synthetic or bookkeeping trees that either are not real
// storage or that macOS refuses to let anyone read.
var alwaysSkip = map[string]bool{
	".Spotlight-V100":         true,
	".fseventsd":              true,
	".DocumentRevisions-V100": true,
	".TemporaryItems":         true,
	".vol":                    true,
}

var skipAbs = map[string]bool{
	"/dev":     true,
	"/net":     true,
	"/home":    true,
	"/Volumes": true,
}

// packageExts are directories macOS presents as single documents. They are
// still walked (so the total is right and the user can drill in) but flagged
// so the UI can treat them as leaves.
var packageExts = map[string]bool{
	".app": true, ".framework": true, ".bundle": true, ".plugin": true,
	".kext": true, ".prefpane": true, ".qlgenerator": true, ".mdimporter": true,
	".xpc": true, ".appex": true, ".photoslibrary": true, ".musiclibrary": true,
	".tvlibrary": true, ".imovielibrary": true, ".theater": true,
	".fcpbundle": true, ".aplibrary": true, ".logicx": true, ".band": true,
	".sparsebundle": true, ".rtfd": true, ".xcodeproj": true, ".xcworkspace": true,
	".playground": true, ".download": true, ".pages": true, ".numbers": true,
	".key": true, ".scptd": true, ".pkg": true, ".workflow": true,
}

// ResolveScanRoot maps a user-visible root to the path that actually holds the
// data. "/" is a sealed, read-only system snapshot on macOS; everything a user
// can reclaim lives on the Data volume, and scanning "/" would both
// double-count firmlinked directories and report space nobody can free.
func ResolveScanRoot(path string) string {
	clean := filepath.Clean(path)
	if clean == "/" {
		if st, err := os.Stat("/System/Volumes/Data"); err == nil && st.IsDir() {
			return "/System/Volumes/Data"
		}
	}
	return clean
}

// SF_DATALESS marks an item whose contents live somewhere else and would have
// to be fetched. golang.org/x/sys/unix does not export it.
const sfDataless = 0x40000000

// isDataless reports whether an item's contents are not present locally.
func isDataless(st *unix.Stat_t) bool {
	return st.Flags&sfDataless != 0
}

// cloudRootsFor lists, for one home directory, the directories whose children
// are provider roots and the provider roots themselves.
func cloudRootsFor(home string) (parents, exact []string) {
	if home == "" {
		return nil, nil
	}
	return []string{
		// Every macOS 12+ file provider domain appears here as a child.
		filepath.Join(home, "Library", "CloudStorage"),
	}, []string{
		filepath.Join(home, "Library", "Mobile Documents"), // iCloud Drive
	}
}

// rootVolumeID identifies the volume a scan starts on, so a child with a
// different one can be recognised as a mount point.
func rootVolumeID(path string) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return 0, &os.PathError{Op: "lstat", Path: path, Err: err}
	}
	return uint64(st.Dev), nil
}

func (s *Scanner) listDirNative(job dirJob, frame *Frame, children *[]dirJob) (int, error) {
	f, err := os.Open(job.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fd := int(f.Fd())

	names, err := f.Readdirnames(-1)
	if err != nil && len(names) == 0 {
		return 0, err
	}
	readErr := err

	frame.Entry = make([]Entry, 0, len(names))
	var st unix.Stat_t
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		if serr := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); serr != nil {
			frame.Entry = append(frame.Entry, Entry{
				Name: name, Kind: KindOther, Flags: flagsFor(name, 0) | FlagNoPerm,
			})
			s.noperm.Add(1)
			continue
		}
		e := s.makeEntry(job, name, &st, children)
		frame.Entry = append(frame.Entry, e)
	}
	return len(frame.Entry), readErr
}

// makeEntry turns a stat result into an Entry, applying hardlink dedupe,
// package detection, mount-point and exclusion rules, and enqueues the
// directory for walking when appropriate.
func (s *Scanner) makeEntry(job dirJob, name string, st *unix.Stat_t, children *[]dirJob) Entry {
	e := Entry{
		Name:    name,
		Logical: st.Size,
		Alloc:   int64(st.Blocks) * 512,
		Mtime:   st.Mtim.Sec,
		Flags:   flagsFor(name, st.Flags),
	}

	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		e.Kind = KindDir
		if packageExts[strings.ToLower(filepath.Ext(name))] {
			e.Kind = KindPackage
		}
	case unix.S_IFLNK:
		// Never follow: a symlink occupies only its own inode, and following
		// would both double-count and risk cycles.
		e.Kind = KindSymlink
		s.files.Add(1)
		s.bytes.Add(e.Alloc)
		return e
	case unix.S_IFREG:
		e.Kind = KindFile
	default:
		e.Kind = KindOther
	}

	if isDataless(st) {
		// Not on this disk: it occupies no local blocks and reading it would
		// pull it over the network.
		e.Flags |= FlagCloud
		e.Alloc = 0
	}

	if e.Kind == KindFile || e.Kind == KindOther {
		// Hardlinks: charge the space to whichever path we reach first and
		// zero it everywhere else, so the tree total matches the volume.
		if st.Nlink > 1 && !s.links.firstSeen(uint64(st.Dev), st.Ino) {
			e.Flags |= FlagDup
			e.Alloc = 0
		}
		s.files.Add(1)
		s.bytes.Add(e.Alloc)
		return e
	}

	// Directory (or package): decide whether to descend.
	full := filepath.Join(job.path, name)
	switch {
	case !s.opt.ScanCloudStorage && (s.isCloudRoot(full) || e.Flags&FlagCloud != 0):
		e.Flags |= FlagCloud
		e.Alloc, e.Logical = 0, 0
	case alwaysSkip[name] || skipAbs[full]:
		e.Flags |= FlagExcluded
	case s.excluded(full):
		e.Flags |= FlagExcluded
	case uint64(st.Dev) != job.dev && !s.opt.CrossDevice:
		e.Flags |= FlagMountPt
	default:
		e.DirID = s.nextID.Add(1)
		*children = append(*children, dirJob{path: full, id: e.DirID, dev: job.dev})
	}
	// A directory's own inode occupies space too; keep it, it is small but
	// real and it makes empty-directory trees show up as nonzero.
	s.bytes.Add(e.Alloc)
	return e
}

func flagsFor(name string, statFlags uint32) uint8 {
	var f uint8
	if strings.HasPrefix(name, ".") || statFlags&uint32(unix.UF_HIDDEN) != 0 {
		f |= FlagHidden
	}
	return f
}

// AllocatedSize is what one already-stat'd file occupies on disk, in the same
// terms the walk records. It is exported for the monitor, which re-measures a
// watched path directly rather than through a scan, and must not report a
// different number for the same file than the chart does.
func AllocatedSize(path string, fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*unix.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return fi.Size()
}
