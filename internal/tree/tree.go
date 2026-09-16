// Package tree holds the scan result in Go and answers viewport queries.
//
// Rule #1 of the design: the tree never crosses into JavaScript. The frontend
// receives small viewport slices keyed by stable uint32 handles, never paths
// and never the whole tree. A multi-million-node scan therefore costs the
// WebView nothing.
package tree

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sr3d/diskspacesaver/internal/scanner"
)

const (
	chunkBits = 15
	chunkSize = 1 << chunkBits
	chunkMask = chunkSize - 1
)

// Invalid is the zero handle; slab index 0 is never a real node.
const Invalid uint32 = 0

// Node flags layered on top of the scanner's entry flags.
const (
	FlagDeleted = 1 << 6 // pruned after a trash/delete; skipped by every query
	FlagPartial = 1 << 7 // subtree incomplete (scan cancelled or errored here)
)

// Node is one filesystem entry. Alloc/Logical are aggregates over the whole
// subtree; Self/SelfLog are the entry's own contribution.
type Node struct {
	Name     string
	Alloc    int64
	Logical  int64
	Self     int64
	SelfLog  int64
	Parent   uint32
	Children []uint32
	Files    int32
	Dirs     int32
	Mtime    int64
	Kind     uint8
	Flags    uint8
	Err      string
}

// Tree is a scan result. Safe for concurrent readers while the builder writes.
type Tree struct {
	mu     sync.RWMutex
	chunks [][]Node
	n      uint32

	Root     uint32
	RootPath string

	// dirIndex maps a scanner DirID to a slab handle.
	dirIndex []uint32

	sorted atomic.Bool
	errs   atomic.Int64
}

// New creates a tree rooted at rootPath, with the root node already in place.
func New(rootPath string) *Tree {
	t := &Tree{RootPath: filepath.Clean(rootPath), dirIndex: make([]uint32, 2)}
	name := filepath.Base(t.RootPath)
	if name == "/" || name == "." {
		name = t.RootPath
	}
	t.Root = t.alloc(Node{Name: name, Kind: uint8(scanner.KindDir), Parent: Invalid})
	t.dirIndex[scanner.RootDirID] = t.Root
	return t
}

// alloc appends a node. Caller must hold the write lock, except during New.
func (t *Tree) alloc(n Node) uint32 {
	id := t.n + 1 // handle 0 stays reserved
	ci := int(id >> chunkBits)
	for ci >= len(t.chunks) {
		t.chunks = append(t.chunks, make([]Node, chunkSize))
	}
	t.chunks[ci][id&chunkMask] = n
	t.n = id
	return id
}

// node returns a pointer into the slab. Chunks never move once allocated, so
// the pointer stays valid as long as the caller holds a lock.
func (t *Tree) node(id uint32) *Node {
	if id == Invalid || id > t.n {
		return nil
	}
	return &t.chunks[id>>chunkBits][id&chunkMask]
}

// Count returns the number of nodes allocated so far.
func (t *Tree) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return int(t.n)
}

// Errors returns how many entries could not be read.
func (t *Tree) Errors() int64 { return t.errs.Load() }

// Apply folds one scanner frame into the tree and bubbles the new bytes up the
// ancestor chain, so every aggregate is queryable mid-scan.
func (t *Tree) Apply(f scanner.Frame) {
	t.mu.Lock()

	var parent uint32
	if int(f.DirID) < len(t.dirIndex) {
		parent = t.dirIndex[f.DirID]
	}
	pn := t.node(parent)
	if pn == nil {
		// A frame for a directory we never introduced: only possible from a
		// corrupt stream. Dropping it is strictly better than corrupting the
		// tree.
		t.mu.Unlock()
		return
	}

	if f.Err != "" {
		pn.Err = f.Err
		pn.Flags |= scanner.FlagNoPerm
		t.errs.Add(1)
	}

	var dAlloc, dLog int64
	var nFiles, nDirs int32

	if cap(pn.Children) == 0 && len(f.Entry) > 0 {
		pn.Children = make([]uint32, 0, len(f.Entry))
	}
	for i := range f.Entry {
		e := &f.Entry[i]
		id := t.alloc(Node{
			Name:    e.Name,
			Alloc:   e.Alloc,
			Logical: e.Logical,
			Self:    e.Alloc,
			SelfLog: e.Logical,
			Parent:  parent,
			Mtime:   e.Mtime,
			Kind:    uint8(e.Kind),
			Flags:   e.Flags,
		})
		pn = t.node(parent) // alloc may have appended a chunk; re-resolve
		pn.Children = append(pn.Children, id)

		if e.Flags&scanner.FlagNoPerm != 0 {
			t.errs.Add(1)
		}
		if e.DirID != 0 {
			for int(e.DirID) >= len(t.dirIndex) {
				t.dirIndex = append(t.dirIndex, make([]uint32, len(t.dirIndex))...)
			}
			t.dirIndex[e.DirID] = id
		}
		switch scanner.Kind(e.Kind) {
		case scanner.KindDir, scanner.KindPackage:
			nDirs++
		default:
			nFiles++
		}
		dAlloc += e.Alloc
		dLog += e.Logical
	}

	// One bubble per directory rather than per entry: the ancestor chain is
	// walked once for the whole frame.
	for id := parent; id != Invalid; {
		n := t.node(id)
		if n == nil {
			break
		}
		if id != parent {
			n.Alloc += dAlloc
			n.Logical += dLog
		}
		n.Files += nFiles
		n.Dirs += nDirs
		id = n.Parent
	}
	pn = t.node(parent)
	pn.Alloc += dAlloc
	pn.Logical += dLog

	t.sorted.Store(false)
	t.mu.Unlock()
}

// Finalize sorts every node's children by size, descending. Sorting is
// deferred to the end of the scan because doing it per-frame would dominate
// the walk; mid-scan queries use a top-K selection instead.
func (t *Tree) Finalize() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id := uint32(1); id <= t.n; id++ {
		n := t.node(id)
		if len(n.Children) > 1 {
			kids := n.Children
			sort.Slice(kids, func(a, b int) bool {
				na, nb := t.node(kids[a]), t.node(kids[b])
				if na.Alloc != nb.Alloc {
					return na.Alloc > nb.Alloc
				}
				return na.Name < nb.Name
			})
		}
	}
	t.sorted.Store(true)
}

// Path reconstructs the absolute path of a node by walking to the root.
func (t *Tree) Path(id uint32) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.pathLocked(id)
}

func (t *Tree) pathLocked(id uint32) string {
	if t.node(id) == nil {
		return ""
	}
	var parts []string
	for cur := id; cur != Invalid && cur != t.Root; {
		n := t.node(cur)
		if n == nil {
			return ""
		}
		parts = append(parts, n.Name)
		cur = n.Parent
	}
	if len(parts) == 0 {
		return t.RootPath
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return filepath.Join(append([]string{t.RootPath}, parts...)...)
}

// Lookup resolves an absolute path to a handle, or Invalid.
func (t *Tree) Lookup(path string) uint32 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	rel, err := filepath.Rel(t.RootPath, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return Invalid
	}
	cur := t.Root
	if rel == "." {
		return cur
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		n := t.node(cur)
		found := Invalid
		for _, c := range n.Children {
			if cn := t.node(c); cn != nil && cn.Name == seg && cn.Flags&FlagDeleted == 0 {
				found = c
				break
			}
		}
		if found == Invalid {
			return Invalid
		}
		cur = found
	}
	return cur
}

// Snapshot copies a node's public fields. Returns ok=false for a bad handle.
func (t *Tree) Snapshot(id uint32) (Node, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := t.node(id)
	if n == nil {
		return Node{}, false
	}
	c := *n
	c.Children = nil
	return c, true
}

// Prune removes a subtree after the files behind it are gone, and subtracts
// its bytes from every ancestor so the chart and the volume bar stay honest.
// It returns the number of bytes reclaimed.
func (t *Tree) Prune(id uint32) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.node(id)
	if n == nil || id == t.Root || n.Flags&FlagDeleted != 0 {
		return 0
	}
	alloc, logical, files, dirs := n.Alloc, n.Logical, n.Files, n.Dirs
	switch scanner.Kind(n.Kind) {
	case scanner.KindDir, scanner.KindPackage:
		dirs++
	default:
		files++
	}

	// Detach from the parent.
	if p := t.node(n.Parent); p != nil {
		for i, c := range p.Children {
			if c == id {
				p.Children = append(p.Children[:i], p.Children[i+1:]...)
				break
			}
		}
	}
	t.markDeleted(id)

	for cur := n.Parent; cur != Invalid; {
		a := t.node(cur)
		if a == nil {
			break
		}
		a.Alloc -= alloc
		a.Logical -= logical
		a.Files -= files
		a.Dirs -= dirs
		cur = a.Parent
	}
	return alloc
}

func (t *Tree) markDeleted(id uint32) {
	stack := []uint32{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := t.node(cur)
		if n == nil {
			continue
		}
		n.Flags |= FlagDeleted
		stack = append(stack, n.Children...)
	}
}

// ReplaceSubtree swaps a freshly rescanned subtree in for an existing node,
// keeping the node's handle stable so the UI's selection survives a rescan.
func (t *Tree) ReplaceSubtree(id uint32, src *Tree) {
	t.mu.Lock()
	defer t.mu.Unlock()
	dst := t.node(id)
	if dst == nil {
		return
	}
	oldAlloc, oldLog, oldFiles, oldDirs := dst.Alloc, dst.Logical, dst.Files, dst.Dirs
	for _, c := range dst.Children {
		t.markDeleted(c)
	}
	dst.Children = nil
	dst.Alloc, dst.Logical = dst.Self, dst.SelfLog
	dst.Files, dst.Dirs = 0, 0
	dst.Err = ""

	src.mu.RLock()
	t.copyInto(id, src, src.Root)
	src.mu.RUnlock()

	d := t.node(id)
	dA, dL := d.Alloc-oldAlloc, d.Logical-oldLog
	dF, dD := d.Files-oldFiles, d.Dirs-oldDirs
	for cur := d.Parent; cur != Invalid; {
		a := t.node(cur)
		if a == nil {
			break
		}
		a.Alloc += dA
		a.Logical += dL
		a.Files += dF
		a.Dirs += dD
		cur = a.Parent
	}
}

// copyInto deep-copies src's subtree under dstID. Both locks held by caller.
func (t *Tree) copyInto(dstID uint32, src *Tree, srcID uint32) {
	sn := src.node(srcID)
	if sn == nil {
		return
	}
	dn := t.node(dstID)
	dn.Err = sn.Err
	dn.Flags = sn.Flags
	for _, sc := range sn.Children {
		scn := src.node(sc)
		if scn == nil || scn.Flags&FlagDeleted != 0 {
			continue
		}
		nid := t.alloc(Node{
			Name: scn.Name, Alloc: scn.Alloc, Logical: scn.Logical,
			Self: scn.Self, SelfLog: scn.SelfLog, Parent: dstID,
			Files: scn.Files, Dirs: scn.Dirs, Mtime: scn.Mtime,
			Kind: scn.Kind, Flags: scn.Flags, Err: scn.Err,
		})
		dn = t.node(dstID)
		dn.Children = append(dn.Children, nid)
		t.copyInto(nid, src, sc)
		dn = t.node(dstID)
		dn.Alloc += scn.Alloc
		dn.Logical += scn.Logical
		dn.Files += scn.Files
		dn.Dirs += scn.Dirs
		switch scanner.Kind(scn.Kind) {
		case scanner.KindDir, scanner.KindPackage:
			dn.Dirs++
		default:
			dn.Files++
		}
	}
}
