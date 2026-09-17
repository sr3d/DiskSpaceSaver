//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// applyPlatformOptions gives the window a macOS shell: the title bar is hidden
// and inlaid, so the tab strip sits where the title would be and the traffic
// lights overlay it. The frontend leaves a gap for them.
func applyPlatformOptions(o *options.App) {
	o.Mac = &mac.Options{
		TitleBar:             mac.TitleBarHiddenInset(),
		Appearance:           mac.DefaultAppearance,
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
		About: &mac.AboutInfo{
			Title:   "DiskSpaceSaver",
			Message: "See what is using your disk, and get it back.\nVersion " + Version + " (" + Build + ")",
		},
	}
}
