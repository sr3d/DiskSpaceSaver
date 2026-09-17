# Windows

## State

Builds and passes its unit tests. **Nothing has been run** — it is
cross-compiled from a Mac, which cannot execute the result. Treat every
behavioural claim below as untested until someone runs it.

```sh
make windows          # amd64 + arm64 into dist/windows/
make windows-verify   # what was produced
make windows-installer # needs makensis (brew install makensis)
```

## What to check first

In rough order of how likely each is to be wrong. The first three exercise code
that has never executed.

1. **It starts at all.** WebView2 must be present; Windows 11 has it, Windows 10
   may not. A missing runtime should prompt rather than fail silently.
2. **Drives are listed** with plausible sizes, and removable drives with no
   media do not appear. Compare against This PC.
3. **A scan's total** against Explorer's "size on disk" for the same folder —
   this is `GetCompressedFileSize`, and the number the chart draws.
4. Chart draws while scanning; hover and click select the arc under the pointer.
5. **Delete goes to the Recycle Bin** and the item can be restored from there.
   This is the one destructive path; try it on something you do not mind losing.
6. Show in Explorer selects the file; Properties opens the system dialog.
7. The preview panel shows real thumbnails for images and PDFs, not generic
   file-type icons.
8. Size filter, bulk selection, drag-resize, `Ctrl+=` zoom, `Alt+←` back.
9. The taskbar icon is the DiskSpaceSaver rings, not the Wails logo.

## Known gaps

- **No elevated scanning.** `cmd/helper` does not build for Windows yet, so
  folders belonging to other users report as unreadable. The plan is a helper
  elevated on demand through UAC, over a named pipe, reusing the frame protocol
  unchanged.
- **No auto-update.** Sparkle is macOS-only; WinSparkle reads the same appcast
  and the same EdDSA keys, so the existing release pipeline carries over with a
  platform field added to the feed.
- **No code signing.** SmartScreen will warn on first run until there is an
  Authenticode certificate.
- **No snapshots.** VSS shadow copies are the equivalent of Time Machine local
  snapshots; the Hidden Space panel hides that section on Windows.
- **Hardlink dedupe is off.** The directory enumeration returns a file ID but no
  link count, so there is no cheap way to limit the map to files that actually
  have several names, and deduping everything would cost hundreds of megabytes
  on a large volume. WinSxS is the case where this shows up as over-counting.
- **No taskbar progress.** The macOS build draws scan progress on the Dock icon.
  The Windows equivalent needs the main window handle, which Wails does not
  expose.

## Reporting back

The useful details are: the Windows build number, whether it is amd64 or arm64,
what you did, what happened, and what you expected. For a wrong size, Explorer's
own figure for the same folder is worth more than a screenshot of ours.
