//go:build darwin

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type seenEntry struct {
	Name    string
	Kind    Kind
	Alloc   int64
	Logical int64
	Dup     bool
}

func walkAll(t *testing.T, opt Options) []seenEntry {
	t.Helper()
	s := New(opt)
	var out []seenEntry
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range s.Frames() {
			for _, e := range f.Entry {
				out = append(out, seenEntry{
					Name: e.Name, Kind: e.Kind, Alloc: e.Alloc,
					Logical: e.Logical, Dup: e.Flags&FlagDup != 0,
				})
			}
		}
	}()
	go func() {
		for range s.Progress() {
		}
	}()
	s.Run(context.Background())
	<-done
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Alloc < out[j].Alloc
	})
	return out
}

// The fast path is only worth having if it is indistinguishable from the slow
// one. This walks the same fixture both ways and compares entry by entry.
func TestBulkPathMatchesPortablePath(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, size int) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mk("a/one.bin", 4096)
	mk("a/two.bin", 300000)
	mk("b/deep/deeper/three.bin", 1024)
	mk(".hidden/secret.bin", 512)
	mk("Thing.app/Contents/MacOS/bin", 2048)
	mk("empty/.keep", 0)
	orig := mk("links/original.bin", 65536)
	if err := os.Link(orig, filepath.Join(root, "links", "alias.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(orig, filepath.Join(root, "links", "point.lnk")); err != nil {
		t.Fatal(err)
	}
	// A directory big enough to need more than one getattrlistbulk batch.
	for i := 0; i < 300; i++ {
		mk(filepath.Join("many", "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".bin"), 128)
	}

	portable := walkAll(t, Options{Root: root, UseBulk: false})
	bulk := walkAll(t, Options{Root: root, UseBulk: true})

	if len(portable) != len(bulk) {
		t.Fatalf("entry counts differ: portable %d, bulk %d", len(portable), len(bulk))
	}
	for i := range portable {
		p, b := portable[i], bulk[i]
		if p.Name != b.Name || p.Kind != b.Kind {
			t.Errorf("entry %d differs: portable %+v, bulk %+v", i, p, b)
			continue
		}
		if p.Alloc != b.Alloc {
			t.Errorf("%s: allocated size differs: portable %d, bulk %d", p.Name, p.Alloc, b.Alloc)
		}
		if p.Dup != b.Dup {
			t.Errorf("%s: hardlink dedupe differs: portable %v, bulk %v", p.Name, p.Dup, b.Dup)
		}
	}

	// The hardlink pair must be charged exactly once on both paths.
	countCharged := func(es []seenEntry) int {
		n := 0
		for _, e := range es {
			if (e.Name == "original.bin" || e.Name == "alias.bin") && !e.Dup {
				n++
			}
		}
		return n
	}
	if got := countCharged(bulk); got != 1 {
		t.Errorf("bulk path charged %d of the hardlink pair, want 1", got)
	}
}

func TestBulkTotalsMatch(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		d := filepath.Join(root, "d", string(rune('a'+i%10)))
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "f"+string(rune('a'+i))+".bin"), make([]byte, 1000*(i+1)), 0o644)
	}
	sum := func(es []seenEntry) (total int64) {
		for _, e := range es {
			total += e.Alloc
		}
		return
	}
	p := sum(walkAll(t, Options{Root: root, UseBulk: false}))
	b := sum(walkAll(t, Options{Root: root, UseBulk: true}))
	if p != b {
		t.Errorf("file totals differ: portable %d, bulk %d", p, b)
	}
	if p == 0 {
		t.Fatal("fixture produced no bytes")
	}
}
