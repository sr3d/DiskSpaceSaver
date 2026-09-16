package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sr3d/diskspacesaver/internal/helperclient"
	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// startHelper runs the real server on a temporary socket. Code-signature
// verification is off because the test binary is not the signed app; every
// other code path — framing, dispatch, containment, trashing — is the same one
// the root daemon runs.
func startHelper(t *testing.T) *helperclient.Client {
	t.Helper()
	// A Unix socket path is capped near 104 bytes, and t.TempDir() paths on
	// macOS are long enough to exceed it once the test name is in them.
	dir, err := os.MkdirTemp("/tmp", "dss")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "h.sock")
	listeners, err := listen(sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &server{verify: false}
	go srv.run(listeners)
	t.Cleanup(func() {
		for _, ln := range listeners {
			ln.Close()
		}
	})

	c := helperclient.New()
	c.SetSocketPath(sock)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.Status().Running {
			return c
		}
		c.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the helper never answered on its socket")
	return nil
}

func TestHelperVersionHandshake(t *testing.T) {
	c := startHelper(t)
	st := c.Status()
	if !st.Running {
		t.Fatalf("helper not running: %+v", st)
	}
	if !st.Compatible {
		t.Errorf("protocol mismatch: %+v", st)
	}
	if !c.Available() {
		t.Error("Available() should be true for a running, compatible helper")
	}
}

// The streamed frames must rebuild into exactly the tree a local walk
// produces: that equivalence is what lets an administrator scan reuse the
// whole UI without a second code path.
func TestHelperScanStreamMatchesLocalWalk(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		d := filepath.Join(root, "dir"+string(rune('a'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 4; j++ {
			p := filepath.Join(d, "f"+string(rune('0'+j))+".bin")
			if err := os.WriteFile(p, make([]byte, 1024*(j+1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Local walk.
	local := tree.New(root)
	sc := scanner.New(scanner.Options{Root: root})
	go func() {
		for range sc.Progress() {
		}
	}()
	go sc.Run(context.Background())
	for f := range sc.Frames() {
		local.Apply(f)
	}
	local.Finalize()

	// Same walk, streamed over the socket.
	c := startHelper(t)
	src := c.NewScanSource(root, nil, false)
	remote := tree.New(root)
	go func() {
		for range src.Progress() {
		}
	}()
	go src.Run(context.Background())
	for f := range src.Frames() {
		remote.Apply(f)
	}
	if err := src.Err(); err != nil {
		t.Fatalf("streamed scan failed: %v", err)
	}
	remote.Finalize()

	ln, _ := local.Snapshot(local.Root)
	rn, _ := remote.Snapshot(remote.Root)
	if ln.Alloc != rn.Alloc || ln.Files != rn.Files || ln.Dirs != rn.Dirs {
		t.Errorf("streamed tree differs from the local one:\n local  %d bytes, %d files, %d dirs\n remote %d bytes, %d files, %d dirs",
			ln.Alloc, ln.Files, ln.Dirs, rn.Alloc, rn.Files, rn.Dirs)
	}
	if local.Count() != remote.Count() {
		t.Errorf("node counts differ: local %d, remote %d", local.Count(), remote.Count())
	}
	if remote.Lookup(filepath.Join(root, "dira", "f0.bin")) == tree.Invalid {
		t.Error("a file present locally is missing from the streamed tree")
	}
}

func TestHelperDeleteEnforcesContainment(t *testing.T) {
	c := startHelper(t)
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(root, "junk.bin")
	if err := os.WriteFile(victim, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	bystander := filepath.Join(outside, "precious.bin")
	if err := os.WriteFile(bystander, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A path outside the declared root is refused even though the caller is
	// authenticated and the file is perfectly writable.
	res, err := c.DeleteWithResult(root, []string{bystander}, false)
	if err != nil {
		t.Fatalf("delete call failed: %v", err)
	}
	if len(res.Succeeded) != 0 {
		t.Error("the helper deleted a path outside the scan root")
	}
	if res.Failed[bystander] == "" {
		t.Error("the refusal was not reported")
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Errorf("the bystander file was touched: %v", err)
	}

	// A protected system path is refused too.
	res, _ = c.DeleteWithResult("/", []string{"/System/Library"}, false)
	if res != nil && len(res.Succeeded) != 0 {
		t.Fatal("the helper accepted a delete under /System")
	}

	// A path inside the root is removed.
	res, err = c.DeleteWithResult(root, []string{victim}, false)
	if err != nil {
		t.Fatalf("delete call failed: %v", err)
	}
	if len(res.Succeeded) != 1 {
		t.Fatalf("the in-root delete did not happen: %+v", res)
	}
	if res.Bytes <= 0 {
		t.Error("no reclaimed byte count was reported")
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("the file is still there")
	}
}

func TestHelperTrashLandsInTheCallersTrash(t *testing.T) {
	c := startHelper(t)
	root := t.TempDir()
	victim := filepath.Join(root, "trash-me.bin")
	if err := os.WriteFile(victim, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := c.DeleteWithResult(root, []string{victim}, true)
	if err != nil {
		t.Fatalf("trash call failed: %v", err)
	}
	if len(res.Succeeded) != 1 {
		t.Fatalf("trashing did not happen: %+v", res)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("the file was not moved out of the scan root")
	}

	// It must be in the calling user's Trash, not root's.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	landed := filepath.Join(home, ".Trash", "trash-me.bin")
	found := false
	for _, cand := range []string{landed, uniqueName(landed)} {
		if _, err := os.Lstat(cand); err == nil {
			found = true
			os.Remove(cand)
			break
		}
	}
	// The temp dir may be on a different volume, in which case Finder's rule
	// puts it in that volume's .Trashes instead; either is correct.
	if !found {
		vol, _ := volumeRoot(root)
		alt := filepath.Join(vol, ".Trashes")
		if _, err := os.Stat(alt); err != nil {
			t.Errorf("the trashed item is in neither %s nor %s", landed, alt)
		}
	}
}
