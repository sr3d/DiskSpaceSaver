//go:build darwin

package scanner

/*
#include <sys/attr.h>
#include <sys/vnode.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>

// getattrlistbulk returns a packed, variable-length record per directory entry.
// Walking it in C avoids doing pointer arithmetic on a []byte from Go for every
// field of every file.
//
// Each record begins with its own length, then a returned-attribute bitmap,
// then the requested attributes in a fixed order. Anything not returned for a
// given entry is simply absent, so the fields have to be consumed in order and
// guarded by the bitmap rather than read at fixed offsets.
typedef struct {
    unsigned int  valid;      // which of the fields below were returned
    unsigned long long fileid;
    unsigned int  objtype;
    unsigned int  nlink;
    long long     logical;    // ATTR_FILE_DATALENGTH / directory size
    long long     alloc;      // ATTR_FILE_ALLOCSIZE
    long long     mtime;
    unsigned int  flags;
    unsigned int  namelen;
    // Offset of the name within the caller's buffer rather than a pointer:
    // this struct lives in Go memory, and Go memory handed to C may not
    // contain Go pointers.
    unsigned int  nameoff;
} dss_bulk_entry;

enum {
    DSS_HAVE_NAME    = 1 << 0,
    DSS_HAVE_TYPE    = 1 << 1,
    DSS_HAVE_ID      = 1 << 2,
    DSS_HAVE_NLINK   = 1 << 3,
    DSS_HAVE_LOGICAL = 1 << 4,
    DSS_HAVE_ALLOC   = 1 << 5,
    DSS_HAVE_MTIME   = 1 << 6,
    DSS_HAVE_FLAGS   = 1 << 7,
};

// dss_bulk_attrlist is the attribute set requested. Keep in sync with the
// decoding order below: the kernel packs them in the order the bitmaps declare.
static void dss_bulk_attrlist(struct attrlist *al) {
    memset(al, 0, sizeof(*al));
    al->bitmapcount = ATTR_BIT_MAP_COUNT;
    al->commonattr  = ATTR_CMN_RETURNED_ATTRS | ATTR_CMN_NAME | ATTR_CMN_OBJTYPE |
                      ATTR_CMN_FILEID | ATTR_CMN_MODTIME | ATTR_CMN_FLAGS;
    al->fileattr    = ATTR_FILE_LINKCOUNT | ATTR_FILE_TOTALSIZE | ATTR_FILE_ALLOCSIZE;
    al->dirattr     = 0;
}

// dss_bulk_read fills entries[] from one getattrlistbulk call.
// Returns the number of entries, 0 at end of directory, or -1 with errno set.
// A record the kernel has written has already been consumed from the
// directory, so truncating the decode loses those entries permanently. The
// caller sizes entries[] from a lower bound on record size; overflowing it
// would be a bug, and is reported rather than quietly dropping files.
#define DSS_BULK_OVERFLOW (-2)

static int dss_bulk_read(int fd, void *buf, size_t buflen,
                         dss_bulk_entry *entries, int max) {
    struct attrlist al;
    dss_bulk_attrlist(&al);

    int count = getattrlistbulk(fd, &al, buf, buflen, 0);
    if (count <= 0) return count;
    if (count > max) return DSS_BULK_OVERFLOW;

    char *cursor = (char *)buf;
    for (int i = 0; i < count; i++) {
        char *field = cursor;
        unsigned int reclen = *(unsigned int *)field;
        field += sizeof(unsigned int);

        attribute_set_t returned = *(attribute_set_t *)field;
        field += sizeof(attribute_set_t);

        dss_bulk_entry *e = &entries[i];
        memset(e, 0, sizeof(*e));

        if (returned.commonattr & ATTR_CMN_NAME) {
            attrreference_t *ref = (attrreference_t *)field;
            const char *nameptr = ((const char *)ref) + ref->attr_dataoffset;
            e->nameoff = (unsigned int)(nameptr - (const char *)buf);
            e->namelen = ref->attr_length ? ref->attr_length - 1 : 0;
            e->valid |= DSS_HAVE_NAME;
            field += sizeof(attrreference_t);
        }
        if (returned.commonattr & ATTR_CMN_OBJTYPE) {
            e->objtype = *(fsobj_type_t *)field;
            e->valid |= DSS_HAVE_TYPE;
            field += sizeof(fsobj_type_t);
        }
        if (returned.commonattr & ATTR_CMN_FILEID) {
            e->fileid = *(u_int64_t *)field;
            e->valid |= DSS_HAVE_ID;
            field += sizeof(u_int64_t);
        }
        if (returned.commonattr & ATTR_CMN_MODTIME) {
            struct timespec *ts = (struct timespec *)field;
            e->mtime = ts->tv_sec;
            e->valid |= DSS_HAVE_MTIME;
            field += sizeof(struct timespec);
        }
        if (returned.commonattr & ATTR_CMN_FLAGS) {
            e->flags = *(u_int32_t *)field;
            e->valid |= DSS_HAVE_FLAGS;
            field += sizeof(u_int32_t);
        }
        if (returned.fileattr & ATTR_FILE_LINKCOUNT) {
            e->nlink = *(u_int32_t *)field;
            e->valid |= DSS_HAVE_NLINK;
            field += sizeof(u_int32_t);
        }
        if (returned.fileattr & ATTR_FILE_TOTALSIZE) {
            e->logical = *(off_t *)field;
            e->valid |= DSS_HAVE_LOGICAL;
            field += sizeof(off_t);
        }
        if (returned.fileattr & ATTR_FILE_ALLOCSIZE) {
            e->alloc = *(off_t *)field;
            e->valid |= DSS_HAVE_ALLOC;
            field += sizeof(off_t);
        }

        cursor += reclen;
    }
    return count;
}
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// errNoBulk signals that the caller should fall back to readdir+fstatat.
var errNoBulk = errors.New("getattrlistbulk: unsupported")

func errBulkUnsupported(err error) bool { return errors.Is(err, errNoBulk) }

// bulkBufBytes is the buffer the kernel packs records into. 64 KiB holds
// several hundred entries, so even a large directory needs only a few calls.
const bulkBufBytes = 64 << 10

// minRecordBytes is a deliberate underestimate of the smallest record
// getattrlistbulk can emit: 4 (length) + 20 (returned-attribute bitmap) +
// 8 (name reference) + 4 (type) + 8 (id) + 16 (mtime) + 4 (flags) plus at
// least a one-character name. Dividing by it gives a count the kernel cannot
// exceed, which is what makes the decode loop safe without truncating.
const minRecordBytes = 48

const bulkMaxEntries = bulkBufBytes / minRecordBytes

type bulkBuf struct {
	raw     []byte
	entries []C.dss_bulk_entry
}

// Buffers are pooled: a scan makes at least one call per directory, and
// allocating these each time would dominate the walk.
var bulkPool = sync.Pool{
	New: func() any {
		return &bulkBuf{
			raw:     make([]byte, bulkBufBytes),
			entries: make([]C.dss_bulk_entry, bulkMaxEntries),
		}
	},
}

// listDirBulk reads a directory with getattrlistbulk(2): one syscall per batch
// of entries, returning name, type, size, allocated size, link count and flags
// together. The portable path needs a readdir plus one fstatat per entry, so
// this replaces N+1 syscalls with roughly N/128.
func (s *Scanner) listDirBulk(job dirJob, frame *Frame, children *[]dirJob) (int, error) {
	f, err := os.Open(job.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fd := C.int(f.Fd())

	buf := bulkPool.Get().(*bulkBuf)
	defer bulkPool.Put(buf)

	frame.Entry = frame.Entry[:0]
	if cap(frame.Entry) == 0 {
		frame.Entry = make([]Entry, 0, 64)
	}

	for {
		// The two-result form hands back errno, which is the only way to tell
		// "this filesystem cannot do it" from a real failure.
		n, callErr := C.dss_bulk_read(fd,
			unsafe.Pointer(&buf.raw[0]), C.size_t(len(buf.raw)),
			&buf.entries[0], C.int(len(buf.entries)))
		if n == C.DSS_BULK_OVERFLOW {
			// Cannot happen given the bound above; falling back is still
			// better than losing entries if it ever does.
			return 0, errNoBulk
		}
		if n < 0 {
			// A filesystem without support reports ENOTSUP; fall back to the
			// portable path rather than failing the directory.
			if errno, ok := callErr.(unix.Errno); ok {
				if errno == unix.ENOTSUP || errno == unix.EINVAL || errno == unix.ENOTTY {
					return 0, errNoBulk
				}
			}
			if len(frame.Entry) > 0 {
				return len(frame.Entry), nil // a partial listing beats none
			}
			if callErr == nil {
				callErr = errors.New("unknown error")
			}
			return 0, os.NewSyscallError("getattrlistbulk", callErr)
		}
		if n == 0 {
			break // end of directory
		}

		for i := 0; i < int(n); i++ {
			e := &buf.entries[i]
			if e.valid&C.DSS_HAVE_NAME == 0 || e.namelen == 0 {
				continue
			}
			off, l := int(e.nameoff), int(e.namelen)
			if off < 0 || l < 0 || off+l > len(buf.raw) {
				continue // a malformed record is skipped, never trusted
			}
			name := string(buf.raw[off : off+l])
			if name == "" || name == "." || name == ".." {
				continue
			}
			frame.Entry = append(frame.Entry, s.bulkEntry(job, name, e, children))
		}
	}
	return len(frame.Entry), nil
}

// bulkEntry mirrors makeEntry for the bulk path.
func (s *Scanner) bulkEntry(job dirJob, name string, e *C.dss_bulk_entry, children *[]dirJob) Entry {
	out := Entry{
		Name:    name,
		Logical: int64(e.logical),
		Alloc:   int64(e.alloc),
		Mtime:   int64(e.mtime),
		Flags:   flagsFor(name, uint32(e.flags)),
	}
	if uint32(e.flags)&sfDataless != 0 {
		out.Flags |= FlagCloud
		out.Alloc = 0
	}

	switch e.objtype {
	case C.VDIR:
		out.Kind = KindDir
		if packageExts[strings.ToLower(filepath.Ext(name))] {
			out.Kind = KindPackage
		}
		// getattrlistbulk returns file attributes, not directory ones, so the
		// size fields are meaningless here and are taken from the stat below
		// instead.
		out.Alloc, out.Logical = 0, 0
	case C.VLNK:
		out.Kind = KindSymlink
		s.files.Add(1)
		s.bytes.Add(out.Alloc)
		return out
	case C.VREG:
		out.Kind = KindFile
	default:
		out.Kind = KindOther
	}

	if out.Kind == KindFile || out.Kind == KindOther {
		if e.valid&C.DSS_HAVE_NLINK != 0 && e.nlink > 1 {
			if !s.links.firstSeen(job.dev, uint64(e.fileid)) {
				out.Flags |= FlagDup
				out.Alloc = 0
			}
		}
		s.files.Add(1)
		s.bytes.Add(out.Alloc)
		return out
	}

	// One stat per directory, which the bulk path can well afford: it has just
	// saved one per file. It supplies the device number — getattrlistbulk
	// reports none, and without it a volume scan would wander onto any other
	// disk mounted inside the tree — and the directory's own size, so both
	// scan paths report the same totals.
	full := filepath.Join(job.path, name)
	var st unix.Stat_t
	statErr := unix.Lstat(full, &st)
	if statErr == nil {
		out.Alloc = int64(st.Blocks) * 512
		out.Logical = st.Size
	}

	switch {
	case !s.opt.ScanCloudStorage && (s.isCloudRoot(full) || out.Flags&FlagCloud != 0):
		out.Flags |= FlagCloud
		out.Alloc, out.Logical = 0, 0
	case alwaysSkip[name] || skipAbs[full]:
		out.Flags |= FlagExcluded
	case s.excluded(full):
		out.Flags |= FlagExcluded
	case statErr != nil:
		out.Flags |= FlagNoPerm
		s.noperm.Add(1)
	case uint64(st.Dev) != job.dev && !s.opt.CrossDevice:
		out.Flags |= FlagMountPt
	default:
		out.DirID = s.nextID.Add(1)
		*children = append(*children, dirJob{path: full, id: out.DirID, dev: job.dev})
	}
	s.bytes.Add(out.Alloc)
	return out
}
