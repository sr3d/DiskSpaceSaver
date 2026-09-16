package scancache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// newCache points a Cache at a temporary directory instead of the user's
// Application Support, so the tests cannot disturb a real cache.
func newCache(t *testing.T) *Cache {
	t.Helper()
	return &Cache{dir: t.TempDir()}
}

func sample() *tree.Tree {
	tr := tree.New("/scan")
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "big", Kind: scanner.KindDir, DirID: 2},
		{Name: "loose.bin", Kind: scanner.KindFile, Alloc: 500, Logical: 480},
	}})
	entries := make([]scanner.Entry, 0, 40)
	for i := 0; i < 40; i++ {
		entries = append(entries, scanner.Entry{
			Name: "f" + string(rune('a'+i)), Kind: scanner.KindFile,
			Alloc: int64(i+1) * 100, Logical: int64(i+1) * 90,
		})
	}
	tr.Apply(scanner.Frame{DirID: 2, Entry: entries})
	tr.Finalize()
	return tr
}

func TestSaveLoadRoundTrip(t *testing.T) {
	c := newCache(t)
	src := sample()

	if err := c.Save("vol-1", "/scan", src); err != nil {
		t.Fatalf("save: %v", err)
	}

	meta, err := c.Stat("vol-1")
	if err != nil || meta == nil {
		t.Fatalf("stat returned %v, %v", meta, err)
	}
	if meta.Root != "/scan" || meta.Nodes != src.Count() {
		t.Errorf("metadata wrong: %+v (want root /scan, %d nodes)", meta, src.Count())
	}
	if time.Since(meta.SavedAt) > time.Minute {
		t.Errorf("saved-at looks wrong: %v", meta.SavedAt)
	}

	dst, savedAt, err := c.Load("vol-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if savedAt.IsZero() {
		t.Error("load did not report when the scan was saved")
	}

	sn, _ := src.Snapshot(src.Root)
	dn, _ := dst.Snapshot(dst.Root)
	if sn.Alloc != dn.Alloc || sn.Files != dn.Files || sn.Dirs != dn.Dirs {
		t.Errorf("totals differ:\n  saved  %+v\n  loaded %+v", sn, dn)
	}

	// The chart draws a loaded cache without rescanning, so its children must
	// come back largest-first.
	kids := dst.Children(dst.Lookup("/scan/big"), 0, 40)
	if len(kids) != 40 {
		t.Fatalf("got %d children after loading", len(kids))
	}
	for i := 1; i < len(kids); i++ {
		if kids[i-1].Size < kids[i].Size {
			t.Fatalf("loaded children are not largest-first at %d", i)
		}
	}
	if dst.Lookup("/scan/big/fa") == tree.Invalid {
		t.Error("a path present when saved does not resolve after loading")
	}
}

func TestMissingEntryIsNotAnError(t *testing.T) {
	c := newCache(t)
	meta, err := c.Stat("never-saved")
	if err != nil || meta != nil {
		t.Errorf("a cache miss should be (nil, nil), got (%v, %v)", meta, err)
	}
	if _, _, err := c.Load("never-saved"); err == nil {
		t.Error("loading a missing entry should fail")
	}
}

// A cache that cannot be read is worse than no cache: it would fail the same
// way on every launch. Loading a corrupt entry must drop it.
func TestCorruptEntryIsDiscarded(t *testing.T) {
	c := newCache(t)
	if err := c.Save("vol-1", "/scan", sample()); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(c.file("vol-1"), []byte("this is not a scan")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Load("vol-1"); err == nil {
		t.Fatal("loading a corrupt entry should fail")
	}
	if meta, _ := c.Stat("vol-1"); meta != nil {
		t.Error("a corrupt entry was left in place to fail again next launch")
	}
}

func TestListAndClear(t *testing.T) {
	c := newCache(t)
	for _, k := range []string{"vol-1", "vol-2"} {
		if err := c.Save(k, filepath.Join("/scan", k), sample()); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.List(); len(got) != 2 {
		t.Errorf("List returned %d entries, want 2", len(got))
	}
	if c.TotalBytes() <= 0 {
		t.Error("TotalBytes reported nothing for a non-empty cache")
	}
	c.Clear()
	if got := c.List(); len(got) != 0 {
		t.Errorf("Clear left %d entries", len(got))
	}
}

func TestPathKeyIsStableAndDistinct(t *testing.T) {
	a := PathKey("/Users/someone/Projects")
	if a != PathKey("/Users/someone/Projects/") {
		t.Error("PathKey should ignore a trailing separator")
	}
	if a == PathKey("/Users/someone/Pictures") {
		t.Error("different folders produced the same key")
	}
}
