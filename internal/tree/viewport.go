package tree

import (
	"sort"

	"github.com/sr3d/diskspacesaver/internal/scanner"
)

// VNode is one arc of the sunburst. It is deliberately small: a viewport is a
// few hundred of these, never the whole tree.
type VNode struct {
	ID      uint32  `json:"id"`
	Name    string  `json:"name"`
	Size    int64   `json:"size"`
	Logical int64   `json:"logical"`
	Kind    uint8   `json:"kind"`
	Flags   uint8   `json:"flags"`
	Files   int32   `json:"files"`
	Dirs    int32   `json:"dirs"`
	Mtime   int64   `json:"mtime"`
	HasKids bool    `json:"hasKids"`
	Other   int32   `json:"other,omitempty"` // >0: synthetic "N other items" arc
	Err     string  `json:"err,omitempty"`
	Kids    []VNode `json:"children,omitempty"`
}

// ViewportSlice is everything the chart needs to draw one frame.
type ViewportSlice struct {
	Focus     VNode   `json:"focus"`
	Ancestors []VNode `json:"ancestors"` // root-first, excluding focus
	RootTotal int64   `json:"rootTotal"`
	Scanning  bool    `json:"scanning"`
	Sorted    bool    `json:"sorted"`
}

// NodeSummary is a flat row for the sidebar list.
type NodeSummary struct {
	ID       uint32  `json:"id"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Logical  int64   `json:"logical"`
	Kind     uint8   `json:"kind"`
	Flags    uint8   `json:"flags"`
	Files    int32   `json:"files"`
	Dirs     int32   `json:"dirs"`
	Mtime    int64   `json:"mtime"`
	HasKids  bool    `json:"hasKids"`
	Fraction float64 `json:"fraction"`
	Err      string  `json:"err,omitempty"`
}

// maxArcsPerRing bounds how many real arcs a single ring can contain before
// the remainder is folded into "other". Even without the angular threshold
// this keeps a pathological directory (200k files in one folder) from
// producing a viewport the WebView chokes on.
const maxArcsPerRing = 200

// Viewport returns the focus node plus depth levels of descendants. Children
// contributing less than minFraction of the focus total are collapsed into a
// single synthetic "other" arc, because an arc thinner than a pixel is noise
// the user cannot click anyway.
// minSize is an absolute byte floor the user sets; minFraction is the angular
// floor that keeps sub-pixel arcs out of the payload. Both fold what they
// exclude into the same "other" wedge, so nothing silently disappears.
func (t *Tree) Viewport(focus uint32, depth int, minFraction float64, minSize int64, scanning bool) ViewportSlice {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if focus == Invalid || t.node(focus) == nil {
		focus = t.Root
	}
	if depth < 0 {
		depth = 0
	}
	if depth > 12 {
		depth = 12
	}

	// The arc floor is angular, so it is a fraction of the *focus* total and
	// is computed once here. Re-deriving it per node would keep a wedge that
	// is 0.1% of its own parent but a sub-pixel sliver of the drawn circle,
	// which is exactly the case that makes a deep viewport enormous.
	var cutoff int64
	if fn := t.node(focus); fn != nil && minFraction > 0 && fn.Alloc > 0 {
		cutoff = int64(float64(fn.Alloc) * minFraction)
	}
	if minSize > cutoff {
		cutoff = minSize
	}

	out := ViewportSlice{
		// Allocated even when empty: a nil slice marshals to null and the
		// chart would dereference it on the very first frame at the root.
		Ancestors: []VNode{},
		Focus:     t.buildV(focus, depth, cutoff),
		Scanning:  scanning,
		Sorted:    t.sorted.Load(),
	}
	if r := t.node(t.Root); r != nil {
		out.RootTotal = r.Alloc
	}
	// Breadcrumbs, root-first.
	for cur := t.node(focus).Parent; cur != Invalid; {
		n := t.node(cur)
		if n == nil {
			break
		}
		out.Ancestors = append(out.Ancestors, VNode{
			ID: cur, Name: n.Name, Size: n.Alloc, Logical: n.Logical,
			Kind: n.Kind, Flags: n.Flags, Files: n.Files, Dirs: n.Dirs,
			HasKids: len(n.Children) > 0,
		})
		cur = n.Parent
	}
	for i, j := 0, len(out.Ancestors)-1; i < j; i, j = i+1, j-1 {
		out.Ancestors[i], out.Ancestors[j] = out.Ancestors[j], out.Ancestors[i]
	}
	return out
}

// buildV expands a node to depth levels. cutoff is an absolute byte count,
// already derived from the focus total by Viewport.
func (t *Tree) buildV(id uint32, depth int, cutoff int64) VNode {
	n := t.node(id)
	v := VNode{
		ID: id, Name: n.Name, Size: n.Alloc, Logical: n.Logical,
		Kind: n.Kind, Flags: n.Flags, Files: n.Files, Dirs: n.Dirs,
		Mtime: n.Mtime, Err: n.Err, HasKids: t.liveKids(n) > 0,
	}
	if depth == 0 || !v.HasKids {
		return v
	}

	kids := t.topChildren(n)

	var otherSize, otherLog int64
	var otherCount int32
	for i, cid := range kids {
		cn := t.node(cid)
		if cn == nil || cn.Flags&FlagDeleted != 0 {
			continue
		}
		if i >= maxArcsPerRing || cn.Alloc < cutoff {
			otherSize += cn.Alloc
			otherLog += cn.Logical
			otherCount++
			continue
		}
		v.Kids = append(v.Kids, t.buildV(cid, depth-1, cutoff))
	}
	if otherCount > 0 {
		v.Kids = append(v.Kids, VNode{
			ID: Invalid, Name: "other", Size: otherSize, Logical: otherLog,
			Kind: uint8(scanner.KindOther), Other: otherCount,
		})
	}
	return v
}

func (t *Tree) liveKids(n *Node) int {
	c := 0
	for _, k := range n.Children {
		if kn := t.node(k); kn != nil && kn.Flags&FlagDeleted == 0 {
			c++
		}
	}
	return c
}

// topChildren returns a node's children largest-first. After Finalize the
// stored order is already correct and this is free. Mid-scan it selects the
// top arcs with a bounded heap instead of sorting a directory that may have
// hundreds of thousands of entries.
func (t *Tree) topChildren(n *Node) []uint32 {
	if t.sorted.Load() {
		return n.Children
	}
	k := maxArcsPerRing
	if len(n.Children) <= k {
		c := append([]uint32(nil), n.Children...)
		sort.Slice(c, func(a, b int) bool {
			na, nb := t.node(c[a]), t.node(c[b])
			return na.Alloc > nb.Alloc
		})
		return c
	}
	// Min-heap of size k over Alloc.
	h := make([]uint32, 0, k)
	less := func(a, b uint32) bool { return t.node(a).Alloc < t.node(b).Alloc }
	push := func(id uint32) {
		h = append(h, id)
		i := len(h) - 1
		for i > 0 {
			p := (i - 1) / 2
			if !less(h[i], h[p]) {
				break
			}
			h[i], h[p] = h[p], h[i]
			i = p
		}
	}
	down := func() {
		i := 0
		for {
			l, r := 2*i+1, 2*i+2
			m := i
			if l < len(h) && less(h[l], h[m]) {
				m = l
			}
			if r < len(h) && less(h[r], h[m]) {
				m = r
			}
			if m == i {
				return
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
	}
	var rest int64
	var restCount int
	for _, c := range n.Children {
		cn := t.node(c)
		if cn == nil || cn.Flags&FlagDeleted != 0 {
			continue
		}
		if len(h) < k {
			push(c)
			continue
		}
		if t.node(h[0]).Alloc < cn.Alloc {
			rest += t.node(h[0]).Alloc
			restCount++
			h[0] = c
			down()
		} else {
			rest += cn.Alloc
			restCount++
		}
	}
	sort.Slice(h, func(a, b int) bool { return t.node(h[a]).Alloc > t.node(h[b]).Alloc })
	return h
}

// ChildPage is a page of a node's children plus what the size floor withheld,
// so the list can say how much it is not showing instead of quietly omitting
// it.
type ChildPage struct {
	Rows        []NodeSummary `json:"rows"`
	Total       int           `json:"total"`
	Hidden      int           `json:"hidden"`
	HiddenBytes int64         `json:"hiddenBytes"`
}

// Children returns a page of a node's children for the sidebar list. Entries
// smaller than minSize are counted rather than returned.
func (t *Tree) Children(id uint32, offset, limit int, minSize int64) ChildPage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	page := ChildPage{Rows: []NodeSummary{}}
	n := t.node(id)
	if n == nil {
		return page
	}
	kids := t.topChildren(n)
	total := n.Alloc
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}

	// The floor is applied before paging, so an offset means "the Nth visible
	// row" rather than a position in a list the user cannot see.
	seen := 0
	for _, cid := range kids {
		cn := t.node(cid)
		if cn == nil || cn.Flags&FlagDeleted != 0 {
			continue
		}
		if minSize > 0 && cn.Alloc < minSize {
			page.Hidden++
			page.HiddenBytes += cn.Alloc
			continue
		}
		page.Total++
		if seen < offset {
			seen++
			continue
		}
		seen++
		if len(page.Rows) >= limit {
			continue
		}
		var frac float64
		if total > 0 {
			frac = float64(cn.Alloc) / float64(total)
		}
		page.Rows = append(page.Rows, NodeSummary{
			ID: cid, Name: cn.Name, Size: cn.Alloc, Logical: cn.Logical,
			Kind: cn.Kind, Flags: cn.Flags, Files: cn.Files, Dirs: cn.Dirs,
			Mtime: cn.Mtime, HasKids: t.liveKids(cn) > 0, Fraction: frac,
			Err: cn.Err,
		})
	}
	return page
}

// ChildCount is the number of live children, for sidebar paging.
func (t *Tree) ChildCount(id uint32) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := t.node(id)
	if n == nil {
		return 0
	}
	return t.liveKids(n)
}

// Ancestors returns the breadcrumb chain, root-first, including the node.
func (t *Tree) Ancestors(id uint32) []NodeSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := []NodeSummary{}
	for cur := id; cur != Invalid; {
		n := t.node(cur)
		if n == nil {
			break
		}
		out = append(out, NodeSummary{
			ID: cur, Name: n.Name, Size: n.Alloc, Logical: n.Logical,
			Kind: n.Kind, Flags: n.Flags, Files: n.Files, Dirs: n.Dirs,
			HasKids: len(n.Children) > 0,
		})
		cur = n.Parent
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// TopN returns the n largest files anywhere in the tree. Used by cmd/scan for
// the M1 go/no-go check and by the "largest files" view.
func (t *Tree) TopN(n int) []NodeSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	type ent struct {
		id uint32
		sz int64
	}
	best := make([]ent, 0, n)
	worst := func() int64 {
		if len(best) == 0 {
			return -1
		}
		return best[len(best)-1].sz
	}
	for id := uint32(1); id <= t.nLocked(); id++ {
		nd := t.node(id)
		if nd == nil || nd.Flags&FlagDeleted != 0 {
			continue
		}
		if scanner.Kind(nd.Kind) == scanner.KindDir {
			continue
		}
		if len(best) == n && nd.Alloc <= worst() {
			continue
		}
		// A package is one item to the user, so list the package itself and
		// not the files inside it — otherwise a 9 GB .bundle and its 8.5 GB
		// payload both appear and the list reads as double-counted.
		if t.insidePackage(nd.Parent) {
			continue
		}
		i := sort.Search(len(best), func(i int) bool { return best[i].sz < nd.Alloc })
		best = append(best, ent{})
		copy(best[i+1:], best[i:])
		best[i] = ent{id, nd.Alloc}
		if len(best) > n {
			best = best[:n]
		}
	}
	out := make([]NodeSummary, 0, len(best))
	for _, e := range best {
		nd := t.node(e.id)
		out = append(out, NodeSummary{
			ID: e.id, Name: nd.Name, Size: nd.Alloc, Logical: nd.Logical,
			Kind: nd.Kind, Flags: nd.Flags, Mtime: nd.Mtime,
		})
	}
	return out
}

func (t *Tree) nLocked() uint32 { return t.n }

// insidePackage reports whether any ancestor is a package bundle.
func (t *Tree) insidePackage(id uint32) bool {
	for cur := id; cur != Invalid; {
		n := t.node(cur)
		if n == nil {
			return false
		}
		if scanner.Kind(n.Kind) == scanner.KindPackage {
			return true
		}
		cur = n.Parent
	}
	return false
}
