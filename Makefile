# DiskSpaceSaver build and release.
#
# The app and the privileged helper must be signed with the same Team ID or
# SMAppService registration fails silently, so both are signed here, helper
# first, and the bundle is verified before anything else happens.

APP           := DiskSpaceSaver
BUNDLE_ID     := com.sr3d.diskspacesaver
HELPER_LABEL  := $(BUNDLE_ID).helper
HELPER_BIN    := diskspacesaver-helper
VERSION       ?= 0.1.0
BUILD         ?= $(shell git rev-list --count HEAD 2>/dev/null || echo 0)

# Signing. TEAM_ID and SIGN_ID come from the environment or a local .env so no
# developer identity is committed.
-include .env
# The Team ID is read back from the signed helper rather than parsed out of
# the certificate's common name: on a personal team those differ (the name
# carries the Apple ID team, the signature carries the real OU), and a plist
# built from the wrong one makes SMAppService fail without saying why.
# Only quoted identity lines count: with none installed, security still prints
# a "0 valid identities found" summary, which must not become the identity.
SIGN_ID       ?= $(shell security find-identity -v -p codesigning | sed -n 's/^[^"]*"\(.*\)"$$/\1/p' | head -1)
NOTARY_PROFILE?= diskspacesaver
FEED_URL      ?= https://updates.diskspacesaver.app/appcast.xml

# Wails always builds into build/bin; releasable artefacts are collected under
# dist/<platform>/ so each target has one obvious place to look. Windows will
# land in dist/windows alongside this.
WAILS_OUT     := build/bin
DIST          := dist
DIST_MAC      := $(DIST)/macos
APP_BUNDLE    := $(DIST_MAC)/$(APP).app
ZIP           := $(DIST_MAC)/$(APP)-$(VERSION).zip
WAILS         := $(shell command -v wails 2>/dev/null || echo $(HOME)/go/bin/wails)
LDFLAGS       := -X main.Version=$(VERSION) -X main.Build=$(BUILD)

# Sparkle powers the in-app updater. It is fetched rather than committed; the
# build tag only switches on once the framework is actually present, so a
# checkout without it still builds and runs.
#
# Not under vendor/ — that directory name is reserved by the Go toolchain,
# which switches the whole module into vendored-dependency mode as soon as it
# appears and then fails because there is no modules.txt.
SPARKLE_VERSION := 2.6.4
SPARKLE_DIR     := third_party/Sparkle
SPARKLE_FW      := $(SPARKLE_DIR)/Sparkle.framework
# Target-aware, not host-aware. Deciding this from a directory on the build
# machine meant a Windows target built on a Mac with Sparkle vendored got
# -tags sparkle, which excludes the stub driver and does not include the darwin
# one — a build failure with no obvious cause.
GO_TAGS       := $(if $(wildcard $(SPARKLE_FW)),sparkle,)

# cgo vets linker flags against an allowlist that excludes @executable_path.
# The updater needs an rpath into the bundle's Frameworks directory, so that
# one pattern is permitted explicitly rather than the check being disabled.
export CGO_LDFLAGS_ALLOW := -Wl,-rpath,.*

# Sparkle's public key, substituted into Info.plist at package time. It is not
# a secret — it is the half that verifies — but it is per-project, so it comes
# from the environment or .env rather than being committed to a template.
SPARKLE_PUBLIC_KEY ?=

# An ad-hoc signature ("-") carries no Team ID, so a build signed that way can
# never install the privileged helper. CI uses it anyway: it exercises the
# whole packaging path — helper placement, framework embedding, plist edits —
# which is where breakages actually happen, and a pull request has no access to
# the real identity. Such a build warns instead of failing, and is never
# something to ship.
ADHOC := $(if $(filter -,$(SIGN_ID)),1,)

.PHONY: all help dev test race lint scan helper app sign verify notarize staple zip release clean bindings icons sparkle dist-dirs windows windows-verify

help:
	@echo "DiskSpaceSaver $(VERSION) (build $(BUILD))"
	@echo
	@echo "  make dev        run with hot reload"
	@echo "  make test       unit tests"
	@echo "  make race       unit tests under the race detector"
	@echo "  make scan       build the scanner CLI into bin/scan"
	@echo "  make helper     build the privileged helper into bin/"
	@echo "  make app        build and sign $(DIST_MAC)/$(APP).app"
	@echo "  make verify     check the bundle's signature and helper placement"
	@echo "  make icons      re-render the icon from docs/diskspacesaver-icon.svg"
	@echo "  make sparkle    vendor Sparkle $(SPARKLE_VERSION), enabling the updater"
	@echo "  make windows    cross-compile the Windows build into $(DIST)/windows/"
	@echo "  make release    app + notarize + staple + zip (needs a Developer ID)"
	@echo
	@echo "  output      $(DIST_MAC)/"
	@echo "  SIGN_ID     $(SIGN_ID)"
	@echo "  updater     $(if $(GO_TAGS),Sparkle vendored — updater enabled,not vendored — run make sparkle)"

all: app

dev:
	$(WAILS) dev

# Built without the updater on purpose: binding generation runs a throwaway
# binary outside the bundle, where a linked Sparkle cannot be found.
bindings:
	$(WAILS) generate module

test:
	go test ./...

race:
	go test -race ./...

lint:
	gofmt -l . | grep -v '^frontend/' | (! grep .) || (echo "run gofmt -w"; exit 1)
	go vet ./...

scan:
	go build -ldflags "$(LDFLAGS)" -o bin/scan ./cmd/scan

helper:
	go build -ldflags "-X main.Build=$(VERSION)-$(BUILD)" -o bin/$(HELPER_BIN) ./cmd/helper

# ---------------------------------------------------------------- packaging

dist-dirs:
	@mkdir -p $(DIST_MAC) $(DIST)/windows

icons:
	./scripts/make-icons.sh

# Vendors Sparkle. Nothing here is committed: the framework is 30 MB of
# third-party binary, and pinning the version is enough to make the build
# reproducible.
sparkle:
	@if [ -d "$(SPARKLE_FW)" ]; then echo "Sparkle $(SPARKLE_VERSION) already vendored"; exit 0; fi; 	echo ">> fetching Sparkle $(SPARKLE_VERSION)"; 	mkdir -p $(SPARKLE_DIR) bin/sparkle; 	tmp=$$(mktemp -d); 	curl -fsSL -o "$$tmp/sparkle.tar.xz" 	  "https://github.com/sparkle-project/Sparkle/releases/download/$(SPARKLE_VERSION)/Sparkle-$(SPARKLE_VERSION).tar.xz"; 	tar -xJf "$$tmp/sparkle.tar.xz" -C "$$tmp"; 	cp -R "$$tmp/Sparkle.framework" $(SPARKLE_DIR)/; 	cp "$$tmp/bin/sign_update" "$$tmp/bin/generate_keys" bin/sparkle/ 2>/dev/null || true; 	rm -rf "$$tmp"; 	echo ">> vendored to $(SPARKLE_FW); rebuild to enable the updater"

$(APP_BUNDLE): helper dist-dirs
	@# -skipbindings: Wails generates them by running a throwaway binary out of
	@# /tmp, which cannot resolve an rpath into the app bundle and aborts as
	@# soon as Sparkle is linked. They are committed; `make bindings` refreshes
	@# them, building without the updater.
	$(WAILS) build -platform darwin/universal -ldflags "$(LDFLAGS)" -trimpath -clean -skipbindings 		$(if $(GO_TAGS),-tags $(GO_TAGS),)
	@echo ">> collecting into $(DIST_MAC)"
	rm -rf "$(APP_BUNDLE)"
	mv "$(WAILS_OUT)/$(APP).app" "$(APP_BUNDLE)"
	@echo ">> icon"
	cp build/darwin/iconfile.icns "$(APP_BUNDLE)/Contents/Resources/iconfile.icns"
	@# Substituted after packaging because Wails renders Info.plist itself.
	@# Left as the placeholder when no key is configured, which Sparkle reads
	@# as an invalid key and refuses every update — the safe direction to fail.
	@if [ -n "$(SPARKLE_PUBLIC_KEY)" ]; then \
		echo ">> update signing key"; \
		plutil -replace SUPublicEDKey -string "$(SPARKLE_PUBLIC_KEY)" \
			"$(APP_BUNDLE)/Contents/Info.plist"; \
		plutil -replace SUFeedURL -string "$(FEED_URL)" \
			"$(APP_BUNDLE)/Contents/Info.plist"; \
	else \
		echo ">> no SPARKLE_PUBLIC_KEY set: updates will be refused by this build"; \
	fi
	@echo ">> embedding the privileged helper"
	mkdir -p "$(APP_BUNDLE)/Contents/Library/LaunchDaemons"
	cp bin/$(HELPER_BIN) "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)"
	@if [ -d "$(SPARKLE_FW)" ]; then 		echo ">> embedding Sparkle $(SPARKLE_VERSION)"; 		mkdir -p "$(APP_BUNDLE)/Contents/Frameworks"; 		rm -rf "$(APP_BUNDLE)/Contents/Frameworks/Sparkle.framework"; 		cp -R "$(SPARKLE_FW)" "$(APP_BUNDLE)/Contents/Frameworks/"; 	fi

app: $(APP_BUNDLE) sign

# Signing runs inside-out and without --deep, which Apple discourages and which
# here would silently rewrite the helper's own identifier to the app's.
#
# The daemon plist is generated between the two signatures: it has to name the
# real Team ID, which is only knowable once something has actually been signed,
# and it has to be in place before the outer signature seals the bundle.
sign:
	@test -n "$(SIGN_ID)" || (echo "SIGN_ID is empty: no codesigning identity found"; exit 1)
	@# Sparkle ships its own nested executables — an XPC service and the
	@# installer — and the hardened runtime rejects any that is unsigned or
	@# signed by someone else. They go first, deepest first.
	@if [ -d "$(APP_BUNDLE)/Contents/Frameworks/Sparkle.framework" ]; then 		echo ">> signing Sparkle"; 		find "$(APP_BUNDLE)/Contents/Frameworks/Sparkle.framework" 			\( -name "*.xpc" -o -name "*.app" \) -print0 		| xargs -0 -I{} codesign --force --options runtime --timestamp 			--sign "$(SIGN_ID)" "{}"; 		codesign --force --options runtime --timestamp 			--sign "$(SIGN_ID)" 			"$(APP_BUNDLE)/Contents/Frameworks/Sparkle.framework"; 	fi
	@echo ">> signing the helper as $(SIGN_ID)"
	codesign --force --options runtime --timestamp \
		--sign "$(SIGN_ID)" \
		--identifier "$(HELPER_LABEL)" \
		"$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)"
	@team=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" 2>&1 \
		| sed -n 's/^TeamIdentifier=//p'); \
	if [ -z "$$team" ] || [ "$$team" = "not set" ]; then \
		if [ -n "$(ADHOC)" ]; then \
			echo ">> ad-hoc signature: no Team ID, so this build cannot install the helper"; \
			team="ADHOCNOTEAM"; \
		else \
			echo "the signature carries no Team ID; SMAppService cannot install this helper"; exit 1; \
		fi; \
	fi; \
	echo ">> daemon plist for team $$team"; \
	sed "s/@TEAM_ID@/$$team/g" build/darwin/helper.plist.in \
		> "$(APP_BUNDLE)/Contents/Library/LaunchDaemons/$(HELPER_LABEL).plist"
	@echo ">> signing the app"
	codesign --force --options runtime --timestamp \
		--sign "$(SIGN_ID)" \
		--identifier "$(BUNDLE_ID)" \
		--entitlements build/darwin/entitlements.plist \
		"$(APP_BUNDLE)"

verify:
	@echo ">> bundle layout"
	@test -x "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" \
		&& echo "   helper executable present" || (echo "   MISSING helper executable"; exit 1)
	@test -f "$(APP_BUNDLE)/Contents/Library/LaunchDaemons/$(HELPER_LABEL).plist" \
		&& echo "   daemon plist present" || (echo "   MISSING daemon plist"; exit 1)
	@plutil -lint "$(APP_BUNDLE)/Contents/Library/LaunchDaemons/$(HELPER_LABEL).plist" >/dev/null \
		&& echo "   daemon plist parses" || exit 1
	@echo ">> signatures"
	@codesign --verify --strict --verbose=2 "$(APP_BUNDLE)" 2>&1 | sed 's/^/   /'
	@codesign --verify --strict "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" \
		&& echo "   helper signature valid" || exit 1
	@echo ">> identifiers"
	@appid=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)" 2>&1 | sed -n 's/^Identifier=//p'); \
	helpid=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" 2>&1 | sed -n 's/^Identifier=//p'); \
	echo "   app    $$appid"; echo "   helper $$helpid"; \
	[ "$$appid" = "$(BUNDLE_ID)" ] || { echo "   app identifier is wrong"; exit 1; }; \
	[ "$$helpid" = "$(HELPER_LABEL)" ] || { echo "   helper identifier was overwritten (--deep?)"; exit 1; }
	@echo ">> team ids"
	@appteam=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)" 2>&1 | sed -n 's/^TeamIdentifier=//p'); \
	helpteam=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" 2>&1 | sed -n 's/^TeamIdentifier=//p'); \
	plistteam=$$(sed -n 's/.*<string>\([A-Z0-9]\{10\}\)<\/string>.*/\1/p' \
		"$(APP_BUNDLE)/Contents/Library/LaunchDaemons/$(HELPER_LABEL).plist" | head -1); \
	echo "   app    $$appteam"; echo "   helper $$helpteam"; echo "   plist  $$plistteam"; \
	if [ -n "$(ADHOC)" ]; then \
		echo "   ad-hoc build: no team, the helper cannot be installed from it"; \
	else \
		[ "$$appteam" = "$$helpteam" ] || { echo "   app and helper teams differ: SMAppService will fail silently"; exit 1; }; \
		[ "$$plistteam" = "$$appteam" ] || { echo "   the plist names a different team than the signature"; exit 1; }; \
	fi
	@echo ">> updater"
	@if [ -d "$(APP_BUNDLE)/Contents/Frameworks/Sparkle.framework" ]; then \
		key=$$(plutil -extract SUPublicEDKey raw "$(APP_BUNDLE)/Contents/Info.plist" 2>/dev/null); \
		echo "   Sparkle embedded"; \
		case "$$key" in \
			@SPARKLE_PUBLIC_KEY@|"") echo "   NO public key: this build will refuse every update";; \
			*) echo "   public key set ($${key%$${key#????}}…)";; \
		esac; \
	else echo "   not built with the updater (run make sparkle)"; fi
	@echo ">> gatekeeper (rejects until notarised; expected for a local build)"
	-@spctl --assess --type execute --verbose=2 "$(APP_BUNDLE)" 2>&1 | sed 's/^/   /'

# ---------------------------------------------------------------- windows

DIST_WIN := $(DIST)/windows
WIN_ARCHES ?= windows/amd64,windows/arm64

# Cross-compiles from macOS. Wails compiles its Windows backend as pure Go, and
# so does our platform layer, so no Windows host and no cgo toolchain are
# needed to produce the binaries. -tags is left empty on purpose: the updater
# is Sparkle, which is macOS-only, so a Windows build takes the stub driver.
windows: dist-dirs
	@echo ">> building $(WIN_ARCHES)"
	CGO_ENABLED=0 $(WAILS) build -platform $(WIN_ARCHES) \
		-ldflags "$(LDFLAGS)" -trimpath -clean -skipbindings
	@echo ">> collecting into $(DIST_WIN)"
	@for f in $(WAILS_OUT)/$(APP)*.exe; do \
		[ -e "$$f" ] || continue; \
		cp "$$f" "$(DIST_WIN)/"; \
		echo "   $$(basename $$f)"; \
	done
	@ls $(DIST_WIN)/*.exe >/dev/null 2>&1 || { echo "no Windows binary was produced"; exit 1; }

# An installer needs makensis, which Wails only warns about: without this check
# a release would publish binaries and quietly no installer at all.
windows-installer: dist-dirs
	@command -v makensis >/dev/null 2>&1 || { \
		echo "makensis not found — install it (brew install makensis) or the installer is skipped silently"; \
		exit 1; }
	CGO_ENABLED=0 $(WAILS) build -platform $(WIN_ARCHES) \
		-ldflags "$(LDFLAGS)" -trimpath -clean -skipbindings -nsis
	@cp $(WAILS_OUT)/*installer.exe $(DIST_WIN)/ 2>/dev/null || true
	@ls $(DIST_WIN)/*installer.exe

windows-verify:
	@echo ">> windows build"
	@ls -la $(DIST_WIN)/*.exe 2>/dev/null || { echo "   nothing built"; exit 1; }
	@for f in $(DIST_WIN)/*.exe; do \
		printf "   %-44s " "$$(basename $$f)"; \
		file -b "$$f" | cut -c1-60; \
	done
	@echo ">> the icon and version resources come from build/windows/"
	@test -f build/windows/icon.ico && echo "   icon.ico present" || echo "   MISSING icon.ico"

# ---------------------------------------------------------------- release

zip: dist-dirs
	@echo ">> zipping with ditto so the signature survives"
	ditto -c -k --keepParent "$(APP_BUNDLE)" "$(ZIP)"

notarize: zip
	xcrun notarytool submit "$(ZIP)" \
		--keychain-profile "$(NOTARY_PROFILE)" --wait

staple:
	xcrun stapler staple "$(APP_BUNDLE)"
	@echo ">> re-zipping the stapled bundle"
	rm -f "$(ZIP)"
	ditto -c -k --keepParent "$(APP_BUNDLE)" "$(ZIP)"

release: app verify notarize staple
	@echo
	@echo "Built, notarised and stapled: $(ZIP)"
	@echo "Next: ./scripts/release.sh $(VERSION) to sign the appcast and publish."

clean:
	rm -rf $(DIST) $(WAILS_OUT) bin frontend/dist
