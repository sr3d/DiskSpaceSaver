package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/sr3d/diskspacesaver/internal/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version and Build are stamped by the release script:
//
//	-ldflags "-X main.Version=1.2.0 -X main.Build=42"
var (
	Version = "0.1.0-dev"
	Build   = "0"
)

func main() {
	updater.SetVersion(Version, Build)
	app := NewApp()

	opts := &options.App{
		Title:       "DiskSpaceSaver",
		Width:       1180,
		Height:      780,
		MinWidth:    860,
		MinHeight:   560,
		AssetServer: &assetserver.Options{Assets: assets},
		// The chart paints its own background; a transparent window would
		// flash the desktop through during resize.
		BackgroundColour: &options.RGBA{R: 18, G: 19, B: 24, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnDomReady:       app.domReady,
		Menu:             app.buildMenu(),
		DragAndDrop: &options.DragAndDrop{
			// Folders dropped on the window start a scan.
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		Bind: []any{app},
	}

	// Window decoration and appearance are the one part of the shell that
	// cannot be shared: macOS hides the title bar and inlays the traffic
	// lights, Windows keeps its own.
	applyPlatformOptions(opts)

	err := wails.Run(opts)
	if err != nil {
		println("DiskSpaceSaver could not start:", err.Error())
	}
}
