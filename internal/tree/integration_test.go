package tree_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// TestScanIntoTreeUnderRace drives the real scanner into the real tree while a
// second goroutine issues viewport queries, which is exactly what the UI does
// during a live scan. Run with -race, this is the guard for the "queryable
// mid-scan" requirement.
func TestScanIntoTreeUnderRace(t *testing.T) {
	root := t.TempDir()
	var want int64
	for i := 0; i < 40; i++ {
		d := filepath.Join(root, "d", fmt.Sprintf("dir%02d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 25; j++ {
			p := filepath.Join(d, fmt.Sprintf("f%02d.bin", j))
			if err := os.WriteFile(p, make([]byte, 1024*(j+1)), 0o644); err != nil {
				t.Fatal(err)
			}
			want++
		}
	}

	s := scanner.New(scanner.Options{Root: root})
	tr := tree.New(root)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the UI, hammering the tree while it fills
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			v := tr.Viewport(tr.Root, 5, 0.001, true)
			_ = v.Focus.Size
			tr.Children(tr.Root, 0, 50)
			tr.Path(tr.Root)
		}
	}()
	go func() {
		for range s.Progress() {
		}
	}()
	go s.Run(context.Background())
	for f := range s.Frames() {
		tr.Apply(f)
	}
	close(stop)
	wg.Wait()

	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	tr.Finalize()

	root0, _ := tr.Snapshot(tr.Root)
	if int64(root0.Files) != want {
		t.Errorf("tree counted %d files, want %d", root0.Files, want)
	}
	if root0.Alloc <= 0 {
		t.Error("root aggregated to zero bytes")
	}
	// Every file must be reachable by path.
	if id := tr.Lookup(filepath.Join(root, "d", "dir00", "f00.bin")); id == tree.Invalid {
		t.Error("a scanned file could not be looked up by path")
	}
}
