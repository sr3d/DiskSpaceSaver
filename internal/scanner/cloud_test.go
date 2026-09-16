package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchesCloudRoot(t *testing.T) {
	home := "/Users/someone"
	parents, exact := cloudRootsFor(home)

	shouldSkip := []string{
		"/Users/someone/Library/CloudStorage/GoogleDrive-a@b.com",
		"/Users/someone/Library/CloudStorage/Dropbox",
		"/Users/someone/Library/CloudStorage/iCloudDrive-iCloudDrive",
		"/Users/someone/Library/Mobile Documents",
	}
	for _, p := range shouldSkip {
		if !matchesCloudRoot(p, parents, exact) {
			t.Errorf("%s should be treated as a cloud provider root", p)
		}
	}

	shouldWalk := []string{
		"/Users/someone/Library/CloudStorage",                          // the container itself
		"/Users/someone/Library/CloudStorage/GoogleDrive-a@b.com/Docs", // inside a provider
		"/Users/someone/Library/Containers",
		"/Users/someone/Library/Mobile Documents Backup",
		"/Users/someone/Documents",
	}
	for _, p := range shouldWalk {
		if matchesCloudRoot(p, parents, exact) {
			t.Errorf("%s should not be treated as a cloud provider root", p)
		}
	}
}

// A provider root met during a walk is reported but not descended into: its
// bytes are not on this disk and stat-ing inside it blocks on the network.
func TestCloudRootIsFlaggedNotWalked(t *testing.T) {
	home := t.TempDir()
	provider := filepath.Join(home, "Library", "CloudStorage", "GoogleDrive-x@y.com")
	if err := os.MkdirAll(filepath.Join(provider, "My Drive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provider, "My Drive", "huge.bin"), make([]byte, 40960), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// cloudPaths caches on first use, so drive the matcher explicitly with
	// this test's home rather than depending on process-wide state.
	parents, exact := cloudRootsFor(home)
	if !matchesCloudRoot(provider, parents, exact) {
		t.Fatal("fixture is not recognised as a provider root")
	}

	s := New(Options{Root: filepath.Join(home, "Library")})
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

	for _, f := range frames {
		for _, e := range f.Entry {
			if e.Name == "huge.bin" {
				t.Error("the walk descended into a cloud provider and counted online-only bytes")
			}
		}
	}
}
