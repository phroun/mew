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
#   make mew-sdl-universal  fat arm64+x86_64 mew-sdl (loads SDL3 at runtime)
#   make macapp-universal   universal bin/mew.app; MACAPP_SDL3=<dylib> to bundle SDL3
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
# mew-sdl's graphical stack is pure Go: it loads the platform's installed SDL3
# and wgpu-native through purego at runtime (no cgo for the window or the GPU).
# cgo is needed only for the Unix PTY (purfecterm's openpty); the Windows PTY is
# pure Go (ConPTY). There is no SDL2 anywhere — the old SDL2.framework/mingw
# machinery predated the SDL3 migration.

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
# The SDL host loads the platform's installed SDL3 at runtime (purego dlopen).
# NOT sdlembed: embedding a second libSDL3 alongside a system one (Homebrew on
# macOS) loads BOTH — duplicate Obj-C classes, and the Metal layer lookup fails.
# Self-contained bundling is a separate, per-platform effort (a signed
# SDL3.framework on macOS), not the embed-and-extract path.
SDL_TAGS := sdl mew webgpu

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

.PHONY: all build mew mew-sdl sdl3 mew-sdl-universal mew-plain windows windows-sdl install uninstall macapp macapp-universal install-macapp uninstall-macapp notarize check vet test clean increment

# Default: build both shipped binaries.
all: build
build: mew mew-sdl

# The terminal host: a maximized root mew editor in the terminal, serving the
# KittyTK protocol. Recognizes --window (hands off to mew-sdl beside it).
mew:
	$(GO) build -tags "$(TUI_TAGS)" -o $(BIN_DIR)/mew ./app/cmd/mew

# The graphical host: the same mew editor in an SDL window. SDL3 + wgpu load
# through purego at runtime from the system install; cgo is used only for the
# Unix PTY, so a C compiler is required on macOS/Linux but no SDL headers.
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
# no cgo, no C toolchain: SDL3 and wgpu-native load through purego at runtime and
# the Windows PTY is ConPTY via syscall — none of it uses cgo (unlike the Unix
# PTY). So this cross-builds from any host with just the Go toolchain — no mingw,
# no SDL2 dev package. SDL3.dll is loaded from the system at runtime (ship it
# beside the exe, or install it, as the old SDL2 build likewise expected).
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
# the native single-arch binary (loads system SDL3 at runtime); for a
# portable universal bundle use macapp-universal below.
# CODESIGN_ID: a "Developer ID Application: Name (TEAMID)" identity to sign the
# bundle for distribution (hardened runtime + timestamp; see macapp.sh). Empty
# = ad-hoc sign (runs locally only). Passed through to macapp.sh by the macapp*
# targets. Notarize + staple afterwards with `make notarize`.
CODESIGN_ID ?=

# MACAPP_SDL3: path to a libSDL3.dylib to embed in the bundle so the .app is a
# self-contained installer. macapp.sh copies it to Contents/Frameworks/
# libSDL3.dylib, which the host's loader prefers over any system SDL3. Leave it
# UNSET and macapp-universal fetches an official universal SDL3 itself (see the
# sdl3 target); set it to override with your own dylib, or to "" via the recipe
# to embed nothing and fall back to a system SDL3.
MACAPP_SDL3 ?=

# The official SDL3 runtime to bundle, fetched (not committed) into a gitignored
# cache by scripts/fetch-sdl3.sh. Pin a different release with SDL3_VERSION; the
# default tracks a known-good libsdl.org release. SDL3_URL overrides the asset
# URL outright if the naming ever changes.
SDL3_VERSION ?= 3.4.12
SDL3_CACHE ?= build/sdl3
SDL3_DYLIB := $(SDL3_CACHE)/libSDL3.dylib

# Fetch the universal SDL3 runtime into the cache (macOS). Idempotent — a cached
# copy is reused; delete $(SDL3_CACHE) to refresh.
sdl3:
	./scripts/fetch-sdl3.sh "$(SDL3_VERSION)" "$(SDL3_CACHE)"
macapp: mew-sdl
	CODESIGN_ID="$(CODESIGN_ID)" MACAPP_SDL3="$(MACAPP_SDL3)" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

# --- macOS universal build (Intel + Apple Silicon) --------------------------
# Builds a fat arm64+x86_64 mew-sdl. It loads SDL3 at runtime (purego dlopen),
# preferring a copy bundled in the .app; pass MACAPP_SDL3 to macapp-universal to
# embed one and make the bundle a self-contained installer. Override
# MAC_UNIVERSAL_ARCHS to build a subset.
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

# Wrap the universal binary in a self-contained .app: it fetches an official
# universal SDL3 (the sdl3 target) and embeds it at Contents/Frameworks/
# libSDL3.dylib, which the host's loader prefers over any system SDL3 — so the
# bundle carries its own runtime and does not collide with a Homebrew SDL3 on
# the target machine. (-tags sdlembed is NOT used: it extracts a temp copy that
# gapfill still loads a system SDL3 alongside — duplicate Obj-C classes, and the
# Metal layer lookup fails.)
#
# Set MACAPP_SDL3=/path/to/libSDL3.dylib to bundle your own instead of fetching.
# If the fetch fails (e.g. an unreachable release), the app is still built but
# WITHOUT a bundled runtime, so it then needs a system SDL3 (brew install sdl3).
macapp-universal: mew-sdl-universal
	@sdl3="$(MACAPP_SDL3)"; \
	if [ -z "$$sdl3" ]; then \
	  if ./scripts/fetch-sdl3.sh "$(SDL3_VERSION)" "$(SDL3_CACHE)"; then \
	    sdl3="$(SDL3_DYLIB)"; \
	  else \
	    echo "macapp-universal: SDL3 fetch failed — building WITHOUT a bundled runtime (the .app will need a system SDL3)"; \
	  fi; \
	fi; \
	CODESIGN_ID="$(CODESIGN_ID)" MACAPP_SDL3="$$sdl3" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

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
