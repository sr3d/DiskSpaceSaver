# Releasing

The pieces that cannot be built or tested from a clean checkout, and what each
one needs.

## One-time setup

### 1. Developer ID

A local build signs with whatever codesigning identity is to hand, which is
enough for `make verify` but not for distribution: Gatekeeper rejects anything
not signed with a **Developer ID Application** certificate and notarised.

```sh
# after enrolling in the Apple Developer Program
security find-identity -v -p codesigning     # confirm the identity is present
echo 'SIGN_ID=Developer ID Application: Your Name (TEAMID)' >> .env
```

`.env` is gitignored. `TEAM_ID` is not set by hand — the Makefile reads it back
off the signed helper, because on a personal team the certificate's common name
and the signature carry different identifiers, and a daemon plist built from
the wrong one makes `SMAppService` fail silently.

### 2. Notarisation credentials

```sh
xcrun notarytool store-credentials diskspacesaver \
  --key AuthKey_XXXXXXXX.p8 --key-id XXXXXXXX --issuer <issuer-uuid>
```

### 3. Sparkle

```sh
# Vendor the framework
curl -L -o /tmp/sparkle.tar.xz \
  https://github.com/sparkle-project/Sparkle/releases/download/2.6.4/Sparkle-2.6.4.tar.xz
mkdir -p vendor/Sparkle bin/sparkle && tar -xJf /tmp/sparkle.tar.xz -C vendor/Sparkle
cp -R vendor/Sparkle/Sparkle.framework build/darwin/
cp vendor/Sparkle/bin/{sign_update,generate_keys} bin/sparkle/

# Generate the EdDSA key pair. The private key goes to the login keychain; the
# public key goes in Info.plist as SUPublicEDKey.
./bin/sparkle/generate_keys
```

Add to `build/darwin/Info.plist`:

```xml
<key>SUPublicEDKey</key>
<string>…the public key generate_keys printed…</string>
```

The Makefile must copy `Sparkle.framework` into
`DiskSpaceSaver.app/Contents/Frameworks/`, sign it and its XPC services before
signing the app, and the Go build must use `-tags sparkle`. Sparkle's own
executables are nested code: sign them inside-out, never with `--deep`.

Without all of this the app still runs; `internal/updater` reports the updater
as unavailable and says why.

### 4. Update service

```sh
cd worker
npm install
npx wrangler secret put ADMIN_TOKEN     # a long random string
npx wrangler deploy
```

Point `updates.diskspacesaver.app` at the Worker, or change `FeedURL` in
`internal/updater/updater.go` and `SUFeedURL` in `Info.plist` to whatever host
is used.

Verify:

```sh
curl https://updates.diskspacesaver.app/health
curl https://updates.diskspacesaver.app/appcast.xml        # empty feed, valid XML
```

## Cutting a release

```sh
vim docs/release-notes/1.2.0.md      # users read this in the update prompt
export GH_REPO=sr3d/diskspacesaver
export UPDATE_TOKEN=…                 # the ADMIN_TOKEN above
./scripts/release.sh 1.2.0            # or 1.2.0 beta
```

The script refuses a dirty tree, runs the tests, builds, signs, notarises,
staples, checks the result passes Gatekeeper, signs the update with the EdDSA
key, publishes the GitHub release, **waits for the asset to be downloadable**,
and only then tells the appcast about it. Publishing the feed first would offer
users a version that does not yet exist.

`.github/workflows/release.yml` does the same on a tag, given these secrets:
`DEVELOPER_ID_P12`, `DEVELOPER_ID_PASSWORD`, `KEYCHAIN_PASSWORD`,
`SIGN_IDENTITY`, `AC_API_KEY_ID`, `AC_API_ISSUER`, `AC_API_KEY`,
`SPARKLE_PRIVATE_KEY`, `UPDATE_TOKEN`.

## Checking a release landed

```sh
curl -H "authorization: Bearer $UPDATE_TOKEN" \
  https://updates.diskspacesaver.app/stats | jq .funnel
```

The funnel is the number that matters: updates **found** against updates that
**finished installing**. A release where the two diverge is broken for the
people who accepted it, and without the event stream that is invisible — it
looks the same as nobody updating.

## Installing the helper

Not done during development, because it installs a root daemon and needs the
user's approval in System Settings.

```sh
make app
open build/bin/DiskSpaceSaver.app
# Preferences → Access → Privileged helper → Install…
# then approve DiskSpaceSaver in System Settings → General → Login Items

launchctl print system/com.sr3d.diskspacesaver.helper   # should list the job
sudo tail -f /var/log/diskspacesaver-helper.log

# Removing it
# Preferences → Access → Remove, or:
#   sudo launchctl bootout system/com.sr3d.diskspacesaver.helper
```

If registration appears to do nothing, the cause is almost always a signing
mismatch. `make verify` checks the three places the Team ID has to agree.
