// Package scanstore owns the live scan sessions. Each session is one tab in
// the UI: its own tree, its own progress, cancellable independently, and
// several may run at once.
package scanstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// Event names emitted to the frontend.
const (
	EventProgress = "scan:progress"
	EventSnapshot = "scan:snapshot"
	EventDone     = "scan:done"
	EventTree     = "tree:changed"
	// EventSubProgress reports a subtree rescan, which has no Session of its
	// own and so cannot ride on EventProgress.
	EventSubProgress = "scan:subprogress"
	EventScans       = "scans:changed"
)

// Status is a session's lifecycle state.
type Status string

const (
	StatusScanning  Status = "scanning"
	StatusDone      Status = "done"
	StatusCancelled Status = "cancelled"
	StatusError     Status = "error"
)

// Emitter delivers an event to the frontend.
type Emitter func(name string, data any)

// Source produces scanner frames. A local walk and a stream from the
// privileged helper both satisfy it, which is what keeps the UI path identical
// for ordinary and administrator scans.
type Source interface {
	Run(ctx context.Context)
	Frames() <-chan scanner.Frame
	Progress() <-chan scanner.Progress
	Err() error
}

// Summary is the tab header data.
type Summary struct {
	ID           string `json:"id"`
	Root         string `json:"root"`
	Label        string `json:"label"`
	VolumeUUID   string `json:"volumeUUID,omitempty"`
	AsAdmin      bool   `json:"asAdmin"`
	Status       Status `json:"status"`
	Files        int64  `json:"files"`
	Dirs         int64  `json:"dirs"`
	Bytes        int64  `json:"bytes"`
	Inaccessible int64  `json:"inaccessible"`
	CurrentPath  string `json:"currentPath,omitempty"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   int64  `json:"finishedAt,omitempty"`
	Stalled      bool   `json:"stalled"`
	StalledMS    int64  `json:"stalledMs,omitempty"`
	// ExpectedBytes is how much this scan is expected to account for, zero
	// when nothing credible was available. Exposed so the interface can say
	// why there is no bar, or that a folder has outgrown its last scan.
	ExpectedBytes int64 `json:"expectedBytes"`
	// Fraction is completion in [0,1], or -1 when it cannot be known. A
	// float64 sentinel rather than a pointer: nil would arrive as JSON null,
	// which the generated Wails model constructor dereferences.
	Fraction  float64 `json:"fraction"`
	FromCache bool    `json:"fromCache"`
	CachedAt  int64   `json:"cachedAt,omitempty"`
	Err       string  `json:"err,omitempty"`
}

// Session is one scan.
type Session struct {
	ID         string
	Root       string
	Label      string
	VolumeUUID string
	AsAdmin    bool
	Tree       *tree.Tree

	// expectedBytes is set once at construction and never written again, so
	// it needs no lock.
	expectedBytes int64

	mu         sync.RWMutex
	status     Status
	prog       scanner.Progress
	startedAt  time.Time
	finishedAt time.Time
	fromCache  bool
	cachedAt   time.Time
	err        string
	// frac is maintained by the progress relay, which already holds the write
	// lock. Deriving it inside Summary would mean writing under an RLock.
	frac float64

	cancel  context.CancelFunc
	running atomic.Bool
}

// Summary snapshots the session for the tab bar.
func (s *Session) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sum := Summary{
		ID: s.ID, Root: s.Root, Label: s.Label, VolumeUUID: s.VolumeUUID,
		AsAdmin: s.AsAdmin, Status: s.status,
		Files: s.prog.Files, Dirs: s.prog.Dirs, Bytes: s.prog.Bytes,
		Inaccessible: s.prog.Inaccessible, CurrentPath: s.prog.CurrentPath,
		Stalled: s.prog.Stalled, StalledMS: s.prog.StalledMS,
		ExpectedBytes: s.expectedBytes, Fraction: s.frac,
		StartedAt: s.startedAt.UnixMilli(), FromCache: s.fromCache, Err: s.err,
	}
	if !s.finishedAt.IsZero() {
		sum.FinishedAt = s.finishedAt.UnixMilli()
	}
	if !s.cachedAt.IsZero() {
		sum.CachedAt = s.cachedAt.UnixMilli()
	}
	// Once the walk is finished the tree is authoritative; the progress
	// counters lag by up to one tick.
	if s.status != StatusScanning && s.Tree != nil {
		if root, ok := s.Tree.Snapshot(s.Tree.Root); ok {
			sum.Files, sum.Dirs, sum.Bytes = int64(root.Files), int64(root.Dirs), root.Alloc
		}
		sum.Inaccessible = s.Tree.Errors()
		sum.CurrentPath = ""
		sum.Stalled, sum.StalledMS = false, 0
	}
	return sum
}

// Scanning reports whether the walk is still in flight.
func (s *Session) Scanning() bool { return s.running.Load() }

// Store holds every open session.
type Store struct {
	mu    sync.RWMutex
	byID  map[string]*Session
	order []string
	seq   atomic.Uint64
	emit  Emitter
}

// New creates a store that publishes through emit.
func New(emit Emitter) *Store {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Store{byID: map[string]*Session{}, emit: emit}
}

// Get returns a session by id.
func (st *Store) Get(id string) (*Session, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	s, ok := st.byID[id]
	return s, ok
}

// List returns every open session in the order it was opened.
func (st *Store) List() []Summary {
	st.mu.RLock()
	sessions := make([]*Session, 0, len(st.order))
	for _, id := range st.order {
		if s, ok := st.byID[id]; ok {
			sessions = append(sessions, s)
		}
	}
	st.mu.RUnlock()

	out := make([]Summary, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Summary())
	}
	return out
}

// FindByRoot returns an existing session for a root, so reopening a volume
// switches to its tab instead of scanning it twice.
func (st *Store) FindByRoot(root string, asAdmin bool) (*Session, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, id := range st.order {
		s := st.byID[id]
		if s != nil && s.Root == root && s.AsAdmin == asAdmin {
			return s, true
		}
	}
	return nil, false
}

func (st *Store) newID() string {
	return fmt.Sprintf("scan-%d-%d", time.Now().UnixNano()/1e6, st.seq.Add(1))
}

// Adopt registers a session built elsewhere — a cache load, for instance —
// without starting a walk.
func (st *Store) Adopt(root, label, volumeUUID string, t *tree.Tree, cachedAt time.Time) *Session {
	s := &Session{
		ID: st.newID(), Root: root, Label: label, VolumeUUID: volumeUUID,
		Tree: t, status: StatusDone, startedAt: time.Now(),
		finishedAt: time.Now(), fromCache: true, cachedAt: cachedAt,
		frac: 1,
	}
	st.add(s)
	st.emit(EventScans, st.List())
	return s
}

func (st *Store) add(s *Session) {
	st.mu.Lock()
	st.byID[s.ID] = s
	st.order = append(st.order, s.ID)
	st.mu.Unlock()
}

// StartOptions configures a new scan.
type StartOptions struct {
	Root       string
	Label      string
	VolumeUUID string
	AsAdmin    bool
	// NewSource builds the frame producer. Swapping it is how an
	// administrator scan reuses this entire pipeline.
	NewSource func(root string) Source
	// SnapshotInterval throttles the growing-chart event. Zero means 250ms.
	SnapshotInterval time.Duration
	// SnapshotDepth is how many rings the live chart draws.
	SnapshotDepth int
	// MinFraction is the arc floor for live snapshots.
	MinFraction float64
	// MinSize is the user's absolute size floor for live snapshots.
	MinSize int64
	// ExpectedBytes is how much this scan is expected to find, for the
	// progress bar. Resolved by the caller, the only layer that can see the
	// scan cache and the volume list. Zero means unknown.
	ExpectedBytes int64
}

// Start creates a session and begins walking. It returns as soon as the
// session exists, so the UI can open the tab immediately.
func (st *Store) Start(opt StartOptions) (*Session, error) {
	root := scanner.ResolveScanRoot(opt.Root)
	label := opt.Label
	if label == "" {
		label = filepath.Base(root)
		if label == "/" || label == "." {
			label = root
		}
	}
	s := &Session{
		ID: st.newID(), Root: root, Label: label, VolumeUUID: opt.VolumeUUID,
		AsAdmin: opt.AsAdmin, Tree: tree.New(root),
		expectedBytes: opt.ExpectedBytes,
		status:        StatusScanning, startedAt: time.Now(),
		frac: Indeterminate,
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running.Store(true)
	st.add(s)
	st.emit(EventScans, st.List())

	go st.run(ctx, s, opt)
	return s, nil
}

func (st *Store) run(ctx context.Context, s *Session, opt StartOptions) {
	defer s.cancel()

	src := opt.NewSource(s.Root)
	snapIv := opt.SnapshotInterval
	if snapIv <= 0 {
		snapIv = 250 * time.Millisecond
	}
	depth := opt.SnapshotDepth
	if depth <= 0 {
		depth = 5
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // progress relay
		defer wg.Done()
		for p := range src.Progress() {
			s.mu.Lock()
			s.prog = p
			s.frac = s.fractionLocked(p.Bytes)
			s.mu.Unlock()
			st.emit(EventProgress, s.Summary())
		}
	}()

	// The chart animation: a throttled root viewport pushed while the tree
	// fills. Emitting per frame would flood the WebView; 250ms is fast enough
	// to read as growth.
	stopSnap := make(chan struct{})
	snapDone := make(chan struct{})
	go func() {
		defer close(snapDone)
		t := time.NewTicker(snapIv)
		defer t.Stop()
		for {
			select {
			case <-stopSnap:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				v := s.Tree.Viewport(s.Tree.Root, depth, opt.MinFraction, opt.MinSize, true)
				st.emit(EventSnapshot, map[string]any{"scanID": s.ID, "viewport": v})
			}
		}
	}()

	go src.Run(ctx)
	for f := range src.Frames() {
		s.Tree.Apply(f)
	}
	close(stopSnap)
	<-snapDone
	wg.Wait()

	s.Tree.Finalize()

	// running is cleared under the same lock as the status, and after it.
	// Clearing it first opened a window where Scanning() said false while
	// Summary() still said "scanning" — and cacheWhenDone polls the former
	// then checks the latter, so it could see the scan as finished, read a
	// status that was not yet StatusDone, and skip saving the cache entirely.
	s.mu.Lock()
	s.finishedAt = time.Now()
	switch {
	case src.Err() != nil:
		s.status, s.err = StatusError, src.Err().Error()
	case ctx.Err() != nil:
		s.status = StatusCancelled
	default:
		s.status = StatusDone
	}
	// Freeze the bar where the scan actually stopped. Only a completed scan
	// earns a full bar; a cancelled one that got a third of the way through
	// should still look a third of the way through.
	if s.status == StatusDone {
		s.frac = 1
	} else if root, ok := s.Tree.Snapshot(s.Tree.Root); ok {
		s.frac = s.fractionLocked(root.Alloc)
	}
	s.running.Store(false)
	s.mu.Unlock()

	st.emit(EventDone, s.Summary())
	st.emit(EventScans, st.List())
}

// Indeterminate is the Fraction value meaning "no credible expectation".
const Indeterminate = -1.0

// runningCap holds a determinate bar just short of full until the scan really
// finishes. Just short, not 0.99: on a well-calibrated expectation the bar
// otherwise visibly sits at ninety-nine percent for the last stretch.
const runningCap = 0.995

// overshoot is how far past its expectation a scan may run before the bar
// gives up on it. Some slack, because every denominator is a little wrong and
// a bar that flickers to indeterminate over a rounding error is worse than one
// that sits at full for a second; but not much, because the case this guards
// against — a folder that has grown a lot since it was last scanned — would
// otherwise hold the bar at full for most of the walk.
const overshoot = 1.05

// fractionLocked turns bytes seen into completion. The caller holds the lock.
//
// Monotonic, because a bar that goes backwards reads as a fault even when the
// arithmetic behind it is right. The floor is nearly free today — Bytes only
// grows — but the progress channel drops samples under load and the totals
// switch source at the end, so it is worth insuring against.
func (s *Session) fractionLocked(bytes int64) float64 {
	return Fraction(bytes, s.expectedBytes, s.frac)
}

// Fraction turns bytes seen into completion against an expectation, holding at
// floor so the result never goes backwards. It is exported because a subtree
// rescan reports progress without a Session, and two copies of these rules
// would drift apart.
func Fraction(bytes, expected int64, floor float64) float64 {
	// Below a megabyte an expectation is noise, not an expectation.
	if expected < 1<<20 {
		return Indeterminate
	}
	f := float64(bytes) / float64(expected)
	if f > overshoot {
		// The scan has walked past everything we expected it to find, so the
		// expectation is not merely imprecise, it is disproved — the folder
		// grew since the cached scan it came from. Pinning the bar near full
		// for the rest of the walk would be a lie that gets worse the longer
		// it runs; saying we no longer know is at least true, and the file
		// count beside the bar keeps moving either way.
		return Indeterminate
	}
	if f > runningCap {
		f = runningCap
	}
	if f < 0 {
		f = 0
	}
	if floor > f {
		return floor // already further along; hold the floor
	}
	return f
}

// Cancel stops a running scan. The partial tree stays usable.
func (st *Store) Cancel(id string) {
	s, ok := st.Get(id)
	if !ok {
		return
	}
	if c := s.cancel; c != nil {
		c()
	}
}

// Close cancels and forgets a session, freeing its tree.
func (st *Store) Close(id string) {
	s, ok := st.Get(id)
	if !ok {
		return
	}
	if c := s.cancel; c != nil {
		c()
	}
	st.mu.Lock()
	delete(st.byID, id)
	for i, oid := range st.order {
		if oid == id {
			st.order = append(st.order[:i], st.order[i+1:]...)
			break
		}
	}
	st.mu.Unlock()
	st.emit(EventScans, st.List())
}

// CloseAll cancels every scan, for shutdown.
func (st *Store) CloseAll() {
	st.mu.RLock()
	ids := append([]string(nil), st.order...)
	st.mu.RUnlock()
	for _, id := range ids {
		st.Cancel(id)
	}
}

// Emit publishes an event through the store's emitter.
func (st *Store) Emit(name string, data any) { st.emit(name, data) }

// Scanning reports whether any session is still walking.
//
// The completion figure that used to live here divided entries seen by a
// hardcoded two million, because at the time there was nothing to divide by.
// There is now — but the expectation comes from the scan cache and the volume
// list, neither of which this package can see, so the caller combines them.
func (st *Store) Scanning() bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, id := range st.order {
		if s := st.byID[id]; s != nil && s.Scanning() {
			return true
		}
	}
	return false
}
