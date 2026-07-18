# mew — build the editor and bump the build counter.
#
#   make            build the mew binary for the host into bin/
#   make windows    cross-build the Windows console .exe into bin/
#   make cross      build both the host binary and the Windows console .exe
#   make check      go vet + full test suite (the pre-flight gate)
#   make test       run the test suite
#   make vet        run go vet
#   make increment  bump the per-commit build counter
#   make clean      remove built binaries

GO ?= go

# Where built binaries land.
BIN_DIR := bin

# The file holding the auto-incremented build counter (see `increment`).
BUILD_FILE := internal/version/version.go

# Windows cross-build target architecture (amd64 or arm64).
WINDOWS_ARCH ?= amd64

.PHONY: all build windows cross check vet test clean increment

# Default: build the editor - the project's deliverable.
all: build

build:
	$(GO) build -o $(BIN_DIR)/mew ./cmd/mew

# Cross-build a Windows console executable. mew is a console editor, so this
# uses Go's default (console) subsystem — no `-H windowsgui`, which would
# detach the console we need. Raw-mode input and terminal resizing are handled
# per-platform (golang.org/x/term drives the Windows console; the SIGWINCH
# resize watcher is a no-op off unix), so no CGO is required.
windows:
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 $(GO) build -o $(BIN_DIR)/mew.exe ./cmd/mew

# Build the host binary and the Windows console executable together.
cross: build windows

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
