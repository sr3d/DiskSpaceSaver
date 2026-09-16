//go:build darwin && sparkle

// Sparkle-backed updater. Built with `-tags sparkle` once Sparkle.framework is
// vendored into Contents/Frameworks; see docs/RELEASING.md.
package updater

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework AppKit
#include <stdlib.h>
#include "sparkle_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"sync"
	"unsafe"
)

type sparkleDriver struct {
	once sync.Once
	err  error
}

func newDriver() driver { return &sparkleDriver{} }

func free(c *C.char) string {
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

func (d *sparkleDriver) Available() (bool, string) {
	if !bool(C.dss_sparkle_available()) {
		return false, "Sparkle.framework is not bundled with this build"
	}
	return true, ""
}

func (d *sparkleDriver) start() error {
	d.once.Do(func() {
		feed := C.CString(FeedURLForChannel(currentChannel()))
		defer C.free(unsafe.Pointer(feed))
		if msg := free(C.dss_sparkle_start(feed)); msg != "" {
			d.err = errors.New(msg)
		}
	})
	return d.err
}

func (d *sparkleDriver) Check(userInitiated bool) (State, error) {
	if ok, why := d.Available(); !ok {
		return State{}, errors.New(why)
	}
	if err := d.start(); err != nil {
		return State{}, err
	}
	if msg := free(C.dss_sparkle_check(C.bool(userInitiated))); msg != "" {
		return State{}, errors.New(msg)
	}
	return d.state(), nil
}

func (d *sparkleDriver) state() State {
	var raw struct {
		UpdateAvailable bool   `json:"updateAvailable"`
		LatestVersion   string `json:"latestVersion"`
		ReleaseNotes    string `json:"releaseNotes"`
		DownloadURL     string `json:"downloadURL"`
	}
	_ = json.Unmarshal([]byte(free(C.dss_sparkle_state_json())), &raw)
	return State{
		Supported:       true,
		UpdateAvailable: raw.UpdateAvailable,
		LatestVersion:   raw.LatestVersion,
		ReleaseNotes:    raw.ReleaseNotes,
		DownloadURL:     raw.DownloadURL,
		CurrentVersion:  CurrentVersion(),
	}
}

func (d *sparkleDriver) InstallAndRelaunch() error {
	if err := d.start(); err != nil {
		return err
	}
	if msg := free(C.dss_sparkle_install()); msg != "" {
		return errors.New(msg)
	}
	return nil
}
