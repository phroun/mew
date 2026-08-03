# KittyTK — build the desktop hosts and bump the build counter.
#
#   make            build both desktop hosts into bin/
#   make tui        build the terminal desktop host
#   make sdl        build the graphical (SDL) desktop host, software
#                   renderer only
#   make webgpu     build the graphical host WITH the WebGPU renderer
#                   compiled in (runtime-selected via kittytk.ini or
#                   --webgpu/--software)
#   make standalone build the graphical host with SDL3 EMBEDDED, so the
#                   binary runs with nothing installed
#   make test       run the test suite (both build tags)
#   make increment  bump the per-commit build counter
#   make clean      remove built binaries

GO ?= go

# Where built binaries land.
BIN_DIR := bin

# The file holding the auto-incremented build counter (see `increment`).
BUILD_FILE := core/version.go

.PHONY: all build tui sdl webgpu standalone test clean increment

# Default: build both desktop hosts - the project's deliverables.
all: build

build: tui sdl

# Terminal desktop host.
tui:
	$(GO) build -o $(BIN_DIR)/kittytk-tui ./cmd/kittytk-tui

# Graphical (SDL) desktop host, software renderer only.
#
# SDL3 is bound through purego, so nothing is linked at build time and
# no SDL dev headers are needed; the library is opened at RUN time from
# the system (see `standalone` to embed it instead).
sdl:
	$(GO) build -tags sdl -o $(BIN_DIR)/kittytk-sdl ./cmd/kittytk-sdl

# Graphical host with the WebGPU renderer compiled in. The engine is still
# chosen at runtime (kittytk.ini renderer=, or --webgpu / --software), so
# this binary replaces the plain sdl one rather than living beside it.
# Note: currently links on macOS/Windows; the Linux link is blocked by an
# upstream goffi relocation issue (packages compile and vet clean).
webgpu:
	$(GO) build -tags "sdl webgpu" -o $(BIN_DIR)/kittytk-sdl ./cmd/kittytk-sdl

# Self-contained graphical host: SDL3 is embedded in the executable and
# unpacked at startup, so the binary runs on a machine with no SDL
# installed. purego resolves symbols via dlopen and cannot link
# statically, so this is distribution-standalone rather than a true
# static link; it costs ~1MB of binary.
standalone:
	$(GO) build -tags "sdl webgpu sdlembed" -o $(BIN_DIR)/kittytk-sdl ./cmd/kittytk-sdl

# Full test suite across both build tags.
test:
	$(GO) test ./...
	$(GO) test -tags sdl ./...

# Remove built binaries.
clean:
	rm -rf $(BIN_DIR)

# Bump the per-commit Build counter in core/version.go. The file holds
# Build on a single line of the form `const Build = N`; the awk script finds
# that line, prints `const Build = N+1` in its place, and writes the result
# back. Version is left alone — bump it by hand for releases.
increment:
	@awk '/^const Build = [0-9]+$$/ { print "const Build = " $$4 + 1; next } { print }' $(BUILD_FILE) > $(BUILD_FILE).tmp
	@mv $(BUILD_FILE).tmp $(BUILD_FILE)
	@grep -E '^const Build' $(BUILD_FILE)
