//go:build darwin

package platform

import "C"

// goVolumesChanged is invoked from the ObjC notification observer on the main
// thread. It must return immediately, so the Go-side hook is dispatched on its
// own goroutine.
//
//export goVolumesChanged
func goVolumesChanged() { fireVolumesChanged() }

// goMouseButton is invoked from the event monitor on the main thread with the
// NSEvent button number. Same contract as above: return at once.
//
//export goMouseButton
func goMouseButton(n C.int) { fireMouseButton(int(n)) }
