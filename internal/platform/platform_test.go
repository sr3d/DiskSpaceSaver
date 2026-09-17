package platform

import (
	"runtime"
	"strings"
	"testing"
)

// Runs on whichever platform the suite is executed on, so the Windows CI job
// checks the Windows implementation and this machine checks the macOS one.
func TestCapabilitiesAreComplete(t *testing.T) {
	c := Capabilities()

	if c.OS != runtime.GOOS {
		t.Errorf("Caps.OS = %q on %s", c.OS, runtime.GOOS)
	}
	// Every label reaches the interface directly. An empty one is a blank
	// button, which is worse than a wrong word.
	for name, v := range map[string]string{
		"RevealLabel":     c.RevealLabel,
		"TrashLabel":      c.TrashLabel,
		"TrashVerb":       c.TrashVerb,
		"PropertiesLabel": c.PropertiesLabel,
		"ModKey":          c.ModKey,
	} {
		if strings.TrimSpace(v) == "" {
			t.Errorf("Caps.%s is empty", name)
		}
	}

	// A platform that cannot install a helper must not claim it can, or
	// Preferences offers an Install button that does nothing.
	if c.HelperInstall && !c.Helper {
		t.Error("HelperInstall is set without Helper")
	}
}

func TestSupportDirIsUsable(t *testing.T) {
	dir, err := SupportDir()
	if err != nil {
		t.Fatalf("SupportDir: %v", err)
	}
	if dir == "" {
		t.Fatal("SupportDir returned an empty path")
	}
	if !strings.Contains(dir, AppDirName) {
		t.Errorf("SupportDir = %q, which does not contain %q", dir, AppDirName)
	}
}

// Purgeable space is the gap between what the system reports as available and
// what is free without evicting anything. Where there is no such concept the
// two are equal and the gap is zero, which must not come out negative.
func TestPurgeableBytes(t *testing.T) {
	if got := PurgeableBytes(Volume{Free: 100, AvailRaw: 40}); got != 60 {
		t.Errorf("PurgeableBytes = %d, want 60", got)
	}
	if got := PurgeableBytes(Volume{Free: 40, AvailRaw: 100}); got != 0 {
		t.Errorf("PurgeableBytes = %d, want 0 when there is no gap", got)
	}
	if got := PurgeableBytes(Volume{Free: 50, AvailRaw: 50}); got != 0 {
		t.Errorf("PurgeableBytes = %d, want 0 when they are equal", got)
	}
}
