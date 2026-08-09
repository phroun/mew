//go:build sdl && darwin

package sdl

import (
	"fmt"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// nativeSurfaceHandles resolves the platform handles WebGPU surface
// creation needs for one SDL window. On macOS the window handle is the
// CAMetalLayer and the display handle is unused.
func nativeSurfaceHandles(win *sdl3.Window) (display, window uintptr, err error) {
	layer := getMetalLayer(win)
	if layer == nil {
		return 0, 0, fmt.Errorf("failed to get Metal layer from window")
	}
	return 0, uintptr(layer), nil
}
