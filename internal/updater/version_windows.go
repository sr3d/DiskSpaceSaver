//go:build windows

package updater

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// readOSVersion uses RtlGetVersion rather than GetVersionEx: the latter lies
// to applications without a compatibility manifest claiming support for the
// running release, and would report Windows 8 on Windows 11.
func readOSVersion() string {
	v := windows.RtlGetVersion()
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
