package updater

import (
	"sync"
)

var (
	version = "0.0.0-dev"
	build   = "0"
)

// SetVersion records the running build, called from main at startup.
func SetVersion(v, b string) {
	if v != "" {
		version = v
	}
	if b != "" {
		build = b
	}
}

// CurrentVersion is the running marketing version.
func CurrentVersion() string { return version }

// CurrentBuild is the running build number.
func CurrentBuild() string { return build }

var (
	osOnce sync.Once
	osVer  string
)

// osVersion is reported with update events so a release that breaks on one
// OS version is visible as such.
func osVersion() string {
	osOnce.Do(func() { osVer = readOSVersion() })
	return osVer
}
