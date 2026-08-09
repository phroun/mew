//go:build windows && wgpuembed

// Package wgpuembed carries the wgpu-native runtime INSIDE the Windows binary so
// the graphical host is self-contained: no wgpu_native.dll to install, ship in a
// lib/ folder, or place on PATH. The DLL is fetched at build time
// (scripts/fetch-wgpu.sh) into this package and embedded; at startup it is
// unpacked to a temp file and WGPU_NATIVE_PATH is pointed at it, which
// go-webgpu's loader honors ahead of its lib/ and PATH search.
//
// This mirrors what -tags sdlembed does for SDL3 (binsdl). Together they make
// mew-sdl.exe a single self-contained file, the whole reason the Windows host
// exists as one download.
package wgpuembed

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed wgpu_native.dll
var wgpuDLL []byte

func init() {
	// An explicit path always wins — never clobber a WGPU_NATIVE_PATH the user
	// set (a system wgpu, a debugging build).
	if os.Getenv("WGPU_NATIVE_PATH") != "" {
		return
	}
	// Unpack once to a temp file and point the loader at it. On any failure fall
	// back silently to go-webgpu's own search (lib/, cwd, PATH) — embedding is an
	// improvement over that, not a replacement that may strand the host.
	dir, err := os.MkdirTemp("", "mew-wgpu-")
	if err != nil {
		return
	}
	path := filepath.Join(dir, "wgpu_native.dll")
	if err := os.WriteFile(path, wgpuDLL, 0o644); err != nil {
		return
	}
	os.Setenv("WGPU_NATIVE_PATH", path)
}
