package main

import (
	"errors"

	"github.com/sr3d/diskspacesaver/internal/helperclient"
	"github.com/sr3d/diskspacesaver/internal/macos"
	"github.com/sr3d/diskspacesaver/internal/updater"
)

// HelperStatus combines what the socket says with what SMAppService says. They
// disagree in the interesting case: registered but not yet approved by the
// user, where the daemon exists and does not run.
type HelperStatus struct {
	helperclient.Status
	Registration  string `json:"registration"`
	NeedsApproval bool   `json:"needsApproval"`
	Bundled       bool   `json:"bundled"`
	Advice        string `json:"advice,omitempty"`
}

// GetHelperStatus reports whether administrator scans are available.
func (a *App) GetHelperStatus() HelperStatus {
	st := HelperStatus{
		Status:       a.help.Status(),
		Registration: macos.HelperRegistration(),
		Bundled:      macos.IsBundled(),
	}
	st.NeedsApproval = st.Registration == "requires-approval"
	switch {
	case !st.Bundled:
		st.Advice = "Administrator scans need the packaged app; they are unavailable in a development build."
	case st.NeedsApproval:
		st.Advice = "Approve DiskSpaceSaver in System Settings under Login Items to finish installing the helper."
	case st.Registration == "not-registered" || st.Registration == "not-found":
		st.Advice = "Install the helper to scan folders that belong to other users."
	case !st.Compatible && st.Running:
		st.Advice = "The installed helper is from a different version. Reinstall it."
	}
	return st
}

// InstallHelper registers the privileged helper with launchd. The user is
// asked to approve it in System Settings the first time.
func (a *App) InstallHelper() (HelperStatus, error) {
	if !macos.IsBundled() {
		return a.GetHelperStatus(), errors.New("the helper can only be installed from the packaged app")
	}
	if err := macos.RegisterHelper(); err != nil {
		return a.GetHelperStatus(), err
	}
	a.help.Close() // drop the cached unavailable status
	st := a.GetHelperStatus()
	a.emit("helper:changed", st)
	return st, nil
}

// UninstallHelper removes the privileged helper.
func (a *App) UninstallHelper() (HelperStatus, error) {
	if err := macos.UnregisterHelper(); err != nil {
		return a.GetHelperStatus(), err
	}
	a.help.Close()
	st := a.GetHelperStatus()
	a.emit("helper:changed", st)
	return st, nil
}

// OpenLoginItemsSettings shows the pane where a pending helper is approved.
func (a *App) OpenLoginItemsSettings() { macos.OpenLoginItemsSettings() }

// ---------------------------------------------------------------- updater

// GetUpdateState returns what the update UI should show.
func (a *App) GetUpdateState() updater.State { return a.upd.State() }

// CheckForUpdates runs a user-initiated check.
func (a *App) CheckForUpdates() updater.State { return a.upd.CheckNow() }

// InstallUpdate downloads, verifies, installs and relaunches.
func (a *App) InstallUpdate() error { return a.upd.InstallAndRelaunch() }

// ---------------------------------------------------------------- cache

// CacheEntry describes one saved scan in Preferences.
type CacheEntry struct {
	Key     string `json:"key"`
	Root    string `json:"root"`
	SavedAt int64  `json:"savedAt"`
	Nodes   int    `json:"nodes"`
	Bytes   int64  `json:"bytes"`
}

// ListCachedScans returns every saved scan.
func (a *App) ListCachedScans() []CacheEntry {
	metas := a.cache.List()
	out := make([]CacheEntry, 0, len(metas))
	for _, m := range metas {
		out = append(out, CacheEntry{
			Key: m.Key, Root: m.Root, SavedAt: m.SavedAt.UnixMilli(),
			Nodes: m.Nodes, Bytes: m.Bytes,
		})
	}
	return out
}

// ClearScanCache deletes every saved scan.
func (a *App) ClearScanCache() { a.cache.Clear() }

// ScanCacheBytes is how much disk the cache occupies.
func (a *App) ScanCacheBytes() int64 { return a.cache.TotalBytes() }
