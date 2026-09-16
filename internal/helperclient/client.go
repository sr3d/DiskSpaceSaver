// Package helperclient talks to the privileged helper over its launchd-managed
// Unix domain socket.
package helperclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sr3d/diskspacesaver/internal/helperproto"
	"github.com/sr3d/diskspacesaver/internal/scanner"
)

// SocketPath is where launchd publishes the helper's socket. It matches
// SockPathName in the daemon plist.
const SocketPath = "/var/run/com.sr3d.diskspacesaver.helper.sock"

// HelperLabel is the launchd label, also the plist filename stem.
const HelperLabel = "com.sr3d.diskspacesaver.helper"

// Status describes the helper's installation state, for the Preferences pane.
type Status struct {
	Installed  bool   `json:"installed"`
	Running    bool   `json:"running"`
	Version    int    `json:"version"`
	Build      string `json:"build"`
	Compatible bool   `json:"compatible"`
	Registered string `json:"registered"` // SMAppService status, human readable
	Err        string `json:"err,omitempty"`
}

// Client dials the helper on demand; it holds no persistent connection,
// because every operation is a short-lived stream.
type Client struct {
	mu     sync.Mutex
	socket string
	cached *Status
	last   time.Time
}

// New creates a client for the standard socket path.
func New() *Client { return &Client{socket: SocketPath} }

// SetSocketPath points the client at a different socket, for tests.
func (c *Client) SetSocketPath(p string) {
	c.mu.Lock()
	c.socket = p
	c.cached = nil
	c.mu.Unlock()
}

// SocketPath reports the socket in use.
func (c *Client) SocketPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socket
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	return d.DialContext(ctx, "unix", c.SocketPath())
}

// Available reports whether the helper answers. The result is cached briefly
// so the UI can ask on every render without hammering the socket.
func (c *Client) Available() bool {
	st := c.Status()
	return st.Running && st.Compatible
}

// Status queries the helper's version, or reports why it could not be reached.
func (c *Client) Status() Status {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.last) < 2*time.Second {
		st := *c.cached
		c.mu.Unlock()
		return st
	}
	c.mu.Unlock()

	st := Status{Installed: helperInstalled()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := c.dial(ctx)
	if err != nil {
		st.Err = describeDialError(err)
		c.store(st)
		return st
	}
	defer conn.Close()

	pc := helperproto.NewConn(conn)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := pc.WriteReq(&helperproto.Request{Op: helperproto.OpVersion}); err != nil {
		st.Err = err.Error()
		c.store(st)
		return st
	}
	msg, err := pc.ReadMsg()
	if err != nil {
		st.Err = err.Error()
		c.store(st)
		return st
	}
	st.Running = true
	st.Installed = true
	st.Version = msg.Version
	st.Build = msg.Build
	st.Compatible = msg.Version == helperproto.ProtocolVersion
	if !st.Compatible {
		st.Err = fmt.Sprintf("the installed helper speaks protocol %d but this build needs %d — reinstall it",
			msg.Version, helperproto.ProtocolVersion)
	}
	c.store(st)
	return st
}

func (c *Client) store(st Status) {
	c.mu.Lock()
	c.cached = &st
	c.last = time.Now()
	c.mu.Unlock()
}

func describeDialError(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "the helper is not installed"
	}
	if errors.Is(err, os.ErrPermission) {
		return "the helper socket refused this process"
	}
	return err.Error()
}

// helperInstalled reports whether the daemon plist is present on disk, which
// is true even when launchd has not started it yet.
func helperInstalled() bool {
	for _, p := range []string{
		filepath.Join("/Library/LaunchDaemons", HelperLabel+".plist"),
		SocketPath,
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Close releases any cached state.
func (c *Client) Close() {
	c.mu.Lock()
	c.cached = nil
	c.mu.Unlock()
}

// ---------------------------------------------------------------- scanning

// ScanSource streams frames from the helper. It satisfies scanstore.Source, so
// an administrator scan runs through exactly the same pipeline as a local one.
type ScanSource struct {
	c      *Client
	root   string
	excl   []string
	bulk   bool
	frames chan scanner.Frame
	prog   chan scanner.Progress
	err    error
	once   sync.Once
}

// NewScanSource prepares a root scan over the helper socket.
func (c *Client) NewScanSource(root string, exclusions []string, bulk bool) *ScanSource {
	return &ScanSource{
		c: c, root: root, excl: exclusions, bulk: bulk,
		frames: make(chan scanner.Frame, 256),
		prog:   make(chan scanner.Progress, 8),
	}
}

func (s *ScanSource) Frames() <-chan scanner.Frame      { return s.frames }
func (s *ScanSource) Progress() <-chan scanner.Progress { return s.prog }
func (s *ScanSource) Err() error                        { return s.err }

// Run streams the helper's frames until the scan ends or ctx is cancelled.
func (s *ScanSource) Run(ctx context.Context) {
	defer s.once.Do(func() {
		close(s.prog)
		close(s.frames)
	})

	conn, err := s.c.dial(ctx)
	if err != nil {
		s.err = fmt.Errorf("could not reach the privileged helper: %s", describeDialError(err))
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close() // unblocks the read loop on cancel
	}()

	uid := currentUID()
	pc := helperproto.NewConn(conn)
	req := &helperproto.Request{
		Op: helperproto.OpScan, Root: s.root, Exclusions: s.excl,
		UseBulk: s.bulk, UID: uid,
	}
	if err := pc.WriteReq(req); err != nil {
		s.err = err
		return
	}

	for {
		msg, err := pc.ReadMsg()
		if err != nil {
			if ctx.Err() == nil && !isClosed(err) {
				s.err = err
			}
			return
		}
		switch msg.T {
		case helperproto.MsgFrame:
			if msg.Frame == nil {
				continue
			}
			select {
			case s.frames <- *msg.Frame:
			case <-ctx.Done():
				return
			}
		case helperproto.MsgProgress:
			if msg.Prog == nil {
				continue
			}
			select {
			case s.prog <- *msg.Prog:
			default:
			}
		case helperproto.MsgError:
			s.err = errors.New(msg.Err)
			return
		case helperproto.MsgDone:
			return
		}
	}
}

func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed)
}

func currentUID() uint32 {
	u, err := user.Current()
	if err != nil {
		return uint32(os.Getuid())
	}
	n, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return uint32(os.Getuid())
	}
	return uint32(n)
}

// ---------------------------------------------------------------- deleting

// Delete asks the helper to remove paths as root. Root is passed so the helper
// can independently verify containment; it never trusts the app's word for it.
func (c *Client) Delete(root string, paths []string, toTrash bool) error {
	res, err := c.DeleteWithResult(root, paths, toTrash)
	if err != nil {
		return err
	}
	for p, msg := range res.Failed {
		return fmt.Errorf("%s: %s", p, msg)
	}
	return nil
}

// DeleteWithResult is Delete with the per-path outcome.
func (c *Client) DeleteWithResult(root string, paths []string, toTrash bool) (*helperproto.OpResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not reach the privileged helper: %s", describeDialError(err))
	}
	defer conn.Close()

	pc := helperproto.NewConn(conn)
	req := &helperproto.Request{
		Op: helperproto.OpDelete, Root: root, Paths: paths,
		ToTrash: toTrash, UID: currentUID(),
	}
	if err := pc.WriteReq(req); err != nil {
		return nil, err
	}
	for {
		msg, err := pc.ReadMsg()
		if err != nil {
			return nil, err
		}
		switch msg.T {
		case helperproto.MsgResult:
			return msg.Result, nil
		case helperproto.MsgError:
			return nil, errors.New(msg.Err)
		case helperproto.MsgDone:
			return nil, errors.New("the helper closed without reporting a result")
		}
	}
}

// ListSnapshots asks the helper for APFS local snapshots.
func (c *Client) ListSnapshots() ([]helperproto.Snapshot, error) {
	msg, err := c.roundTrip(&helperproto.Request{Op: helperproto.OpListSnapshots}, helperproto.MsgSnapshots)
	if err != nil {
		return nil, err
	}
	return msg.Snapshot, nil
}

// DeleteSnapshots removes local snapshots by name.
func (c *Client) DeleteSnapshots(names []string) error {
	_, err := c.roundTrip(&helperproto.Request{
		Op: helperproto.OpDeleteSnapshots, Snapshots: names,
	}, helperproto.MsgResult)
	return err
}

func (c *Client) roundTrip(req *helperproto.Request, want helperproto.MsgType) (*helperproto.Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not reach the privileged helper: %s", describeDialError(err))
	}
	defer conn.Close()
	pc := helperproto.NewConn(conn)
	if err := pc.WriteReq(req); err != nil {
		return nil, err
	}
	for {
		msg, err := pc.ReadMsg()
		if err != nil {
			return nil, err
		}
		switch msg.T {
		case want:
			return msg, nil
		case helperproto.MsgError:
			return nil, errors.New(msg.Err)
		case helperproto.MsgDone:
			return nil, errors.New("the helper closed without answering")
		}
	}
}
