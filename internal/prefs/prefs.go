// Package prefs stores user preferences as JSON in Application Support.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	"github.com/sr3d/diskspacesaver/internal/platform"
)

// CurrentVersion is the preferences schema version. It exists so a changed
// default can reach people who already have a preferences file: without it,
// every existing install would keep the old value forever and only new users
// would see the new behaviour.
const CurrentVersion = 2

// DefaultMinItemSize hides entries below 10 MB. Most volumes hold tens of
// thousands of small files that no one is going to reclaim space by deleting,
// and showing them buries the handful that matter.
//
// Base 10, because that is what the app shows by default: a threshold of
// 10*1024*1024 would be labelled "10.5 MB", which reads like a typo.
const DefaultMinItemSize = 10 * 1000 * 1000

// Prefs is the full, versioned preference set. Every field is safe to send to
// the frontend.
type Prefs struct {
	// Version is the schema version this file was written with.
	Version int `json:"version"`
	// Base2 switches sizes to KiB/MiB/GiB. Off by default so numbers match
	// Finder, which counts in base 10.
	Base2 bool `json:"base2"`
	// Theme is "system", "light" or "dark".
	Theme string `json:"theme"`
	// ColorScheme names the sunburst palette.
	ColorScheme string `json:"colorScheme"`
	// ShowHidden reveals dot-files in the chart and sidebar. They are always
	// scanned, because they occupy real space.
	ShowHidden bool `json:"showHidden"`
	// Exclusions are absolute paths never descended into.
	Exclusions []string `json:"exclusions"`
	// UpdateChannel is "stable" or "beta".
	UpdateChannel string `json:"updateChannel"`
	// AutoCheckUpdates checks on launch and daily.
	AutoCheckUpdates bool `json:"autoCheckUpdates"`
	// SendStats opts into anonymous update telemetry.
	SendStats bool `json:"sendStats"`
	// InstallID is a random UUID; it identifies an installation, not a person.
	InstallID string `json:"installID"`
	// AllowPermanentDelete unlocks Delete Permanently, which bypasses the
	// Trash and cannot be undone. Off until the user asks for it.
	AllowPermanentDelete bool `json:"allowPermanentDelete"`
	// ConfirmTrash asks before moving items to the Trash.
	ConfirmTrash bool `json:"confirmTrash"`
	// UseCache reopens the last scan for a volume instantly.
	UseCache bool `json:"useCache"`
	// UseBulkScan enables the getattrlistbulk fast path.
	UseBulkScan bool `json:"useBulkScan"`
	// MinArcFraction is the angular floor below which arcs fold into "other".
	MinArcFraction float64 `json:"minArcFraction"`
	// Rings is how many levels the sunburst draws from the focus.
	Rings int `json:"rings"`
	// UIScale magnifies the whole interface, including text. 1 is normal.
	UIScale float64 `json:"uiScale"`
	// MinItemSize hides entries smaller than this many bytes from the chart
	// and the list. Zero shows everything. Hidden entries are still scanned
	// and still counted in every total — this only controls what is drawn.
	MinItemSize int64 `json:"minItemSize"`
	// SidebarWidth is the width of the list beside the chart, in points.
	SidebarWidth int `json:"sidebarWidth"`
}

// Sidebar width limits, in points.
const (
	MinSidebarWidth = 220
	MaxSidebarWidth = 900
)

// UI scale limits. Below the minimum the traffic lights stop fitting the title
// bar; above the maximum the sidebar columns no longer fit a small window.
const (
	MinUIScale = 0.75
	MaxUIScale = 2.0
)

// UIScaleSteps are the stops Zoom In and Zoom Out move between, chosen so each
// press is a visible change without being a jump.
var UIScaleSteps = []float64{0.75, 0.85, 1.0, 1.15, 1.3, 1.5, 1.75, 2.0}

// NextUIScale returns the neighbouring stop in the given direction. Anything
// off-ladder (a hand-edited preferences file) snaps to the nearest stop first,
// so the ladder is always escapable.
func NextUIScale(current float64, direction int) float64 {
	nearest, best := 0, 1e9
	for i, s := range UIScaleSteps {
		if d := abs(s - current); d < best {
			nearest, best = i, d
		}
	}
	// Already exactly on a stop: move off it. Otherwise snapping is the move.
	if best < 1e-6 {
		nearest += direction
	} else if direction > 0 && UIScaleSteps[nearest] < current {
		nearest++
	} else if direction < 0 && UIScaleSteps[nearest] > current {
		nearest--
	}
	if nearest < 0 {
		nearest = 0
	}
	if nearest >= len(UIScaleSteps) {
		nearest = len(UIScaleSteps) - 1
	}
	return UIScaleSteps[nearest]
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// Defaults returns the shipped configuration.
func Defaults() Prefs {
	return Prefs{
		Version:              CurrentVersion,
		Base2:                false,
		Theme:                "system",
		ColorScheme:          "spectrum",
		ShowHidden:           true,
		Exclusions:           []string{},
		UpdateChannel:        "stable",
		AutoCheckUpdates:     true,
		SendStats:            true,
		InstallID:            uuid.NewString(),
		AllowPermanentDelete: false,
		ConfirmTrash:         true,
		UseCache:             true,
		UseBulkScan:          true,
		MinArcFraction:       0.0008,
		Rings:                5,
		UIScale:              1,
		MinItemSize:          DefaultMinItemSize,
		SidebarWidth:         350,
	}
}

// Store is a concurrency-safe preference file.
type Store struct {
	mu   sync.RWMutex
	p    Prefs
	path string
}

// SupportDir is where preferences and the scan cache live, created on demand.
// The location is the platform's business — Application Support on macOS,
// %APPDATA% on Windows — so this just forwards.
func SupportDir() (string, error) { return platform.SupportDir() }

// Load reads preferences, filling in defaults for anything missing. A corrupt
// file is replaced rather than surfaced: losing preferences is a nuisance,
// refusing to launch is not acceptable.
func Load() *Store {
	s := &Store{p: Defaults()}
	dir, err := SupportDir()
	if err != nil {
		return s
	}
	s.path = filepath.Join(dir, "prefs.json")
	b, err := os.ReadFile(s.path)
	if err != nil {
		_ = s.save()
		return s
	}
	// The file's own version has to be read before defaults are applied.
	// Unmarshalling into a struct that already holds the defaults leaves a
	// missing field at its default value, so an old file without a version
	// would read as current and never be migrated.
	var stamp struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(b, &stamp)

	p := Defaults()
	if err := json.Unmarshal(b, &p); err != nil {
		_ = s.save()
		return s
	}
	p.Version = stamp.Version
	s.p = p.migrated().normalized()
	if s.p.Version != CurrentVersion {
		s.p.Version = CurrentVersion
		_ = s.save()
	}
	if s.p.InstallID == "" {
		s.p.InstallID = uuid.NewString()
		_ = s.save()
	}
	return s
}

// migrated brings a preferences file forward to the current schema.
func (p Prefs) migrated() Prefs {
	// v2 introduced the size filter. A file written before it has no opinion
	// about the setting, so it takes the new default rather than being pinned
	// to "show everything" for the life of the install. Someone who has
	// actually chosen a value keeps it.
	if p.Version < 2 && p.MinItemSize == 0 {
		p.MinItemSize = DefaultMinItemSize
	}
	p.Version = CurrentVersion
	return p
}

func (p Prefs) normalized() Prefs {
	switch p.Theme {
	case "light", "dark", "system":
	default:
		p.Theme = "system"
	}
	if p.UpdateChannel != "beta" {
		p.UpdateChannel = "stable"
	}
	if p.Rings < 3 {
		p.Rings = 3
	}
	if p.Rings > 9 {
		p.Rings = 9
	}
	if p.MinArcFraction <= 0 || p.MinArcFraction > 0.05 {
		p.MinArcFraction = 0.0008
	}
	if p.Exclusions == nil {
		p.Exclusions = []string{}
	}
	// A zero here means a preferences file written before this setting
	// existed, not a request for an invisible interface.
	if p.UIScale == 0 {
		p.UIScale = 1
	}
	if p.UIScale < MinUIScale {
		p.UIScale = MinUIScale
	}
	if p.UIScale > MaxUIScale {
		p.UIScale = MaxUIScale
	}
	if p.MinItemSize < 0 {
		p.MinItemSize = 0
	}
	if p.SidebarWidth == 0 {
		p.SidebarWidth = 350
	}
	if p.SidebarWidth < MinSidebarWidth {
		p.SidebarWidth = MinSidebarWidth
	}
	if p.SidebarWidth > MaxSidebarWidth {
		p.SidebarWidth = MaxSidebarWidth
	}
	return p
}

// Get returns a copy of the current preferences.
func (s *Store) Get() Prefs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p
}

// Set replaces the preferences and persists them.
func (s *Store) Set(p Prefs) (Prefs, error) {
	s.mu.Lock()
	id := s.p.InstallID
	p = p.normalized()
	if p.InstallID == "" {
		p.InstallID = id
	}
	s.p = p
	err := s.save()
	out := s.p
	s.mu.Unlock()
	return out, err
}

// save writes atomically: a crash mid-write must not leave a truncated file.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
