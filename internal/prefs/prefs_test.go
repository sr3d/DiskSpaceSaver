package prefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextUIScaleWalksTheLadder(t *testing.T) {
	if got := NextUIScale(1.0, +1); got != 1.15 {
		t.Errorf("zoom in from 1.0 gave %v, want 1.15", got)
	}
	if got := NextUIScale(1.0, -1); got != 0.85 {
		t.Errorf("zoom out from 1.0 gave %v, want 0.85", got)
	}
	// The ends clamp rather than wrapping or running off.
	if got := NextUIScale(2.0, +1); got != 2.0 {
		t.Errorf("zoom in at the maximum gave %v, want 2.0", got)
	}
	if got := NextUIScale(0.75, -1); got != 0.75 {
		t.Errorf("zoom out at the minimum gave %v, want 0.75", got)
	}
}

// A value between stops — a hand-edited preferences file — must still move in
// the direction asked for rather than sticking.
func TestNextUIScaleSnapsOffLadderValues(t *testing.T) {
	if got := NextUIScale(1.07, +1); got <= 1.07 {
		t.Errorf("zoom in from 1.07 gave %v, which is not larger", got)
	}
	if got := NextUIScale(1.07, -1); got >= 1.07 {
		t.Errorf("zoom out from 1.07 gave %v, which is not smaller", got)
	}
	if got := NextUIScale(9.0, -1); got > MaxUIScale {
		t.Errorf("an absurd value did not come back into range: %v", got)
	}
}

func TestNormalizeFillsAndClampsScale(t *testing.T) {
	// A preferences file from before this setting existed has no value at all.
	if got := (Prefs{}).normalized().UIScale; got != 1 {
		t.Errorf("a missing scale became %v, want 1", got)
	}
	if got := (Prefs{UIScale: 0.1}).normalized().UIScale; got != MinUIScale {
		t.Errorf("a tiny scale became %v, want %v", got, MinUIScale)
	}
	if got := (Prefs{UIScale: 99}).normalized().UIScale; got != MaxUIScale {
		t.Errorf("a huge scale became %v, want %v", got, MaxUIScale)
	}
}

// A changed default has to reach people who already have a preferences file,
// or only new installs ever see it.
func TestMigrationAppliesTheSizeFilterDefault(t *testing.T) {
	// A file written before the filter existed: no version, no opinion.
	old := Prefs{Base2: true, Rings: 6}
	got := old.migrated().normalized()
	if got.MinItemSize != DefaultMinItemSize {
		t.Errorf("minItemSize after migration = %d, want %d", got.MinItemSize, DefaultMinItemSize)
	}
	if got.Version != CurrentVersion {
		t.Errorf("version after migration = %d, want %d", got.Version, CurrentVersion)
	}
	if !got.Base2 || got.Rings != 6 {
		t.Error("migration discarded settings it should have kept")
	}

	// Someone who has chosen a value keeps it, including a deliberate zero
	// once they are already on the current schema.
	chosen := Prefs{Version: CurrentVersion, MinItemSize: 0}
	if got := chosen.migrated().normalized(); got.MinItemSize != 0 {
		t.Errorf("a deliberate 'show everything' was overwritten with %d", got.MinItemSize)
	}
	custom := Prefs{MinItemSize: 1234}
	if got := custom.migrated().normalized(); got.MinItemSize != 1234 {
		t.Errorf("an existing choice was overwritten with %d", got.MinItemSize)
	}
}

// Exercises migration through Load, which is where it actually has to work:
// the struct is filled with defaults before the file is decoded, so a version
// read from the decoded struct would always look current.
func TestLoadMigratesAFileWithoutAVersion(t *testing.T) {
	// Point the support directory at a temporary home. Where that lands is
	// the platform's business, so the test asks for it rather than assuming.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	dir, err := SupportDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Skipf("support directory %s is outside the temporary home; cannot isolate", dir)
	}
	// A file as written before the size filter existed.
	old := `{"base2":true,"theme":"dark","rings":7,"minItemSize":0}`
	if err := os.WriteFile(filepath.Join(dir, "prefs.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Load().Get()
	if got.MinItemSize != DefaultMinItemSize {
		t.Errorf("minItemSize after Load = %d, want %d", got.MinItemSize, DefaultMinItemSize)
	}
	if got.Version != CurrentVersion {
		t.Errorf("version after Load = %d, want %d", got.Version, CurrentVersion)
	}
	if !got.Base2 || got.Theme != "dark" || got.Rings != 7 {
		t.Errorf("Load discarded existing settings: %+v", got)
	}

	// Loading again must not undo a later deliberate choice.
	if _, err := Load().Set(func() Prefs { p := got; p.MinItemSize = 0; return p }()); err != nil {
		t.Fatal(err)
	}
	if again := Load().Get(); again.MinItemSize != 0 {
		t.Errorf("a deliberate 'show everything' was migrated away again: %d", again.MinItemSize)
	}
}
