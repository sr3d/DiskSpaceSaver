# DiskSpaceSaver — Build Spec v2

Goal: a **feature-complete DaisyDisk clone** for macOS. Stack: **Go + Wails v2 + React/TypeScript**, with small cgo/ObjC shims where macOS demands it.
Distribution: direct download, signed + notarized, **in-app updater in v1**. Not App Store (sandbox blocks full-volume scans and the privileged helper).

---

## 1. Feature parity checklist (DaisyDisk → v1.0)

| Area | Behavior | v1.0 |
|---|---|---|
| Volume list | All mounted volumes (internal, external, network) with used/free/purgeable bars + icons; live updates on mount/unmount | ✅ |
| Scan targets | Volume, arbitrary folder, drag folder onto window or Dock icon, "Scan as Administrator" | ✅ |
| Multiple scans | Several volumes scanned in parallel, each its own tab/document; switch without losing results | ✅ |
| Live scan | Chart grows while scanning, progress, cancel | ✅ |
| Sunburst | Radial chart, ~5 rings, hover highlight + path/size in center, click to zoom, center to zoom out, breadcrumbs, smooth zoom animation | ✅ |
| Sidebar | Children of focus sorted by size with bars, hover sync, keyboard nav | ✅ |
| Item actions | Show in Finder, Quick Look, Get Info, Open, copy path, Move to Trash, **Delete permanently** (opt-in, confirm) | ✅ |
| Collector | Drop tray for items across scans; shows total reclaimable; batch delete; remove from tray | ✅ |
| Hidden space | Explains used − visible: purgeable, APFS local snapshots (list + delete via `tmutil`), system-protected, hardlink/clone delta | ✅ |
| Scan as admin | Privileged helper; sees all users' data and protected paths; deletes as root | ✅ |
| Scan cache | Persist last scan per volume; reopen instantly, offer rescan; rescan a subtree | ✅ |
| Preferences | Size units (base-10/base-2), color scheme, dark/light/system, show hidden files, exclusions list, update channel | ✅ |
| Updater | Check on launch + daily, "Check for Updates…" menu, release notes, one-click install/relaunch | ✅ |
| Polish | Native menu bar, Dock badge/progress during scan, Touch Bar not needed, VoiceOver labels on sidebar | ✅ |
| Cloud storage scan | Dropbox/Google Drive/iCloud via provider APIs (DaisyDisk 4 feature) | ⏭ v1.1 — separate OAuth + API per provider; large scope, ship after core |

---

## 2. Architecture

```
┌──────────────── React/TS (WebKit) ────────────────┐
│ VolumePicker · ScanTabs · Sunburst(canvas) ·       │
│ Sidebar · Breadcrumbs · Collector · HiddenSpace ·  │
│ Preferences · UpdatePrompt                         │
└──────────── Wails bindings + events ──────────────┘
┌──────────────────── Go app ───────────────────────┐
│ scanner/   (lib) parallel walk, dedupe, live aggr. │
│ tree/      in-memory tree, viewport queries, cache │
│ helperclient/  talks to root helper over socket    │
│ updater/   Sparkle bridge                          │
│ macos/     cgo: Trash, Finder, QL, volumes, TCC    │
│ app.go     bound API                               │
└───────────────────────────────────────────────────┘
┌──────── Privileged helper (Go, same scanner lib) ─┐
│ launchd daemon via SMAppService, root, Unix socket │
│ code-signature-verified peer; scan + delete as root│
└───────────────────────────────────────────────────┘
        ┌── Cloudflare Worker + Durable Object ──┐
        │ appcast feed · release state · update events│
        └──────────────────────────────────────────┘
```

**Rule #1: the tree lives in Go.** The UI only receives *viewport slices* (focus node + D levels, tiny arcs collapsed into "other"). Node handles are stable `uint32` slab indices, never paths.

---

## 3. Scanner (`scanner/` — shared library, used by app and helper)

- Worker pool over a directory channel. v1 path: `os.ReadDir` + `Lstat`; M6 fast path: `getattrlistbulk(2)` via cgo (one syscall per dir batch: name, type, logical size, allocated blocks, inode, nlink, flags). This is the main APFS speed lever.
- Per entry: logical size, allocated (`st_blocks*512` — the chart's number), dev+inode, nlink, symlink flag (never follow), package flag (`.app`, `.photoslibrary`, etc. — leaf in UI, summed).
- Hardlink dedupe: `map[devInode]` for nlink>1. APFS clones: not cheaply detectable per file — report as part of the hidden-space delta.
- Stay on one `st_dev`; skip `/dev`, `.Spotlight-V100`, `.fseventsd`, `.DocumentRevisions-V100`. "Macintosh HD" scans `/System/Volumes/Data` presented as root; don't double-count firmlinks.
- Sizes bubble up via atomic adds on the ancestor chain → queryable mid-scan. Defer child sorting until scan end; during scan return top-K via small heap.
- EPERM collected per node, not fatal → surfaced as "N inaccessible items — grant Full Disk Access or scan as admin".
- Output modes: in-process (normal scan) or **streamed over the helper socket** (admin scan): length-prefixed msgpack frames `{dir, entries[]}`; the app rebuilds the tree locally so the UI path is identical for both.
- Events: `scan:progress` every 100ms `{files, dirs, bytes, currentPath}`; `scan:snapshot` throttled root viewport for the growing animation. Cancel via context.

## 4. Tree + cache (`tree/`)
```go
type Node struct { Name string; Size, Logical int64; Kind uint8; Parent *Node; Children []*Node; Err string }
```
- Cache: on scan completion, serialize the tree (flat arrays, zstd) to `~/Library/Application Support/DiskSpaceSaver/scans/<volumeUUID>.bin` with scan timestamp. On open, load instantly, badge "scanned 3h ago — Rescan".
- After Trash/delete: prune subtree, re-aggregate ancestors, emit `tree:changed` so chart/sidebar/collector refresh.

## 5. Bound API (`app.go`)
```go
ListVolumes() []Volume                          // + event volumes:changed (DiskArbitration or FSEvents on /Volumes)
StartScan(path string, asAdmin bool) (scanID string, err error)
CancelScan(scanID string)
ListScans() []ScanSummary; CloseScan(scanID string)
GetViewport(scanID string, nodeID uint32, depth int, minFraction float64) ViewportSlice
GetChildren(scanID, nodeID, offset, limit)  []NodeSummary
GetAncestors(scanID, nodeID) []NodeSummary
RevealInFinder(nodeID) / QuickLook(nodeID) / GetInfo(nodeID) / OpenItem(nodeID)
MoveToTrash(items []ItemRef) (Result, error)   // ItemRef = {scanID, nodeID}
DeletePermanently(items []ItemRef) (Result, error)
Rescan(scanID, nodeID) error
Collector: Add/Remove/List/Clear/DeleteAll
HiddenSpace(volume) HiddenSpaceReport         // purgeable, snapshots[], protected, dedupeDelta
DeleteLocalSnapshots(names []string) error    // via helper if needed
CheckFullDiskAccess() FDAStatus; OpenFDASettings()
Helper: Status()/Install()/Uninstall()
Updater: CheckNow()/InstallAndRelaunch()
Prefs: Get()/Set()
```

## 6. macOS shims (`macos/`, cgo + ObjC)
- **Trash**: `NSFileManager trashItemAtURL:` (Finder undo works). Permanent delete: `os.RemoveAll` (or helper for root-owned).
- **Finder / Get Info / Open**: `NSWorkspace activateFileViewerSelectingURLs:`, AppleScript `tell application "Finder" to open information window`, `NSWorkspace openURL:`.
- **Quick Look**: `QLPreviewPanel` via ObjC with a data source over the selected path (proper, not `qlmanage`). Space toggles.
- **Volumes**: `NSFileManager mountedVolumeURLs...` + `NSURLVolumeTotalCapacityKey`, `NSURLVolumeAvailableCapacityForImportantUsageKey` (Finder's number), plain available → purgeable = difference. `NSURLVolumeUUIDStringKey` for cache keys. Mount events: `NSWorkspace` `didMountNotification`/`didUnmountNotification`.
- **Snapshots**: `tmutil listlocalsnapshots /`, `tmutil deletelocalsnapshots <date>` (root not required for TM snapshots, but helper covers edge cases).
- **FDA check**: read `~/Library/Mail` or TCC db path; EPERM ⇒ not granted. Deep link `x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles`.
- **Drag-in**: Wails `OnFileDrop` for window; Dock/Finder "Open With" via `CFBundleDocumentTypes` (folders) + Wails `OnUrlOpen`/file-open handler.
- **Dock progress**: `NSDockTile` badge with % during scan.

## 7. Privileged helper ("Scan as Administrator")
- Helper = second Go binary `diskspacesaver-helper` built from the same repo, embedded at `Contents/Library/LaunchDaemons/` with plist, registered via **`SMAppService.daemon`** (macOS 13+; set min OS 13). Registration prompts the user once in System Settings → Login Items; app shows a guided sheet.
- Transport: launchd-managed **Unix domain socket** (`Sockets` in plist), not NSXPCConnection (avoids ObjC XPC plumbing from Go). Helper runs as root.
- Security: on every connection helper takes the peer PID (`LOCAL_PEERPID`/`getpeereid`), gets `SecCodeCopyGuestWithAttributes` → `SecCodeCheckValidity` against a designated requirement (Team ID + bundle ID) — reject anything else. Commands are a fixed enum: `Scan(path)`, `Delete(paths, toTrash bool)`, `DeleteSnapshots`, `Version`. No shell, no arbitrary paths outside the scanned root for deletes.
- Root Trash: `trashItemAtURL:` as root puts files in root's Trash — instead move to `<volume>/.Trashes/<uid>/` for the calling user (what Finder does), or fall back to permanent delete with explicit confirmation.
- Versioning: app checks `Version` on connect; mismatch → re-register helper.
- Uninstall path in Preferences (`SMAppService.unregister`).

## 8. Updater
- **Client: Sparkle 2** embedded via cgo (`Sparkle.framework` in `Contents/Frameworks`), driven from Go: `SPUStandardUpdaterController` at launch, `checkForUpdates:` from the app menu, EdDSA key pair, `SUFeedURL` + `SUPublicEDKey` in Info.plist. Sparkle handles download verification, atomic app replacement, quarantine, relaunch.
- **Binaries: GitHub Releases.** Each release = tag `vX.Y.Z` with the notarized, stapled `.zip` (built with `ditto -c -k --keepParent`) as an asset. Download URLs in the appcast point at `github.com/<org>/<repo>/releases/download/vX.Y.Z/<App>-X.Y.Z.zip`.
- **Feed + telemetry: Cloudflare Worker + Durable Object** (single DO instance, free tier):
  - `GET /appcast.xml` — renders the Sparkle appcast from stored release metadata (version, build, min OS, notes, GitHub asset URL, EdDSA signature, length, channel).
  - `POST /releases` — admin-token protected; called by `release.sh` after `sign_update` (or by a GitHub Action on `release: published`).
  - `POST /events` — **the app reports Sparkle lifecycle events to the DO.** Implement `SPUUpdaterDelegate` in the ObjC shim and forward to Go, which POSTs `{installID(random UUID, stored in prefs), event, fromVersion, toVersion, osVersion, arch, channel, ts}` for: `didFindValidUpdate`, `userDidSkip`, `willInstallUpdate`, `didFinishUpdateCycle` (with error if any), plus a `launch` event so you get active installs by version. Fire-and-forget with a 3s timeout; never block UI; respect a "Send anonymous update stats" pref (default on, no PII).
  - `GET /stats` — token-protected JSON: installs by version, update funnel (found → installed → failed), OS distribution.
  - Channels: `stable` / `beta` via `SUFeedURL` query param.
- Release pipeline (`Makefile` + GitHub Action): `wails build -platform darwin/universal` → codesign hardened runtime (app + helper) → `notarytool submit --wait` → staple → zip → `sign_update` → `gh release create` with asset → `POST /releases`.

## 9. Frontend (React + TS)
- Sunburst on **`<canvas>`**: `d3-hierarchy` partition, manual arc drawing, DPR scaling, polar hit-test on the slice. 5 rings from focus; arcs < 0.4° collapse to gray "other" (click → sidebar list). Zoom = 300ms domain interpolation. Hue by top-level child, lightness by depth; packages distinct hue; inaccessible hatched.
- Scan phase: redraw on `scan:snapshot` via rAF; root ring splits as data arrives.
- Multi-scan tabs; Collector is global across tabs.
- Hidden Space panel: stacked bar (visible files / purgeable / snapshots / protected / unknown delta) with actions.
- Store: Zustand for UI state; tree slice in a ref, canvas drawn imperatively.
- Keyboard: `⌘↑` up · `Enter` zoom · `Space` Quick Look · `⌘I` Get Info · `⌘⌫` Trash · `⌘R` rescan · `⌘,` prefs · `⌘T` new scan.
- Base-10 sizes by default to match Finder; toggle in prefs.

## 10. Milestones

| M | Deliverable | Go/no-go |
|---|---|---|
| M1 | Scanner CLI (lib + `cmd/scan`): top-N, timing, streaming frame mode | ~1M files < 20s pre-cgo; memory OK |
| M2 | Wails shell, volume list w/ live mount events, StartScan/GetViewport, live progress, scan tabs | Chart draws during scan |
| M3 | Canvas sunburst + sidebar + breadcrumbs + zoom anim | 60fps, 5 rings |
| M4 | Actions (Finder/QL/GetInfo/Open/Trash/permanent), tree prune, FDA prompt, drag-in, Dock progress | Finder undo works |
| M5 | **Privileged helper**: SMAppService registration UI, socket + code-sign check, root scan streamed into same UI, root delete | Admin scan of `/Users` shows other users |
| M6 | Hidden Space panel (purgeable, snapshots list/delete, dedupe delta), Collector, scan cache, preferences | Totals match Finder within ~2% |
| M7 | `getattrlistbulk` fast path, hardlink verification, universal binary | 2–3× faster than M1 |
| M8 | Sparkle + Worker/DO feed + release script, signing/notarization/DMG | Clean Mac: opens, updates itself, helper installs |

Retire risk early: **M1** (scan speed) and **M5** (helper — SMAppService + code-sign verification is the fiddliest part; prototype the socket handshake right after M2 if it worries you).

## 11. Gotchas
- SMAppService requires the helper plist `BundleProgram` path relative to the .app and identical Team ID; any codesign mismatch = silent failure. Test with `launchctl print system/<label>`.
- Wails WKWebView: drag *out* to Finder unsupported → Collector is in-app drag only.
- Don't `rm` anything with the helper unless the path is under the scan root and the user confirmed permanent delete.
- Sparkle + Go: app must be launched from a `.app` bundle for updater to work; skip updater init in `wails dev`.
- `/` is a sealed snapshot; always scan `/System/Volumes/Data`.

## 12. Decisions
- **Min macOS: 13 Ventura** (`LSMinimumSystemVersion` 13.0). Required by `SMAppService`; dev machine is Sequoia 15.5. Build/test on 15, don't use APIs newer than 13 without `@available` guards.
- **Name: DiskSpaceSaver.** Bundle ID `com.<org>.diskspacesaver`; helper label `com.<org>.diskspacesaver.helper`; Sparkle feed `https://updates.diskspacesaver.<tld>/appcast.xml`. Team ID: fill in.
