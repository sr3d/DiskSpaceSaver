//go:build !darwin && !windows

package updater

func readOSVersion() string { return "unknown" }
