package tree

import (
	"bytes"
	"testing"

	"github.com/sr3d/diskspacesaver/internal/scanner"
)

// buildWide makes a root with one wide directory whose entries arrive in
// ascending size — the order a scan discovers them, and the opposite of the
// order the chart needs.
func buildWide(t *testing.T) *Tree {
	t.Helper()
	tr := New("/scan")
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "wide", Kind: scanner.KindDir, DirID: 2},
		{Name: "small", Kind: scanner.KindDir, DirID: 3},
	}})

	entries := make([]scanner.Entry, 0, 300)
	for i := 0; i < 300; i++ {
		entries = append(entries, scanner.Entry{
			Name:    "f" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Kind:    scanner.KindFile,
			Alloc:   int64(i+1) * 1000,
			Logical: int64(i+1) * 900,
		})
	}
	tr.Apply(scanner.Frame{DirID: 2, Entry: entries})
	tr.Apply(scanner.Frame{DirID: 3, Entry: []scanner.Entry{
		{Name: "z.bin", Kind: scanner.KindFile, Alloc: 10},
	}})
	tr.Finalize()
	return tr
}

func roundTrip(t *testing.T, src *Tree) *Tree {
	t.Helper()
	var buf bytes.Buffer
	if err := src.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestCodecPreservesTotalsAndPaths(t *testing.T) {
	src := buildWide(t)
	dst := roundTrip(t, src)

	sn, _ := src.Snapshot(src.Root)
	dn, _ := dst.Snapshot(dst.Root)
	if sn.Alloc != dn.Alloc || sn.Logical != dn.Logical || sn.Files != dn.Files || sn.Dirs != dn.Dirs {
		t.Errorf("root differs after a round trip:\n  before %+v\n  after  %+v", sn, dn)
	}
	if src.Count() != dst.Count() {
		t.Errorf("node count: before %d, after %d", src.Count(), dst.Count())
	}
	if dst.RootPath != src.RootPath {
		t.Errorf("root path: %q", dst.RootPath)
	}
	if id := dst.Lookup("/scan/small/z.bin"); id == Invalid {
		t.Error("a deep path no longer resolves after a round trip")
	}
}

// A cache is loaded and drawn without a rescan, so the order children come back
// in is the order the chart and the sidebar use. Slab order is discovery order,
// not size order, so a decoded tree that merely claims to be sorted renders
// every folder in an arbitrary sequence.
func TestCodecRestoresChildrenInSizeOrder(t *testing.T) {
	src := buildWide(t)
	dst := roundTrip(t, src)

	before := src.Children(src.Lookup("/scan/wide"), 0, 300)
	after := dst.Children(dst.Lookup("/scan/wide"), 0, 300)
	if len(before) != len(after) {
		t.Fatalf("child count: before %d, after %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Name != after[i].Name || before[i].Size != after[i].Size {
			t.Fatalf("child %d differs after a round trip: before %s (%d), after %s (%d)",
				i, before[i].Name, before[i].Size, after[i].Name, after[i].Size)
		}
	}
	for i := 1; i < len(after); i++ {
		if after[i-1].Size < after[i].Size {
			t.Fatalf("decoded children are not largest-first at %d: %d then %d",
				i, after[i-1].Size, after[i].Size)
		}
	}
}

// The arc floor keeps the first maxArcsPerRing children and folds the rest
// into "other". If the decoded order is wrong that cutoff discards large items
// and keeps small ones, so the chart is not merely shuffled but wrong.
func TestCodecViewportKeepsTheLargestArcs(t *testing.T) {
	dst := roundTrip(t, buildWide(t))
	v := dst.Viewport(dst.Lookup("/scan/wide"), 1, 0, false)

	var largest int64
	var other *VNode
	for i := range v.Focus.Kids {
		k := &v.Focus.Kids[i]
		if k.Other > 0 {
			other = k
			continue
		}
		if k.Size > largest {
			largest = k.Size
		}
	}
	if largest != 300_000 {
		t.Errorf("the largest child is missing from the viewport: biggest drawn arc is %d", largest)
	}
	if other == nil {
		t.Fatal("300 children should have produced an 'other' wedge")
	}
	// The 100 smallest (1000..100000) are the ones that should have been
	// folded away.
	var wantOther int64
	for i := 1; i <= 100; i++ {
		wantOther += int64(i) * 1000
	}
	if other.Size != wantOther {
		t.Errorf("the 'other' wedge totals %d, want %d — the wrong children were folded away",
			other.Size, wantOther)
	}
}
