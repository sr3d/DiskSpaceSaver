// Package prefs stores user preferences as JSON in Application Support.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// AppDirName is the folder under Application Support that holds prefs and the
// scan cache.
const AppDirName = "DiskSpaceSaver"

// Prefs is the full, versioned preference set. Every field is safe to send to
// the frontend.
type Prefs struct {
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
}

// Defaults returns the shipped configuration.
func Defaults() Prefs {
	return Prefs{
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
	}
}

// Store is a concurrency-safe preference file.
type Store struct {
	mu   sync.RWMutex
	p    Prefs
	path string
}

// SupportDir is ~/Library/Application Support/DiskSpaceSaver, created on demand.
func SupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", AppDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

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
	p := Defaults()
	if err := json.Unmarshal(b, &p); err != nil {
		_ = s.save()
		return s
	}
	s.p = p.normalized()
	if s.p.InstallID == "" {
		s.p.InstallID = uuid.NewString()
		_ = s.save()
	}
	return s
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
