package tree

import (
	"testing"

	"github.com/sr3d/diskspacesaver/internal/scanner"
)

// build wires a small tree by hand so the aggregation logic can be checked
// without touching a filesystem.
//
//	root
//	├── big/        (dir 2)
//	│   ├── x.bin   4000
//	│   └── y.bin   1000
//	├── small/      (dir 3)
//	│   └── z.bin    100
//	└── loose.bin    500
func build() *Tree {
	t := New("/scan")
	t.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "big", Kind: scanner.KindDir, DirID: 2},
		{Name: "small", Kind: scanner.KindDir, DirID: 3},
		{Name: "loose.bin", Kind: scanner.KindFile, Alloc: 500, Logical: 480},
	}})
	t.Apply(scanner.Frame{DirID: 2, Entry: []scanner.Entry{
		{Name: "x.bin", Kind: scanner.KindFile, Alloc: 4000, Logical: 3900},
		{Name: "y.bin", Kind: scanner.KindFile, Alloc: 1000, Logical: 900},
	}})
	t.Apply(scanner.Frame{DirID: 3, Entry: []scanner.Entry{
		{Name: "z.bin", Kind: scanner.KindFile, Alloc: 100, Logical: 90},
	}})
	return t
}

func TestAggregation(t *testing.T) {
	tr := build()
	root, _ := tr.Snapshot(tr.Root)
	if root.Alloc != 5600 {
		t.Errorf("root alloc = %d, want 5600", root.Alloc)
	}
	if root.Logical != 5370 {
		t.Errorf("root logical = %d, want 5370", root.Logical)
	}
	if root.Files != 4 {
		t.Errorf("root files = %d, want 4", root.Files)
	}
	if root.Dirs != 2 {
		t.Errorf("root dirs = %d, want 2", root.Dirs)
	}
	big := tr.Lookup("/scan/big")
	if bn, _ := tr.Snapshot(big); bn.Alloc != 5000 {
		t.Errorf("big alloc = %d, want 5000", bn.Alloc)
	}
}

func TestPathAndLookupRoundTrip(t *testing.T) {
	tr := build()
	id := tr.Lookup("/scan/big/x.bin")
	if id == Invalid {
		t.Fatal("lookup failed")
	}
	if got := tr.Path(id); got != "/scan/big/x.bin" {
		t.Errorf("Path = %q", got)
	}
	if tr.Lookup("/etc/passwd") != Invalid {
		t.Error("lookup escaped the scan root")
	}
	if tr.Lookup("/scan/nope") != Invalid {
		t.Error("lookup invented a node")
	}
}

func TestPruneReaggregates(t *testing.T) {
	tr := build()
	tr.Finalize()
	x := tr.Lookup("/scan/big/x.bin")
	if freed := tr.Prune(x); freed != 4000 {
		t.Errorf("Prune freed %d, want 4000", freed)
	}
	root, _ := tr.Snapshot(tr.Root)
	if root.Alloc != 1600 {
		t.Errorf("root alloc after prune = %d, want 1600", root.Alloc)
	}
	if root.Files != 3 {
		t.Errorf("root files after prune = %d, want 3", root.Files)
	}
	big, _ := tr.Snapshot(tr.Lookup("/scan/big"))
	if big.Alloc != 1000 {
		t.Errorf("big alloc after prune = %d, want 1000", big.Alloc)
	}
	if tr.Lookup("/scan/big/x.bin") != Invalid {
		t.Error("pruned node still resolvable")
	}

	// Pruning a directory takes its whole subtree with it.
	if freed := tr.Prune(tr.Lookup("/scan/big")); freed != 1000 {
		t.Errorf("Prune(big) freed %d, want 1000", freed)
	}
	root, _ = tr.Snapshot(tr.Root)
	if root.Alloc != 600 || root.Dirs != 1 {
		t.Errorf("after pruning big: alloc=%d dirs=%d, want 600 and 1", root.Alloc, root.Dirs)
	}
	if tr.Prune(tr.Root) != 0 {
		t.Error("pruning the root should be refused")
	}
}

func TestViewportSortsAndCollapses(t *testing.T) {
	tr := build()
	tr.Finalize()
	v := tr.Viewport(tr.Root, 2, 0, false)
	if len(v.Focus.Kids) != 3 {
		t.Fatalf("focus has %d children, want 3", len(v.Focus.Kids))
	}
	if v.Focus.Kids[0].Name != "big" {
		t.Errorf("children not sorted by size: first is %q", v.Focus.Kids[0].Name)
	}
	if len(v.Focus.Kids[0].Kids) != 2 {
		t.Error("depth 2 did not reach grandchildren")
	}
	if v.RootTotal != 5600 {
		t.Errorf("RootTotal = %d", v.RootTotal)
	}

	// A 10% floor folds "small" (1.8%) and "loose.bin" (8.9%) into "other".
	v = tr.Viewport(tr.Root, 1, 0.10, false)
	var other *VNode
	for i := range v.Focus.Kids {
		if v.Focus.Kids[i].Other > 0 {
			other = &v.Focus.Kids[i]
		}
	}
	if other == nil {
		t.Fatal("no synthetic other arc produced")
	}
	if other.Other != 2 || other.Size != 600 {
		t.Errorf("other arc = %d items / %d bytes, want 2 / 600", other.Other, other.Size)
	}
	if other.ID != Invalid {
		t.Error("synthetic arc must not claim a real handle")
	}
}

func TestBreadcrumbs(t *testing.T) {
	tr := build()
	tr.Finalize()
	x := tr.Lookup("/scan/big/x.bin")
	anc := tr.Ancestors(x)
	if len(anc) != 3 || anc[0].Name != "scan" || anc[1].Name != "big" || anc[2].Name != "x.bin" {
		t.Errorf("Ancestors = %+v", anc)
	}
	v := tr.Viewport(tr.Lookup("/scan/big"), 1, 0, false)
	if len(v.Ancestors) != 1 || v.Ancestors[0].ID != tr.Root {
		t.Errorf("viewport ancestors = %+v", v.Ancestors)
	}
}

func TestTopChildrenMidScanUsesHeap(t *testing.T) {
	tr := New("/scan")
	entries := make([]scanner.Entry, 0, 500)
	for i := 0; i < 500; i++ {
		entries = append(entries, scanner.Entry{
			Name: string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: scanner.KindFile,
			Alloc: int64(i),
		})
	}
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: entries})
	// Deliberately not Finalized: this exercises the mid-scan selection path.
	kids := tr.Children(tr.Root, 0, 10)
	if len(kids) != 10 {
		t.Fatalf("got %d children", len(kids))
	}
	if kids[0].Size != 499 || kids[9].Size != 490 {
		t.Errorf("mid-scan top-K wrong: first=%d last=%d", kids[0].Size, kids[9].Size)
	}
}

func TestPermissionErrorsRecorded(t *testing.T) {
	tr := New("/scan")
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "locked", Kind: scanner.KindDir, DirID: 2},
	}})
	tr.Apply(scanner.Frame{DirID: 2, Err: "permission denied"})
	if tr.Errors() != 1 {
		t.Errorf("Errors = %d, want 1", tr.Errors())
	}
	n, _ := tr.Snapshot(tr.Lookup("/scan/locked"))
	if n.Err == "" || n.Flags&scanner.FlagNoPerm == 0 {
		t.Error("inaccessible directory not marked on the node")
	}
}

// The arc floor must be a fraction of the focus total, not of each node's own
// parent. A deep ring is otherwise unbounded: a wedge that is 20% of its
// parent can be a sub-pixel sliver of the drawn circle.
func TestArcCutoffIsRelativeToFocus(t *testing.T) {
	tr := New("/scan")
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "huge", Kind: scanner.KindDir, DirID: 2},
		{Name: "tiny", Kind: scanner.KindDir, DirID: 3},
	}})
	tr.Apply(scanner.Frame{DirID: 2, Entry: []scanner.Entry{
		{Name: "h.bin", Kind: scanner.KindFile, Alloc: 1_000_000},
	}})
	// "tiny" is 0.1% of the root but its children are 25% of *it*.
	tr.Apply(scanner.Frame{DirID: 3, Entry: []scanner.Entry{
		{Name: "a.bin", Kind: scanner.KindFile, Alloc: 250},
		{Name: "b.bin", Kind: scanner.KindFile, Alloc: 250},
		{Name: "c.bin", Kind: scanner.KindFile, Alloc: 250},
		{Name: "d.bin", Kind: scanner.KindFile, Alloc: 250},
	}})
	tr.Finalize()

	// 1% of the root total (~10,000 bytes) must exclude every child of "tiny".
	v := tr.Viewport(tr.Root, 3, 0.01, false)
	var tiny *VNode
	for i := range v.Focus.Kids {
		if v.Focus.Kids[i].Name == "tiny" {
			tiny = &v.Focus.Kids[i]
		}
	}
	if tiny != nil {
		for _, k := range tiny.Kids {
			if k.Other == 0 {
				t.Errorf("child %q (%d bytes) survived a cutoff of 1%% of the focus total",
					k.Name, k.Size)
			}
		}
	}

	// Focusing on "tiny" makes the same children the whole circle, so now they
	// must all be drawn.
	v = tr.Viewport(tr.Lookup("/scan/tiny"), 2, 0.01, false)
	if len(v.Focus.Kids) != 4 {
		t.Errorf("after focusing, got %d children, want 4 — cutoff did not rescale", len(v.Focus.Kids))
	}
}
