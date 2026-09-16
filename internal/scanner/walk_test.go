package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// buildTree lays out a small fixture and returns its root.
func buildTree(t *testing.T) string {
	t.Helper()
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
	mk("a/two.bin", 8192)
	mk("b/deep/three.bin", 1024)
	mk(".hidden/secret.bin", 512)
	mk("Thing.app/Contents/big.bin", 2048)

	// A hardlink pair: both names, one allocation.
	orig := mk("links/original.bin", 16384)
	if err := os.Link(orig, filepath.Join(root, "links", "alias.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(orig, filepath.Join(root, "links", "point.lnk")); err != nil {
		t.Fatal(err)
	}
	return root
}

func collect(t *testing.T, opt Options) (map[uint32]Frame, *Scanner) {
	t.Helper()
	s := New(opt)
	frames := map[uint32]Frame{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range s.Frames() {
			frames[f.DirID] = f
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
	return frames, s
}

func TestWalkFindsEverything(t *testing.T) {
	root := buildTree(t)
	frames, _ := collect(t, Options{Root: root})

	names := map[string]Entry{}
	for _, f := range frames {
		for _, e := range f.Entry {
			names[e.Name] = e
		}
	}
	for _, want := range []string{"one.bin", "two.bin", "three.bin", "secret.bin", "big.bin", "original.bin", "alias.bin", "point.lnk"} {
		if _, ok := names[want]; !ok {
			t.Errorf("entry %q missing from frames", want)
		}
	}
	if k := names["Thing.app"].Kind; k != KindPackage {
		t.Errorf("Thing.app kind = %v, want KindPackage", k)
	}
	if names[".hidden"].Flags&FlagHidden == 0 {
		t.Error(".hidden not flagged hidden")
	}
	if names["point.lnk"].Kind != KindSymlink {
		t.Error("symlink not classified as KindSymlink")
	}
}

// Every frame's parent must be introduced before the frame itself arrives, or
// a streaming consumer could not build the tree incrementally.
func TestFrameOrderingInvariant(t *testing.T) {
	root := buildTree(t)
	s := New(Options{Root: root})
	seen := map[uint32]bool{RootDirID: true}
	go func() {
		for range s.Progress() {
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range s.Frames() {
			if !seen[f.DirID] {
				t.Errorf("frame for dir %d arrived before its parent introduced it", f.DirID)
			}
			for _, e := range f.Entry {
				if e.DirID != 0 {
					seen[e.DirID] = true
				}
			}
		}
	}()
	s.Run(context.Background())
	<-done
}

func TestHardlinkDedupe(t *testing.T) {
	root := buildTree(t)
	frames, _ := collect(t, Options{Root: root})

	var dup, full int
	for _, f := range frames {
		for _, e := range f.Entry {
			if e.Name != "original.bin" && e.Name != "alias.bin" {
				continue
			}
			if e.Flags&FlagDup != 0 {
				dup++
				if e.Alloc != 0 {
					t.Errorf("%s: duplicate hardlink still charged %d bytes", e.Name, e.Alloc)
				}
			} else {
				full++
				if e.Alloc == 0 {
					t.Errorf("%s: first hardlink charged nothing", e.Name)
				}
			}
		}
	}
	if full != 1 || dup != 1 {
		t.Errorf("hardlink pair: got %d charged and %d deduped, want 1 and 1", full, dup)
	}
}

func TestExclusionsAndCancel(t *testing.T) {
	root := buildTree(t)
	frames, _ := collect(t, Options{Root: root, Exclusions: []string{filepath.Join(root, "a")}})
	for _, f := range frames {
		for _, e := range f.Entry {
			if e.Name == "one.bin" {
				t.Error("excluded directory was descended into")
			}
			if e.Name == "a" && e.Flags&FlagExcluded == 0 {
				t.Error("excluded directory not flagged")
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(Options{Root: root})
	go func() {
		for range s.Frames() {
		}
	}()
	go func() {
		for range s.Progress() {
		}
	}()
	s.Run(ctx) // must return promptly rather than deadlock
}

func TestResolveScanRoot(t *testing.T) {
	if got := ResolveScanRoot("/"); got != "/System/Volumes/Data" {
		t.Errorf("ResolveScanRoot(/) = %q, want the Data volume", got)
	}
	if got := ResolveScanRoot("/Users/"); got != "/Users" {
		t.Errorf("ResolveScanRoot(/Users/) = %q", got)
	}
}
