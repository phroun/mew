//go:build sdl && webgpu

package sdl

// exposeWebGPUObjects copies GPU objects from WebGPURenderer to Platform
// This is a temporary bridge during refactoring - will be removed once
// Platform no longer needs direct access to GPU objects
func (p *Platform) exposeWebGPUObjects() {
	if gpuRenderer, ok := p.renderer.(*WebGPURenderer); ok {
		p.gpuInstance = gpuRenderer.instance
		p.gpuAdapter = gpuRenderer.adapter
		p.gpuDevice = gpuRenderer.device
		p.gpuQueue = gpuRenderer.queue
		p.blitPipeline = gpuRenderer.blitPipeline
		p.blitSampler = gpuRenderer.blitSampler
		p.blitLayout = gpuRenderer.blitLayout
		p.blitUniformBuffer = gpuRenderer.blitUniformBuffer
		p.blitUniformLayout = gpuRenderer.blitUniformLayout
		p.blitUniformBindGroup = gpuRenderer.blitUniformBindGroup
		p.cubePipeline = gpuRenderer.cubePipeline
		p.cubeVertexBuffer = gpuRenderer.cubeVertexBuffer
		p.cubeIndexBuffer = gpuRenderer.cubeIndexBuffer
		p.cubeUniformBuffer = gpuRenderer.cubeUniformBuffer
		p.cubeUniformLayout = gpuRenderer.cubeUniformLayout
		p.cubeUniformBindGroup = gpuRenderer.cubeUniformBindGroup
		p.cubeLayout = gpuRenderer.blitLayout // Note: cubeLayout reuses blitLayout
	}
}
