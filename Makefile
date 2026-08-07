# mew — build the editor binaries and bump the build counter.
#
#   make            build mew and mew-sdl into bin/
#   make mew        build the terminal (KittyTK TUI) host into bin/mew
#   make mew-sdl    build the graphical (SDL) host into bin/mew-sdl
#   make mew-plain  build the bare terminal editor (no host) into bin/mew-plain
#   make windows    cross-build the Windows console mew.exe into bin/
#   make windows-sdl build the Windows GUI mew-sdl.exe (with icon, and with
#                   mew.exe carried inside it so one file installs both)
#                   — pure Go, cross-builds from any host (no mingw/cgo)
#                   Then scripts\install-windows.ps1 installs the binaries and
#                   adds a Start Menu shortcut (see that script's header).
#   make install    build and install mew + mew-sdl into $(PREFIX)/bin
#   make uninstall  remove the installed binaries
#   make macapp     wrap the graphical binary in bin/mew.app (macOS icon + name)
#   make mew-sdl-universal  fat arm64+x86_64 mew-sdl (SDL3 embedded, no framework)
#   make macapp-universal   universal, self-contained bin/mew.app (no external SDL)
#   make install-macapp    install mew.app into $(MACAPP_DIR) (default /Applications)
#   make notarize   notarize + staple bin/mew.app for distribution (needs a
#                   Developer ID signature via CODESIGN_ID and NOTARY_PROFILE)
#   make check      go vet + full test suite (the pre-flight gate)
#   make test       run the test suite
#   make vet        run go vet
#   make increment  bump the per-commit build counter
#   make clean      remove built binaries
#
# mew (the terminal host) recognizes --window: it hands off to the mew-sdl
# binary sitting beside it, so `mew --window …` opens a graphical window while
# `mew …` stays in the terminal — one command, either surface. --detach does
# the same but returns the shell immediately (the window outlives it). Keep the
# two binaries in the same directory for the handoff to find mew-sdl.
#
# mew-sdl's graphical stack is pure Go: it loads SDL3 and wgpu-native through
# purego at runtime (no cgo for the window or the GPU), and with -tags sdlembed
# the SDL3 library is baked INTO the binary (Zyko0/go-sdl3's binsdl, one gzipped
# dylib/dll per GOOS/GOARCH), so the app ships self-contained with no framework
# to install. cgo is needed only for the Unix PTY (purfecterm's openpty); the
# Windows PTY is pure Go (ConPTY). There is no SDL2 anywhere — the old
# SDL2.framework/mingw machinery predated the SDL3 migration.

GO ?= go

# Where built binaries land.
BIN_DIR := bin

# Install location: binaries go in $(PREFIX)/bin (on PATH, co-located so the
# --window handoff finds mew-sdl). System prefixes need root - `sudo make
# install` - or install per-user with `make install PREFIX=$$HOME/.local`.
# DESTDIR supports staged/packaging installs.
PREFIX ?= /usr/local
DESTDIR ?=
INSTALL_BIN := $(DESTDIR)$(PREFIX)/bin

# Where `make install-macapp` puts mew.app on macOS. /Applications needs root;
# a per-user install with no sudo is `make install-macapp MACAPP_DIR=$$HOME/Applications`.
MACAPP_DIR ?= /Applications

# Build tags: the KittyTK host (kittytk) with the real mew-backed editor (mew),
# and the graphical SDL backend (sdl) for the windowed twin. The SDL host also
# carries `webgpu`, since the default renderer (see LoadHostConfig) needs it;
# software rendering still works when [window] renderer=software is set.
TUI_TAGS := kittytk mew
# sdlembed bakes libSDL3 into the binary (binsdl), so the graphical host ships
# as one self-contained file with no SDL to install — the same choice kittytk's
# own SDL host makes.
SDL_TAGS := sdl mew webgpu sdlembed

# The file holding the auto-incremented build counter (see `increment`).
BUILD_FILE := internal/version/version.go

# Windows cross-build target architecture (amd64 or arm64).
WINDOWS_ARCH ?= amd64

# rsrc turns assets/mew.ico into a .syso resource object the Go linker embeds
# into the Windows binary (the app icon). Pinned; fetched on demand via `go run`.
RSRC ?= go run github.com/akavel/rsrc@v0.10.2

# The icon resource object lives in the GRAPHICAL binary's package, so the icon
# lands on mew-sdl.exe (the windowed app shown in Explorer / the taskbar) rather
# than the console mew.exe. Arch-suffixed so the Go toolchain links it only for
# the matching windows build and never for other platforms.
WINDOWS_SYSO := app/cmd/mew-sdl/rsrc_windows_$(WINDOWS_ARCH).syso

# The console binary carried inside the graphical one, gzipped. A build
# artefact, not checked in; see the windows-sdl target.
CONSOLE_PAYLOAD := app/internal/selfinstall/payload/mew.exe.gz

.PHONY: all build mew mew-sdl mew-sdl-universal mew-plain windows windows-sdl install uninstall macapp macapp-universal install-macapp uninstall-macapp notarize check vet test clean increment

# Default: build both shipped binaries.
all: build
build: mew mew-sdl

# The terminal host: a maximized root mew editor in the terminal, serving the
# KittyTK protocol. Recognizes --window (hands off to mew-sdl beside it).
mew:
	$(GO) build -tags "$(TUI_TAGS)" -o $(BIN_DIR)/mew ./app/cmd/mew

# The graphical host: the same mew editor in an SDL window. SDL3 + wgpu load
# through purego at runtime (SDL3 embedded via sdlembed); cgo is used only for
# the Unix PTY, so a C compiler is required on macOS/Linux but no SDL headers.
mew-sdl:
	$(GO) build -tags "$(SDL_TAGS)" -o $(BIN_DIR)/mew-sdl ./app/cmd/mew-sdl

# The bare terminal editor - mew driving the terminal directly, none of the
# host machinery. The reference build for evaluating and comparing behavior.
mew-plain:
	$(GO) build -o $(BIN_DIR)/mew-plain ./app/cmd/mew

# Cross-build a Windows console executable of the terminal host. It is pure Go
# (the SDL/cgo path is only under -tags sdl), so this builds without cgo and
# keeps Go's default console subsystem — mew is a console editor, so no
# `-H windowsgui`, which would detach the console we need. No icon: this is the
# console binary, and the icon rides on the GUI mew-sdl.exe (see windows-sdl).
windows:
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 $(GO) build -tags "$(TUI_TAGS)" -o $(BIN_DIR)/mew.exe ./app/cmd/mew

# Build the Windows GUI host (mew-sdl.exe) with the embedded app icon. Pure Go,
# no cgo, no C toolchain: SDL3 is embedded (sdlembed) and loaded through purego,
# wgpu-native loads at runtime, and the Windows PTY is ConPTY via syscall — none
# of it uses cgo (unlike the Unix PTY). So this cross-builds from any host with
# just the Go toolchain — no mingw, no SDL2 dev package, no SDL2.dll to ship.
# The syso prerequisite carries the icon (the Go linker embeds it); -H windowsgui
# detaches the console (this is a windowed app, not a terminal one).

# The console binary, gzipped into the graphical one's package so a //go:embed
# can carry it (see app/internal/selfinstall/payload/README.md). Compressed in
# Go rather than through gzip(1): these targets are run ON Windows, where a
# shell pipeline is not something to assume.
#
# The point is a SINGLE downloaded mew-sdl.exe that installs both: the console
# build is what answers `mew --version` from a shell, and what belongs on the
# PATH. Without this step the graphical binary still builds and simply has
# nothing to extract.
$(CONSOLE_PAYLOAD): windows
	@mkdir -p $(dir $(CONSOLE_PAYLOAD))
	$(GO) run ./app/tools/gzipfile $(BIN_DIR)/mew.exe $(CONSOLE_PAYLOAD)

windows-sdl: $(WINDOWS_SYSO) $(CONSOLE_PAYLOAD)
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 $(GO) build -tags "$(SDL_TAGS) embedconsole" -ldflags "-H windowsgui" -o $(BIN_DIR)/mew-sdl.exe ./app/cmd/mew-sdl

# Build the Windows icon resource object from assets/mew.ico (regenerated when
# the icon changes). It lives in the mew-sdl package, so the Go linker embeds it
# into mew-sdl.exe automatically.
$(WINDOWS_SYSO): assets/mew.ico
	$(RSRC) -ico assets/mew.ico -arch $(WINDOWS_ARCH) -o $(WINDOWS_SYSO)

# Install both binaries onto PATH, co-located so `mew --window` can find
# mew-sdl beside it. Needs write access to $(PREFIX)/bin (use sudo, or set a
# per-user PREFIX). Depends on build, so it compiles first.
install: build
	install -d "$(INSTALL_BIN)"
	install -m 0755 "$(BIN_DIR)/mew" "$(INSTALL_BIN)/mew"
	install -m 0755 "$(BIN_DIR)/mew-sdl" "$(INSTALL_BIN)/mew-sdl"
	@echo "installed mew and mew-sdl to $(INSTALL_BIN)"

# Remove the installed binaries.
uninstall:
	rm -f "$(INSTALL_BIN)/mew" "$(INSTALL_BIN)/mew-sdl"
	@echo "removed mew and mew-sdl from $(INSTALL_BIN)"

# Wrap the graphical binary in a macOS .app bundle (bin/mew.app) so it gets a
# real application name and a Dock / task-switcher icon. Drop assets/mew.icns
# (or a 1024x1024 assets/mew.png, converted on macOS) for the icon. This wraps
# the native single-arch binary (SDL3 embedded, nothing to install); for a
# portable universal bundle use macapp-universal below.
# CODESIGN_ID: a "Developer ID Application: Name (TEAMID)" identity to sign the
# bundle for distribution (hardened runtime + timestamp; see macapp.sh). Empty
# = ad-hoc sign (runs locally only). Passed through to macapp.sh by the macapp*
# targets. Notarize + staple afterwards with `make notarize`.
CODESIGN_ID ?=
macapp: mew-sdl
	CODESIGN_ID="$(CODESIGN_ID)" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

# --- macOS universal build (Intel + Apple Silicon) --------------------------
# SDL3 is embedded per-arch (binsdl carries a darwin arm64 AND a darwin amd64
# libSDL3), so a universal build needs no external framework at all — each arch
# slice bakes in its own SDL3, and lipo joins them. Override MAC_UNIVERSAL_ARCHS
# to build a subset.
MAC_UNIVERSAL_ARCHS ?= arm64 amd64

# Build mew-sdl for each arch and lipo them into one fat binary at bin/mew-sdl.
# Run on macOS (needs clang + lipo). The only cgo in the build is the Unix PTY,
# so CC="clang -arch <arch>" cross-compiles that C for each slice against the
# macOS SDK — no SDL headers, no pkg-config, no framework, no rpath. lipo strips
# signatures, so re-apply an ad-hoc one (arm64 refuses to run unsigned).
# Distribution still needs Developer ID + notarization; ad-hoc only satisfies
# "runs on this machine".
mew-sdl-universal:
	@command -v lipo >/dev/null || { echo "lipo not found — run on macOS"; exit 1; }
	@for a in $(MAC_UNIVERSAL_ARCHS); do \
	  ca=$$( [ "$$a" = amd64 ] && echo x86_64 || echo "$$a" ); \
	  echo "building mew-sdl for darwin/$$a ($$ca)"; \
	  GOOS=darwin GOARCH=$$a CGO_ENABLED=1 \
	    CC="clang -arch $$ca" \
	    $(GO) build -tags "$(SDL_TAGS)" -o "$(BIN_DIR)/mew-sdl.$$a" ./app/cmd/mew-sdl || exit 1; \
	done
	lipo -create $(foreach a,$(MAC_UNIVERSAL_ARCHS),$(BIN_DIR)/mew-sdl.$(a)) -output "$(BIN_DIR)/mew-sdl"
	@rm -f $(foreach a,$(MAC_UNIVERSAL_ARCHS),$(BIN_DIR)/mew-sdl.$(a))
	@codesign --force --sign - "$(BIN_DIR)/mew-sdl" 2>/dev/null || echo "note: codesign unavailable; arm64 may refuse to run unsigned"
	@lipo -info "$(BIN_DIR)/mew-sdl"

# Wrap the universal binary in a .app. With SDL3 embedded the bundle is already
# self-contained and runs on Intel and Apple Silicon with nothing to install —
# macapp.sh just adds the icon, Info.plist, and an ad-hoc signature.
macapp-universal: mew-sdl-universal
	CODESIGN_ID="$(CODESIGN_ID)" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

# Notarize and staple bin/mew.app for distribution. Run AFTER building it signed
# with a Developer ID (make macapp-universal CODESIGN_ID="Developer ID Application: …").
# Needs a stored notarytool credential profile — create it once with:
#   xcrun notarytool store-credentials <profile> \
#     --apple-id you@example.com --team-id TEAMID --password <app-specific-pw>
# then set NOTARY_PROFILE to <profile>. notarytool uploads a zip; stapler then
# attaches the ticket to the .app itself so it validates offline. Ship the .app
# (e.g. zipped or in a dmg) after this.
NOTARY_PROFILE ?=
notarize:
	@test -n "$(NOTARY_PROFILE)" || { echo "set NOTARY_PROFILE (see the target comment)"; exit 1; }
	@test -d "$(BIN_DIR)/mew.app" || { echo "no $(BIN_DIR)/mew.app — run: make macapp-universal CODESIGN_ID=\"Developer ID Application: …\""; exit 1; }
	ditto -c -k --keepParent "$(BIN_DIR)/mew.app" "$(BIN_DIR)/mew-notarize.zip"
	xcrun notarytool submit "$(BIN_DIR)/mew-notarize.zip" --keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple "$(BIN_DIR)/mew.app"
	xcrun stapler validate "$(BIN_DIR)/mew.app"
	@rm -f "$(BIN_DIR)/mew-notarize.zip"
	@echo "notarized + stapled $(BIN_DIR)/mew.app — ready to distribute"

# Install the bundle into $(MACAPP_DIR) (default /Applications). The terminal
# mew (once installed on PATH) launches this bundle for --window/--detach when
# it is present, so the window gets the Dock icon and name. Needs write access
# to $(MACAPP_DIR) - use sudo, or a per-user MACAPP_DIR=$$HOME/Applications.
install-macapp: macapp
	mkdir -p "$(MACAPP_DIR)"
	rm -rf "$(MACAPP_DIR)/mew.app"
	cp -R "$(BIN_DIR)/mew.app" "$(MACAPP_DIR)/mew.app"
	@echo "installed mew.app to $(MACAPP_DIR)"

# Remove the installed bundle.
uninstall-macapp:
	rm -rf "$(MACAPP_DIR)/mew.app"
	@echo "removed mew.app from $(MACAPP_DIR)"

# Pre-flight gate: vet then the full test suite. Run before committing, and
# reused by CI / hooks so there is one definition of "the checks pass".
check: vet test

# Static analysis.
vet:
	$(GO) vet ./...

# Full test suite.
test:
	$(GO) test ./...

# Remove built binaries and the generated Windows icon resource object.
clean:
	rm -rf $(BIN_DIR)
	rm -f app/cmd/mew/rsrc_windows_*.syso app/cmd/mew-sdl/rsrc_windows_*.syso
	rm -f $(CONSOLE_PAYLOAD)

# Bump the per-commit Build counter in internal/version/version.go. The file
# holds Build on a single line of the form `const Build = N`; the awk script
# finds that line, prints `const Build = N+1` in its place, and writes the
# result back. Version is left alone — bump it by hand for releases.
increment:
	@awk '/^const Build = [0-9]+$$/ { print "const Build = " $$4 + 1; next } { print }' $(BUILD_FILE) > $(BUILD_FILE).tmp
	@mv $(BUILD_FILE).tmp $(BUILD_FILE)
	@grep -E '^const Build' $(BUILD_FILE)
