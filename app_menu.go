package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// buildMenu builds the native menu bar. Items that act on the current
// selection send a command to the frontend, which owns selection state; the
// menu is a second route to the same keyboard shortcuts, not a parallel
// implementation.
func (a *App) buildMenu() *menu.Menu {
	m := menu.NewMenu()
	cmd := func(name string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) { a.emit("menu:command", name) }
	}

	m.Append(menu.AppMenu())

	file := m.AddSubmenu("File")
	file.AddText("New Scan…", keys.CmdOrCtrl("t"), cmd("newScan"))
	file.AddText("Scan Folder…", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("scanFolder"))
	file.AddText("Scan as Administrator…", keys.Combo("t", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("scanAdmin"))
	file.AddSeparator()
	file.AddText("Rescan", keys.CmdOrCtrl("r"), cmd("rescan"))
	file.AddText("Close Scan", keys.CmdOrCtrl("w"), cmd("closeScan"))

	edit := m.AddSubmenu("Edit")
	edit.AddText("Copy Path", keys.Combo("c", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("copyPath"))
	edit.AddSeparator()
	// No accelerator: Cmd-A must keep working inside text fields. The
	// frontend binds it on keydown when focus is not in an input.
	edit.AddText("Select All in Folder", nil, cmd("selectAll"))

	item := m.AddSubmenu("Item")
	item.AddText("Show in Finder", keys.Combo("r", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("reveal"))
	// Space and Return are bound in the frontend, guarded against text
	// input; as menu accelerators they would swallow every keystroke.
	item.AddText("Quick Look", nil, cmd("quickLook"))
	item.AddText("Get Info", keys.CmdOrCtrl("i"), cmd("getInfo"))
	item.AddText("Open", keys.CmdOrCtrl("o"), cmd("open"))
	item.AddSeparator()
	item.AddText("Add to Collector", keys.CmdOrCtrl("d"), cmd("collect"))
	item.AddText("Move to Trash", keys.CmdOrCtrl("backspace"), cmd("trash"))
	item.AddText("Delete Permanently…", keys.Combo("backspace", keys.CmdOrCtrlKey, keys.OptionOrAltKey), cmd("deleteForever"))

	view := m.AddSubmenu("View")
	// Back and forward retrace where the user has been, which is not the same
	// as walking up the tree — arriving somewhere from a breadcrumb or an arc
	// should be retraceable too.
	view.AddText("Back", keys.CmdOrCtrl("["), cmd("back"))
	view.AddText("Forward", keys.CmdOrCtrl("]"), cmd("forward"))
	view.AddSeparator()
	// Chart navigation. "Zoom" here means moving through the tree, which is
	// why the interface-size items below are named after size instead.
	view.AddText("Zoom In", nil, cmd("zoomIn"))
	view.AddText("Zoom Out", keys.CmdOrCtrl("up"), cmd("zoomOut"))
	view.AddText("Back to Volumes", keys.Combo("up", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("home"))
	view.AddSeparator()
	// Cmd-= is what macOS apps bind for "bigger": the key is labelled both =
	// and +, and binding the unshifted form is the convention. The shifted
	// press falls through to the window, where the keyboard handler takes it.
	view.AddText("Bigger", keys.CmdOrCtrl("="), cmd("uiBigger"))
	view.AddText("Smaller", keys.CmdOrCtrl("-"), cmd("uiSmaller"))
	view.AddText("Actual Size", keys.CmdOrCtrl("0"), cmd("uiReset"))
	view.AddSeparator()
	view.AddText("Show Collector", keys.CmdOrCtrl("l"), cmd("toggleCollector"))
	// Cmd-H is the system Hide item installed by AppMenu.
	view.AddText("Hidden Space…", keys.Combo("h", keys.CmdOrCtrlKey, keys.ShiftKey), cmd("hiddenSpace"))
	view.AddSeparator()
	view.AddText("Toggle Full Screen", keys.Combo("f", keys.CmdOrCtrlKey, keys.ControlKey), func(*menu.CallbackData) {
		runtime.WindowFullscreen(a.ctx)
	})

	win := m.AddSubmenu("Window")
	win.AddText("Minimize", keys.CmdOrCtrl("m"), func(*menu.CallbackData) { runtime.WindowMinimise(a.ctx) })
	win.AddText("Zoom", nil, func(*menu.CallbackData) { runtime.WindowToggleMaximise(a.ctx) })

	help := m.AddSubmenu("Help")
	help.AddText("Check for Updates…", nil, func(*menu.CallbackData) {
		a.emit("updater:changed", a.upd.CheckNow())
		a.emit("menu:command", "showUpdates")
	})
	help.AddText("Preferences…", keys.CmdOrCtrl(","), cmd("preferences"))
	help.AddSeparator()
	help.AddText("Grant Full Disk Access…", nil, func(*menu.CallbackData) { _ = a.OpenFDASettings() })

	return m
}

// domReady fires once the frontend is live. Anything emitted before this would
// be dropped, so the first volume list is pushed here.
func (a *App) domReady(ctx context.Context) {
	if vols, err := a.ListVolumes(); err == nil {
		a.emit("volumes:changed", vols)
	}
	a.emit("prefs:changed", a.prefs.Get())
	a.emit("helper:changed", a.GetHelperStatus())

	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		a.handleFileDrop(paths)
	})
}

// handleFileDrop starts a scan for each folder dropped on the window.
func (a *App) handleFileDrop(paths []string) {
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		target := p
		if !info.IsDir() {
			target = filepath.Dir(p) // dropping a file scans the folder holding it
		}
		if id, err := a.StartScan(target, false); err == nil {
			a.emit("scan:opened", id)
		} else {
			a.emit("app:error", err.Error())
		}
	}
}
