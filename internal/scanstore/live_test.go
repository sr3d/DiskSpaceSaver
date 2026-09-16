package scanstore_test

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/scanstore"
)

// TestLiveViewportDoesNotStallTheWalk reproduces the UI's behaviour during a
// scan: a snapshot ticker querying the viewport every 250ms while the builder
// writes. If a viewport query holds the read lock long enough, the frame
// consumer starves, the frame channel fills, and every worker blocks.
func TestLiveViewportDoesNotStallTheWalk(t *testing.T) {
	root := os.Getenv("DSS_LIVE_ROOT")
	if root == "" {
		t.Skip("set DSS_LIVE_ROOT to a large directory to run this")
	}

	var slowest time.Duration
	var queries int
	st := scanstore.New(func(name string, data any) {
		if name == scanstore.EventSnapshot {
			queries++
		}
	})

	start := time.Now()
	s, err := st.Start(scanstore.StartOptions{
		Root: root,
		NewSource: func(r string) scanstore.Source {
			return scanner.New(scanner.Options{Root: r})
		},
		SnapshotDepth:    5,
		MinFraction:      0.0008,
		SnapshotInterval: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Watch for the walk going quiet: if the file count does not move for
	// several seconds while the status is still "scanning", it has stalled.
	var lastFiles int64
	var stuckSince time.Time
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("scan did not finish within 3 minutes (files=%d)", lastFiles)
		}
		sum := s.Summary()
		q0 := time.Now()
		v := s.Tree.Viewport(s.Tree.Root, 5, 0.0008, true)
		if d := time.Since(q0); d > slowest {
			slowest = d
		}
		_ = v
		if sum.Status != scanstore.StatusScanning {
			t.Logf("finished in %s: %d files, %d dirs, %d nodes; slowest viewport %s over %d snapshots",
				time.Since(start).Round(time.Millisecond), sum.Files, sum.Dirs, s.Tree.Count(), slowest, queries)
			if slowest > 250*time.Millisecond {
				t.Errorf("a viewport query took %s while holding the read lock; the builder cannot keep up", slowest)
			}
			return
		}
		files := int64(s.Tree.Count())
		if files != lastFiles {
			lastFiles = files
			stuckSince = time.Time{}
		} else if stuckSince.IsZero() {
			stuckSince = time.Now()
		} else if time.Since(stuckSince) > 8*time.Second {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			os.WriteFile("/private/tmp/claude-501/-Users-ale-projects-github-sr3d-diskspacesaver/5d36559a-5632-4052-88e2-ae46275cb8e5/scratchpad/stall.txt", buf[:n], 0o644)
			t.Fatalf("walk stalled at %d nodes for 8s (slowest viewport %s)", files, slowest)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
