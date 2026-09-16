// Package scanner walks a directory tree in parallel and emits Frames: one
// frame per directory, carrying every entry in it. Frames are the only output
// format. A normal in-process scan pipes them over a Go channel; an
// administrator scan serialises the identical frames over the helper socket.
// Both cases feed the same tree builder, so the UI path never branches.
package scanner

// Kind classifies a directory entry.
type Kind uint8

const (
	KindFile Kind = iota
	KindDir
	KindSymlink
	KindPackage // .app, .photoslibrary, ... — a directory shown as a leaf
	KindOther   // sockets, devices, fifos
)

// Entry flags.
const (
	FlagDup      = 1 << iota // hardlink whose size was already counted elsewhere
	FlagHidden               // name starts with "." or has UF_HIDDEN
	FlagMountPt              // a different st_dev; not descended into
	FlagExcluded             // matched an exclusion rule; not descended into
	FlagNoPerm               // could not be read (EPERM/EACCES)
)

// Entry is one directory entry as the scanner saw it.
//
// Alloc is the number the chart uses: allocated blocks (st_blocks*512), which
// is what the volume actually gives up when the file is deleted. Logical is
// st_size, which is what Finder's Get Info shows.
type Entry struct {
	Name    string `msgpack:"n"`
	Kind    Kind   `msgpack:"k"`
	Flags   uint8  `msgpack:"f"`
	Logical int64  `msgpack:"l"`
	Alloc   int64  `msgpack:"a"`
	// DirID is the scanner-assigned id of this entry when it is a directory
	// that will be descended into. Zero for everything else. A frame for
	// DirID always arrives after the frame that introduced it, so a consumer
	// can build the tree incrementally with no reordering buffer.
	DirID uint32 `msgpack:"d"`
	// Mtime is unix seconds, for the "oldest files" style views and for
	// cache staleness checks.
	Mtime int64 `msgpack:"m"`
}

// Frame is the complete listing of one directory.
type Frame struct {
	DirID uint32  `msgpack:"i"`
	Entry []Entry `msgpack:"e"`
	// Err is set when the directory could not be opened or fully read. The
	// frame is still emitted (possibly with a partial or empty Entry list) so
	// the tree records the node as inaccessible rather than dropping it.
	Err string `msgpack:"x"`
}

// Progress is emitted periodically while a scan runs.
type Progress struct {
	Files        int64  `json:"files"`
	Dirs         int64  `json:"dirs"`
	Bytes        int64  `json:"bytes"`
	Inaccessible int64  `json:"inaccessible"`
	CurrentPath  string `json:"currentPath"`
	Done         bool   `json:"done"`
	Err          string `json:"err,omitempty"`
}

// RootDirID is the id the scanner always assigns to the scan root.
const RootDirID uint32 = 1
