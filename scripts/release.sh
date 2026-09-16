#!/usr/bin/env bash
#
# Cut a release: build, notarise, staple, publish to GitHub Releases, then tell
# the update service about it.
#
# The appcast is only updated at the very end, after the asset is downloadable.
# Publishing the feed first would offer users a version that does not yet
# exist.
#
# Required:
#   SIGN_ID          Developer ID Application identity
#   NOTARY_PROFILE   notarytool keychain profile (xcrun notarytool store-credentials)
#   SPARKLE_KEY      path to the EdDSA private key, or it is read from the keychain
#   UPDATE_TOKEN     admin token for the update service
#   GH_REPO          owner/repo for the release assets
set -euo pipefail

VERSION="${1:-}"
CHANNEL="${2:-stable}"
if [[ -z "$VERSION" ]]; then
  echo "usage: $0 <version> [stable|beta]" >&2
  exit 2
fi
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "version must look like 1.2.3 or 1.2.3-beta.1" >&2
  exit 2
fi

: "${GH_REPO:?set GH_REPO to owner/repo}"
: "${UPDATE_TOKEN:?set UPDATE_TOKEN}"
UPDATE_HOST="${UPDATE_HOST:-https://updates.diskspacesaver.app}"

APP="DiskSpaceSaver"
BUILD="$(git rev-list --count HEAD)"
OUT="dist/macos"
ZIP="$OUT/$APP-$VERSION.zip"

say() { printf '\n== %s\n' "$*"; }

say "checking the tree is clean"
if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is dirty; commit or stash first" >&2
  exit 1
fi

say "tests"
go test ./...

say "vendoring Sparkle if needed"
make sparkle

say "building and signing $APP $VERSION (build $BUILD)"
make app VERSION="$VERSION" BUILD="$BUILD"
make verify VERSION="$VERSION"

say "notarising"
make notarize VERSION="$VERSION"

say "stapling"
make staple VERSION="$VERSION"

say "verifying the stapled bundle passes Gatekeeper"
spctl --assess --type execute --verbose=2 "$OUT/$APP.app"

say "signing the update with the EdDSA key"
# sign_update ships with Sparkle. It prints the signature and the length that
# the appcast enclosure must carry; Sparkle refuses an update whose signature
# does not verify, so this is the step that makes the channel safe.
SIGN_OUTPUT="$(./bin/sparkle/sign_update "$ZIP" ${SPARKLE_KEY:+-f "$SPARKLE_KEY"})"
SIGNATURE="$(sed -n 's/.*sparkle:edSignature="\([^"]*\)".*/\1/p' <<<"$SIGN_OUTPUT")"
LENGTH="$(sed -n 's/.*length="\([0-9]*\)".*/\1/p' <<<"$SIGN_OUTPUT")"
if [[ -z "$SIGNATURE" || -z "$LENGTH" ]]; then
  echo "could not parse sign_update output:" >&2
  echo "$SIGN_OUTPUT" >&2
  exit 1
fi

say "publishing v$VERSION to GitHub Releases"
NOTES_FILE="docs/release-notes/$VERSION.md"
if [[ ! -f "$NOTES_FILE" ]]; then
  echo "write $NOTES_FILE first; users read it in the update prompt" >&2
  exit 1
fi
git tag -a "v$VERSION" -m "$APP $VERSION"
git push origin "v$VERSION"
gh release create "v$VERSION" "$ZIP" \
  --repo "$GH_REPO" \
  --title "$APP $VERSION" \
  --notes-file "$NOTES_FILE" \
  ${CHANNEL:+$([[ "$CHANNEL" == "beta" ]] && echo --prerelease)}

ASSET_URL="https://github.com/$GH_REPO/releases/download/v$VERSION/$APP-$VERSION.zip"

say "waiting for the asset to be downloadable"
for i in $(seq 1 30); do
  if curl -fsSLI -o /dev/null "$ASSET_URL"; then
    echo "  asset is live"
    break
  fi
  [[ $i -eq 30 ]] && { echo "asset never became downloadable" >&2; exit 1; }
  sleep 5
done

say "telling the update service"
curl -fsS -X POST "$UPDATE_HOST/releases" \
  -H "authorization: Bearer $UPDATE_TOKEN" \
  -H 'content-type: application/json' \
  -d "$(jq -n \
        --arg version "$VERSION" \
        --arg build "$BUILD" \
        --arg url "$ASSET_URL" \
        --arg signature "$SIGNATURE" \
        --arg notes "$(cat "$NOTES_FILE")" \
        --arg channel "$CHANNEL" \
        --argjson length "$LENGTH" \
        '{version:$version, build:$build, url:$url, length:$length,
          signature:$signature, notes:$notes, channel:$channel,
          minimumSystemVersion:"13.0"}')"

say "confirming the feed offers it"
curl -fsS "$UPDATE_HOST/appcast.xml?channel=$CHANNEL" | grep -q "$VERSION" \
  && echo "  appcast lists $VERSION" \
  || { echo "the appcast does not list $VERSION" >&2; exit 1; }

say "released $APP $VERSION on the $CHANNEL channel"
