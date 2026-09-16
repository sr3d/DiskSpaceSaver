//go:build darwin && sparkle

package updater

import "C"

// goSparkleEvent is called from the Sparkle delegate on the main thread. It
// must return immediately, so reporting happens on its own goroutine inside
// Report.
//
//export goSparkleEvent
func goSparkleEvent(event, version, errMsg *C.char) {
	name := C.GoString(event)
	to := C.GoString(version)
	if active != nil {
		active.Report(name, CurrentVersion(), to)
	}
}
