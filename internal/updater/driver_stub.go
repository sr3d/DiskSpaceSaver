//go:build !sparkle

package updater

import "errors"

// unavailableDriver stands in until the app is built against Sparkle. It keeps
// the whole update path exercisable — state, scheduling, UI — without shipping
// a framework, and says plainly why it is inert rather than failing silently.
type unavailableDriver struct{}

func newDriver() driver { return unavailableDriver{} }

func (unavailableDriver) Available() (bool, string) {
	return false, "this build has no updater: rebuild with -tags sparkle and Sparkle.framework in Contents/Frameworks"
}

func (unavailableDriver) Check(bool) (State, error) {
	return State{CurrentVersion: CurrentVersion()},
		errors.New("this build cannot check for updates")
}

func (unavailableDriver) InstallAndRelaunch() error {
	return errors.New("this build cannot install updates")
}
