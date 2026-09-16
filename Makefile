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
SIGN_ID       ?= $(shell security find-identity -v -p codesigning | head -1 | sed -e 's/^[^"]*"//' -e 's/"$$//')
NOTARY_PROFILE?= diskspacesaver

BUILD_DIR     := build/bin
APP_BUNDLE    := $(BUILD_DIR)/$(APP).app
WAILS         := $(shell command -v wails 2>/dev/null || echo $(HOME)/go/bin/wails)
LDFLAGS       := -X main.Version=$(VERSION) -X main.Build=$(BUILD)

.PHONY: all help dev test race lint scan helper app sign verify notarize staple zip dmg release clean bindings

help:
	@echo "DiskSpaceSaver $(VERSION) (build $(BUILD))"
	@echo
	@echo "  make dev        run with hot reload"
	@echo "  make test       unit tests"
	@echo "  make race       unit tests under the race detector"
	@echo "  make scan       build the scanner CLI into bin/scan"
	@echo "  make helper     build the privileged helper into bin/"
	@echo "  make app        build and sign $(APP).app with the helper embedded"
	@echo "  make verify     check the bundle's signature and helper placement"
	@echo "  make release    app + notarize + staple + zip (needs a Developer ID)"
	@echo
	@echo "  TEAM_ID=$(TEAM_ID)"
	@echo "  SIGN_ID=$(SIGN_ID)"

all: app

dev:
	$(WAILS) dev

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

$(APP_BUNDLE): helper
	$(WAILS) build -platform darwin/universal -ldflags "$(LDFLAGS)" -trimpath -clean
	@echo ">> embedding the privileged helper"
	mkdir -p "$(APP_BUNDLE)/Contents/Library/LaunchDaemons"
	cp bin/$(HELPER_BIN) "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)"

app: $(APP_BUNDLE) sign

# Signing runs inside-out and without --deep, which Apple discourages and which
# here would silently rewrite the helper's own identifier to the app's.
#
# The daemon plist is generated between the two signatures: it has to name the
# real Team ID, which is only knowable once something has actually been signed,
# and it has to be in place before the outer signature seals the bundle.
sign:
	@test -n "$(SIGN_ID)" || (echo "SIGN_ID is empty: no codesigning identity found"; exit 1)
	@echo ">> signing the helper as $(SIGN_ID)"
	codesign --force --options runtime --timestamp \
		--sign "$(SIGN_ID)" \
		--identifier "$(HELPER_LABEL)" \
		"$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)"
	@team=$$(codesign -dv --verbose=4 "$(APP_BUNDLE)/Contents/MacOS/$(HELPER_BIN)" 2>&1 \
		| sed -n 's/^TeamIdentifier=//p'); \
	if [ -z "$$team" ] || [ "$$team" = "not set" ]; then \
		echo "the signature carries no Team ID; SMAppService cannot install this helper"; exit 1; \
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
	[ "$$appteam" = "$$helpteam" ] || { echo "   app and helper teams differ: SMAppService will fail silently"; exit 1; }; \
	[ "$$plistteam" = "$$appteam" ] || { echo "   the plist names a different team than the signature"; exit 1; }
	@echo ">> gatekeeper (rejects until notarised; expected for a local build)"
	-@spctl --assess --type execute --verbose=2 "$(APP_BUNDLE)" 2>&1 | sed 's/^/   /'

# ---------------------------------------------------------------- release

zip:
	@echo ">> zipping with ditto so the signature survives"
	ditto -c -k --keepParent "$(APP_BUNDLE)" "$(BUILD_DIR)/$(APP)-$(VERSION).zip"

notarize: zip
	xcrun notarytool submit "$(BUILD_DIR)/$(APP)-$(VERSION).zip" \
		--keychain-profile "$(NOTARY_PROFILE)" --wait

staple:
	xcrun stapler staple "$(APP_BUNDLE)"
	@echo ">> re-zipping the stapled bundle"
	rm -f "$(BUILD_DIR)/$(APP)-$(VERSION).zip"
	ditto -c -k --keepParent "$(APP_BUNDLE)" "$(BUILD_DIR)/$(APP)-$(VERSION).zip"

release: app verify notarize staple
	@echo
	@echo "Built, notarised and stapled: $(BUILD_DIR)/$(APP)-$(VERSION).zip"
	@echo "Next: ./scripts/release.sh $(VERSION) to sign the appcast and publish."

clean:
	rm -rf build/bin bin frontend/dist
