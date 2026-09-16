//go:build darwin

package macos

import "C"

// goVolumesChanged is invoked from the ObjC notification observer on the main
// thread. It must return immediately, so the Go-side hook is dispatched on its
// own goroutine.
//
//export goVolumesChanged
func goVolumesChanged() { fireVolumesChanged() }
