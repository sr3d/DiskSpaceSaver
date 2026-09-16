// Package helperproto defines the wire protocol between the app and the
// privileged helper: length-prefixed msgpack messages over a Unix domain
// socket.
//
// The command set is a closed enum on purpose. The helper runs as root, so it
// must never accept anything resembling a shell command, a format string, or
// an open-ended path list. Everything it will do is enumerated here.
package helperproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/vmihailenco/msgpack/v5"
)

// SocketName is the launchd socket entry name in the daemon plist.
const SocketName = "Helper"

// ProtocolVersion is bumped whenever the message shapes change. The app checks
// it on connect and re-registers the helper on mismatch.
const ProtocolVersion = 1

// MaxMessage caps a single message at 8 MiB. Frames are one directory each, so
// even a directory with a million entries stays well under this; anything
// larger is a bug or an attack.
const MaxMessage = 8 << 20

// Op is the closed set of things the helper will do.
type Op uint8

const (
	OpVersion Op = iota + 1
	OpScan
	OpDelete
	OpListSnapshots
	OpDeleteSnapshots
	OpCancel
)

// Request is what the app sends. Exactly one Op per connection for streaming
// operations; OpCancel may be sent on a second connection with the same JobID.
type Request struct {
	Op    Op     `msgpack:"o"`
	JobID string `msgpack:"j,omitempty"`

	// OpScan
	Root        string   `msgpack:"r,omitempty"`
	Exclusions  []string `msgpack:"x,omitempty"`
	CrossDevice bool     `msgpack:"c,omitempty"`
	UseBulk     bool     `msgpack:"b,omitempty"`

	// OpDelete: Paths must all be under Root, which the helper re-checks.
	Paths   []string `msgpack:"p,omitempty"`
	ToTrash bool     `msgpack:"t,omitempty"`
	// UID is the calling user, so root-side trashing lands in that user's
	// .Trashes directory instead of root's.
	UID uint32 `msgpack:"u,omitempty"`

	// OpDeleteSnapshots
	Snapshots []string `msgpack:"s,omitempty"`
}

// MsgType tags a response message.
type MsgType uint8

const (
	MsgFrame MsgType = iota + 1
	MsgProgress
	MsgDone
	MsgError
	MsgVersion
	MsgResult
	MsgSnapshots
)

// Msg is the response envelope.
type Msg struct {
	T MsgType `msgpack:"t"`

	Frame *scanner.Frame    `msgpack:"f,omitempty"`
	Prog  *scanner.Progress `msgpack:"g,omitempty"`
	Err   string            `msgpack:"e,omitempty"`

	Version  int        `msgpack:"v,omitempty"`
	Build    string     `msgpack:"b,omitempty"`
	Result   *OpResult  `msgpack:"r,omitempty"`
	Snapshot []Snapshot `msgpack:"s,omitempty"`
}

// OpResult reports the outcome of a delete.
type OpResult struct {
	Succeeded []string          `msgpack:"s,omitempty" json:"succeeded"`
	Failed    map[string]string `msgpack:"f,omitempty" json:"failed"`
	Bytes     int64             `msgpack:"b,omitempty" json:"bytes"`
}

// Snapshot is one APFS local Time Machine snapshot.
type Snapshot struct {
	Name  string `msgpack:"n" json:"name"`
	Date  string `msgpack:"d" json:"date"`
	Bytes int64  `msgpack:"b" json:"bytes"`
}

var ErrTooLarge = errors.New("helperproto: message exceeds maximum size")

// Conn wraps a stream with length-prefixed msgpack framing.
type Conn struct {
	w   io.Writer
	r   io.Reader
	buf []byte
	hdr [4]byte
}

// NewConn wraps a bidirectional stream.
func NewConn(rw io.ReadWriter) *Conn { return &Conn{w: rw, r: rw} }

// NewWriter wraps a write-only stream (cmd/scan --frames).
func NewWriter(w io.Writer) *Conn { return &Conn{w: w} }

// NewReader wraps a read-only stream.
func NewReader(r io.Reader) *Conn { return &Conn{r: r} }

// WriteMsg encodes and writes one message.
func (c *Conn) WriteMsg(m *Msg) error { return c.writeAny(m) }

// WriteReq encodes and writes one request.
func (c *Conn) WriteReq(r *Request) error { return c.writeAny(r) }

func (c *Conn) writeAny(v any) error {
	b, err := msgpack.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return ErrTooLarge
	}
	binary.LittleEndian.PutUint32(c.hdr[:], uint32(len(b)))
	if _, err := c.w.Write(c.hdr[:]); err != nil {
		return err
	}
	_, err = c.w.Write(b)
	return err
}

func (c *Conn) readRaw() ([]byte, error) {
	if _, err := io.ReadFull(c.r, c.hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(c.hdr[:])
	if n > MaxMessage {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, n)
	}
	if cap(c.buf) < int(n) {
		c.buf = make([]byte, n)
	}
	b := c.buf[:n]
	if _, err := io.ReadFull(c.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ReadMsg reads one response message. Returns io.EOF at end of stream.
func (c *Conn) ReadMsg() (*Msg, error) {
	b, err := c.readRaw()
	if err != nil {
		return nil, err
	}
	var m Msg
	if err := msgpack.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ReadReq reads one request.
func (c *Conn) ReadReq() (*Request, error) {
	b, err := c.readRaw()
	if err != nil {
		return nil, err
	}
	var r Request
	if err := msgpack.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// FrameWriter is the convenience wrapper cmd/scan uses to dump a frame stream.
type FrameWriter struct{ c *Conn }

func NewFrameWriter(w io.Writer) *FrameWriter { return &FrameWriter{c: NewWriter(w)} }

func (f *FrameWriter) Write(fr scanner.Frame) error {
	return f.c.WriteMsg(&Msg{T: MsgFrame, Frame: &fr})
}

func (f *FrameWriter) Close() error { return f.c.WriteMsg(&Msg{T: MsgDone}) }
