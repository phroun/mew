# KittyTK — build the desktop hosts and bump the build counter.
#
#   make            build both desktop hosts into bin/
#   make tui        build the terminal desktop host
#   make sdl        build the graphical (SDL) desktop host  (needs SDL2 dev libs)
#   make test       run the test suite (both build tags)
#   make increment  bump the per-commit build counter
#   make clean      remove built binaries

GO ?= go

# Where built binaries land.
BIN_DIR := bin

# The file holding the auto-incremented build counter (see `increment`).
BUILD_FILE := core/version.go

.PHONY: all build tui sdl test clean increment

# Default: build both desktop hosts - the project's deliverables.
all: build

build: tui sdl

# Terminal desktop host.
tui:
	$(GO) build -o $(BIN_DIR)/kittytk-tui ./cmd/kittytk-tui

# Graphical (SDL) desktop host. Requires the sdl build tag and SDL2 dev libs.
sdl:
	$(GO) build -tags sdl -o $(BIN_DIR)/kittytk-sdl ./cmd/kittytk-sdl

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
