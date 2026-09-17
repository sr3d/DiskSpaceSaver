//go:build windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// applyPlatformOptions gives the window a standard Windows shell.
//
// Deliberately not frameless: reproducing the minimise, maximise and close
// buttons — with their hover states, their snap-layouts flyout and their
// right-to-left behaviour — is a lot of surface to get subtly wrong, and users
// notice immediately. The system title bar sits above the tab strip instead.
func applyPlatformOptions(o *options.App) {
	o.Windows = &windows.Options{
		// Mica where the OS supports it, which is Windows 11 22621 and later;
		// older releases fall back on their own.
		BackdropType:                      windows.Mica,
		Theme:                             windows.SystemDefault,
		DisableFramelessWindowDecorations: false,
		// The chart owns its background; a transparent webview would show the
		// desktop through during a resize.
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
		// Pinch-zoom would fight the interface scale, which is a preference
		// with its own shortcuts and is remembered between launches.
		DisablePinchZoom: true,
	}
}
