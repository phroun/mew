# mew — build the editor binaries and bump the build counter.
#
#   make            build mew and mew-sdl into bin/
#   make mew        build the terminal (KittyTK TUI) host into bin/mew
#   make mew-sdl    build the graphical (SDL) host into bin/mew-sdl
#   make mew-plain  build the bare terminal editor (no host) into bin/mew-plain
#   make windows    cross-build the Windows console mew.exe into bin/
#   make install    build and install mew + mew-sdl into $(PREFIX)/bin
#   make uninstall  remove the installed binaries
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

# Build tags: the KittyTK host (kittytk) with the real mew-backed editor (mew),
# and the graphical SDL backend (sdl) for the windowed twin.
TUI_TAGS := kittytk mew
SDL_TAGS := sdl mew

# The file holding the auto-incremented build counter (see `increment`).
BUILD_FILE := internal/version/version.go

# Windows cross-build target architecture (amd64 or arm64).
WINDOWS_ARCH ?= amd64

.PHONY: all build mew mew-sdl mew-plain windows install uninstall check vet test clean increment

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
# `-H windowsgui`, which would detach the console we need. (mew-sdl is not
# cross-built: SDL2 + cgo don't cross-compile cleanly.)
windows:
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 $(GO) build -tags "$(TUI_TAGS)" -o $(BIN_DIR)/mew.exe ./app/cmd/mew

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

# Pre-flight gate: vet then the full test suite. Run before committing, and
# reused by CI / hooks so there is one definition of "the checks pass".
check: vet test

# Static analysis.
vet:
	$(GO) vet ./...

# Full test suite.
test:
	$(GO) test ./...

# Remove built binaries.
clean:
	rm -rf $(BIN_DIR)

# Bump the per-commit Build counter in internal/version/version.go. The file
# holds Build on a single line of the form `const Build = N`; the awk script
# finds that line, prints `const Build = N+1` in its place, and writes the
# result back. Version is left alone — bump it by hand for releases.
increment:
	@awk '/^const Build = [0-9]+$$/ { print "const Build = " $$4 + 1; next } { print }' $(BUILD_FILE) > $(BUILD_FILE).tmp
	@mv $(BUILD_FILE).tmp $(BUILD_FILE)
	@grep -E '^const Build' $(BUILD_FILE)
