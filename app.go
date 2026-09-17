package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sr3d/diskspacesaver/internal/collector"
	"github.com/sr3d/diskspacesaver/internal/helperclient"
	"github.com/sr3d/diskspacesaver/internal/platform"
	"github.com/sr3d/diskspacesaver/internal/prefs"
	"github.com/sr3d/diskspacesaver/internal/scancache"
	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/scanstore"
	"github.com/sr3d/diskspacesaver/internal/tree"
	"github.com/sr3d/diskspacesaver/internal/updater"
)

// App is the object Wails binds. Every exported method is callable from the
// frontend; nothing here returns a tree, only viewport slices and handles.
type App struct {
	ctx   context.Context
	prefs *prefs.Store
	scans *scanstore.Store
	tray  *collector.Collector
	cache *scancache.Cache
	help  *helperclient.Client
	upd   *updater.Updater

	dockMu   sync.Mutex
	dockStop chan struct{}

	cacheMu     sync.Mutex
	cacheTimers map[string]*time.Timer

	previewMu sync.Mutex
	previews  map[string]string

	rootMu    sync.Mutex
	rootLocks map[string]*sync.Mutex
}

// lockRoot serialises work for one scan root and returns the unlock.
//
// Decoding a large saved scan takes seconds, during which the session does not
// exist yet — so a second click cannot be caught by looking for an open tab.
// Without this, opening a cached scan twice in quick succession decoded the
// tree twice and left two tabs for the same volume.
func (a *App) lockRoot(root string) func() {
	a.rootMu.Lock()
	if a.rootLocks == nil {
		a.rootLocks = map[string]*sync.Mutex{}
	}
	mu, ok := a.rootLocks[root]
	if !ok {
		mu = &sync.Mutex{}
		a.rootLocks[root] = mu
	}
	a.rootMu.Unlock()

	mu.Lock()
	return mu.Unlock
}

// NewApp builds the application services.
func NewApp() *App {
	p := prefs.Load()
	a := &App{
		prefs: p,
		tray:  collector.New(),
		cache: scancache.Open(),
		help:  helperclient.New(),
	}
	a.scans = scanstore.New(a.emit)
	a.upd = updater.New(p)
	a.cacheTimers = map[string]*time.Timer{}
	return a
}

func (a *App) emit(name string, data any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

// startup runs once the WebView exists.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	platform.WatchVolumes(func() {
		vols, err := platform.ListVolumes(true)
		if err != nil {
			return
		}
		a.emit("volumes:changed", vols)
	})

	// The fourth and fifth mouse buttons. On macOS they are taken off the
	// event queue before the WebView sees them, so this is the only route they
	// have; on Windows the WebView delivers them to the DOM and this is a
	// no-op. Either way they end up in the same command table as the menu.
	platform.WatchMouseButtons(func(button int) {
		switch button {
		case 3:
			a.emit("menu:command", "back")
		case 4:
			a.emit("menu:command", "forward")
		}
	})

	a.startDockIndicator()
	a.upd.Start(ctx, a.emit)
}

// shutdown stops scans and releases OS resources. Returning true would veto
// the close; we never do.
func (a *App) shutdown(ctx context.Context) {
	a.stopDockIndicator()
	platform.UnwatchVolumes()
	platform.UnwatchMouseButtons()
	platform.SetTaskbarBadge("")
	platform.SetTaskbarProgress(-1)
	a.scans.CloseAll()
	a.help.Close()
}

// startDockIndicator keeps the Dock icon showing scan activity.
func (a *App) startDockIndicator() {
	a.dockMu.Lock()
	defer a.dockMu.Unlock()
	if a.dockStop != nil {
		return
	}
	stop := make(chan struct{})
	a.dockStop = stop
	go func() {
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		var wasScanning bool
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				scanning, frac, files := a.scanProgress()
				switch {
				case scanning:
					platform.SetTaskbarProgress(frac)
					if frac >= 0 {
						platform.SetTaskbarBadge(fmt.Sprintf("%d%%", int(frac*100)))
					} else {
						// No honest percentage to show, so show the one number
						// that is certainly true.
						platform.SetTaskbarBadge(formatCount(files))
					}
					wasScanning = true
				case wasScanning:
					platform.SetTaskbarProgress(-1)
					platform.SetTaskbarBadge("")
					wasScanning = false
				}
			}
		}
	}()
}

// scanProgress combines every running scan into one figure for the taskbar.
//
// Weighted by expected bytes rather than averaged: a terabyte drive and a
// two-gigabyte folder are not half the work each. Deliberately not monotonic,
// unlike a single scan's own bar — starting a second scan really does mean
// there is more left to do, and the indicator should say so.
func (a *App) scanProgress() (scanning bool, fraction float64, files int64) {
	var seen, expected int64
	var anyUnknown bool

	for _, sum := range a.scans.List() {
		if sum.Status != scanstore.StatusScanning {
			continue
		}
		scanning = true
		files += sum.Files + sum.Dirs
		if sum.Fraction < 0 || sum.ExpectedBytes <= 0 {
			anyUnknown = true
			continue
		}
		seen += sum.Bytes
		expected += sum.ExpectedBytes
	}
	if !scanning || expected <= 0 {
		return scanning, scanstore.Indeterminate, files
	}

	fraction = float64(seen) / float64(expected)
	if fraction > 0.995 {
		fraction = 0.995
	}
	if anyUnknown && fraction > 0.9 {
		// Part of the work has no expectation at all, so the combined figure
		// must not imply the whole thing is nearly done.
		fraction = 0.9
	}
	return true, fraction, files
}

func (a *App) stopDockIndicator() {
	a.dockMu.Lock()
	defer a.dockMu.Unlock()
	if a.dockStop != nil {
		close(a.dockStop)
		a.dockStop = nil
	}
}

// ---------------------------------------------------------------- volumes

// ListVolumes returns every mounted volume with its capacity bars.
func (a *App) ListVolumes() ([]platform.Volume, error) {
	return platform.ListVolumes(true)
}

// ---------------------------------------------------------------- scanning

// StartScan begins scanning a path and returns the new scan's id. A scan of
// the same root that is still running is returned instead of started twice; a
// finished one is not, because asking to scan again means asking for fresh
// numbers.
func (a *App) StartScan(path string, asAdmin bool) (string, error) {
	if path == "" {
		return "", errors.New("no path given")
	}
	root := scanner.ResolveScanRoot(path)
	st, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("cannot scan %s: %w", path, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a folder", path)
	}
	if s, ok := a.scans.FindByRoot(root, asAdmin); ok && s.Scanning() {
		return s.ID, nil
	}

	p := a.prefs.Get()
	if asAdmin && !a.help.Available() {
		return "", errors.New("the privileged helper is not installed — install it in Preferences to scan as administrator")
	}

	opt := scanstore.StartOptions{
		Root:          root,
		VolumeUUID:    a.volumeUUIDFor(root),
		AsAdmin:       asAdmin,
		Label:         a.labelFor(root),
		SnapshotDepth: p.Rings,
		MinFraction:   p.MinArcFraction,
		MinSize:       p.MinItemSize,
		ExpectedBytes: a.expectedBytesFor(root, a.volumeUUIDFor(root), asAdmin),
	}
	if asAdmin {
		opt.NewSource = func(r string) scanstore.Source {
			return a.help.NewScanSource(r, p.Exclusions, p.UseBulkScan)
		}
	} else {
		opt.NewSource = func(r string) scanstore.Source {
			return scanner.New(scanner.Options{
				Root:       r,
				Exclusions: p.Exclusions,
				UseBulk:    p.UseBulkScan,
			})
		}
	}

	s, err := a.scans.Start(opt)
	if err != nil {
		return "", err
	}
	go a.cacheWhenDone(s)
	a.emit("scan:opened", s.ID)
	return s.ID, nil
}

// cacheWhenDone persists a completed scan so reopening the volume is instant.
func (a *App) cacheWhenDone(s *scanstore.Session) {
	for s.Scanning() {
		time.Sleep(250 * time.Millisecond)
	}
	if !a.prefs.Get().UseCache {
		return
	}
	sum := s.Summary()
	if sum.Status != scanstore.StatusDone {
		return // never cache a partial or failed walk
	}
	if err := a.cache.Save(cacheKey(s.Root, sum.VolumeUUID), s.Root, s.Tree, s.AsAdmin); err != nil {
		runtime.LogWarningf(a.ctx, "could not cache scan of %s: %v", s.Root, err)
	}
}

// invalidateCache re-saves a scan's cache after the tree changed under it.
//
// Without this the cache still lists files the user has just deleted, and
// reopening the volume shows them again — the cache is loaded and drawn
// without a rescan, so a stale one is indistinguishable from the truth.
//
// The save is debounced: deleting a folder full of items fires one tree change
// per item, and rewriting a multi-million node tree each time would cost more
// than the scan did.
func (a *App) invalidateCache(s *scanstore.Session) {
	if !a.prefs.Get().UseCache || s == nil || s.Scanning() {
		return
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if t, ok := a.cacheTimers[s.ID]; ok {
		t.Stop()
	}
	a.cacheTimers[s.ID] = time.AfterFunc(3*time.Second, func() {
		a.cacheMu.Lock()
		delete(a.cacheTimers, s.ID)
		a.cacheMu.Unlock()

		// Only a complete tree may be written back. Checking Scanning() alone
		// let a cancelled scan's partial tree be saved under the real cache
		// key the moment anything was deleted from it — which both returns a
		// truncated tree on the next open and poisons the size expectation
		// derived from it. cacheWhenDone has always checked the status; this
		// path did not.
		sum := s.Summary()
		if sum.Status != scanstore.StatusDone && !sum.FromCache {
			return
		}
		key := cacheKey(s.Root, s.VolumeUUID)
		if err := a.cache.Save(key, s.Root, s.Tree, s.AsAdmin); err != nil {
			// A cache we cannot refresh is worse than none: drop it so the
			// next open rescans instead of showing deleted files.
			a.cache.Delete(key)
		}
	})
}

// expectedBytesFor estimates how much a scan of root will account for, which
// is what turns the progress bar from a spinner into a measurement. Zero means
// no credible expectation and the bar stays indeterminate.
//
// Byte totals, never file counts: our counter counts names and the filesystem
// counts inodes, and hardlinks put those 14% apart on this machine's volume —
// a bar that passes 100% and then stalls is worse than no bar. Bytes are also
// the one quantity that means the same on both sides, since a cached scan
// records the tree root's allocated total, which is exactly what a walk
// accumulates.
func (a *App) expectedBytesFor(root, uuid string, asAdmin bool) int64 {
	// A previous scan of this same root is the best expectation available:
	// same instrument, same units. It works for any folder, not just whole
	// drives, because loose folders are cached under a path hash.
	if a.prefs.Get().UseCache {
		if meta, err := a.cache.Stat(cacheKey(root, uuid)); err == nil && meta != nil {
			// An administrator scan reaches trees an ordinary one cannot, so
			// its total is not a valid expectation for an ordinary scan of the
			// same root, nor the other way round.
			if meta.Vouched && meta.Bytes > 0 && meta.AsAdmin == asAdmin {
				return meta.Bytes
			}
		}
	}

	// Otherwise, if the root *is* a volume, what the system says it is using.
	// Matched exactly rather than by longest prefix: a prefix match would hand
	// a scan of one folder the whole volume's total, and the bar would crawl
	// at a few percent and never move.
	vols, err := platform.ListVolumes(false)
	if err != nil {
		return 0
	}
	for _, v := range vols {
		if v.Path == root || (v.IsRoot && (root == "/" || root == "/System/Volumes/Data")) {
			// Used plus purgeable, not Used alone. Used is Finder's figure,
			// and Finder counts purgeable space as available, so it is short
			// by whatever the system currently thinks it could evict — 32 GB
			// on this machine, but that number moves. A walk finds purgeable
			// files like any others, so the sum, which is just "the bytes that
			// are not free", is the quantity a walk is trying to reach.
			//
			// It is still an overshoot, which is the right direction to be
			// wrong in: it also covers snapshots, clone sharing and trees we
			// are refused, none of which a walk can reach. Measured here,
			// 826.7 GB against a full scan's 758.5 GB — the bar climbs into
			// the nineties and then completes.
			return v.Used + v.Purgeable
		}
	}
	return 0
}

func cacheKey(root, uuid string) string {
	if uuid != "" && (root == "/" || root == "/System/Volumes/Data" || strings.HasPrefix(root, "/Volumes/")) {
		return uuid
	}
	return scancache.PathKey(root)
}

func (a *App) volumeUUIDFor(root string) string {
	vols, err := platform.ListVolumes(false)
	if err != nil {
		return ""
	}
	best, bestLen := "", -1
	for _, v := range vols {
		mount := v.Path
		if v.IsRoot {
			mount = "/"
		}
		if root == mount || strings.HasPrefix(root, strings.TrimSuffix(mount, "/")+"/") {
			if len(mount) > bestLen {
				best, bestLen = v.UUID, len(mount)
			}
		}
	}
	return best
}

func (a *App) labelFor(root string) string {
	vols, _ := platform.ListVolumes(false)
	for _, v := range vols {
		if v.Path == root || (v.IsRoot && root == "/System/Volumes/Data") {
			return v.Name
		}
	}
	base := filepath.Base(root)
	if base == "/" || base == "." {
		return root
	}
	return base
}

// CachedScan describes a previously saved scan for a path.
//
// Found is a field rather than this being a nullable pointer: Wails generates
// a model constructor that indexes the payload, so returning nil without an
// error throws inside the binding before any of our code runs.
type CachedScan struct {
	Found     bool   `json:"found"`
	Key       string `json:"key"`
	Root      string `json:"root"`
	SavedAt   int64  `json:"savedAt"`
	Nodes     int    `json:"nodes"`
	Bytes     int64  `json:"bytes"`
	AgeMillis int64  `json:"ageMillis"`
}

// LookupCachedScan reports whether a cached scan exists for a path, so the UI
// can offer "scanned 3h ago — open instantly, or rescan".
func (a *App) LookupCachedScan(path string) CachedScan {
	if !a.prefs.Get().UseCache {
		return CachedScan{}
	}
	root := scanner.ResolveScanRoot(path)
	meta, err := a.cache.Stat(cacheKey(root, a.volumeUUIDFor(root)))
	if err != nil || meta == nil {
		return CachedScan{}
	}
	return CachedScan{
		Found: true,
		Key:   meta.Key, Root: meta.Root, SavedAt: meta.SavedAt.UnixMilli(),
		Nodes: meta.Nodes, Bytes: meta.Bytes,
		AgeMillis: time.Since(meta.SavedAt).Milliseconds(),
	}
}

// OpenCachedScan loads a saved scan into a new tab without walking the disk.
func (a *App) OpenCachedScan(path string) (string, error) {
	root := scanner.ResolveScanRoot(path)
	defer a.lockRoot(root)()

	// Already open: switch to that tab rather than loading a second copy.
	if s, ok := a.scans.FindByRoot(root, false); ok {
		return s.ID, nil
	}
	key := cacheKey(root, a.volumeUUIDFor(root))
	t, savedAt, err := a.cache.Load(key)
	if err != nil {
		return "", fmt.Errorf("could not open the saved scan: %w", err)
	}
	s := a.scans.Adopt(root, a.labelFor(root), a.volumeUUIDFor(root), t, savedAt)
	return s.ID, nil
}

// CancelScan stops a running scan; its partial results stay usable.
func (a *App) CancelScan(scanID string) { a.scans.Cancel(scanID) }

// ListScans returns every open tab.
func (a *App) ListScans() []scanstore.Summary { return a.scans.List() }

// CloseScan discards a scan and everything the tray holds from it.
func (a *App) CloseScan(scanID string) {
	a.scans.Close(scanID)
	a.emit("collector:changed", a.tray.RemoveScan(scanID))
}

// GetScan returns one tab's summary.
func (a *App) GetScan(scanID string) (*scanstore.Summary, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	sum := s.Summary()
	return &sum, nil
}

// formatCount renders a count with thousands separators, for the badge.
func formatCount(n int64) string {
	out := strconv.FormatInt(n, 10)
	for i := len(out) - 3; i > 0; i -= 3 {
		out = out[:i] + "," + out[i:]
	}
	return out
}

func errNoScan(id string) error { return fmt.Errorf("no such scan: %s", id) }

// ---------------------------------------------------------------- queries

// GetViewport returns the focus node plus depth rings of descendants, with
// arcs below minFraction folded into a single "other" wedge.
func (a *App) GetViewport(scanID string, nodeID uint32, depth int, minFraction float64) (*tree.ViewportSlice, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	p := a.prefs.Get()
	if depth <= 0 {
		depth = p.Rings
	}
	if minFraction < 0 {
		minFraction = p.MinArcFraction
	}
	v := s.Tree.Viewport(nodeID, depth, minFraction, p.MinItemSize, s.Scanning())
	return &v, nil
}

// GetChildren returns a page of a node's children for the sidebar, along with
// a count of what the size filter withheld.
func (a *App) GetChildren(scanID string, nodeID uint32, offset, limit int) (*tree.ChildPage, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	page := s.Tree.Children(nodeID, offset, limit, a.prefs.Get().MinItemSize)
	return &page, nil
}

// SetMinItemSize sets the size below which entries are hidden from the chart
// and the list, and returns the updated preferences.
func (a *App) SetMinItemSize(bytes int64) (prefs.Prefs, error) {
	p := a.prefs.Get()
	p.MinItemSize = bytes
	return a.SetPrefs(p)
}

// SetSidebarWidth remembers how wide the user dragged the list.
func (a *App) SetSidebarWidth(width int) (prefs.Prefs, error) {
	p := a.prefs.Get()
	p.SidebarWidth = width
	return a.SetPrefs(p)
}

// SetSelectionWidth remembers how wide the user dragged the selection column.
func (a *App) SetSelectionWidth(width int) (prefs.Prefs, error) {
	p := a.prefs.Get()
	p.SelectionWidth = width
	return a.SetPrefs(p)
}

// SetCollectorWidth remembers how wide the user dragged the collector.
func (a *App) SetCollectorWidth(width int) (prefs.Prefs, error) {
	p := a.prefs.Get()
	p.CollectorWidth = width
	return a.SetPrefs(p)
}

// GetChildCount is how many children a node has, for sidebar paging.
func (a *App) GetChildCount(scanID string, nodeID uint32) (int, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return 0, errNoScan(scanID)
	}
	return s.Tree.ChildCount(nodeID), nil
}

// GetAncestors returns the breadcrumb chain, root first.
func (a *App) GetAncestors(scanID string, nodeID uint32) ([]tree.NodeSummary, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	return s.Tree.Ancestors(nodeID), nil
}

// GetPath resolves a handle to its absolute path, for display and copying.
func (a *App) GetPath(scanID string, nodeID uint32) (string, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return "", errNoScan(scanID)
	}
	p := s.Tree.Path(nodeID)
	if p == "" {
		return "", errors.New("that item is no longer in the scan")
	}
	return p, nil
}

// GetLargest returns the biggest files in a scan, for the "largest items" list.
func (a *App) GetLargest(scanID string, n int) ([]tree.NodeSummary, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	if n <= 0 || n > 500 {
		n = 100
	}
	return s.Tree.TopN(n), nil
}

// FindNode resolves a path inside a scan to a handle, so a breadcrumb typed by
// hand or a drag-in of a subfolder can focus the chart.
func (a *App) FindNode(scanID, path string) (uint32, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return 0, errNoScan(scanID)
	}
	id := s.Tree.Lookup(path)
	if id == tree.Invalid {
		return 0, fmt.Errorf("%s is not in this scan", path)
	}
	return id, nil
}

// ---------------------------------------------------------------- prefs

// GetPrefs returns the current preferences.
func (a *App) GetPrefs() prefs.Prefs { return a.prefs.Get() }

// SetPrefs persists preferences and notifies the UI.
func (a *App) SetPrefs(p prefs.Prefs) (prefs.Prefs, error) {
	out, err := a.prefs.Set(p)
	if err != nil {
		return out, err
	}
	a.emit("prefs:changed", out)
	return out, nil
}

// ZoomUI moves the interface scale one stop, or back to normal when reset is
// set. The ladder lives in Go so the menu, the keyboard and the Preferences
// control cannot drift apart.
func (a *App) ZoomUI(direction int, reset bool) (prefs.Prefs, error) {
	p := a.prefs.Get()
	if reset {
		p.UIScale = 1
	} else {
		p.UIScale = prefs.NextUIScale(p.UIScale, direction)
	}
	return a.SetPrefs(p)
}

// SetUIScale sets the interface scale directly, for the Preferences control.
func (a *App) SetUIScale(scale float64) (prefs.Prefs, error) {
	p := a.prefs.Get()
	p.UIScale = scale
	return a.SetPrefs(p)
}

// AddExclusion adds a folder to the never-scan list.
func (a *App) AddExclusion(path string) (prefs.Prefs, error) {
	p := a.prefs.Get()
	clean := filepath.Clean(path)
	for _, e := range p.Exclusions {
		if e == clean {
			return p, nil
		}
	}
	p.Exclusions = append(p.Exclusions, clean)
	sort.Strings(p.Exclusions)
	return a.SetPrefs(p)
}

// RemoveExclusion drops a folder from the never-scan list.
func (a *App) RemoveExclusion(path string) (prefs.Prefs, error) {
	p := a.prefs.Get()
	out := p.Exclusions[:0]
	for _, e := range p.Exclusions {
		if e != path {
			out = append(out, e)
		}
	}
	p.Exclusions = out
	return a.SetPrefs(p)
}

// ---------------------------------------------------------------- system

// CheckFullDiskAccess reports whether the app can read every user's files.
func (a *App) CheckFullDiskAccess() platform.AccessStatus { return platform.CheckFullAccess() }

// GetCapabilities tells the frontend what this platform can do, so it can
// label itself correctly and leave out what does not exist here.
func (a *App) GetCapabilities() platform.Caps { return platform.Capabilities() }

// OpenFDASettings deep-links to whatever grants full access, where the
// platform has such a thing.
func (a *App) OpenFDASettings() error { return platform.OpenAccessSettings() }

// ChooseFolder opens the native folder picker and returns what was chosen.
func (a *App) ChooseFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Choose a folder to scan",
		CanCreateDirectories: false,
	})
}

// AppInfo is what the About panel and the updater UI show.
type AppInfo struct {
	Version  string        `json:"version"`
	Build    string        `json:"build"`
	Bundled  bool          `json:"bundled"`
	BundleID string        `json:"bundleID"`
	TeamID   string        `json:"teamID"`
	Caps     platform.Caps `json:"caps"`
}

// GetAppInfo describes the running build.
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Version:  Version,
		Build:    Build,
		Bundled:  platform.IsPackaged(),
		BundleID: platform.AppID(),
		TeamID:   platform.SigningID(),
		Caps:     platform.Capabilities(),
	}
}
