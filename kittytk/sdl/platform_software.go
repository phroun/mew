//go:build sdl && !webgpu

package sdl

// exposeWebGPUObjects is a no-op when not building with webgpu tag
func (p *Platform) exposeWebGPUObjects() {
	// Software renderer doesn't need GPU object exposure
}
