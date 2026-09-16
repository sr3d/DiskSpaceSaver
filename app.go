package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sr3d/diskspacesaver/internal/collector"
	"github.com/sr3d/diskspacesaver/internal/helperclient"
	"github.com/sr3d/diskspacesaver/internal/macos"
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

	macos.WatchVolumes(func() {
		vols, err := macos.ListVolumes(true)
		if err != nil {
			return
		}
		a.emit("volumes:changed", vols)
	})

	a.startDockIndicator()
	a.upd.Start(ctx, a.emit)
}

// shutdown stops scans and releases OS resources. Returning true would veto
// the close; we never do.
func (a *App) shutdown(ctx context.Context) {
	a.stopDockIndicator()
	macos.UnwatchVolumes()
	macos.SetDockBadge("")
	macos.SetDockProgress(-1)
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
				scanning, frac := a.scans.AnyScanning()
				switch {
				case scanning:
					macos.SetDockProgress(frac)
					macos.SetDockBadge(fmt.Sprintf("%d%%", int(frac*100)))
					wasScanning = true
				case wasScanning:
					macos.SetDockProgress(-1)
					macos.SetDockBadge("")
					wasScanning = false
				}
			}
		}
	}()
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
func (a *App) ListVolumes() ([]macos.Volume, error) {
	return macos.ListVolumes(true)
}

// ---------------------------------------------------------------- scanning

// StartScan begins scanning a path and returns the new scan's id. When a scan
// of the same root is already open it is returned instead of duplicated.
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
	if err := a.cache.Save(cacheKey(s.Root, sum.VolumeUUID), s.Root, s.Tree); err != nil {
		runtime.LogWarningf(a.ctx, "could not cache scan of %s: %v", s.Root, err)
	}
}

func cacheKey(root, uuid string) string {
	if uuid != "" && (root == "/" || root == "/System/Volumes/Data" || strings.HasPrefix(root, "/Volumes/")) {
		return uuid
	}
	return scancache.PathKey(root)
}

func (a *App) volumeUUIDFor(root string) string {
	vols, err := macos.ListVolumes(false)
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
	vols, _ := macos.ListVolumes(false)
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
type CachedScan struct {
	Key       string `json:"key"`
	Root      string `json:"root"`
	SavedAt   int64  `json:"savedAt"`
	Nodes     int    `json:"nodes"`
	Bytes     int64  `json:"bytes"`
	AgeMillis int64  `json:"ageMillis"`
}

// LookupCachedScan reports whether a cached scan exists for a path, so the UI
// can offer "scanned 3h ago — open instantly, or rescan".
func (a *App) LookupCachedScan(path string) *CachedScan {
	if !a.prefs.Get().UseCache {
		return nil
	}
	root := scanner.ResolveScanRoot(path)
	meta, err := a.cache.Stat(cacheKey(root, a.volumeUUIDFor(root)))
	if err != nil || meta == nil {
		return nil
	}
	return &CachedScan{
		Key: meta.Key, Root: meta.Root, SavedAt: meta.SavedAt.UnixMilli(),
		Nodes: meta.Nodes, Bytes: meta.Bytes,
		AgeMillis: time.Since(meta.SavedAt).Milliseconds(),
	}
}

// OpenCachedScan loads a saved scan into a new tab without walking the disk.
func (a *App) OpenCachedScan(path string) (string, error) {
	root := scanner.ResolveScanRoot(path)
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

func errNoScan(id string) error { return fmt.Errorf("no such scan: %s", id) }

// ---------------------------------------------------------------- queries

// GetViewport returns the focus node plus depth rings of descendants, with
// arcs below minFraction folded into a single "other" wedge.
func (a *App) GetViewport(scanID string, nodeID uint32, depth int, minFraction float64) (*tree.ViewportSlice, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	if depth <= 0 {
		depth = a.prefs.Get().Rings
	}
	if minFraction < 0 {
		minFraction = a.prefs.Get().MinArcFraction
	}
	v := s.Tree.Viewport(nodeID, depth, minFraction, s.Scanning())
	return &v, nil
}

// GetChildren returns a page of a node's children for the sidebar.
func (a *App) GetChildren(scanID string, nodeID uint32, offset, limit int) ([]tree.NodeSummary, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return nil, errNoScan(scanID)
	}
	return s.Tree.Children(nodeID, offset, limit), nil
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
func (a *App) CheckFullDiskAccess() macos.FDAStatus { return macos.CheckFullDiskAccess() }

// OpenFDASettings deep-links to the Full Disk Access pane.
func (a *App) OpenFDASettings() error { return macos.OpenURL(macos.FDASettingsURL) }

// ChooseFolder opens the native folder picker and returns what was chosen.
func (a *App) ChooseFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Choose a folder to scan",
		CanCreateDirectories: false,
	})
}

// AppInfo is what the About panel and the updater UI show.
type AppInfo struct {
	Version  string `json:"version"`
	Build    string `json:"build"`
	Bundled  bool   `json:"bundled"`
	BundleID string `json:"bundleID"`
	TeamID   string `json:"teamID"`
}

// GetAppInfo describes the running build.
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Version:  Version,
		Build:    Build,
		Bundled:  macos.IsBundled(),
		BundleID: macos.BundleID(),
		TeamID:   macos.TeamID(),
	}
}
