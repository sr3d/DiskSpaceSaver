package updater

import (
	"os/exec"
	"strings"
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

func osVersion() string {
	osOnce.Do(func() {
		out, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
		if err != nil {
			osVer = "unknown"
			return
		}
		osVer = strings.TrimSpace(string(out))
	})
	return osVer
}
