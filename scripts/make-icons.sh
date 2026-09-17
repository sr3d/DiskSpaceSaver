#!/usr/bin/env bash
#
# Render docs/diskspacesaver-icon.svg into the icon assets the build consumes.
#
# The outputs are committed, so a build machine needs no SVG rasteriser — only
# whoever changes the artwork runs this.
#
# Each size is rendered from the vector at its native resolution rather than
# downscaled from one large bitmap: a 16px icon resampled from 1024px turns to
# mush, and this artwork has three concentric rings that need every pixel.
set -euo pipefail

cd "$(dirname "$0")/.."
SRC="docs/diskspacesaver-icon.svg"
OUT_ICONSET="$(mktemp -d)/DiskSpaceSaver.iconset"
mkdir -p "$OUT_ICONSET" build/darwin build/windows

# macOS reserves a margin around an app icon so every icon in the Dock has the
# same visual weight: on a 1024pt canvas the artwork occupies 824pt, centred.
# The source SVG is drawn edge to edge, so it is inset here rather than being
# altered.
CONTENT_RATIO=0.8046875   # 824 / 1024

render() {                # render <size> <destination>
  local size="$1" dest="$2"
  local inner
  inner=$(python3 -c "print(round($size * $CONTENT_RATIO))")
  local pad
  pad=$(python3 -c "print(($size - $inner) / 2)")

  local html="$OUT_ICONSET/../page.html"
  cat > "$html" <<HTML
<!doctype html><meta charset="utf-8">
<style>
  html,body { margin:0; padding:0; background:transparent; }
  body { width:${size}px; height:${size}px; }
  img { position:absolute; left:${pad}px; top:${pad}px; width:${inner}px; height:${inner}px; }
</style>
<img src="file://$PWD/$SRC">
HTML

  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "$inner" -h "$inner" "$SRC" -o "$dest.inner.png"
    # Compose onto a transparent canvas of the full size.
    python3 - "$dest.inner.png" "$dest" "$size" <<'PY'
import subprocess, sys
inner, dest, size = sys.argv[1], sys.argv[2], int(sys.argv[3])
subprocess.run(["sips", "-p", str(size), str(size), inner, "--out", dest],
               check=True, stdout=subprocess.DEVNULL)
PY
    rm -f "$dest.inner.png"
    return
  fi

  local chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  if [[ ! -x "$chrome" ]]; then
    echo "need rsvg-convert (brew install librsvg) or Google Chrome to render the icon" >&2
    exit 1
  fi
  "$chrome" --headless --disable-gpu --hide-scrollbars \
    --default-background-color=00000000 \
    --force-device-scale-factor=1 \
    --screenshot="$dest" --window-size="$size,$size" \
    "file://$html" >/dev/null 2>&1
}

echo "rendering $SRC"
for size in 16 32 64 128 256 512 1024; do
  render "$size" "$OUT_ICONSET/../out-$size.png"
  printf '  %4dpx\n' "$size"
done

cp "$OUT_ICONSET/../out-16.png"   "$OUT_ICONSET/icon_16x16.png"
cp "$OUT_ICONSET/../out-32.png"   "$OUT_ICONSET/icon_16x16@2x.png"
cp "$OUT_ICONSET/../out-32.png"   "$OUT_ICONSET/icon_32x32.png"
cp "$OUT_ICONSET/../out-64.png"   "$OUT_ICONSET/icon_32x32@2x.png"
cp "$OUT_ICONSET/../out-128.png"  "$OUT_ICONSET/icon_128x128.png"
cp "$OUT_ICONSET/../out-256.png"  "$OUT_ICONSET/icon_128x128@2x.png"
cp "$OUT_ICONSET/../out-256.png"  "$OUT_ICONSET/icon_256x256.png"
cp "$OUT_ICONSET/../out-512.png"  "$OUT_ICONSET/icon_256x256@2x.png"
cp "$OUT_ICONSET/../out-512.png"  "$OUT_ICONSET/icon_512x512.png"
cp "$OUT_ICONSET/../out-1024.png" "$OUT_ICONSET/icon_512x512@2x.png"

iconutil --convert icns --output build/darwin/iconfile.icns "$OUT_ICONSET"
echo "wrote build/darwin/iconfile.icns"

# Windows. Wails only generates an .ico when none exists, and it downscales a
# single 1024px source; these are the same native-resolution renders the macOS
# iconset uses. Entries above 256px are not representable in the directory.
go run ./cmd/mkicon -o build/windows/icon.ico \
  "$OUT_ICONSET/../out-16.png" \
  "$OUT_ICONSET/../out-32.png" \
  "$OUT_ICONSET/../out-64.png" \
  "$OUT_ICONSET/../out-128.png" \
  "$OUT_ICONSET/../out-256.png"

# Wails generates its own icns from this file; ours replaces it during
# packaging. It is also the source Wails would fall back to for the Windows
# icon if the .ico above were ever missing.
cp "$OUT_ICONSET/../out-1024.png" build/appicon.png
echo "wrote build/appicon.png"

rm -rf "$(dirname "$OUT_ICONSET")"
