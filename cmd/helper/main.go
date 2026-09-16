// Command diskspacesaver-helper is the privileged helper: a launchd daemon
// running as root that scans and deletes on behalf of the app.
//
// It exists because a normal user process cannot read other users' home
// folders or many system directories, so a scan run without it reports large
// "unreadable" regions. Running as root is a serious privilege, so the helper
// is deliberately small and deliberately rigid:
//
//   - every request is one of a fixed enum; there is no shell, no eval, no
//     pass-through of arbitrary arguments;
//   - every connection is authenticated by the peer's code signature against a
//     designated requirement naming both the bundle ID and the Team ID;
//   - every delete is re-checked against the scan root the caller declared,
//     here, rather than trusting the app's own containment check.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sr3d/diskspacesaver/internal/helperauth"
	"github.com/sr3d/diskspacesaver/internal/helperproto"
	"github.com/sr3d/diskspacesaver/internal/scanner"
)

// Build is stamped by the release script and checked by the app on connect.
var Build = "dev"

const (
	defaultBundleID = "com.sr3d.diskspacesaver"
	socketEntry     = helperproto.SocketName
)

func main() {
	var (
		socketPath = flag.String("socket", "", "listen on this path instead of adopting launchd's socket (testing only)")
		teamID     = flag.String("team", "", "Team ID a caller must be signed with")
		bundleID   = flag.String("bundle", defaultBundleID, "bundle identifier a caller must have")
		insecure   = flag.Bool("insecure-skip-verify", false, "skip caller code-signature checks (testing only; refused when running as root)")
		idle       = flag.Duration("idle", 0, "exit after this long with no connections (0 = never)")
	)
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("diskspacesaver-helper: ")

	// Refusing this combination is the whole point: a root daemon that skips
	// caller verification is a local privilege escalation, so the flag only
	// works in unprivileged tests.
	if *insecure && os.Geteuid() == 0 {
		log.Fatal("refusing to run as root without caller verification")
	}

	srv := &server{
		requirement: helperauth.Requirement(*bundleID, *teamID),
		verify:      !*insecure,
		idle:        *idle,
	}
	if srv.verify && *teamID == "" {
		log.Fatal("no Team ID given; a caller could not be verified")
	}

	listeners, err := listen(*socketPath)
	if err != nil {
		log.Fatalf("could not listen: %v", err)
	}
	log.Printf("ready (build %s, protocol %d, %d listener(s), verification %v)",
		Build, helperproto.ProtocolVersion, len(listeners), srv.verify)

	srv.run(listeners)
}

func listen(path string) ([]net.Listener, error) {
	if path != "" {
		_ = os.Remove(path)
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		// The app connects as the logged-in user, so the socket has to be
		// reachable by it; the code-signature check, not the file mode, is
		// what keeps other programs out.
		if err := os.Chmod(path, 0o666); err != nil {
			return nil, err
		}
		return []net.Listener{ln}, nil
	}
	return helperauth.LaunchdListeners(socketEntry)
}

type server struct {
	requirement string
	verify      bool
	idle        time.Duration

	mu       sync.Mutex
	lastSeen time.Time
}

func (s *server) run(listeners []net.Listener) {
	s.lastSeen = time.Now()
	if s.idle > 0 {
		go s.idleExit()
	}
	var wg sync.WaitGroup
	for _, ln := range listeners {
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			for {
				conn, err := ln.Accept()
				if err != nil {
					if errors.Is(err, net.ErrClosed) {
						return
					}
					log.Printf("accept: %v", err)
					continue
				}
				go s.serve(conn)
			}
		}(ln)
	}
	wg.Wait()
}

func (s *server) idleExit() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		idleFor := time.Since(s.lastSeen)
		s.mu.Unlock()
		if idleFor > s.idle {
			log.Printf("exiting after %s idle; launchd will restart on demand", idleFor.Round(time.Second))
			os.Exit(0)
		}
	}
}

func (s *server) touch() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *server) serve(conn net.Conn) {
	defer conn.Close()
	s.touch()

	uc, ok := conn.(*net.UnixConn)
	if !ok {
		log.Print("rejecting a connection that is not a Unix socket")
		return
	}

	pc := helperproto.NewConn(conn)
	callerUID, err := s.authenticate(uc)
	if err != nil {
		log.Printf("rejected a caller: %v", err)
		_ = pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgError, Err: err.Error()})
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	req, err := pc.ReadReq()
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if err := s.dispatch(pc, req, callerUID); err != nil {
		_ = pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgError, Err: err.Error()})
	}
}

// authenticate proves the caller is the app this helper was installed for.
func (s *server) authenticate(uc *net.UnixConn) (uint32, error) {
	uid, err := helperauth.PeerUID(uc)
	if err != nil {
		return 0, fmt.Errorf("could not read caller credentials: %w", err)
	}
	if !s.verify {
		return uid, nil
	}
	pid, err := helperauth.PeerPID(uc)
	if err != nil {
		return 0, fmt.Errorf("could not read caller pid: %w", err)
	}
	if err := helperauth.VerifyPID(pid, s.requirement); err != nil {
		return 0, err
	}
	return uid, nil
}

func (s *server) dispatch(pc *helperproto.Conn, req *helperproto.Request, callerUID uint32) error {
	switch req.Op {
	case helperproto.OpVersion:
		return pc.WriteMsg(&helperproto.Msg{
			T: helperproto.MsgVersion, Version: helperproto.ProtocolVersion, Build: Build,
		})

	case helperproto.OpScan:
		return s.scan(pc, req)

	case helperproto.OpDelete:
		res, err := s.delete(req, callerUID)
		if err != nil {
			return err
		}
		return pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgResult, Result: res})

	case helperproto.OpListSnapshots:
		snaps, err := listSnapshots()
		if err != nil {
			return err
		}
		return pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgSnapshots, Snapshot: snaps})

	case helperproto.OpDeleteSnapshots:
		res := &helperproto.OpResult{Failed: map[string]string{}}
		for _, n := range req.Snapshots {
			if err := deleteSnapshot(n); err != nil {
				res.Failed[n] = err.Error()
				continue
			}
			res.Succeeded = append(res.Succeeded, n)
		}
		return pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgResult, Result: res})

	default:
		return fmt.Errorf("unsupported operation %d", req.Op)
	}
}

func (s *server) scan(pc *helperproto.Conn, req *helperproto.Request) error {
	root := scanner.ResolveScanRoot(req.Root)
	if !filepath.IsAbs(root) {
		return errors.New("scan root must be an absolute path")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fmt.Errorf("cannot scan %s", req.Root)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := scanner.New(scanner.Options{
		Root:        root,
		Exclusions:  req.Exclusions,
		CrossDevice: req.CrossDevice,
		UseBulk:     req.UseBulk,
	})

	go func() {
		for p := range sc.Progress() {
			pp := p
			// A write failure means the app has gone; stopping the walk then
			// is the point, so the error is deliberately not logged.
			if pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgProgress, Prog: &pp}) != nil {
				cancel()
				return
			}
		}
	}()

	go sc.Run(ctx)
	for f := range sc.Frames() {
		fr := f
		if err := pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgFrame, Frame: &fr}); err != nil {
			cancel()
			for range sc.Frames() { // drain so the workers can exit
			}
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return pc.WriteMsg(&helperproto.Msg{T: helperproto.MsgDone})
}

// delete removes paths as root.
//
// Containment is re-checked here rather than relying on the app's own check:
// the app is the thing being authenticated, not the thing being trusted, and a
// compromised caller must still be unable to name an arbitrary path.
func (s *server) delete(req *helperproto.Request, callerUID uint32) (*helperproto.OpResult, error) {
	root := filepath.Clean(req.Root)
	if !filepath.IsAbs(root) || root == "/" {
		return nil, errors.New("refusing to delete without a specific scan root")
	}
	uid := req.UID
	if uid == 0 {
		uid = callerUID // never fall back to root's own Trash
	}

	res := &helperproto.OpResult{Failed: map[string]string{}}
	for _, p := range req.Paths {
		clean := filepath.Clean(p)
		if !withinRoot(root, clean) {
			res.Failed[p] = "refused: outside the scanned folder"
			continue
		}
		if isProtectedPath(clean) {
			res.Failed[p] = "refused: this is part of the operating system"
			continue
		}

		size := treeSize(clean)
		var err error
		if req.ToTrash {
			err = trashAsUser(clean, uid)
		} else {
			err = os.RemoveAll(clean)
		}
		if err != nil {
			res.Failed[p] = err.Error()
			continue
		}
		res.Succeeded = append(res.Succeeded, p)
		res.Bytes += size
	}
	return res, nil
}

func withinRoot(root, path string) bool {
	if root == path {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	// Rel gives ".." components for anything outside root, including via a
	// path that climbed out and came back.
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// protectedPrefixes are trees that must never be removed no matter what the
// caller asks for. Deleting from them does not free meaningful space and can
// leave the machine unbootable.
var protectedPrefixes = []string{
	"/System", "/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/lib", "/usr/libexec",
	"/Library/Apple", "/private/var/db/ConfigurationProfiles", "/dev",
}

func isProtectedPath(p string) bool {
	for _, pre := range protectedPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	// The Data volume presents the same trees under a different prefix.
	const dataVol = "/System/Volumes/Data"
	if strings.HasPrefix(p, dataVol+"/") {
		return isProtectedPath(strings.TrimPrefix(p, dataVol))
	}
	return false
}

// trashAsUser moves a path into the calling user's Trash rather than root's.
//
// NSFileManager's trashItemAtURL: run as root would put the item in root's
// Trash, where the user cannot see or restore it. Finder's own behaviour is a
// per-volume .Trashes/<uid> directory, so that is what is reproduced here.
func trashAsUser(path string, uid uint32) error {
	vol, err := volumeRoot(path)
	if err != nil {
		return err
	}

	var dest string
	if vol == "/" || vol == "/System/Volumes/Data" {
		home, err := homeForUID(uid)
		if err != nil {
			return err
		}
		dest = filepath.Join(home, ".Trash")
	} else {
		dest = filepath.Join(vol, ".Trashes", strconv.FormatUint(uint64(uid), 10))
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("could not open the Trash: %w", err)
	}
	if err := os.Chown(dest, int(uid), -1); err != nil && !os.IsPermission(err) {
		return fmt.Errorf("could not prepare the Trash: %w", err)
	}

	target := filepath.Join(dest, filepath.Base(path))
	target = uniqueName(target)
	if err := os.Rename(path, target); err != nil {
		// A rename across devices cannot work; the caller is told plainly
		// rather than silently having the file copied and deleted.
		return fmt.Errorf("could not move to the Trash: %w", err)
	}
	_ = chownTree(target, uid)
	return nil
}

func uniqueName(path string) string {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s %d%s", stem, i, ext)
		if _, err := os.Lstat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return fmt.Sprintf("%s %d%s", stem, time.Now().UnixNano(), ext)
}

// chownTree hands a trashed item to the user so they can empty it themselves.
func chownTree(root string, uid uint32) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		_ = os.Lchown(p, int(uid), -1)
		return nil
	})
}

func volumeRoot(path string) (string, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, nil
		}
		var pst syscall.Stat_t
		if err := syscall.Lstat(parent, &pst); err != nil {
			return dir, nil
		}
		if pst.Dev != st.Dev {
			return dir, nil
		}
		dir = parent
	}
}

func homeForUID(uid uint32) (string, error) {
	out, err := exec.Command("/usr/bin/dscl", ".", "-search", "/Users",
		"UniqueID", strconv.FormatUint(uint64(uid), 10)).Output()
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			name := fields[0]
			home, err := exec.Command("/usr/bin/dscl", ".", "-read",
				"/Users/"+name, "NFSHomeDirectory").Output()
			if err == nil {
				parts := strings.SplitN(strings.TrimSpace(string(home)), ": ", 2)
				if len(parts) == 2 && parts[1] != "" {
					return strings.TrimSpace(parts[1]), nil
				}
			}
		}
	}
	return "", fmt.Errorf("could not find the home directory for uid %d", uid)
}

func treeSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += int64(st.Blocks) * 512
		}
		return nil
	})
	return total
}

func listSnapshots() ([]helperproto.Snapshot, error) {
	out, err := exec.Command("/usr/bin/tmutil", "listlocalsnapshots", "/").Output()
	if err != nil {
		return []helperproto.Snapshot{}, nil
	}
	snaps := []helperproto.Snapshot{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "com.apple.TimeMachine.") {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(line, "com.apple.TimeMachine."), ".local")
		snaps = append(snaps, helperproto.Snapshot{Name: line, Date: date})
	}
	return snaps, nil
}

func deleteSnapshot(name string) error {
	date := strings.TrimSuffix(strings.TrimPrefix(name, "com.apple.TimeMachine."), ".local")
	// The date stamp is the only thing passed on, and it is validated first so
	// nothing resembling a flag or a path can reach tmutil.
	if date == "" || !validSnapshotDate(date) {
		return fmt.Errorf("not a snapshot name: %q", name)
	}
	out, err := exec.Command("/usr/bin/tmutil", "deletelocalsnapshots", date).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmutil: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// validSnapshotDate accepts only "YYYY-MM-DD-HHMMSS".
func validSnapshotDate(s string) bool {
	_, err := time.Parse("2006-01-02-150405", s)
	return err == nil
}
