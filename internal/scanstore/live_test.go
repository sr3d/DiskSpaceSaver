package scanstore_test

import (
	"os"
	"path/filepath"
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
		v := s.Tree.Viewport(s.Tree.Root, 5, 0.0008, 0, true)
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
			dump := filepath.Join(t.TempDir(), "stall.txt")
			_ = os.WriteFile(dump, buf[:n], 0o644)
			t.Logf("goroutine dump written to %s", dump)
			t.Fatalf("walk stalled at %d nodes for 8s (slowest viewport %s)", files, slowest)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// makeTree writes a directory of files big enough that the walk takes long
// enough to sample, without depending on anything outside the test.
func makeTree(t *testing.T, dirs, filesPer int, size int) (root string, bytes int64) {
	t.Helper()
	root = t.TempDir()
	buf := make([]byte, size)
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, "d"+string(rune('a'+d%26))+string(rune('a'+d/26)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < filesPer; f++ {
			p := filepath.Join(dir, "f"+string(rune('a'+f%26))+".bin")
			if err := os.WriteFile(p, buf, 0o644); err != nil {
				t.Fatal(err)
			}
			bytes += int64(size)
		}
	}
	return root, bytes
}

func run(t *testing.T, root string, expected int64) (*scanstore.Session, []float64) {
	t.Helper()
	var seen []float64
	st := scanstore.New(func(name string, data any) {
		if name != scanstore.EventProgress {
			return
		}
		if sum, ok := data.(scanstore.Summary); ok {
			seen = append(seen, sum.Fraction)
		}
	})
	s, err := st.Start(scanstore.StartOptions{
		Root: root,
		NewSource: func(r string) scanstore.Source {
			return scanner.New(scanner.Options{Root: r})
		},
		SnapshotDepth:    3,
		SnapshotInterval: 20 * time.Millisecond,
		ExpectedBytes:    expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for s.Summary().Status == scanstore.StatusScanning {
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s, seen
}

// The bar's contract, end to end against a real walk rather than against the
// arithmetic alone: it never goes backwards, never claims to be finished while
// it is still running, and reads exactly full when it is.
func TestProgressFractionOverARealScan(t *testing.T) {
	root, want := makeTree(t, 12, 20, 96*1024)

	// An expectation deliberately a little larger than the truth, which is how
	// a volume's used bytes behaves: the walk cannot reach it.
	s, seen := run(t, root, want+want/10)

	sum := s.Summary()
	if sum.Status != scanstore.StatusDone {
		t.Fatalf("status %q: %s", sum.Status, sum.Err)
	}
	if sum.Fraction != 1 {
		t.Errorf("a completed scan reads %v, not full", sum.Fraction)
	}
	if sum.ExpectedBytes != want+want/10 {
		t.Errorf("expectation not carried through: %d", sum.ExpectedBytes)
	}

	prev := 0.0
	for i, f := range seen {
		if f < 0 {
			t.Fatalf("sample %d was indeterminate despite a good expectation", i)
		}
		if f < prev {
			t.Errorf("sample %d went backwards: %v after %v", i, f, prev)
		}
		if f >= 1 {
			t.Errorf("sample %d claimed the scan was finished while it ran: %v", i, f)
		}
		prev = f
	}
}

// With nothing to divide by, every reading must say so. A bar sitting at zero
// would be indistinguishable from a scan that never started.
func TestProgressIsIndeterminateWithoutAnExpectation(t *testing.T) {
	root, _ := makeTree(t, 4, 8, 4096)
	s, seen := run(t, root, 0)

	for i, f := range seen {
		if f != scanstore.Indeterminate {
			t.Fatalf("sample %d claimed %v with no expectation to divide by", i, f)
		}
	}
	// Completion is knowable even when the way there was not.
	if got := s.Summary().Fraction; got != 1 {
		t.Errorf("a finished scan reads %v, not full", got)
	}
}

// A cancelled scan should look like what it was — stopped part way — not like
// one that finished.
func TestCancelledScanFreezesTheBar(t *testing.T) {
	root, want := makeTree(t, 26, 26, 128*1024)

	var st *scanstore.Store
	st = scanstore.New(func(name string, data any) {
		if name != scanstore.EventProgress {
			return
		}
		if sum, ok := data.(scanstore.Summary); ok && sum.Fraction > 0.1 {
			st.Cancel(sum.ID)
		}
	})
	s, err := st.Start(scanstore.StartOptions{
		Root: root,
		NewSource: func(r string) scanstore.Source {
			return scanner.New(scanner.Options{Root: r})
		},
		SnapshotDepth:    3,
		SnapshotInterval: 10 * time.Millisecond,
		ExpectedBytes:    want,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for s.Summary().Status == scanstore.StatusScanning {
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}

	sum := s.Summary()
	if sum.Status != scanstore.StatusCancelled {
		t.Skipf("scan finished as %q before the cancel landed", sum.Status)
	}
	if sum.Fraction == 1 {
		t.Error("a cancelled scan reads as fully complete")
	}
}
