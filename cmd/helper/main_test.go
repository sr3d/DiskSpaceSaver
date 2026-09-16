package main

import (
	"os"
	"path/filepath"
	"testing"
)

// withinRoot is the last line of defence before a root-owned delete. The app
// checks containment too, but the app is the thing being authenticated, not
// the thing being trusted.
func TestWithinRootRefusesEscapes(t *testing.T) {
	root := "/Users/someone/Projects"

	inside := []string{
		"/Users/someone/Projects/a.bin",
		"/Users/someone/Projects/deep/nested/file",
		"/Users/someone/Projects/x/../y.bin", // normalises back inside
	}
	for _, p := range inside {
		if !withinRoot(root, filepath.Clean(p)) {
			t.Errorf("%s should be inside %s", p, root)
		}
	}

	outside := []string{
		"/Users/someone/Projects",            // the root itself is never a target
		"/Users/someone",                     // the parent
		"/Users/someone/Projects/../secrets", // climbs out
		"/Users/someone/Projects2/a.bin",     // prefix, not a child
		"/etc/passwd",
		"/",
		"relative/path",
	}
	for _, p := range outside {
		if withinRoot(root, filepath.Clean(p)) {
			t.Errorf("%s must not be treated as inside %s", p, root)
		}
	}
}

func TestProtectedPathsAreRefused(t *testing.T) {
	protected := []string{
		"/System", "/System/Library/CoreServices", "/bin/sh", "/usr/lib/dyld",
		"/sbin/mount", "/Library/Apple/System",
		// The same trees reached through the Data volume's prefix.
		"/System/Volumes/Data/bin/sh",
		"/System/Volumes/Data/usr/libexec/anything",
	}
	for _, p := range protected {
		if !isProtectedPath(p) {
			t.Errorf("%s should be protected", p)
		}
	}

	allowed := []string{
		"/Users/someone/Library/Caches/big",
		"/Applications/Something.app",
		"/System Volumes Report.txt", // not a prefix match
		"/usr/local/lib/thing",
		"/binfile",
	}
	for _, p := range allowed {
		if isProtectedPath(p) {
			t.Errorf("%s should not be protected", p)
		}
	}
}

// Only a real tmutil date stamp may reach the command line, so nothing that
// looks like a flag or a path can be smuggled through a snapshot name.
func TestSnapshotNameValidation(t *testing.T) {
	good := []string{"2026-09-14-091500", "2024-01-01-000000"}
	for _, s := range good {
		if !validSnapshotDate(s) {
			t.Errorf("%q should be a valid snapshot date", s)
		}
	}
	bad := []string{
		"", "--all", "2026-09-14", "../../etc", "2026-13-40-999999",
		"2026-09-14-091500 ; rm -rf /", "-f",
	}
	for _, s := range bad {
		if validSnapshotDate(s) {
			t.Errorf("%q must be rejected", s)
		}
	}
}

func TestUniqueNameAvoidsClobbering(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "report.txt")
	if got := uniqueName(p); got != p {
		t.Errorf("an unused name should be returned unchanged, got %s", got)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniqueName(p)
	if second == p {
		t.Fatal("uniqueName returned a path that already exists")
	}
	if filepath.Ext(second) != ".txt" {
		t.Errorf("the extension should be preserved, got %s", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if third := uniqueName(p); third == p || third == second {
		t.Errorf("uniqueName repeated an existing name: %s", third)
	}
}
