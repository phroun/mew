# mew — build the editor binaries and bump the build counter.
#
#   make            build mew and mew-sdl into bin/
#   make mew        build the terminal (KittyTK TUI) host into bin/mew
#   make mew-sdl    build the graphical (SDL) host into bin/mew-sdl
#   make mew-plain  build the bare terminal editor (no host) into bin/mew-plain
#   make windows    cross-build the Windows console mew.exe into bin/
#   make windows-sdl build the Windows GUI mew-sdl.exe (with icon) — run on Windows
#                   Then scripts\install-windows.ps1 installs the binaries and
#                   adds a Start Menu shortcut (see that script's header).
#   make install    build and install mew + mew-sdl into $(PREFIX)/bin
#   make uninstall  remove the installed binaries
#   make macapp     wrap the graphical binary in bin/mew.app (macOS icon + name)
#   make mew-sdl-universal  fat arm64+x86_64 mew-sdl (needs a universal SDL2.framework)
#   make macapp-universal   universal bin/mew.app with SDL2.framework embedded
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
# mew-sdl requires SDL2 and cgo; the others are pure Go.

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
# and the graphical SDL backend (sdl) for the windowed twin.
TUI_TAGS := kittytk mew
SDL_TAGS := sdl mew

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

.PHONY: all build mew mew-sdl mew-sdl-universal mew-plain windows windows-sdl install uninstall macapp macapp-universal install-macapp uninstall-macapp notarize check vet test clean increment

# Default: build both shipped binaries.
all: build
build: mew mew-sdl

# The terminal host: a maximized root mew editor in the terminal, serving the
# KittyTK protocol. Recognizes --window (hands off to mew-sdl beside it).
mew:
	$(GO) build -tags "$(TUI_TAGS)" -o $(BIN_DIR)/mew ./app/cmd/mew

# The graphical host: the same mew editor in an SDL window. Needs SDL2 + cgo.
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

# Build the Windows GUI host (mew-sdl.exe) with the embedded app icon. It uses
# SDL2 + cgo, so it needs a WINDOWS C toolchain AND SDL2's Windows/mingw
# development headers+libs — the plain host gcc (and the host's Linux SDL2) can't
# produce it. go-sdl2 links Windows SDL2 with a plain `-lSDL2` and includes
# <SDL2/SDL.h>, so it just needs those on the compiler's search path (it does NOT
# use pkg-config on Windows). Two ways to satisfy that:
#
# DEFAULT here is a cross build from macOS/Linux that STATIC-links SDL2, so the
# only prerequisites are the mingw-w64 cross toolchain and the SDL2 mingw dev
# package unpacked under $(SDL2_DIR) — `make windows-sdl` then produces a single
# self-contained mew-sdl.exe with no SDL2.dll to ship. Override the variables
# below to point at your SDL2, pick a version, or change linkage; clear them for
# a native Windows build (see the examples under the variables).
#
# Static link details: it links libSDL2.a plus the Windows system libs it needs,
# using `-static` (a global link MODE, immune to flag ordering — otherwise
# go-sdl2's own bare `-lSDL2` can grab the dynamic import lib first) inside a
# --start-group/--end-group so SDL2 and its deps resolve regardless of order, and
# drops -lSDL2main since the Go runtime owns the entry point. The exe is fully
# static (libgcc/winpthread baked in too). SDL2_STATIC= (empty) instead links
# dynamically, and then mew-sdl.exe needs SDL2.dll beside it at runtime.
#
# go-sdl2 links Windows SDL2 with a plain `-lSDL2` and includes <SDL2/SDL.h> (no
# pkg-config on Windows), so it just needs those on the compiler search path;
# SDL2_MINGW is turned into CGO_CFLAGS/CGO_LDFLAGS below. The syso prerequisite
# carries the icon (the Go linker embeds it); -H windowsgui detaches the console
# (this is a windowed app, not a terminal one).
#
# Native Windows build (mingw gcc + SDL2 already in the sysroot), dynamic:
#     make windows-sdl WINDOWS_CC= SDL2_MINGW= SDL2_STATIC=
# Cross build with a different SDL2 location:
#     make windows-sdl SDL2_DIR=/path/to/SDL2-2.32.10/x86_64-w64-mingw32/..  # or
#     make windows-sdl SDL2_MINGW=/abs/path/x86_64-w64-mingw32
WINDOWS_CC ?= x86_64-w64-mingw32-gcc
SDL2_STATIC ?= 1

# Where the SDL2 mingw dev package (SDL2-devel-<ver>-mingw from libsdl.org) is
# unpacked. SDL2_MINGW is its amd64 arch subdir — the directory holding
# include/SDL2/SDL.h and lib/libSDL2.a. Point SDL2_DIR (or SDL2_VERSION, or
# SDL2_MINGW itself) at your copy; SDL2_DIR defaults under $(HOME).
SDL2_VERSION ?= 2.32.10
SDL2_DIR ?= $(HOME)/projects/vendor/SDL2-$(SDL2_VERSION)
SDL2_MINGW ?= $(SDL2_DIR)/x86_64-w64-mingw32

# Windows system libs SDL2's static archive pulls in (mirrors
# `sdl2-config --static-libs`, minus -lSDL2main). Only used for a static link.
SDL2_WIN_SYSLIBS := -lm -ldinput8 -ldxguid -ldxerr8 -luser32 -lgdi32 -lwinmm -limm32 -lole32 -loleaut32 -lshell32 -lsetupapi -lversion -luuid

# Assemble the cgo flags for the cross build from SDL2_MINGW (+ SDL2_STATIC).
WINDOWS_SDL_CFLAGS  := $(if $(SDL2_MINGW),-I$(SDL2_MINGW)/include)
WINDOWS_SDL_LDFLAGS := $(if $(SDL2_MINGW),-L$(SDL2_MINGW)/lib)
ifneq ($(SDL2_STATIC),)
WINDOWS_SDL_LDFLAGS += -static -Wl,--start-group -lSDL2 $(SDL2_WIN_SYSLIBS) -Wl,--end-group
endif

# The env prefix for `go build`: CC to cross-compile, CGO_CFLAGS/CGO_LDFLAGS to
# find (and optionally static-link) SDL2. Each part appears only when relevant.
WINDOWS_SDL_ENV = $(if $(WINDOWS_CC),CC=$(WINDOWS_CC) )$(if $(strip $(WINDOWS_SDL_CFLAGS)),CGO_CFLAGS="$(WINDOWS_SDL_CFLAGS)" )$(if $(strip $(WINDOWS_SDL_LDFLAGS)),CGO_LDFLAGS="$(WINDOWS_SDL_LDFLAGS)" )

windows-sdl: $(WINDOWS_SYSO)
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=1 $(WINDOWS_SDL_ENV)$(GO) build -tags "$(SDL_TAGS)" -ldflags "-H windowsgui" -o $(BIN_DIR)/mew-sdl.exe ./app/cmd/mew-sdl

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
# the native single-arch binary (Homebrew SDL2, linked dynamically); for a
# portable universal bundle use macapp-universal below.
# CODESIGN_ID: a "Developer ID Application: Name (TEAMID)" identity to sign the
# bundle for distribution (hardened runtime + timestamp; see macapp.sh). Empty
# = ad-hoc sign (runs locally only). Passed through to macapp.sh by the macapp*
# targets. Notarize + staple afterwards with `make notarize`.
CODESIGN_ID ?=
macapp: mew-sdl
	CODESIGN_ID="$(CODESIGN_ID)" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

# --- macOS universal build (Intel + Apple Silicon) --------------------------
# A universal mew-sdl needs SDL2 for BOTH arm64 and x86_64. The easy source is
# the universal SDL2.framework from libsdl.org's macOS dmg (it carries both
# slices; the dmg installs it to /Library/Frameworks or ~/Library/Frameworks).
# Point MACAPP_SDL2_FW at the directory CONTAINING SDL2.framework. Override
# MAC_UNIVERSAL_ARCHS to build a subset.
MACAPP_SDL2_FW ?= $(HOME)/Library/Frameworks
MAC_UNIVERSAL_ARCHS ?= arm64 amd64

# Build mew-sdl for each arch against the universal framework and lipo them into
# one fat binary at bin/mew-sdl. Run on macOS (needs clang + lipo). PKG_CONFIG is
# neutralized (/usr/bin/true) so go-sdl2's darwin `pkg-config: sdl2` directive
# doesn't drag in the single-arch Homebrew SDL2 — the CGO flags point at the
# framework instead. The @rpath that lets the binary find the embedded framework
# is added AFTER linking (install_name_tool -add_rpath), not via CGO_LDFLAGS: the
# SDL build pulls in several cgo packages and an env CGO_LDFLAGS is attributed to
# each, so a link-time -rpath would reach the linker several times (harmless, but
# it warns "duplicate -rpath ignored"). Adding it once post-link is exactly one
# LC_RPATH and no warnings. lipo strips signatures, so re-apply an ad-hoc one
# (arm64 refuses to run unsigned). Distribution still needs Developer ID +
# notarization; ad-hoc only satisfies "runs on this machine".
mew-sdl-universal:
	@command -v lipo >/dev/null || { echo "lipo not found — run on macOS"; exit 1; }
	@test -d "$(MACAPP_SDL2_FW)/SDL2.framework" || { echo "SDL2.framework not in $(MACAPP_SDL2_FW) — set MACAPP_SDL2_FW"; exit 1; }
	@for a in $(MAC_UNIVERSAL_ARCHS); do \
	  ca=$$( [ "$$a" = amd64 ] && echo x86_64 || echo "$$a" ); \
	  echo "building mew-sdl for darwin/$$a ($$ca)"; \
	  GOOS=darwin GOARCH=$$a CGO_ENABLED=1 \
	    CC="clang -arch $$ca" \
	    PKG_CONFIG=/usr/bin/true \
	    CGO_CFLAGS="-F$(MACAPP_SDL2_FW) -I$(MACAPP_SDL2_FW)/SDL2.framework/Headers" \
	    CGO_LDFLAGS="-F$(MACAPP_SDL2_FW) -framework SDL2" \
	    $(GO) build -tags "$(SDL_TAGS)" -o "$(BIN_DIR)/mew-sdl.$$a" ./app/cmd/mew-sdl || exit 1; \
	done
	lipo -create $(foreach a,$(MAC_UNIVERSAL_ARCHS),$(BIN_DIR)/mew-sdl.$(a)) -output "$(BIN_DIR)/mew-sdl"
	@rm -f $(foreach a,$(MAC_UNIVERSAL_ARCHS),$(BIN_DIR)/mew-sdl.$(a))
	install_name_tool -add_rpath @executable_path/../Frameworks "$(BIN_DIR)/mew-sdl"
	@codesign --force --sign - "$(BIN_DIR)/mew-sdl" 2>/dev/null || echo "note: codesign unavailable; arm64 may refuse to run unsigned"
	@lipo -info "$(BIN_DIR)/mew-sdl"

# Wrap the universal binary in a .app AND embed the universal SDL2.framework, so
# the bundle is self-contained and runs on Intel and Apple Silicon with no
# external SDL2. macapp.sh reads MACAPP_SDL2_FW to embed the framework and
# ad-hoc-signs the bundle.
macapp-universal: mew-sdl-universal
	MACAPP_SDL2_FW="$(MACAPP_SDL2_FW)" CODESIGN_ID="$(CODESIGN_ID)" ./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

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

# Bump the per-commit Build counter in internal/version/version.go. The file
# holds Build on a single line of the form `const Build = N`; the awk script
# finds that line, prints `const Build = N+1` in its place, and writes the
# result back. Version is left alone — bump it by hand for releases.
increment:
	@awk '/^const Build = [0-9]+$$/ { print "const Build = " $$4 + 1; next } { print }' $(BUILD_FILE) > $(BUILD_FILE).tmp
	@mv $(BUILD_FILE).tmp $(BUILD_FILE)
	@grep -E '^const Build' $(BUILD_FILE)
