//go:build darwin

package updater

import (
	"os/exec"
	"strings"
)

func readOSVersion() string {
	out, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
