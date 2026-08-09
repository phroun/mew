//go:build sdl && !webgpu

package sdl

import "fmt"

// NewWebGPURenderer returns an error when WebGPU is not compiled in.
// To use WebGPU rendering, rebuild with: go build -tags "sdl,webgpu"
func NewWebGPURenderer(vsync bool) (Renderer, error) {
	return nil, fmt.Errorf("WebGPU renderer not available: rebuild with -tags \"sdl,webgpu\"")
}
