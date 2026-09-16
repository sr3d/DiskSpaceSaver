// Package updater drives the in-app updater and reports its lifecycle events.
//
// The actual download, signature check, atomic replacement and relaunch are
// Sparkle's job (see sparkle_darwin.go). This package owns scheduling, the
// state the UI renders, and the anonymous event reporting.
package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/sr3d/diskspacesaver/internal/prefs"
)

// FeedURL is the Sparkle appcast, served by the Cloudflare Worker.
const FeedURL = "https://updates.diskspacesaver.app/appcast.xml"

// EventsURL receives update lifecycle events.
const EventsURL = "https://updates.diskspacesaver.app/events"

// Event names, mirroring Sparkle's SPUUpdaterDelegate callbacks.
const (
	EventLaunch      = "launch"
	EventFoundUpdate = "didFindValidUpdate"
	EventNoUpdate    = "didNotFindUpdate"
	EventUserSkipped = "userDidSkip"
	EventWillInstall = "willInstallUpdate"
	EventCycleFinish = "didFinishUpdateCycle"
)

// State is what the update UI renders.
type State struct {
	Supported       bool   `json:"supported"`
	Checking        bool   `json:"checking"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	ReleaseNotes    string `json:"releaseNotes,omitempty"`
	DownloadURL     string `json:"downloadURL,omitempty"`
	LastChecked     int64  `json:"lastChecked,omitempty"`
	Err             string `json:"err,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// driver is the platform update mechanism. Sparkle implements it in a real
// bundle; everything else gets a driver that explains why updating is off.
type driver interface {
	Available() (bool, string)
	Check(userInitiated bool) (State, error)
	InstallAndRelaunch() error
}

// Updater schedules checks and publishes state.
type Updater struct {
	prefs *prefs.Store
	drv   driver

	mu    sync.RWMutex
	state State

	emit   func(name string, data any)
	stop   chan struct{}
	once   sync.Once
	client *http.Client
}

// New builds an updater bound to the preference store.
func New(p *prefs.Store) *Updater {
	u := &Updater{
		prefs:  p,
		drv:    newDriver(),
		stop:   make(chan struct{}),
		client: &http.Client{Timeout: 3 * time.Second},
	}
	ok, why := u.drv.Available()
	u.state = State{Supported: ok, CurrentVersion: CurrentVersion(), Reason: why}
	return u
}

// Start begins the launch check and the daily timer.
func (u *Updater) Start(ctx context.Context, emit func(string, any)) {
	u.mu.Lock()
	u.emit = emit
	u.mu.Unlock()

	u.Report(EventLaunch, "", "")
	if !u.state.Supported || !u.prefs.Get().AutoCheckUpdates {
		return
	}
	go func() {
		// A short delay keeps launch responsive; the user has not asked for
		// an update dialog in the first seconds of using the app.
		select {
		case <-time.After(20 * time.Second):
		case <-ctx.Done():
			return
		case <-u.stop:
			return
		}
		u.check(false)

		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if u.prefs.Get().AutoCheckUpdates {
					u.check(false)
				}
			case <-ctx.Done():
				return
			case <-u.stop:
				return
			}
		}
	}()
}

// Stop halts scheduled checks.
func (u *Updater) Stop() { u.once.Do(func() { close(u.stop) }) }

// State returns the current update state.
func (u *Updater) State() State {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.state
}

// CheckNow runs a user-initiated check.
func (u *Updater) CheckNow() State { return u.check(true) }

func (u *Updater) check(userInitiated bool) State {
	u.setState(func(s *State) { s.Checking = true; s.Err = "" })

	st, err := u.drv.Check(userInitiated)
	st.CurrentVersion = CurrentVersion()
	st.Checking = false
	st.LastChecked = time.Now().UnixMilli()
	if err != nil {
		st.Err = err.Error()
	}
	ok, why := u.drv.Available()
	st.Supported, st.Reason = ok, why

	u.mu.Lock()
	u.state = st
	emit := u.emit
	u.mu.Unlock()
	if emit != nil {
		emit("updater:changed", st)
	}

	switch {
	case err != nil:
		u.Report(EventCycleFinish, CurrentVersion(), "")
	case st.UpdateAvailable:
		u.Report(EventFoundUpdate, CurrentVersion(), st.LatestVersion)
	default:
		u.Report(EventNoUpdate, CurrentVersion(), "")
	}
	return st
}

func (u *Updater) setState(fn func(*State)) {
	u.mu.Lock()
	fn(&u.state)
	st := u.state
	emit := u.emit
	u.mu.Unlock()
	if emit != nil {
		emit("updater:changed", st)
	}
}

// InstallAndRelaunch downloads, verifies, installs and restarts.
func (u *Updater) InstallAndRelaunch() error {
	u.Report(EventWillInstall, CurrentVersion(), u.State().LatestVersion)
	return u.drv.InstallAndRelaunch()
}

// event is the anonymous payload. It deliberately carries no path, hostname,
// user name or address — only what is needed to see whether updates land.
type event struct {
	InstallID   string `json:"installID"`
	Event       string `json:"event"`
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion,omitempty"`
	OSVersion   string `json:"osVersion"`
	Arch        string `json:"arch"`
	Channel     string `json:"channel"`
	TS          int64  `json:"ts"`
	Err         string `json:"err,omitempty"`
}

// Report sends one lifecycle event, fire and forget. It never blocks the UI
// and never retries: a lost statistic is not worth a stalled interface.
func (u *Updater) Report(name, from, to string) {
	p := u.prefs.Get()
	if !p.SendStats {
		return
	}
	ev := event{
		InstallID: p.InstallID, Event: name, FromVersion: from, ToVersion: to,
		OSVersion: osVersion(), Arch: runtime.GOARCH,
		Channel: p.UpdateChannel, TS: time.Now().UnixMilli(),
	}
	go func() {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		req, err := http.NewRequest(http.MethodPost, EventsURL, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := u.client.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}

// FeedURLForChannel appends the channel selector the Worker understands.
func FeedURLForChannel(channel string) string {
	if channel == "beta" {
		return FeedURL + "?channel=beta"
	}
	return FeedURL
}
