package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sr3d/diskspacesaver/internal/collector"
	"github.com/sr3d/diskspacesaver/internal/platform"
	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/scanstore"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// ItemRef names an item by the handle the UI already holds. Paths never come
// from the frontend, so the frontend cannot ask us to touch a file outside a
// scan even if it is compromised.
type ItemRef struct {
	ScanID string `json:"scanID"`
	NodeID uint32 `json:"nodeID"`
}

// Result reports the outcome of a batch operation.
type Result struct {
	Succeeded []string          `json:"succeeded"`
	Failed    map[string]string `json:"failed"`
	Bytes     int64             `json:"bytes"`
}

func newResult() Result {
	return Result{Succeeded: []string{}, Failed: map[string]string{}}
}

// resolve turns an ItemRef into a session and a verified absolute path.
//
// The path is re-derived from the tree and then re-checked against the scan
// root. Both steps matter: the handle proves the item was scanned, and the
// containment check means a stale or forged handle still cannot escape.
func (a *App) resolve(ref ItemRef) (*scanstore.Session, string, error) {
	s, ok := a.scans.Get(ref.ScanID)
	if !ok {
		return nil, "", errNoScan(ref.ScanID)
	}
	if ref.NodeID == tree.Invalid {
		return nil, "", errors.New("that arc is a group, not a single item")
	}
	n, ok := s.Tree.Snapshot(ref.NodeID)
	if !ok || n.Flags&tree.FlagDeleted != 0 {
		return nil, "", errors.New("that item is no longer in the scan")
	}
	path := s.Tree.Path(ref.NodeID)
	if path == "" {
		return nil, "", errors.New("could not determine that item's location")
	}
	if !withinRoot(s.Root, path) {
		return nil, "", fmt.Errorf("refusing to touch %s: outside the scanned folder", path)
	}
	return s, path, nil
}

// withinRoot is the containment guard for every destructive operation.
func withinRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return false // never operate on the scan root itself
	}
	if root == "/" {
		return strings.HasPrefix(path, "/") && len(path) > 1
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func (a *App) paths(refs []ItemRef) ([]string, Result) {
	res := newResult()
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		_, p, err := a.resolve(r)
		if err != nil {
			res.Failed[fmt.Sprintf("%s#%d", r.ScanID, r.NodeID)] = err.Error()
			continue
		}
		out = append(out, p)
	}
	return out, res
}

// ---------------------------------------------------------------- reveal etc

// RevealInFinder selects the items in Finder.
func (a *App) RevealInFinder(refs []ItemRef) error {
	ps, res := a.paths(refs)
	if len(ps) == 0 {
		return firstFailure(res, "nothing to reveal")
	}
	return platform.Reveal(ps)
}

// QuickLook toggles the Quick Look panel over the items.
func (a *App) QuickLook(refs []ItemRef, index int) error {
	ps, res := a.paths(refs)
	if len(ps) == 0 {
		return firstFailure(res, "nothing to preview")
	}
	platform.PreviewItems(ps, index)
	return nil
}

// CloseQuickLook hides the preview panel.
func (a *App) CloseQuickLook() { platform.ClosePreview() }

// GetInfo opens Finder's Get Info window.
func (a *App) GetInfo(ref ItemRef) error {
	_, p, err := a.resolve(ref)
	if err != nil {
		return err
	}
	return platform.ShowProperties(p)
}

// OpenItem opens an item with its default application.
func (a *App) OpenItem(ref ItemRef) error {
	_, p, err := a.resolve(ref)
	if err != nil {
		return err
	}
	return platform.Open(p)
}

// CopyPath puts an item's path on the clipboard.
func (a *App) CopyPath(ref ItemRef) (string, error) {
	_, p, err := a.resolve(ref)
	if err != nil {
		return "", err
	}
	if err := runtime.ClipboardSetText(a.ctx, p); err != nil {
		return "", err
	}
	return p, nil
}

func firstFailure(res Result, fallback string) error {
	for _, v := range res.Failed {
		return errors.New(v)
	}
	return errors.New(fallback)
}

// ---------------------------------------------------------------- deleting

// MoveToTrash moves items to the Trash, where Finder's undo can put them back,
// and prunes them from the tree so the chart updates immediately.
func (a *App) MoveToTrash(refs []ItemRef) (Result, error) {
	return a.deleteItems(refs, true)
}

// DeletePermanently erases items outright. It is refused unless the user has
// turned it on in Preferences, because there is no way back from it.
func (a *App) DeletePermanently(refs []ItemRef) (Result, error) {
	if !a.prefs.Get().AllowPermanentDelete {
		return newResult(), errors.New("permanent delete is switched off — enable it in Preferences if you are sure")
	}
	return a.deleteItems(refs, false)
}

func (a *App) deleteItems(refs []ItemRef, toTrash bool) (Result, error) {
	res := newResult()
	touched := map[string]bool{}

	for _, ref := range refs {
		s, path, err := a.resolve(ref)
		if err != nil {
			res.Failed[fmt.Sprintf("%s#%d", ref.ScanID, ref.NodeID)] = err.Error()
			continue
		}
		n, _ := s.Tree.Snapshot(ref.NodeID)

		var opErr error
		if toTrash {
			_, opErr = platform.Trash(path)
		} else {
			opErr = os.RemoveAll(path)
		}

		// A root-owned item can only be removed by the helper. Escalating is
		// only offered for a scan the user explicitly ran as administrator.
		if opErr != nil && s.AsAdmin && a.help.Available() && isPermissionError(opErr) {
			opErr = a.help.Delete(s.Root, []string{path}, toTrash)
		}

		if opErr != nil {
			res.Failed[path] = humanDeleteError(opErr, toTrash)
			continue
		}
		res.Succeeded = append(res.Succeeded, path)
		res.Bytes += n.Alloc
		s.Tree.Prune(ref.NodeID)
		a.tray.Remove(ref.ScanID, ref.NodeID)
		touched[ref.ScanID] = true
	}

	for id := range touched {
		if s, ok := a.scans.Get(id); ok {
			a.emit(scanstore.EventTree, map[string]any{"scanID": id, "summary": s.Summary()})
			a.invalidateCache(s)
		}
	}
	if len(res.Succeeded) > 0 {
		a.emit("collector:changed", a.tray.List())
	}
	if len(res.Failed) > 0 && len(res.Succeeded) == 0 {
		return res, firstFailure(res, "nothing could be deleted")
	}
	return res, nil
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission")
}

func humanDeleteError(err error, toTrash bool) string {
	msg := err.Error()
	if isPermissionError(err) {
		if toTrash {
			return msg + " — try Scan as Administrator, or grant Full Disk Access"
		}
		return msg + " — this item is owned by another user or protected by the system"
	}
	return msg
}

// ---------------------------------------------------------------- rescan

// Rescan re-walks one subtree and swaps the result in, keeping the node's
// handle so the UI's focus and selection survive.
//
// It returns the scan id to show afterwards: the same one for a subtree
// rescan, a new one when the whole scan was restarted.
func (a *App) Rescan(scanID string, nodeID uint32) (string, error) {
	s, ok := a.scans.Get(scanID)
	if !ok {
		return "", errNoScan(scanID)
	}
	if nodeID == tree.Invalid || nodeID == s.Tree.Root {
		return a.rescanAll(s)
	}
	path := s.Tree.Path(nodeID)
	if path == "" {
		return "", errors.New("that item is no longer in the scan")
	}
	if _, err := os.Stat(path); err != nil {
		// Gone since the last scan: prune rather than fail.
		s.Tree.Prune(nodeID)
		a.emit(scanstore.EventTree, map[string]any{"scanID": scanID, "summary": s.Summary()})
		a.invalidateCache(s)
		return scanID, nil
	}

	p := a.prefs.Get()
	sub := scanner.New(scanner.Options{Root: path, Exclusions: p.Exclusions, UseBulk: p.UseBulkScan})
	subTree := tree.New(path)
	go func() {
		for range sub.Progress() {
		}
	}()
	go sub.Run(a.ctx)
	for f := range sub.Frames() {
		subTree.Apply(f)
	}
	if err := sub.Err(); err != nil {
		return "", err
	}
	subTree.Finalize()
	s.Tree.ReplaceSubtree(nodeID, subTree)

	a.emit(scanstore.EventTree, map[string]any{"scanID": scanID, "summary": s.Summary()})
	a.invalidateCache(s)
	return scanID, nil
}

// rescanAll restarts a whole scan, returning the id of the replacement tab.
func (a *App) rescanAll(s *scanstore.Session) (string, error) {
	root, admin := s.Root, s.AsAdmin
	a.CloseScan(s.ID)
	return a.StartScan(root, admin)
}

// ---------------------------------------------------------------- collector

// CollectorAdd drops items into the tray.
func (a *App) CollectorAdd(refs []ItemRef) (collector.State, error) {
	var items []collector.Item
	for _, ref := range refs {
		s, path, err := a.resolve(ref)
		if err != nil {
			continue
		}
		n, _ := s.Tree.Snapshot(ref.NodeID)
		items = append(items, collector.Item{
			ScanID: ref.ScanID, NodeID: ref.NodeID, Path: path, Name: n.Name,
			Size: n.Alloc, Kind: n.Kind, ScanRoot: s.Root, AsAdmin: s.AsAdmin,
		})
	}
	if len(items) == 0 {
		return a.tray.List(), errors.New("those items could not be collected")
	}
	st := a.tray.Add(items...)
	a.emit("collector:changed", st)
	return st, nil
}

// CollectorRemove takes one item back out of the tray.
func (a *App) CollectorRemove(scanID string, nodeID uint32) collector.State {
	st := a.tray.Remove(scanID, nodeID)
	a.emit("collector:changed", st)
	return st
}

// CollectorList returns the tray's contents and total.
func (a *App) CollectorList() collector.State { return a.tray.List() }

// CollectorClear empties the tray without deleting anything.
func (a *App) CollectorClear() collector.State {
	st := a.tray.Clear()
	a.emit("collector:changed", st)
	return st
}

// CollectorHas reports whether an item is already in the tray.
func (a *App) CollectorHas(scanID string, nodeID uint32) bool {
	return a.tray.Has(scanID, nodeID)
}

// CollectorDeleteAll empties the tray by deleting everything in it.
func (a *App) CollectorDeleteAll(permanent bool) (Result, error) {
	st := a.tray.List()
	refs := make([]ItemRef, 0, len(st.Items))
	for _, it := range st.Items {
		refs = append(refs, ItemRef{ScanID: it.ScanID, NodeID: it.NodeID})
	}
	if len(refs) == 0 {
		return newResult(), errors.New("the collector is empty")
	}
	if permanent {
		return a.DeletePermanently(refs)
	}
	return a.MoveToTrash(refs)
}

// ---------------------------------------------------------------- preview

// Preview is a rendered look at one item, for the panel beside the list.
type Preview struct {
	NodeID uint32 `json:"nodeID"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Kind   uint8  `json:"kind"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"`
	// Image is a PNG data URI, empty when the system cannot render this file.
	Image string `json:"image"`
	// UTType is the file's type, shown when there is no image to show.
	UTType string `json:"utType"`
	Ext    string `json:"ext"`
}

// previewCache keeps recently rendered thumbnails so moving up and down the
// list with the arrow keys does not re-render the same file repeatedly.
const previewCacheMax = 48

// PreviewItem renders a thumbnail for an item. Anything Finder can preview —
// images, PDFs, video, documents — comes back with an image; everything else
// comes back with the metadata only, which is still worth showing.
func (a *App) PreviewItem(ref ItemRef, maxPoints int) (*Preview, error) {
	s, path, err := a.resolve(ref)
	if err != nil {
		return nil, err
	}
	n, _ := s.Tree.Snapshot(ref.NodeID)

	p := &Preview{
		NodeID: ref.NodeID, Name: n.Name, Path: path, Kind: n.Kind,
		Size: n.Alloc, Mtime: n.Mtime,
		Ext: strings.TrimPrefix(strings.ToLower(filepath.Ext(n.Name)), "."),
	}

	key := fmt.Sprintf("%s#%d@%d", ref.ScanID, ref.NodeID, maxPoints)
	if img, ok := a.previewFromCache(key); ok {
		p.Image = img
		return p, nil
	}
	// Directories have no meaningful thumbnail beyond a folder icon, and
	// rendering one for every folder the user clicks costs a QuickLook round
	// trip to say nothing.
	if scanner.Kind(n.Kind) == scanner.KindDir {
		return p, nil
	}
	p.Image = platform.Thumbnail(path, maxPoints, 2)
	a.previewToCache(key, p.Image)
	return p, nil
}

func (a *App) previewFromCache(key string) (string, bool) {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	v, ok := a.previews[key]
	return v, ok
}

func (a *App) previewToCache(key, image string) {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if a.previews == nil {
		a.previews = map[string]string{}
	}
	if len(a.previews) >= previewCacheMax {
		// A plain flush rather than a true LRU: the cache exists to make
		// arrow-key browsing smooth, and at this size tracking recency costs
		// more than an occasional re-render.
		a.previews = map[string]string{}
	}
	a.previews[key] = image
}
