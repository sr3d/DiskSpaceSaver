# DiskSpaceSaver

A disk space analyser for macOS: a sunburst chart of any volume or folder, with
the tools to act on what it shows.

Go + Wails v2 + React/TypeScript, with small Objective-C shims where macOS
requires them.

```
make dev        # run with hot reload
make test       # unit tests
make race       # unit tests under the race detector
make app        # build and sign DiskSpaceSaver.app
make verify     # check the bundle signs and the helper is placed correctly
make scan       # the scanner as a CLI, for benchmarking
```

Requires macOS 13 or later (`SMAppService`, used to install the privileged
helper), Go 1.25+, Node 20+ and Wails v2.13.

## How it is put together

```
React / TypeScript (WKWebView)
  VolumePicker · ScanTabs · Sunburst (canvas) · Sidebar · Breadcrumbs
  Collector · HiddenSpace · Preferences
        │  Wails bindings and events
Go
  internal/scanner     parallel walk, frame output, hardlink dedupe
  internal/tree        slab tree, viewport queries, cache codec
  internal/scanstore   one session per tab, live progress
  internal/macos       cgo: volumes, Trash, Finder, Quick Look, SMAppService
  internal/helper*     protocol, client and peer authentication
  internal/updater     Sparkle bridge and update reporting
        │  length-prefixed msgpack over a Unix socket
cmd/helper             root launchd daemon: scan and delete as root
        │
worker/                Cloudflare Worker: appcast feed and update statistics
```

**The tree lives in Go.** The frontend never receives it. It asks for viewport
slices — a focus node plus a few rings, with sub-pixel arcs folded into a
single "other" wedge — addressed by stable `uint32` handles. A five-million
node scan costs the WebView nothing, and no filesystem path is ever accepted
from JavaScript.

**One scanner, two transports.** The scanner's only output is a `Frame` per
directory. An ordinary scan pipes frames over a Go channel; an administrator
scan serialises the identical frames over the helper's socket. Both feed the
same tree builder, so the UI has no idea which it is looking at. A test walks
the same tree both ways and compares the results node for node.

## Numbers

Measured on an M1 Pro. `make scan` reproduces them.

| Tree | Portable path | `getattrlistbulk` |
|---|---|---|
| 1M synthetic files | 4.2s (235k entries/sec) | **2.4s (420k entries/sec)** |
| `~/Library`, 1.02M nodes | 22.7s | **15.4s** |
| Whole data volume, 5.2M nodes | — | **48s**, 1.3 GB heap |

For scale, on the same `~/Library` tree `find` alone takes 42s and `du -s`
takes 69s.

Accuracy against the same volume: the scan sees 755.3 GB, and adding the 31.8
GB macOS reports as purgeable lands within **0.73%** of Finder's used figure.

## Keyboard

| | |
|---|---|
| `⌘=` / `⌘-` / `⌘0` | interface bigger, smaller, back to normal |
| `Return` / `⌘↑` | zoom into the selected folder, zoom back out |
| `Space` | Quick Look |
| `⌘I` · `⌘⇧R` · `⌘O` | Get Info · Show in Finder · Open |
| `⌘D` | add to the collector |
| `⌘⌫` / `⌘⌥⌫` | Trash · delete permanently |
| `⌘T` · `⌘R` · `⌘L` · `⌘,` | new scan · rescan · collector · preferences |

The interface scale runs from 75% to 200% and is remembered between launches;
it is also in Preferences under Appearance. It scales layout rather than
painting over it, so text stays sharp and the chart re-renders at the new size
instead of being magnified.

## Things that are deliberate

**Cloud storage is not counted.** iCloud Drive, Google Drive and the rest
project their contents into the data volume through the FileProvider
framework. They are indistinguishable from ordinary folders — same device
number, no mount entry, no flag on the root — and `stat`-ing a file that has
not been downloaded blocks on the network. Walking into one counted tens of
gigabytes that were never on the disk and parked every worker until the scan
appeared frozen. They are shown, marked, and left alone.

The trade is deliberate but not free: files you *have* downloaded do occupy
real space, and they are no longer counted either. Doing this properly means
asking each provider's API which files are local, which is a separate feature.

**Allocated size, not file size.** The chart draws `st_blocks * 512`: what the
volume actually gives back when the file is deleted. File size is shown
alongside, because that is the number Finder's Get Info displays.

**Hardlinks are charged once.** The first path reached pays; the rest are
marked and count zero, so the total matches the volume rather than the sum of
the names.

**Sizes are base 10 by default**, to match Finder.

**Snapshot sizes are not shown.** Local Time Machine snapshots share blocks
with live files, so there is no single honest number for what they occupy and
macOS exposes none. They are listed and can be deleted; no size is invented.

**Permanent delete is off** until switched on in Preferences.

## The privileged helper

`cmd/helper` is a launchd daemon running as root, installed through
`SMAppService` and reached over a launchd-managed Unix socket. It exists so a
scan can see other users' files instead of reporting them as unreadable.

Because it runs as root it trusts nothing:

- Every connection is authenticated by the peer's **code signature**, checked
  against a designated requirement naming both the bundle ID and the Team ID.
  The peer pid is only used to obtain a code object, never taken as proof.
- Commands are a **closed enum**. No shell, no arbitrary arguments.
- Containment is **re-checked in the helper**, not inherited from the app's
  check, and a protected-prefix list refuses system trees outright.
- Trashing as root moves items to the **calling user's** Trash, where they can
  be restored, rather than root's.

The app and the helper must carry the same Team ID or `SMAppService`
registration fails with no diagnostic at all. `make verify` asserts that the
app signature, the helper signature and the daemon plist all agree.

## Not finished

- **Cloud storage scanning** via provider APIs — the v1.1 feature above.
- **`SMAppService` registration has not been exercised.** The helper builds,
  signs, passes `make verify`, and its protocol and containment rules are
  covered by tests over a real socket, but nothing here has installed a root
  daemon. Run `make app` and use Preferences → Install to try it, then check
  `launchctl print system/com.sr3d.diskspacesaver.helper`.
- **The updater is inert without Sparkle.** `internal/updater` owns scheduling,
  state and reporting, and the Sparkle bridge is written; vendor
  `Sparkle.framework` into `Contents/Frameworks` and build with `-tags sparkle`
  to switch it on. Until then it reports why it is unavailable rather than
  pretending to work. See `docs/RELEASING.md`.
- **Nothing is notarised or deployed.** Local builds are signed with an Apple
  Development certificate, so Gatekeeper rejects them; that is expected.
  Notarisation needs a Developer ID, and the appcast needs a Cloudflare
  account. Both pipelines are written and unrun.
- **The native menu bar, Dock progress and Quick Look** work only in the
  packaged app, not in a browser, so they are unverified beyond compiling and
  launching.

## Licence

Not yet chosen.
