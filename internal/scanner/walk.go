package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures a scan.
type Options struct {
	// Root is the directory to scan. Use ResolveScanRoot first for volumes.
	Root string
	// Workers defaults to NumCPU (capped); directory walking on APFS is
	// syscall-bound, so more workers than cores still helps a little.
	Workers int
	// ShowHidden only affects the Flags we set; hidden entries are always
	// scanned, because they occupy real space. The UI does the filtering.
	ShowHidden bool
	// Exclusions are absolute path prefixes that are not descended into.
	Exclusions []string
	// CrossDevice descends into other mounted filesystems. Off by default:
	// a volume scan must not wander into other volumes.
	CrossDevice bool
	// UseBulk enables the getattrlistbulk(2) fast path (M7). Falls back to
	// the readdir+fstatat path automatically if the syscall is unavailable.
	UseBulk bool
	// ScanCloudStorage descends into cloud file-provider folders. Off by
	// default: their contents are not on this disk, and stat-ing an item that
	// has not been downloaded blocks on the network. Scanning the root of a
	// provider directly still works — this only governs ones met on the way.
	ScanCloudStorage bool
	// ProgressInterval throttles Progress emission. Zero means 100ms.
	ProgressInterval time.Duration
}

func (o *Options) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	n := runtime.NumCPU()
	if n < 4 {
		n = 4
	}
	if n > 16 {
		n = 16
	}
	return n
}

type dirJob struct {
	path string
	id   uint32
	dev  uint64
}

// Scanner walks a tree and emits Frames.
type Scanner struct {
	opt Options

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []dirJob
	pending int
	closed  bool

	nextID atomic.Uint32
	links  linkSet

	files, dirs, bytes, noperm atomic.Int64
	curPath                    atomic.Pointer[string]

	// lastAdvance tracks when the counters last moved, so a walk blocked in a
	// stat syscall can be reported rather than looking like a freeze.
	lastAdvance atomic.Int64
	lastTotal   atomic.Int64

	frames chan Frame
	prog   chan Progress
	err    atomic.Pointer[error]

	// Cloud provider locations for the user running this scan, resolved once.
	cloudParents, cloudExact []string
}

// isCloudRoot reports whether path is a cloud provider's root directory.
func (s *Scanner) isCloudRoot(path string) bool {
	return matchesCloudRoot(path, s.cloudParents, s.cloudExact)
}

// New builds a Scanner. Nothing runs until Run is called.
func New(opt Options) *Scanner {
	s := &Scanner{opt: opt, frames: make(chan Frame, 256), prog: make(chan Progress, 8)}
	s.cond = sync.NewCond(&s.mu)
	s.links.init()
	s.cloudParents, s.cloudExact = homeCloudRoots()
	return s
}

// Frames is the output stream. It is closed when the walk finishes or is
// cancelled; always drain it.
func (s *Scanner) Frames() <-chan Frame { return s.frames }

// Progress is a throttled status stream, closed after Frames.
func (s *Scanner) Progress() <-chan Progress { return s.prog }

// Err reports why the scan stopped, if it stopped badly.
func (s *Scanner) Err() error {
	if p := s.err.Load(); p != nil {
		return *p
	}
	return nil
}

// Run walks the tree. It returns when every worker has finished; the caller is
// expected to be draining Frames concurrently.
func (s *Scanner) Run(ctx context.Context) {
	defer close(s.prog)
	defer close(s.frames)

	root := s.opt.Root
	// The root's volume identity seeds every job: a child on a different
	// volume is a mount point and is not descended into.
	vol, err := rootVolumeID(root)
	if err != nil {
		var ie error = err
		s.err.Store(&ie)
		return
	}
	s.nextID.Store(RootDirID)
	s.push(dirJob{path: root, id: RootDirID, dev: vol})

	stopProg := s.startProgress(ctx)
	defer stopProg()

	var wg sync.WaitGroup
	n := s.opt.workers()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(ctx)
		}()
	}
	wg.Wait()
}

func (s *Scanner) startProgress(ctx context.Context) func() {
	iv := s.opt.ProgressInterval
	if iv <= 0 {
		iv = 100 * time.Millisecond
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(finished)
		t := time.NewTicker(iv)
		defer t.Stop()
		for {
			select {
			case <-done:
				s.emitProgress(true)
				return
			case <-ctx.Done():
				s.emitProgress(true)
				return
			case <-t.C:
				s.emitProgress(false)
			}
		}
	}()
	// Stopping waits for the emitter to exit. Run closes the progress channel
	// on return, and a ticker still in flight would send on a closed channel.
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}

func (s *Scanner) emitProgress(final bool) {
	now := time.Now().UnixMilli()
	total := s.files.Load() + s.dirs.Load()
	if total != s.lastTotal.Load() {
		s.lastTotal.Store(total)
		s.lastAdvance.Store(now)
	} else if s.lastAdvance.Load() == 0 {
		s.lastAdvance.Store(now)
	}

	p := Progress{
		Files:        s.files.Load(),
		Dirs:         s.dirs.Load(),
		Bytes:        s.bytes.Load(),
		Inaccessible: s.noperm.Load(),
		Done:         final,
	}
	if cp := s.curPath.Load(); cp != nil {
		p.CurrentPath = *cp
	}
	if err := s.Err(); err != nil {
		p.Err = err.Error()
	}
	if !final {
		if stuck := now - s.lastAdvance.Load(); stuck > 3000 {
			p.Stalled = true
			p.StalledMS = stuck
		}
	}
	select {
	case s.prog <- p:
	default: // a slow consumer must never stall the walk
	}
}

func (s *Scanner) push(j dirJob) {
	s.mu.Lock()
	s.queue = append(s.queue, j)
	s.pending++
	s.mu.Unlock()
	s.cond.Signal()
}

func (s *Scanner) pushAll(js []dirJob) {
	if len(js) == 0 {
		return
	}
	s.mu.Lock()
	s.queue = append(s.queue, js...)
	s.pending += len(js)
	s.mu.Unlock()
	s.cond.Broadcast()
}

// pop blocks until work is available, or until the walk is over. ok=false
// means every directory has been processed and the worker should exit.
func (s *Scanner) pop() (dirJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.closed {
			return dirJob{}, false
		}
		if n := len(s.queue); n > 0 {
			// LIFO: depth-first keeps the working set small and gets the
			// chart's deep branches filled in early.
			j := s.queue[n-1]
			s.queue = s.queue[:n-1]
			return j, true
		}
		if s.pending == 0 {
			s.closed = true
			s.cond.Broadcast()
			return dirJob{}, false
		}
		s.cond.Wait()
	}
}

func (s *Scanner) finish() {
	s.mu.Lock()
	s.pending--
	last := s.pending == 0 && len(s.queue) == 0
	if last {
		s.closed = true
	}
	s.mu.Unlock()
	if last {
		s.cond.Broadcast()
	}
}

// stop unblocks every waiting worker, for cancellation.
func (s *Scanner) stop() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *Scanner) worker(ctx context.Context) {
	// A cancelled context has to wake workers that are parked in cond.Wait.
	stopOnce := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.stop()
		case <-stopOnce:
		}
	}()
	defer close(stopOnce)

	for {
		if ctx.Err() != nil {
			return
		}
		job, ok := s.pop()
		if !ok {
			return
		}
		s.readDir(ctx, job)
		s.finish()
	}
}

func (s *Scanner) excluded(path string) bool {
	for _, ex := range s.opt.Exclusions {
		if ex == "" {
			continue
		}
		sep := string(filepath.Separator)
		if path == ex || strings.HasPrefix(path, strings.TrimSuffix(ex, sep)+sep) {
			return true
		}
	}
	return false
}

func (s *Scanner) readDir(ctx context.Context, job dirJob) {
	p := job.path
	s.curPath.Store(&p)
	s.dirs.Add(1)

	frame := Frame{DirID: job.id}
	var children []dirJob

	entries, err := s.listDir(job, &frame, &children, ctx)
	_ = entries
	if err != nil {
		if isPerm(err) {
			s.noperm.Add(1)
			frame.Err = "permission denied"
		} else {
			frame.Err = err.Error()
		}
	}

	select {
	case s.frames <- frame:
	case <-ctx.Done():
		return
	}
	s.pushAll(children)
}

// listDir fills frame.Entry and collects subdirectories to enqueue.
func (s *Scanner) listDir(job dirJob, frame *Frame, children *[]dirJob, ctx context.Context) (int, error) {
	if s.opt.UseBulk {
		n, err := s.listDirBulk(job, frame, children)
		if err == nil || !errBulkUnsupported(err) {
			return n, err
		}
		// fall through to the per-platform listing
	}
	return s.listDirNative(job, frame, children)
}

func isPerm(err error) bool {
	return os.IsPermission(err)
}

// linkSet records (dev, inode) pairs for files with nlink > 1. Sharded because
// every worker hits it on hardlink-heavy trees (/System, node_modules).
type linkSet struct {
	shards [64]struct {
		mu sync.Mutex
		m  map[[2]uint64]struct{}
		_  [40]byte // keep shards on separate cache lines
	}
}

func (l *linkSet) init() {
	for i := range l.shards {
		l.shards[i].m = make(map[[2]uint64]struct{}, 64)
	}
}

// firstSeen reports whether this is the first time we have seen this inode.
func (l *linkSet) firstSeen(dev, ino uint64) bool {
	k := [2]uint64{dev, ino}
	sh := &l.shards[ino&63]
	sh.mu.Lock()
	_, dup := sh.m[k]
	if !dup {
		sh.m[k] = struct{}{}
	}
	sh.mu.Unlock()
	return !dup
}
