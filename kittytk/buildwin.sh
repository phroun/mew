#!/bin/bash
# Cross-build the graphical (SDL) KittyTK host as a Windows executable.
#
# SDL3 and the WebGPU renderer's native library are both bound through
# purego/goffi and opened at RUN time, so NOTHING is linked at build time:
# no CGO, no MinGW toolchain, no SDL import libraries or dev headers. This is
# a plain Go cross-compile — the SDL2 + CGO static-link dance the previous
# version of this script did is obsolete since the SDL3 migration.
#
# The `sdlembed` tag bundles SDL3 into the .exe (unpacked at startup) so it
# runs on a machine with nothing installed; drop it to load a system
# SDL3.dll from the PATH instead. See the Makefile for the native targets.
set -euo pipefail

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -tags "sdl webgpu sdlembed" -ldflags "-H windowsgui" \
  -o dist/kittytk-sdl.exe ./cmd/kittytk-sdl

echo "built dist/kittytk-sdl.exe"
