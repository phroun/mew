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
#   make install-macapp    install mew.app into $(MACAPP_DIR) (default /Applications)
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

.PHONY: all build mew mew-sdl mew-plain windows windows-sdl install uninstall macapp install-macapp uninstall-macapp check vet test clean increment

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
#   • On Windows: install Go, MSYS2's mingw-w64 gcc, and the SDL2 dev libs into
#     the mingw sysroot, then `make windows-sdl` — the default gcc on PATH is
#     already a Windows compiler and finds SDL2 with no extra flags.
#
#   • Cross-compile from Linux/macOS: install the mingw-w64 toolchain, download
#     SDL2's mingw dev package (SDL2-devel-<ver>-mingw from libsdl.org), and point
#     WINDOWS_CC at the cross gcc and SDL2_MINGW at the matching arch subdir:
#         make windows-sdl WINDOWS_CC=x86_64-w64-mingw32-gcc \
#              SDL2_MINGW=/path/SDL2-<ver>/x86_64-w64-mingw32
#     SDL2_MINGW is the dir holding include/SDL2/SDL.h and lib/libSDL2.dll.a; the
#     recipe turns it into CGO_CFLAGS/CGO_LDFLAGS. (If SDL2 is already in the
#     mingw sysroot, leave SDL2_MINGW unset.)
#
# At RUNTIME mew-sdl.exe needs SDL2.dll beside it (from the dev package's bin/, or
# the SDL2 runtime zip). The syso prerequisite carries the icon (the Go linker
# embeds it automatically); -H windowsgui detaches the console — this is a
# windowed app, not a terminal one. WINDOWS_CC/SDL2_MINGW are empty by default
# (native Windows build); set them to cross-compile.
WINDOWS_CC ?=
SDL2_MINGW ?=
windows-sdl: $(WINDOWS_SYSO)
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=1 $(if $(WINDOWS_CC),CC=$(WINDOWS_CC) )$(if $(SDL2_MINGW),CGO_CFLAGS="-I$(SDL2_MINGW)/include" CGO_LDFLAGS="-L$(SDL2_MINGW)/lib" )$(GO) build -tags "$(SDL_TAGS)" -ldflags "-H windowsgui" -o $(BIN_DIR)/mew-sdl.exe ./app/cmd/mew-sdl

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
# (or a 1024x1024 assets/mew.png, converted on macOS) for the icon.
macapp: mew-sdl
	./scripts/macapp.sh "$(BIN_DIR)/mew-sdl" assets "$(BIN_DIR)"

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
