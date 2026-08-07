//go:build sdl && webgpu

package sdl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"reflect"
	"time"
	"unsafe"

	gputypes "github.com/gogpu/gputypes"
	wgpu "github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/allbackends"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// baseLayer holds an OS window's cached desktop-chrome texture: the
// wallpaper, menu bar, status bar, dock and desktop content, everything
// the compositor draws underneath the window layers.
//
// It is kept across frames for the same reason a window's texture is,
// only more so. This is the full-surface texture, so repainting it costs
// the wallpaper fill over every pixel, a full BGRA conversion and a
// full-surface upload — and it used to also allocate and free the
// texture itself every single frame.
type baseLayer struct {
	texture     *wgpu.Texture
	textureView *wgpu.TextureView
	bindGroup   *wgpu.BindGroup
	widthPx     uint32
	heightPx    uint32

	painted   paintSignature
	paintedAt time.Time

	// paintedBackend is the raster backend the cached pixels came from.
	// A resize or font zoom REPLACES the OS window's backend with a
	// fresh zero-filled one without going through the renderer, so a
	// cache that only compared sizes could serve a black surface when
	// the new backend happens to match the old one's dimensions. Identity
	// makes that impossible instead of unlikely.
	paintedBackend *raster.Backend
}

func (b *baseLayer) release() {
	if b.bindGroup != nil {
		b.bindGroup.Release()
		b.bindGroup = nil
	}
	if b.textureView != nil {
		b.textureView.Release()
		b.textureView = nil
	}
	if b.texture != nil {
		b.texture.Release()
		b.texture = nil
	}
}

// WindowSurface holds GPU resources for a single window's off-screen rendering
type WindowSurface struct {
	texture     *wgpu.Texture
	textureView *wgpu.TextureView
	bindGroup   *wgpu.BindGroup
	width       uint32
	height      uint32

	// Per-window uniform buffer (for positioning)
	uniformBuffer    *wgpu.Buffer
	uniformBindGroup *wgpu.BindGroup

	// Transform state for compositing
	translateX float32
	translateY float32
	rotation   float32 // radians
	scaleX     float32
	scaleY     float32
	opacity    float32

	dirty bool // texture holds nothing usable yet (fresh or just resized)

	// painted is the signature of what this texture currently holds, so
	// a frame can tell "nothing about this window changed" from "repaint
	// it". Zero value means never painted, which reads as stale.
	painted   paintSignature
	paintedAt time.Time // drives the heartbeat repaint

	// caret is this window's platform-caret request from its last paint,
	// already in SURFACE coordinates. Cached alongside the texture for
	// the same reason: a window whose paint is skipped still wants the
	// caret where it last put it, and recomputing it would mean painting.
	caret core.TextCaret

	// UI Window compositor support (for child windows within an OS window)
	uiWindow interface{}     // UI Window trinket (interface{} to avoid import cycle)
	backend  *raster.Backend // Per-window raster backend for UI windows
	zOrder   int             // Z-order for compositing (higher = on top)

	// owner is the id of the OS window this child belongs to. One map
	// holds every OS window's child surfaces, so eviction has to ask
	// whose they are: a torn-off window compositing its own popups
	// reports none of the desktop's children, and without this would
	// evict all of them — every frame, recreating each texture the next.
	owner uint32
}

// WebGPURenderer implements GPU-accelerated rendering with WebGPU.
// Supports 2D transforms (rotation, scale), 3D effects, and compositing.
type WebGPURenderer struct {
	vsync bool

	// WebGPU core objects
	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	device   *wgpu.Device
	queue    *wgpu.Queue

	// 2D blit pipeline (for presenting raster backend)
	blitPipeline         *wgpu.RenderPipeline
	blitSampler          *wgpu.Sampler
	blitLayout           *wgpu.BindGroupLayout
	blitUniformBuffer    *wgpu.Buffer
	blitUniformLayout    *wgpu.BindGroupLayout
	blitUniformBindGroup *wgpu.BindGroup

	// Rotation/scale effect state
	rotationStartTime           time.Time
	rotationActivationTime      time.Time
	rotationDeactivationTime    time.Time
	rotationAngleAtDeactivation float64
	rotationEnabled             bool

	// Per-window surfaces for compositing
	windowSurfaces       map[uint32]*WindowSurface // windowID -> surface
	firstCompositorFrame bool                      // Track first compositor call

	// The desktop's tiled wallpaper (one small texture, repeat-sampled)
	// and the two samplers that repeat it: crisp keeps hard pixel edges,
	// smooth interpolates when the drawn size is not the image's own.
	wallpaper              wallpaperCache
	wallpaperSampler       *wgpu.Sampler
	wallpaperSmoothSampler *wgpu.Sampler

	// Cached base layers (desktop chrome), one per OS window. Held
	// across frames: the base is the largest texture on screen, and it
	// used to be allocated, painted, converted and uploaded in full
	// every frame.
	baseLayers map[uint32]*baseLayer

	frameSeq uint64

	// Repaint accounting for KITTYTK_COMPOSITOR_STATS.
	framePainted  int
	frameSkipped  int
	statsFrames   int
	statsPainted  int
	statsSkipped  int
	statsLastEmit time.Time

	// 3D cube rendering
	cubePipeline         *wgpu.RenderPipeline
	cubeVertexBuffer     *wgpu.Buffer
	cubeIndexBuffer      *wgpu.Buffer
	cubeUniformBuffer    *wgpu.Buffer
	cubeUniformLayout    *wgpu.BindGroupLayout
	cubeUniformBindGroup *wgpu.BindGroup
}

// NewWebGPURenderer creates a GPU-accelerated renderer
func NewWebGPURenderer(vsync bool) (Renderer, error) {
	r := &WebGPURenderer{
		vsync:                vsync,
		windowSurfaces:       make(map[uint32]*WindowSurface),
		firstCompositorFrame: true,
	}
	return r, nil
}

// Initialize sets up WebGPU context
func (r *WebGPURenderer) Initialize() error {
	// Create WebGPU instance
	instance, err := wgpu.CreateInstance(nil)
	if err != nil {
		return fmt.Errorf("failed to create WebGPU instance: %w", err)
	}
	r.instance = instance

	// Request adapter
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		PowerPreference: gputypes.PowerPreferenceHighPerformance,
	})
	if err != nil {
		instance.Release()
		return fmt.Errorf("failed to request WebGPU adapter: %w", err)
	}
	r.adapter = adapter

	// Request device and queue
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{
		Label: "KittyTK Device",
	})
	if err != nil {
		adapter.Release()
		instance.Release()
		return fmt.Errorf("failed to request WebGPU device: %w", err)
	}
	r.device = device
	r.queue = device.Queue()

	// Initialize blit pipeline (for 2D rendering)
	if err := r.initBlitPipeline(); err != nil {
		r.Shutdown()
		return fmt.Errorf("failed to initialize blit pipeline: %w", err)
	}

	// Initialize cube pipeline (for 3D effects)
	if err := r.initCubePipeline(); err != nil {
		r.Shutdown()
		return fmt.Errorf("failed to initialize cube pipeline: %w", err)
	}

	r.rotationStartTime = time.Now()
	return nil
}

// Shutdown cleans up WebGPU resources
func (r *WebGPURenderer) Shutdown() {
	// Clean up per-window compositor surfaces and cached base layers
	for id, surf := range r.windowSurfaces {
		r.releaseWindowSurface(surf)
		delete(r.windowSurfaces, id)
	}
	for id := range r.baseLayers {
		r.releaseBaseLayer(id)
	}
	r.wallpaper.release()
	if r.wallpaperSampler != nil {
		r.wallpaperSampler.Release()
	}
	if r.wallpaperSmoothSampler != nil {
		r.wallpaperSmoothSampler.Release()
	}

	// Clean up cube resources
	if r.cubeUniformBindGroup != nil {
		r.cubeUniformBindGroup.Release()
	}
	if r.cubeUniformBuffer != nil {
		r.cubeUniformBuffer.Release()
	}
	if r.cubeIndexBuffer != nil {
		r.cubeIndexBuffer.Release()
	}
	if r.cubeVertexBuffer != nil {
		r.cubeVertexBuffer.Release()
	}
	if r.cubePipeline != nil {
		r.cubePipeline.Release()
	}
	if r.cubeUniformLayout != nil {
		r.cubeUniformLayout.Release()
	}

	// Clean up blit resources
	if r.blitUniformBindGroup != nil {
		r.blitUniformBindGroup.Release()
	}
	if r.blitUniformBuffer != nil {
		r.blitUniformBuffer.Release()
	}
	if r.blitUniformLayout != nil {
		r.blitUniformLayout.Release()
	}
	if r.blitLayout != nil {
		r.blitLayout.Release()
	}
	if r.blitSampler != nil {
		r.blitSampler.Release()
	}
	if r.blitPipeline != nil {
		r.blitPipeline.Release()
	}

	// Clean up core objects
	if r.device != nil {
		r.device.Release()
	}
	if r.adapter != nil {
		r.adapter.Release()
	}
	if r.instance != nil {
		r.instance.Release()
	}
}

// CreateWindowRenderer creates WebGPU surface and resources for a window
func (r *WebGPURenderer) CreateWindowRenderer(w *nativeWin, pxW, pxH int) error {
	// Create off-screen texture for this window
	texture, err := r.device.CreateTexture(&wgpu.TextureDescriptor{
		Size:          wgpu.Extent3D{Width: uint32(pxW), Height: uint32(pxH), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatBGRA8Unorm,
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
	})
	if err != nil {
		return fmt.Errorf("failed to create window texture: %w", err)
	}

	// Create texture view
	textureView, err := r.device.CreateTextureView(texture, nil)
	if err != nil {
		texture.Release()
		return fmt.Errorf("failed to create texture view: %w", err)
	}

	// Create bind group for compositing
	bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: textureView},
			{Binding: 1, Sampler: r.blitSampler},
		},
	})
	if err != nil {
		textureView.Release()
		texture.Release()
		return fmt.Errorf("failed to create bind group: %w", err)
	}

	// Create WindowSurface and store it
	surf := &WindowSurface{
		texture:     texture,
		textureView: textureView,
		bindGroup:   bindGroup,
		width:       uint32(pxW),
		height:      uint32(pxH),
		translateX:  0,
		translateY:  0,
		rotation:    0,
		scaleX:      1.0,
		scaleY:      1.0,
		opacity:     1.0,
		dirty:       true,
	}

	r.windowSurfaces[w.id] = surf
	return nil
}

// DestroyWindowRenderer cleans up WebGPU resources for a window
func (r *WebGPURenderer) DestroyWindowRenderer(w *nativeWin) {
	r.releaseBaseLayer(w.id)

	surf, ok := r.windowSurfaces[w.id]
	if !ok {
		return
	}
	r.releaseWindowSurface(surf)
	delete(r.windowSurfaces, w.id)
}

// ResizeWindowRenderer adjusts WebGPU resources when window size changes
func (r *WebGPURenderer) ResizeWindowRenderer(w *nativeWin, pxW, pxH int) error {
	// Destroy old resources
	r.DestroyWindowRenderer(w)

	// Create new resources at new size
	return r.CreateWindowRenderer(w, pxW, pxH)
}

// Present renders using WebGPU pipeline
func (r *WebGPURenderer) Present(w *nativeWin, backend *raster.Backend) error {
	img := backend.Image()
	if img == nil {
		return fmt.Errorf("backend image is nil")
	}

	// The window's texture is CACHED, and its pixels are re-uploaded only
	// when the platform repainted them. Allocating and filling a
	// full-surface texture per frame is what made dragging a torn-off
	// window stutter while the composited desktop stayed smooth.
	layer, err := r.refreshPresentTexture(w, backend)
	if err != nil {
		return err
	}
	bindGroup := layer.bindGroup

	// Fullscreen quad carrying the current rotation-demo effect state.
	aspect := float32(1)
	if b := img.Bounds(); b.Dy() > 0 {
		aspect = float32(b.Dx()) / float32(b.Dy())
	}
	size := backend.Size()
	r.writeCombinedUniforms(r.blitUniformBuffer,
		core.UnitRect{Width: size.Width, Height: size.Height}, size, aspect)

	// Surface configuration can reset the Metal layer's opacity state,
	// and an opaque layer discards alpha whatever we clear to.
	if w.transparent {
		reassertWindowAlpha(w.window)
	}
	if w.layerRadiusPx > 0 {
		roundWindowLayer(w.window, w.layerRadiusPx)
	}

	// Get surface texture
	surfaceTexture, _, err := w.gpuSurface.GetCurrentTexture()
	if err != nil {
		return err
	}

	surfaceView, err := surfaceTexture.CreateView(nil)
	if err != nil {
		return err
	}

	// Create command encoder
	encoder, err := r.device.CreateCommandEncoder(nil)
	if err != nil {
		surfaceView.Release()
		return err
	}

	// Begin render pass. A transparent (per-pixel alpha) window must
	// clear to alpha 0 or its rounded corners composite as opaque black.
	clearAlpha := 1.0
	if w.transparent {
		clearAlpha = 0.0
	}
	renderPass, _ := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       surfaceView,
				LoadOp:     gputypes.LoadOpClear,
				StoreOp:    gputypes.StoreOpStore,
				ClearValue: wgpu.Color{R: 0.0, G: 0.0, B: 0.0, A: clearAlpha},
			},
		},
	})

	// Draw fullscreen quad with backend texture. (Under
	// KITTYTK_ALPHA_TEST a transparent window presents the bare alpha-0
	// clear instead — see alphaPresentTest.)
	if !(alphaPresentTest && w.transparent) {
		renderPass.SetPipeline(r.blitPipeline)
		renderPass.SetBindGroup(0, bindGroup, nil)
		renderPass.SetBindGroup(1, r.blitUniformBindGroup, nil)
		renderPass.Draw(6, 1, 0, 0) // Draw quad (6 vertices)
	}
	renderPass.End()

	// Rotation demo: spin the cube over the scene while the effect runs.
	var cubeCleanup func()
	if _, _, _, active := r.effectParams(); active {
		if cleanup, cubeErr := r.recordCubePass(encoder, surfaceView, layer.textureView, aspect); cubeErr == nil {
			cubeCleanup = cleanup
		}
	}

	// Submit and present
	cmdBuffer, _ := encoder.Finish()
	_, err = r.queue.Submit(cmdBuffer)

	// NOW we can release resources after GPU has the commands. The
	// window's own texture is NOT among them — it is cached across
	// frames.
	if cubeCleanup != nil {
		cubeCleanup()
	}
	// Finish() transferred the native command encoder to this command buffer;
	// Release() recycles it into the device's encoder pool. Skipping it leaks
	// one native encoder PER PRESENT — the pool allocates a fresh one every
	// frame — so a session that presents a lot inflates native memory without
	// bound. Release is nil-safe, so a failed Finish (nil buffer) is fine.
	cmdBuffer.Release()
	surfaceView.Release()

	if err != nil {
		return err
	}
	w.gpuSurface.Present(surfaceTexture)

	return nil
}

// ApplyWindowShape applies rounded corners (WebGPU uses fragment shader clipping)
func (r *WebGPURenderer) ApplyWindowShape(w *nativeWin, radiusPx int, transparent bool) error {
	// WebGPU doesn't need OS-level window shaping - handled in fragment shader
	return nil
}

// SupportsFeature checks WebGPU renderer capabilities
func (r *WebGPURenderer) SupportsFeature(feature RendererFeature) bool {
	switch feature {
	case FeatureRotation, FeatureScale, Feature3DCube, FeatureCompositing:
		return true
	default:
		return false
	}
}

// createBlitBindGroupLayout builds one of the blit pipeline's bind group
// layouts from blitBindGroups, so the shape the shaders are tested
// against is literally the shape the pipeline is built with.
func (r *WebGPURenderer) createBlitBindGroupLayout(group int) (*wgpu.BindGroupLayout, error) {
	kinds := blitBindGroups[group]
	entries := make([]gputypes.BindGroupLayoutEntry, 0, len(kinds))
	for i, kind := range kinds {
		entry := gputypes.BindGroupLayoutEntry{
			Binding:    uint32(i),
			Visibility: wgpu.ShaderStageFragment,
		}
		switch kind {
		case bindingBuffer:
			// Read by both stages: the vertex stage places the quad, the
			// fragment stage reads the shadow parameters from the same block.
			entry.Visibility = wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
			entry.Buffer = &gputypes.BufferBindingLayout{
				Type:           0, // Uniform
				MinBindingSize: combinedUniformSize,
			}
		case bindingTexture:
			entry.Texture = &gputypes.TextureBindingLayout{
				SampleType:    1, // Float
				ViewDimension: 2, // 2D
			}
		case bindingSampler:
			entry.Sampler = &gputypes.SamplerBindingLayout{
				Type: 1, // Filtering
			}
		}
		entries = append(entries, entry)
	}
	return r.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
}

// initBlitPipeline creates the 2D rendering pipeline
func (r *WebGPURenderer) initBlitPipeline() error {
	// Create shader modules
	vertexShader, err := r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Blit Vertex Shader",
		WGSL:  blitVertexShader,
	})
	if err != nil {
		return fmt.Errorf("failed to create vertex shader: %w", err)
	}
	defer vertexShader.Release()

	fragmentShader, err := r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Blit Fragment Shader",
		WGSL:  blitFragmentShader,
	})
	if err != nil {
		return fmt.Errorf("failed to create fragment shader: %w", err)
	}
	defer fragmentShader.Release()

	// Create sampler. Named constants, not the numbers that used to be
	// here with comments claiming 2 meant ClampToEdge and 1 meant Linear
	// — 2 is Repeat and 1 is Nearest. It never mattered for ordinary
	// layers, which sample inside [0,1] at 1:1, but it made the
	// wallpaper's sampler below look like a copy of a working example
	// when it was nothing of the kind.
	r.blitSampler, err = r.device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeNearest,
		MinFilter:    gputypes.FilterModeNearest,
	})
	if err != nil {
		return fmt.Errorf("failed to create sampler: %w", err)
	}

	// The wallpaper's sampler REPEATS instead of clamping, which is what
	// tiles the desktop background in hardware. Nearest filtering keeps a
	// hard-edged pattern crisp: the quad maps the tile 1:1 to pixels, so
	// there is nothing to interpolate and linear would only soften it.
	r.wallpaperSampler, err = r.device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU: gputypes.AddressModeRepeat,
		AddressModeV: gputypes.AddressModeRepeat,
		MagFilter:    gputypes.FilterModeNearest,
		MinFilter:    gputypes.FilterModeNearest,
	})
	if err != nil {
		return fmt.Errorf("failed to create wallpaper sampler: %w", err)
	}

	// The smooth counterpart, for a wallpaper scaled away from its own
	// resolution — a photograph stretched to cover, where nearest
	// neighbour would alias badly.
	r.wallpaperSmoothSampler, err = r.device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU: gputypes.AddressModeRepeat,
		AddressModeV: gputypes.AddressModeRepeat,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
	})
	if err != nil {
		return fmt.Errorf("failed to create smooth wallpaper sampler: %w", err)
	}

	// Create uniform buffer for combined uniforms (effects + position +
	// shadow parameters) — see combinedUniformData.
	r.blitUniformBuffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  combinedUniformSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create uniform buffer: %w", err)
	}

	// Both bind group layouts come from blitBindGroups, which the shader
	// tests hold the WGSL to. Group order matters to the Metal slot
	// numbering — see the comment there.
	r.blitLayout, err = r.createBlitBindGroupLayout(0)
	if err != nil {
		return fmt.Errorf("failed to create texture bind group layout: %w", err)
	}
	r.blitUniformLayout, err = r.createBlitBindGroupLayout(1)
	if err != nil {
		return fmt.Errorf("failed to create uniform bind group layout: %w", err)
	}

	// Create pipeline layout with 2 bind groups: texture, combined uniforms
	pipelineLayout, err := r.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{r.blitLayout, r.blitUniformLayout},
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline layout: %w", err)
	}
	defer pipelineLayout.Release()

	// Create render pipeline
	r.blitPipeline, err = r.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     vertexShader,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragmentShader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    wgpu.TextureFormatBGRA8Unorm,
					WriteMask: 0xF,
					Blend: &gputypes.BlendState{
						Color: gputypes.BlendComponent{
							SrcFactor: gputypes.BlendFactorSrcAlpha,
							DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
							Operation: gputypes.BlendOperationAdd,
						},
						Alpha: gputypes.BlendComponent{
							SrcFactor: gputypes.BlendFactorOne,
							DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
							Operation: gputypes.BlendOperationAdd,
						},
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: gputypes.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create render pipeline: %w", err)
	}

	// Create uniform bind group
	r.blitUniformBindGroup, err = r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitUniformLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: r.blitUniformBuffer, Size: combinedUniformSize},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create uniform bind group: %w", err)
	}

	return nil
}

// initCubePipeline creates the 3D cube rendering pipeline for the
// rotation demo. The cube textures its faces with the UI content and
// draws with back-face culling over the presented scene (no depth
// buffer needed). Both present paths share these resources — the
// Platform borrows them through exposeWebGPUObjects.
func (r *WebGPURenderer) initCubePipeline() error {
	// Cube geometry: 24 vertices (4 per face), 36 indices (12 triangles)
	// Vertex format: [x, y, z, u, v]
	cubeVertices := []float32{
		// Front face
		-0.2, -0.2, 0.2, 0.0, 1.0,
		0.2, -0.2, 0.2, 1.0, 1.0,
		0.2, 0.2, 0.2, 1.0, 0.0,
		-0.2, 0.2, 0.2, 0.0, 0.0,
		// Back face
		-0.2, -0.2, -0.2, 1.0, 1.0,
		-0.2, 0.2, -0.2, 1.0, 0.0,
		0.2, 0.2, -0.2, 0.0, 0.0,
		0.2, -0.2, -0.2, 0.0, 1.0,
		// Top face
		-0.2, 0.2, -0.2, 0.0, 1.0,
		-0.2, 0.2, 0.2, 0.0, 0.0,
		0.2, 0.2, 0.2, 1.0, 0.0,
		0.2, 0.2, -0.2, 1.0, 1.0,
		// Bottom face
		-0.2, -0.2, -0.2, 1.0, 1.0,
		0.2, -0.2, -0.2, 0.0, 1.0,
		0.2, -0.2, 0.2, 0.0, 0.0,
		-0.2, -0.2, 0.2, 1.0, 0.0,
		// Right face
		0.2, -0.2, -0.2, 1.0, 1.0,
		0.2, 0.2, -0.2, 1.0, 0.0,
		0.2, 0.2, 0.2, 0.0, 0.0,
		0.2, -0.2, 0.2, 0.0, 1.0,
		// Left face
		-0.2, -0.2, -0.2, 0.0, 1.0,
		-0.2, -0.2, 0.2, 1.0, 1.0,
		-0.2, 0.2, 0.2, 1.0, 0.0,
		-0.2, 0.2, -0.2, 0.0, 0.0,
	}

	cubeIndices := []uint16{
		0, 1, 2, 0, 2, 3, // Front
		4, 5, 6, 4, 6, 7, // Back
		8, 9, 10, 8, 10, 11, // Top
		12, 13, 14, 12, 14, 15, // Bottom
		16, 17, 18, 16, 18, 19, // Right
		20, 21, 22, 20, 22, 23, // Left
	}

	vertexData := make([]byte, len(cubeVertices)*4)
	for i, v := range cubeVertices {
		binary.LittleEndian.PutUint32(vertexData[i*4:], math.Float32bits(v))
	}
	var err error
	r.cubeVertexBuffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  uint64(len(vertexData)),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create cube vertex buffer: %w", err)
	}
	r.queue.WriteBuffer(r.cubeVertexBuffer, 0, vertexData)

	indexData := make([]byte, len(cubeIndices)*2)
	for i, idx := range cubeIndices {
		binary.LittleEndian.PutUint16(indexData[i*2:], idx)
	}
	r.cubeIndexBuffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  uint64(len(indexData)),
		Usage: wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create cube index buffer: %w", err)
	}
	r.queue.WriteBuffer(r.cubeIndexBuffer, 0, indexData)

	// Uniform buffer for the MVP matrix (64 bytes for mat4x4)
	r.cubeUniformBuffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  64,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create cube uniform buffer: %w", err)
	}

	r.cubeUniformLayout, err = r.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: &gputypes.BufferBindingLayout{
					Type:             0, // Uniform
					MinBindingSize:   64,
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create cube uniform layout: %w", err)
	}

	vertexShader, err := r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Cube Vertex Shader",
		WGSL:  cubeVertexShader,
	})
	if err != nil {
		return fmt.Errorf("failed to create cube vertex shader: %w", err)
	}
	defer vertexShader.Release()

	fragmentShader, err := r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Cube Fragment Shader",
		WGSL:  cubeFragmentShader,
	})
	if err != nil {
		return fmt.Errorf("failed to create cube fragment shader: %w", err)
	}
	defer fragmentShader.Release()

	// Texture+sampler bind group reuses the blit layout at group 0.
	pipelineLayout, err := r.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{r.blitLayout, r.cubeUniformLayout},
	})
	if err != nil {
		return fmt.Errorf("failed to create cube pipeline layout: %w", err)
	}
	defer pipelineLayout.Release()

	r.cubePipeline, err = r.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     vertexShader,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{
				{
					ArrayStride: 20, // 5 floats * 4 bytes
					StepMode:    gputypes.VertexStepModeVertex,
					Attributes: []gputypes.VertexAttribute{
						{Format: gputypes.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 0},  // position
						{Format: gputypes.VertexFormatFloat32x2, Offset: 12, ShaderLocation: 1}, // texCoord
					},
				},
			},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragmentShader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    wgpu.TextureFormatBGRA8Unorm,
					WriteMask: 0xF,
					Blend: &gputypes.BlendState{
						Color: gputypes.BlendComponent{
							SrcFactor: gputypes.BlendFactorSrcAlpha,
							DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
							Operation: gputypes.BlendOperationAdd,
						},
						Alpha: gputypes.BlendComponent{
							SrcFactor: gputypes.BlendFactorOne,
							DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
							Operation: gputypes.BlendOperationAdd,
						},
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  gputypes.PrimitiveTopologyTriangleList,
			CullMode:  gputypes.CullModeBack, // Keep back-face culling for clean look
			FrontFace: gputypes.FrontFaceCCW,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create cube render pipeline: %w", err)
	}

	r.cubeUniformBindGroup, err = r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.cubeUniformLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: r.cubeUniformBuffer, Size: 64},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create cube uniform bind group: %w", err)
	}

	return nil
}

// effectParams reports the rotation demo's eased state at this instant:
// the scene angle/scale for the blit shader (enabledF 1.0 while active)
// and whether the effect still needs frames (active covers the ease-out
// tail after deactivation).
func (r *WebGPURenderer) effectParams() (angle, enabledF, scale float32, active bool) {
	scale = 1.0
	easeOutCubic := func(t float64) float64 {
		t = math.Min(t, 1.0)
		return 1.0 - math.Pow(1.0-t, 3.0)
	}

	if r.rotationEnabled {
		timeSinceActivation := time.Since(r.rotationActivationTime).Seconds()

		// Scale eases in over 0.5 seconds from 1.0 to 2.0
		scale = float32(1.0 + easeOutCubic(timeSinceActivation/0.5))

		// Rotation eases in over 1.0 seconds up to full speed
		rotationEased := easeOutCubic(timeSinceActivation / 1.0)
		elapsedRotation := time.Since(r.rotationStartTime).Seconds()
		angle = float32(elapsedRotation * 0.1 * rotationEased)

		return angle, 1.0, scale, true
	}

	timeSinceDeactivation := time.Since(r.rotationDeactivationTime).Seconds()
	if timeSinceDeactivation >= 0.5 {
		return 0, 0, 1.0, false
	}

	// Easing OUT: scale returns to 1.0 while the angle accelerates
	// forward to the next full turn, so the scene lands upright.
	scale = float32(2.0 - easeOutCubic(timeSinceDeactivation/0.5))

	currentAngle := r.rotationAngleAtDeactivation
	twoPi := 2.0 * math.Pi
	normalizedAngle := math.Mod(currentAngle, twoPi)
	if normalizedAngle < 0 {
		normalizedAngle += twoPi
	}
	angleRemaining := twoPi - normalizedAngle

	normalRotation := timeSinceDeactivation * 0.1
	easeInOutCubic := func(t float64) float64 {
		if t < 0.5 {
			return 4.0 * t * t * t
		}
		return 1.0 - math.Pow(-2.0*t+2.0, 3.0)/2.0
	}
	catchUp := angleRemaining * easeInOutCubic(math.Min(timeSinceDeactivation/0.5, 1.0))

	targetAngle := currentAngle + angleRemaining
	current := currentAngle + normalRotation + catchUp
	if current > targetAngle {
		current = targetAngle
	}
	return float32(current), 1.0, scale, true
}

// recordCubePass draws the rotation demo's spinning cube over the
// already-rendered scene: its own render pass loading the existing
// surface content, with the cube's faces textured by contentView. The
// returned cleanup must run after Submit; recording is skipped (nil
// cleanup, nil error) when the cube pipeline is unavailable.
func (r *WebGPURenderer) recordCubePass(encoder *wgpu.CommandEncoder, surfaceView, contentView *wgpu.TextureView, aspect float32) (func(), error) {
	if r.cubePipeline == nil {
		return nil, nil
	}

	elapsed := time.Since(r.rotationStartTime).Seconds()
	cubeAngle := float32(elapsed * 0.5) // Rotate faster than the background
	floatOffset := float32(math.Sin(elapsed*1.5) * 0.15)

	easeOutCubic := func(t float64) float64 {
		t = math.Min(t, 1.0)
		return 1.0 - math.Pow(1.0-t, 3.0)
	}
	var cubeScale float32
	if r.rotationEnabled {
		timeSinceActivation := time.Since(r.rotationActivationTime).Seconds()
		scaleProgress := timeSinceActivation / 0.5
		cubeScale = float32(easeOutCubic(scaleProgress) * 1.5) // Ease from 0 to 1.5
		if scaleProgress >= 1.0 {
			// Slight pulsing after the initial ease
			cubeScale *= float32(math.Sin(elapsed*2.0)*0.05 + 1.0)
		}
	} else {
		timeSinceDeactivation := time.Since(r.rotationDeactivationTime).Seconds()
		cubeScale = float32((1.0 - easeOutCubic(timeSinceDeactivation/0.5)) * 1.5) // Ease to 0
	}

	mvp := createMVPMatrix(aspect, cubeAngle, cubeScale, floatOffset)
	mvpBytes := (*[64]byte)(unsafe.Pointer(&mvp[0]))[:]
	r.queue.WriteBuffer(r.cubeUniformBuffer, 0, mvpBytes)

	cubeBindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: contentView},
			{Binding: 1, Sampler: r.blitSampler},
		},
	})
	if err != nil {
		return nil, err
	}

	cubePass, _ := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       surfaceView,
				LoadOp:     gputypes.LoadOpLoad, // Keep existing content
				StoreOp:    gputypes.StoreOpStore,
				ClearValue: wgpu.Color{R: 0.0, G: 0.0, B: 0.0, A: 0.0},
			},
		},
	})
	cubePass.SetPipeline(r.cubePipeline)
	cubePass.SetBindGroup(0, cubeBindGroup, nil)
	cubePass.SetBindGroup(1, r.cubeUniformBindGroup, nil)
	cubePass.SetVertexBuffer(0, r.cubeVertexBuffer, 0)
	cubePass.SetIndexBuffer(r.cubeIndexBuffer, gputypes.IndexFormatUint16, 0)
	cubePass.DrawIndexed(36, 1, 0, 0, 0)
	cubePass.End()

	return func() { cubeBindGroup.Release() }, nil
}

// SetRotationEnabled toggles the 2D rotation effect
func (r *WebGPURenderer) SetRotationEnabled(enabled bool) {
	if enabled == r.rotationEnabled {
		return
	}

	now := time.Now()
	if enabled {
		r.rotationActivationTime = now
		r.rotationStartTime = now
	} else {
		// Store angle at deactivation for smooth ease-out
		elapsed := now.Sub(r.rotationActivationTime).Seconds()
		if elapsed > 0.5 { // After ease-in completes
			elapsedAfterEaseIn := elapsed - 0.5
			r.rotationAngleAtDeactivation = elapsedAfterEaseIn * 0.1
		}
		r.rotationDeactivationTime = now
	}
	r.rotationEnabled = enabled
}

// SetWindowTransform sets the transform for a window
func (r *WebGPURenderer) SetWindowTransform(windowID uint32, translateX, translateY, rotation, scaleX, scaleY, opacity float32) {
	surf, ok := r.windowSurfaces[windowID]
	if !ok {
		return
	}

	surf.translateX = translateX
	surf.translateY = translateY
	surf.rotation = rotation
	surf.scaleX = scaleX
	surf.scaleY = scaleY
	surf.opacity = opacity
}

// RotationEnabled returns whether rotation effect is active
func (r *WebGPURenderer) RotationEnabled() bool {
	return r.rotationEnabled
}

// RenderFrame implements the Renderer interface for per-window compositing.
// For now, this is a simple implementation that just renders and presents.
func (r *WebGPURenderer) RenderFrame(w *nativeWin, windows []*nativeWin, renderWindow func(*nativeWin)) error {
	// Call the render callback for this window
	renderWindow(w)

	// Present the rendered content
	return r.Present(w, w.backend)
}

// RenderFrameWithChildWindows implements per-child-window GPU compositing.
// Each UI child window is rendered to its own GPU texture, then all textures
// are composited together with Z-ordering, transforms, and effects.
func (r *WebGPURenderer) RenderFrameWithChildWindows(
	osWindow *nativeWin,
	childWindowList *platform.ChildWindowList,
	scale int,
	renderWindow func(*nativeWin),
) error {
	if r.firstCompositorFrame {
		if childWindowList != nil {
			fmt.Printf("GPU compositor active: compositing %d UI child windows\n", len(childWindowList.Windows))
		}
		r.firstCompositorFrame = false
	}

	if childWindowList == nil {
		// Nothing to composite at all: render and present as one surface.
		renderWindow(osWindow)
		return r.Present(osWindow, osWindow.backend)
	}

	// Step 0: the Desktop base layer (background, menu, status, dock —
	// NOT windows), repainted only when something it draws changed. Its
	// texture is cached; see refreshBaseLayer.
	base, err := r.refreshBaseLayer(osWindow, childWindowList, renderWindow)
	if err != nil {
		return err
	}

	// Frame-level pixel metrics: the surface's pixel size, its
	// pixels-per-unit, and one aspect ratio (width/height) shared by
	// every layer so the rotation demo turns the scene rigidly.
	surfacePxW, surfacePxH := 0, 0
	framePpuW, framePpuH := 1.0, 1.0
	frameAspect := float32(1)
	if img := osWindow.backend.Image(); img != nil && img.Bounds().Dy() > 0 {
		surfacePxW = img.Bounds().Dx()
		surfacePxH = img.Bounds().Dy()
		frameAspect = float32(surfacePxW) / float32(surfacePxH)
		frameSize := osWindow.backend.Size()
		if frameSize.Width > 0 && frameSize.Height > 0 {
			framePpuW = float64(surfacePxW) / float64(frameSize.Width)
			framePpuH = float64(surfacePxH) / float64(frameSize.Height)
		}
	}

	// Evict surfaces whose child window is gone, so closed windows release
	// their textures and backends instead of accumulating forever. Only
	// THIS OS window's children are in scope — see WindowSurface.owner.
	liveWindows := make(map[uint32]bool, len(childWindowList.Windows))
	for _, childIface := range childWindowList.Windows {
		liveWindows[uint32(reflect.ValueOf(childIface).Pointer())] = true
	}
	for id, surf := range r.windowSurfaces {
		if surf.uiWindow != nil && surf.owner == osWindow.id && !liveWindows[id] {
			r.releaseWindowSurface(surf)
			delete(r.windowSurfaces, id)
		}
	}

	// The frame's platform-caret request. Child windows and overlays each
	// paint into a texture of their own, so their requests never reach
	// the base layer's painter — they are gathered here in paint order
	// and the last visible one wins, exactly the rule a single-surface
	// frame follows. Seeded from the base layer so desktop chrome can
	// hold the caret when nothing above claims it.
	frameCaret := osWindow.baseCaret

	// Step 1: Render each child window to its own texture
	type WindowLike interface {
		Bounds() core.UnitRect
		Paint(*core.Painter)
	}

	for _, childIface := range childWindowList.Windows {
		win, ok := childIface.(WindowLike)
		if !ok {
			continue
		}

		bounds := win.Bounds()
		if bounds.Width <= 0 || bounds.Height <= 0 {
			continue
		}

		// Calculate pixel dimensions
		backendImg := osWindow.backend.Image()
		if backendImg == nil {
			continue
		}
		backendSize := osWindow.backend.Size()
		metrics := osWindow.backend.Metrics()

		backendBounds := backendImg.Bounds()
		// The host's unit size is the divisor for the per-unit pixel scale. When
		// the OS window is dragged to a near-zero height (or width) its unit
		// extent rounds to 0, and float64(n)/0 is +Inf — which math.Round carries
		// into int() as MaxInt64, blowing up NewScaled ("huge or negative
		// dimensions") on the next child window. There is nothing to composite a
		// child onto when the host has collapsed to zero area, so skip it.
		if backendSize.Width <= 0 || backendSize.Height <= 0 {
			continue
		}
		pixelsPerUnitW := float64(backendBounds.Dx()) / float64(backendSize.Width)
		pixelsPerUnitH := float64(backendBounds.Dy()) / float64(backendSize.Height)

		widthPx := int(math.Round(float64(bounds.Width) * pixelsPerUnitW))
		heightPx := int(math.Round(float64(bounds.Height) * pixelsPerUnitH))

		if widthPx <= 0 || heightPx <= 0 {
			continue
		}

		// Get stable window ID
		winValue := reflect.ValueOf(childIface)
		windowID := uint32(winValue.Pointer())
		surf, ok := r.windowSurfaces[windowID]

		if !ok || surf.backend == nil {
			// Create new surface
			backend, err := raster.NewScaled(widthPx, heightPx, scale)
			if err != nil {
				continue
			}
			backend.SetCellMetrics(metrics)
			backend.SetFontSize(osWindow.backend.FontSize())

			// Create GPU texture
			texture, err := r.device.CreateTexture(&wgpu.TextureDescriptor{
				Usage:     wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
				Dimension: wgpu.TextureDimension2D,
				Size: wgpu.Extent3D{
					Width:              uint32(widthPx),
					Height:             uint32(heightPx),
					DepthOrArrayLayers: 1,
				},
				Format:        wgpu.TextureFormatBGRA8Unorm,
				MipLevelCount: 1,
				SampleCount:   1,
			})
			if err != nil {
				continue
			}

			textureView, err := r.device.CreateTextureView(texture, nil)
			if err != nil {
				texture.Release()
				continue
			}

			bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Layout: r.blitLayout,
				Entries: []wgpu.BindGroupEntry{
					{Binding: 0, TextureView: textureView},
					{Binding: 1, Sampler: r.blitSampler},
				},
			})
			if err != nil {
				textureView.Release()
				texture.Release()
				continue
			}

			// Create per-window uniform buffer with position
			surfaceSize := osWindow.backend.Size()
			uniformBuffer, uniformBindGroup, err := r.createWindowUniformBuffer(bounds, surfaceSize, frameAspect)
			if err != nil {
				bindGroup.Release()
				textureView.Release()
				texture.Release()
				continue
			}

			surf = &WindowSurface{
				texture:          texture,
				textureView:      textureView,
				bindGroup:        bindGroup,
				uniformBuffer:    uniformBuffer,
				uniformBindGroup: uniformBindGroup,
				width:            uint32(widthPx),
				height:           uint32(heightPx),
				uiWindow:         childIface,
				owner:            osWindow.id,
				backend:          backend,
				dirty:            true,
				scaleX:           1.0,
				scaleY:           1.0,
				opacity:          1.0,
			}
			r.windowSurfaces[windowID] = surf
		}

		// A moved window only needs its uniforms rewritten (done every frame
		// below); texture and backend recreation is for size changes alone.
		needsUpdate := int(surf.width) != widthPx || int(surf.height) != heightPx

		if needsUpdate {
			// Invalidate the window BEFORE we recreate the backend so any
			// content-level caching repaints in full.
			type Invalidator interface {
				Invalidate()
			}
			if invalidatable, ok := win.(Invalidator); ok {
				invalidatable.Invalidate()
			}

			newBackend, err := raster.NewScaled(widthPx, heightPx, scale)
			if err != nil {
				continue // Skip this window this frame
			}

			// Store old resources for cleanup AFTER GPU finishes with them
			oldTexture := surf.texture
			oldTextureView := surf.textureView
			oldBindGroup := surf.bindGroup
			oldUniformBuffer := surf.uniformBuffer
			oldUniformBindGroup := surf.uniformBindGroup

			surf.backend = newBackend
			surf.backend.SetCellMetrics(metrics)
			surf.backend.SetFontSize(osWindow.backend.FontSize())

			texture, err := r.device.CreateTexture(&wgpu.TextureDescriptor{
				Usage:     wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
				Dimension: wgpu.TextureDimension2D,
				Size: wgpu.Extent3D{
					Width:              uint32(widthPx),
					Height:             uint32(heightPx),
					DepthOrArrayLayers: 1,
				},
				Format:        wgpu.TextureFormatBGRA8Unorm,
				MipLevelCount: 1,
				SampleCount:   1,
			})
			if err != nil {
				continue
			}

			textureView, err := r.device.CreateTextureView(texture, nil)
			if err != nil {
				texture.Release()
				continue
			}

			bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Layout: r.blitLayout,
				Entries: []wgpu.BindGroupEntry{
					{Binding: 0, TextureView: textureView},
					{Binding: 1, Sampler: r.blitSampler},
				},
			})
			if err != nil {
				textureView.Release()
				texture.Release()
				continue
			}

			// Recreate uniform buffer with new position
			surfaceSize := osWindow.backend.Size()
			uniformBuffer, uniformBindGroup, err := r.createWindowUniformBuffer(bounds, surfaceSize, frameAspect)
			if err != nil {
				bindGroup.Release()
				textureView.Release()
				texture.Release()
				continue
			}

			surf.texture = texture
			surf.textureView = textureView
			surf.bindGroup = bindGroup
			surf.uniformBuffer = uniformBuffer
			surf.uniformBindGroup = uniformBindGroup
			surf.width = uint32(widthPx)
			surf.height = uint32(heightPx)
			surf.dirty = true

			// Clean up old resources NOW (new resources are already assigned)
			if oldUniformBindGroup != nil {
				oldUniformBindGroup.Release()
			}
			if oldUniformBuffer != nil {
				oldUniformBuffer.Release()
			}
			if oldBindGroup != nil {
				oldBindGroup.Release()
			}
			if oldTextureView != nil {
				oldTextureView.Release()
			}
			if oldTexture != nil {
				oldTexture.Release()
			}
		}

		// Refresh this window's position uniforms EVERY frame. Bounds and the
		// OS surface size both feed the NDC transform, so a window drag AND
		// a desktop resize are covered without recreating any GPU resources;
		// baking them only at (re)creation left stale transforms behind
		// whenever the desktop itself changed size.
		r.writeCombinedUniforms(surf.uniformBuffer, bounds, osWindow.backend.Size(), frameAspect)

		// Repaint only what changed. A window whose subtree nobody
		// touched still has its pixels in its texture from last frame,
		// and repainting it would cost a full CPU paint, a full BGRA
		// conversion and a full texture upload to produce the same
		// bytes. Position is deliberately not part of the signature —
		// the uniforms above already moved the quad, so dragging a
		// window repaints nothing.
		sig := paintSignature{
			fontSize: osWindow.backend.FontSize(),
			metrics:  metrics,
			widthPx:  widthPx,
			heightPx: heightPx,
		}
		sig.revision, sig.hasRevision = subtreeRepaintRevision(childIface)

		now := time.Now()
		if needsRepaint(surf.painted, sig, now.Sub(surf.paintedAt),
			heartbeatInterval(windowID), surf.dirty, compositorAlwaysRepaint) {
			surf.backend.BeginFrame()
			painter := core.NewPainter(surf.backend)
			painter.ResetTextCaretRequest()
			win.Paint(painter)
			surf.backend.EndFrame()

			// A window paints into its OWN texture, so a caret request
			// made inside it is in the window's local coordinates and
			// never reaches the painter the base layer applies. Carry it
			// out here, shifted to surface coordinates.
			surf.caret = caretInSurface(painter.TextCaretRequest(), bounds)

			// Upload to GPU texture
			if img := surf.backend.Image(); img != nil {
				r.uploadPixels(surf.texture, img)
			}

			surf.painted = sig
			surf.paintedAt = now
			surf.dirty = false
			r.framePainted++
		} else {
			r.frameSkipped++
		}
	}
	r.reportCompositorStats(len(childWindowList.Windows))

	// Step 2 is gone: the base layer's texture, view and bind group live
	// in the cache across frames rather than being rebuilt here.
	desktopView, desktopBindGroup := base.textureView, base.bindGroup

	// Step 3: Composite all textures
	if osWindow.transparent {
		reassertWindowAlpha(osWindow.window)
	}
	if osWindow.layerRadiusPx > 0 {
		roundWindowLayer(osWindow.window, osWindow.layerRadiusPx)
	}
	surfaceTexture, _, err := osWindow.gpuSurface.GetCurrentTexture()
	if err != nil {
		return err
	}

	surfaceView, err := surfaceTexture.CreateView(nil)
	if err != nil {
		return err
	}
	// DON'T defer - release after Submit

	encoder, err := r.device.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}

	// A transparent (per-pixel alpha) OS window clears to alpha 0 so its
	// rounded corners composite against what is behind it.
	clearAlpha := 1.0
	if osWindow.transparent {
		clearAlpha = 0.0
	}
	renderPass, _ := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       surfaceView,
				LoadOp:     gputypes.LoadOpClear,
				StoreOp:    gputypes.StoreOpStore,
				ClearValue: wgpu.Color{R: 0.0, G: 0.0, B: 0.0, A: clearAlpha},
			},
		},
	})

	renderPass.SetPipeline(r.blitPipeline)

	surfaceSize := osWindow.backend.Size()

	// Overlay/halo GPU resources must outlive Submit below; the deferred
	// cleanups run when this function returns, after Present.
	var overlayCleanups []func()
	defer func() {
		for _, cleanup := range overlayCleanups {
			cleanup()
		}
	}()

	// The wallpaper is the bottom of everything: one small tile repeated
	// across the surface by the sampler. The base layer above it cleared
	// to transparent, so it shows through wherever the desktop chrome
	// does not paint.
	if cleanup, err := r.drawWallpaper(renderPass, childWindowList.Wallpaper,
		surfaceSize, surfacePxW, surfacePxH, frameAspect); err == nil {
		if cleanup != nil {
			overlayCleanups = append(overlayCleanups, cleanup)
		}
	} else {
		fmt.Fprintf(os.Stderr, "compositor: wallpaper layer failed: %v\n", err)
	}

	// Then the Desktop base: a fullscreen quad carrying the current
	// effect state like every other layer.
	r.writeCombinedUniforms(r.blitUniformBuffer,
		core.UnitRect{Width: surfaceSize.Width, Height: surfaceSize.Height},
		surfaceSize, frameAspect)

	renderPass.SetBindGroup(0, desktopBindGroup, nil)
	renderPass.SetBindGroup(1, r.blitUniformBindGroup, nil)
	renderPass.Draw(6, 1, 0, 0) // Draw quad

	// Windows never paint over desktop chrome: their quads are scissored
	// to the client area (surface minus menu bar, status bar, dock),
	// exactly like the software path's client-area clip. The tear-off
	// halo deliberately escapes the clip — it bleeds over the bars.
	clipX, clipY, clipW, clipH, clipOK := 0, 0, 0, 0, false
	if !childWindowList.ClientArea.IsEmpty() && surfacePxW > 0 && surfacePxH > 0 {
		clipX, clipY, clipW, clipH, clipOK = scissorPx(
			childWindowList.ClientArea, framePpuW, framePpuH, surfacePxW, surfacePxH)
	}

	// tearHaloWindow is the tear-off drag affordance surface a child
	// window may expose (window.Window does).
	type tearHaloWindow interface {
		TearIndicatorActive() bool
		PaintTearHalo(*core.Painter, core.UnitRect)
	}

	// Draw each child window with its pre-baked uniforms, bottom to top,
	// each preceded by its tear-off halo when active (the halo ring shows
	// only beyond the window frame, and windows above may cover it).
	for _, childIface := range childWindowList.Windows {
		winValue := reflect.ValueOf(childIface)
		windowID := uint32(winValue.Pointer())
		surf, ok := r.windowSurfaces[windowID]
		if !ok {
			continue
		}

		var winBounds core.UnitRect
		if wl, isWin := childIface.(WindowLike); isWin {
			winBounds = wl.Bounds()
		}

		haloActive := false
		if hw, isHalo := childIface.(tearHaloWindow); isHalo && hw.TearIndicatorActive() && !winBounds.IsEmpty() {
			haloActive = true
			// The halo rect is the window outset by its margin; give
			// drawOverlay those bounds so the stroke lands well inside
			// the padded texture instead of on its edge.
			bounds := winBounds
			cleanup, _, err := r.drawOverlay(renderPass, osWindow,
				outsetBounds(bounds, overlayStrokeOffset),
				func(p *core.Painter) { hw.PaintTearHalo(p, bounds) }, scale)
			if err == nil {
				overlayCleanups = append(overlayCleanups, cleanup)
			}
		}

		// A window with its tear-off halo up escapes the client-area
		// clip along with the halo — the "lifting over the chrome, about
		// to break out" affordance (matches the software path).
		if clipOK && !haloActive {
			renderPass.SetScissorRect(uint32(clipX), uint32(clipY), uint32(clipW), uint32(clipH))
		}

		// Drop shadow beneath the window, sharing its clip state.
		if cleanup, err := r.drawShadow(renderPass, desktopBindGroup, winBounds, core.UnitRect{},
			surfaceSize, framePpuW, framePpuH, frameAspect, windowShadowSpec); err == nil && cleanup != nil {
			overlayCleanups = append(overlayCleanups, cleanup)
		}

		// Paint order IS z-order, and the last request of a frame wins
		// (see core/textcaret.go), so a window higher in the stack takes
		// the caret from one below.
		if surf.caret.Requested() {
			frameCaret = surf.caret
		}

		renderPass.SetBindGroup(0, surf.bindGroup, nil)
		renderPass.SetBindGroup(1, surf.uniformBindGroup, nil)
		renderPass.Draw(6, 1, 0, 0) // Draw quad at window position
		if clipOK && !haloActive {
			renderPass.SetScissorRect(0, 0, uint32(surfacePxW), uint32(surfacePxH))
		}
	}

	// Overlay layers, bottom to top: the menu bar's open dropdown first,
	// then popups (combo box lists, context menus). A popup opened FROM a
	// menu must paint above the menu that spawned it, so popups come last.

	// Step 4: The active menu bar dropdown, over its drop shadow. The
	// anchor (the title on the bar) joins the shadow shape so title and
	// menu read as one piece casting it.
	if childWindowList.MenuDropdown != nil {
		if bounds, anchor, paint, ok := overlayBoundsAndPaint(childWindowList.MenuDropdown); ok {
			if cleanup, err := r.drawShadow(renderPass, desktopBindGroup, bounds, anchor,
				surfaceSize, framePpuW, framePpuH, frameAspect, overlayShadowSpec); err == nil && cleanup != nil {
				overlayCleanups = append(overlayCleanups, cleanup)
			}
			if cleanup, caret, err := r.drawOverlay(renderPass, osWindow, bounds, paint, scale); err == nil {
				overlayCleanups = append(overlayCleanups, cleanup)
				if caret.Requested() {
					frameCaret = caret
				}
			} else {
				fmt.Fprintf(os.Stderr, "compositor: menu dropdown layer failed: %v\n", err)
			}
		}
	}

	// Step 5: Popups — the topmost layer, each over its drop shadow
	// (unioned with its opening control, e.g. the combo box).
	for _, popupIface := range childWindowList.Popups {
		bounds, anchor, paint, ok := overlayBoundsAndPaint(popupIface)
		if !ok {
			continue
		}
		if cleanup, err := r.drawShadow(renderPass, desktopBindGroup, bounds, anchor,
			surfaceSize, framePpuW, framePpuH, frameAspect, overlayShadowSpec); err == nil && cleanup != nil {
			overlayCleanups = append(overlayCleanups, cleanup)
		}
		if cleanup, caret, err := r.drawOverlay(renderPass, osWindow, bounds, paint, scale); err == nil {
			overlayCleanups = append(overlayCleanups, cleanup)
			if caret.Requested() {
				frameCaret = caret
			}
		} else {
			fmt.Fprintf(os.Stderr, "compositor: popup layer failed: %v\n", err)
		}
	}
	renderPass.End()

	// The frame's caret goes to the platform, which owns the surface.
	// Empty means no layer asked for it, which correctly hides it.
	osWindow.frameCaret = frameCaret
	osWindow.frameCaretSet = true

	// Rotation demo: while the effect runs, spin the cube (textured with
	// the desktop content) over the composited scene.
	if _, _, _, active := r.effectParams(); active {
		if cleanup, err := r.recordCubePass(encoder, surfaceView, desktopView, frameAspect); err == nil && cleanup != nil {
			overlayCleanups = append(overlayCleanups, cleanup)
		}
	}

	cmdBuffer, _ := encoder.Finish()
	_, err = r.queue.Submit(cmdBuffer)

	// Release temporary resources after GPU has the commands. The base
	// layer's texture is NOT among them — it is cached across frames.
	// Finish() transferred the native command encoder to this command buffer;
	// Release() recycles it into the device's encoder pool. Skipping it leaks
	// one native encoder per composited frame (see Present). Release is nil-safe.
	cmdBuffer.Release()
	surfaceView.Release()

	if err != nil {
		return err
	}
	osWindow.gpuSurface.Present(surfaceTexture)

	return nil
}

// uploadBackendToTexture creates a GPU texture from a raster backend.
// uploadPixels converts a backend's RGBA image to BGRA and writes it
// into an existing GPU texture sized to match.
func (r *WebGPURenderer) uploadPixels(texture *wgpu.Texture, img *image.RGBA) {
	bounds := img.Bounds()
	width := uint32(bounds.Dx())
	height := uint32(bounds.Dy())

	pixelData, bytesPerRow := bgraPixels(img)

	r.queue.WriteTexture(
		&wgpu.ImageCopyTexture{
			Texture:  texture,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
			Aspect:   0,
		},
		pixelData,
		&wgpu.ImageDataLayout{
			Offset:       0,
			BytesPerRow:  bytesPerRow,
			RowsPerImage: height,
		},
		&wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
	)
}

// createBoundTexture creates a texture sized for the image, uploads the
// image into it, and builds the sampler bind group the blit pipeline
// binds at group 0.
func (r *WebGPURenderer) createBoundTexture(img *image.RGBA) (*wgpu.Texture, *wgpu.TextureView, *wgpu.BindGroup, error) {
	bounds := img.Bounds()
	width := uint32(bounds.Dx())
	height := uint32(bounds.Dy())

	texture, err := r.device.CreateTexture(&wgpu.TextureDescriptor{
		Size: wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatBGRA8Unorm,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	r.uploadPixels(texture, img)

	textureView, err := r.device.CreateTextureView(texture, nil)
	if err != nil {
		texture.Release()
		return nil, nil, nil, err
	}

	bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: textureView},
			{Binding: 1, Sampler: r.blitSampler},
		},
	})
	if err != nil {
		textureView.Release()
		texture.Release()
		return nil, nil, nil, err
	}

	return texture, textureView, bindGroup, nil
}

// uploadBackendToTexture creates a GPU texture from a raster backend.
func (r *WebGPURenderer) uploadBackendToTexture(backend *raster.Backend) (*wgpu.Texture, *wgpu.TextureView, *wgpu.BindGroup, error) {
	img := backend.Image()
	if img == nil {
		return nil, nil, nil, fmt.Errorf("backend has no image")
	}
	return r.createBoundTexture(img)
}

// writeCombinedUniforms writes the shared block into an existing uniform
// buffer for an ordinary textured layer: the rotation demo's current
// effect state plus the quad position from windowNDC, with mode 0 so the
// fragment stage blits its texture. Every layer gets the same effect
// values, which is what makes the whole composited scene rotate rigidly.
func (r *WebGPURenderer) writeCombinedUniforms(buffer *wgpu.Buffer, bounds core.UnitRect, surfaceSize core.UnitSize, aspect float32) {
	angle, enabledF, scale, _ := r.effectParams()
	x, y, w, h := windowNDC(bounds, surfaceSize)
	data := combinedUniformData{
		angle, enabledF, scale, aspect,
		x, y, w, h, // pos_x, pos_y, size_w, size_h
		// mode 0 and the shadow fields unused; the rest of the block
		// stays zeroed.
	}
	data.setNoTiling()
	r.queue.WriteBuffer(buffer, 0, combinedUniformBytes(&data))
}

// createWindowUniformBuffer creates a uniform buffer for a window with position baked in.
func (r *WebGPURenderer) createWindowUniformBuffer(bounds core.UnitRect, surfaceSize core.UnitSize, aspect float32) (*wgpu.Buffer, *wgpu.BindGroup, error) {
	buffer, err := r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  combinedUniformSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, nil, err
	}

	r.writeCombinedUniforms(buffer, bounds, surfaceSize, aspect)

	bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitUniformLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: buffer, Size: combinedUniformSize},
		},
	})
	if err != nil {
		buffer.Release()
		return nil, nil, err
	}

	return buffer, bindGroup, nil
}

// releaseWindowSurface frees every GPU resource a WindowSurface owns.
func (r *WebGPURenderer) releaseWindowSurface(surf *WindowSurface) {
	if surf.uniformBindGroup != nil {
		surf.uniformBindGroup.Release()
		surf.uniformBindGroup = nil
	}
	if surf.uniformBuffer != nil {
		surf.uniformBuffer.Release()
		surf.uniformBuffer = nil
	}
	if surf.bindGroup != nil {
		surf.bindGroup.Release()
		surf.bindGroup = nil
	}
	if surf.textureView != nil {
		surf.textureView.Release()
		surf.textureView = nil
	}
	if surf.texture != nil {
		surf.texture.Release()
		surf.texture = nil
	}
	surf.backend = nil
}

// overlayStrokeOffset is the padding around overlay layers (popups, menu
// dropdowns) so outer strokes drawn just outside their nominal bounds
// still land on the texture: 2 device pixels per side.
const overlayStrokeOffset = core.Unit(2)

// minOverlayTexPx is the smallest overlay-layer texture, in device pixels per
// axis, that composites reliably. WebGPU requires a buffer-to-texture copy's
// bytesPerRow to be a multiple of 256, and narrow overlay textures (a short
// ≡/Help dropdown, or any dropdown once the font is shrunk) blitted fully
// transparent on the Windows/DX12 backend — the shadow showed but the body did
// not. Padding every overlay texture up to at least this size in each axis
// keeps the row stride comfortably above that boundary; the extra pixels are
// transparent and the quad grows to match, so nothing is distorted or moved.
const minOverlayTexPx = 256

// overlayBoundsAndPaint extracts the Bounds rect and Paint function from
// an overlay value (window.PopupOverlay, or the anonymous menu dropdown
// struct) by field name — reflection sidesteps the import cycle between
// the sdl and window packages.
func overlayBoundsAndPaint(overlay interface{}) (bounds, anchor core.UnitRect, paint func(*core.Painter), ok bool) {
	v := reflect.ValueOf(overlay)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return core.UnitRect{}, core.UnitRect{}, nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}

	boundsField := v.FieldByName("Bounds")
	paintField := v.FieldByName("Paint")
	if !boundsField.IsValid() || !paintField.IsValid() {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}

	bounds, bOK := boundsField.Interface().(core.UnitRect)
	if !bOK || bounds.Width <= 0 || bounds.Height <= 0 {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}
	paint, pOK := paintField.Interface().(func(*core.Painter))
	if !pOK || paint == nil {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}

	// Anchor is optional: the opening control's rect, unioned into the
	// popup's drop shadow so both cast one shape.
	if anchorField := v.FieldByName("Anchor"); anchorField.IsValid() {
		if a, aOK := anchorField.Interface().(core.UnitRect); aOK {
			anchor = a
		}
	}
	return bounds, anchor, paint, true
}

// drawShadow records one drop shadow into the render pass: an analytic
// rounded-rect distance field (caster unioned with its anchor, cast
// down-right, faded across the blur) evaluated by the fragment stage.
// Nothing is rasterized on the CPU and nothing is cached, so a moving or
// resizing window costs exactly what a still one does, and the falloff
// is exact at any density instead of being resampled from an image.
//
// It draws through the SAME pipeline as every other layer, switching
// behaviour with the uniform block's mode field rather than with a
// pipeline of its own. That is not a stylistic choice — see
// blitBindGroups for the slot-numbering rule a second pipeline broke,
// silently, by putting a uniform in group 0.
//
// The returned cleanup must run after Submit; it is nil when there is
// nothing to draw.
func (r *WebGPURenderer) drawShadow(
	renderPass *wgpu.RenderPassEncoder,
	textureBindGroup *wgpu.BindGroup,
	caster, anchor core.UnitRect,
	surfaceSize core.UnitSize,
	ppuW, ppuH float64,
	aspect float32,
	spec shadowSpec,
) (func(), error) {
	if caster.IsEmpty() || textureBindGroup == nil {
		return nil, nil
	}

	quad := shadowQuadBounds(caster, anchor, spec)
	if shadowDebugFlag != 0 && shadowDebugCount < 8 {
		shadowDebugCount++
		x, y, w, h := windowNDC(quad, surfaceSize)
		fmt.Fprintf(os.Stderr,
			"kittytk-shadow: caster=%+v quad=%+v surface=%+v ndc=(%.3f,%.3f %.3fx%.3f) ppu=(%.2f,%.2f)\n",
			caster, quad, surfaceSize, x, y, w, h, ppuW, ppuH)
	}

	// Shape parameters, in pixels relative to the quad's origin — the
	// space the shader's distance field works in.
	c1x0, c1y0, c1x1, c1y1 := rectPxIn(quad, caster.Translated(spec.offsetX, spec.offsetY), ppuW, ppuH)
	var c2x0, c2y0, c2x1, c2y1 float32
	var px0, py0, px1, py1 float32
	if !anchor.IsEmpty() {
		c2x0, c2y0, c2x1, c2y1 = rectPxIn(quad, anchor.Translated(spec.offsetX, spec.offsetY), ppuW, ppuH)
		// The hole is the anchor where it actually sits, not where its
		// shadow falls.
		px0, py0, px1, py1 = rectPxIn(quad, anchor, ppuW, ppuH)
	}

	buffer, bindGroup, err := r.createShadowUniformBuffer(quad, surfaceSize, aspect, shadowUniforms{
		blurPx:   float32(float64(spec.blur) * ppuW),
		alpha:    spec.alpha,
		radiusPx: float32(float64(spec.radius) * ppuW),
		quadPxW:  float32(float64(quad.Width) * ppuW),
		quadPxH:  float32(float64(quad.Height) * ppuH),
		rect1:    [4]float32{c1x0, c1y0, c1x1, c1y1},
		rect2:    [4]float32{c2x0, c2y0, c2x1, c2y1},
		punch:    [4]float32{px0, py0, px1, py1},
	})
	if err != nil {
		return nil, err
	}

	// The texture bind group is bound only to satisfy the pipeline's
	// layout; the shadow branch ignores what it samples.
	renderPass.SetBindGroup(0, textureBindGroup, nil)
	renderPass.SetBindGroup(1, bindGroup, nil)
	renderPass.Draw(6, 1, 0, 0)

	cleanup := func() {
		bindGroup.Release()
		buffer.Release()
	}
	return cleanup, nil
}

// shadowUniforms is drawShadow's half of the shared uniform block, all
// lengths already converted to pixels within the shadow's quad.
type shadowUniforms struct {
	blurPx   float32
	alpha    float32
	radiusPx float32
	quadPxW  float32
	quadPxH  float32
	rect1    [4]float32 // caster, cast-shifted: minX, minY, maxX, maxY
	rect2    [4]float32 // anchor, cast-shifted (all zero when absent)
	punch    [4]float32 // anchor where it sits: the hole (all zero when absent)
}

// createShadowUniformBuffer builds a uniform buffer holding the same
// block every layer uses, with mode set so the fragment stage takes the
// shadow branch. The quad is positioned by the identical vertex path, so
// a shadow rotates and scales with the scene like its caster.
func (r *WebGPURenderer) createShadowUniformBuffer(
	quad core.UnitRect,
	surfaceSize core.UnitSize,
	aspect float32,
	s shadowUniforms,
) (*wgpu.Buffer, *wgpu.BindGroup, error) {
	buffer, err := r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  combinedUniformSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, nil, err
	}

	angle, enabledF, scale, _ := r.effectParams()
	x, y, w, h := windowNDC(quad, surfaceSize)
	data := combinedUniformData{
		angle, enabledF, scale, aspect,
		x, y, w, h,
		1, s.blurPx, s.alpha, s.radiusPx,
		s.quadPxW, s.quadPxH,
		s.rect1[0], s.rect1[1], s.rect1[2], s.rect1[3],
		s.rect2[0], s.rect2[1], s.rect2[2], s.rect2[3],
		s.punch[0], s.punch[1], s.punch[2], s.punch[3],
		shadowDebugFlag,
	}
	data.setNoTiling() // unused by the shadow branch, but never zero
	r.queue.WriteBuffer(buffer, 0, combinedUniformBytes(&data))

	bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitUniformLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: buffer, Size: combinedUniformSize},
		},
	})
	if err != nil {
		buffer.Release()
		return nil, nil, err
	}
	return buffer, bindGroup, nil
}

// wallpaperCache is the desktop's background tile on the GPU: one small
// texture, bound with a REPEAT-addressed sampler so the hardware tiles
// it across the whole surface.
//
// This is what lets the wallpaper be any size at no cost. The old path
// filled every pixel of the desktop on the CPU from an 8x8 bitmap; here
// a 16x16 pattern and a 512x512 photograph are the same single quad, and
// the tile uploads only when its revision moves.
type wallpaperCache struct {
	texture     *wgpu.Texture
	textureView *wgpu.TextureView
	bindGroup   *wgpu.BindGroup
	widthPx     uint32
	heightPx    uint32
	revision    uint64
	smooth      bool
	valid       bool
}

// boolToFloat is the shader's idea of a flag.
func boolToFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func (w *wallpaperCache) release() {
	if w.bindGroup != nil {
		w.bindGroup.Release()
		w.bindGroup = nil
	}
	if w.textureView != nil {
		w.textureView.Release()
		w.textureView = nil
	}
	if w.texture != nil {
		w.texture.Release()
		w.texture = nil
	}
	w.valid = false
}

// drawWallpaper records the tiled desktop background: a fullscreen quad
// whose texture coordinates run 0..surface/tile, sampled with repeat.
// The tile is uploaded only when its revision changes.
//
// It draws FIRST, under the base layer, which is why the base layer
// clears to transparent — see the desktop's FrameBase.
func (r *WebGPURenderer) drawWallpaper(
	renderPass *wgpu.RenderPassEncoder,
	layer *platform.WallpaperLayer,
	surfaceSize core.UnitSize,
	surfacePxW, surfacePxH int,
	aspect float32,
) (func(), error) {
	if layer == nil || layer.Tile == nil || surfacePxW <= 0 || surfacePxH <= 0 {
		return nil, nil
	}
	bounds := layer.Tile.Bounds()
	tileW, tileH := bounds.Dx(), bounds.Dy()
	if tileW <= 0 || tileH <= 0 {
		return nil, nil
	}

	// Where one copy lands, in surface pixels. The GPU does the scaling,
	// so the texture stays the image's own size whatever the mode — a
	// stretched 4K photograph uploads exactly once, at 4K, and the
	// sampler handles the rest.
	drawW, drawH, offX, offY := layer.Layout.Resolve(tileW, tileH, surfacePxW, surfacePxH)
	if drawW <= 0 || drawH <= 0 {
		return nil, nil
	}

	sampler := r.wallpaperSampler
	if layer.Layout.Smooth {
		sampler = r.wallpaperSmoothSampler
	}

	if !r.wallpaper.valid || r.wallpaper.revision != layer.Revision ||
		r.wallpaper.widthPx != uint32(tileW) || r.wallpaper.heightPx != uint32(tileH) ||
		r.wallpaper.smooth != layer.Layout.Smooth {
		r.wallpaper.release()

		texture, textureView, _, err := r.createBoundTexture(layer.Tile)
		if err != nil {
			return nil, err
		}
		// Rebound against the repeat sampler; createBoundTexture's own
		// bind group clamps, which would stretch one tile's edge pixels
		// across the whole desktop instead of repeating it.
		bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: r.blitLayout,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, TextureView: textureView},
				{Binding: 1, Sampler: sampler},
			},
		})
		if err != nil {
			textureView.Release()
			texture.Release()
			return nil, err
		}
		r.wallpaper = wallpaperCache{
			texture:     texture,
			textureView: textureView,
			bindGroup:   bindGroup,
			widthPx:     uint32(tileW),
			heightPx:    uint32(tileH),
			revision:    layer.Revision,
			smooth:      layer.Layout.Smooth,
			valid:       true,
		}
	}

	// Uniforms for a fullscreen quad. The texture spans surface/drawn
	// copies, shifted so the anchored copy starts where the layout put
	// it. A fractional remainder is what the last partial row and column
	// should show, so no rounding here.
	buffer, err := r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  combinedUniformSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	angle, enabledF, scale, _ := r.effectParams()
	x, y, w, h := windowNDC(
		core.UnitRect{Width: surfaceSize.Width, Height: surfaceSize.Height}, surfaceSize)
	data := combinedUniformData{angle, enabledF, scale, aspect, x, y, w, h}
	tileX, tileY := layer.Layout.Tiling.Axes()
	data.setTiling(
		float32(surfacePxW)/float32(drawW), float32(surfacePxH)/float32(drawH),
		float32(offX)/float32(drawW), float32(offY)/float32(drawH),
		boolToFloat(tileX), boolToFloat(tileY))
	r.queue.WriteBuffer(buffer, 0, combinedUniformBytes(&data))

	bindGroup, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: r.blitUniformLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: buffer, Size: combinedUniformSize},
		},
	})
	if err != nil {
		buffer.Release()
		return nil, err
	}

	renderPass.SetBindGroup(0, r.wallpaper.bindGroup, nil)
	renderPass.SetBindGroup(1, bindGroup, nil)
	renderPass.Draw(6, 1, 0, 0)

	return func() {
		bindGroup.Release()
		buffer.Release()
	}, nil
}

// refreshBaseLayer returns the OS window's cached base-layer texture,
// repainting and re-uploading it only when the desktop chrome changed.
//
// The saving is the largest one the compositor has: the base is the
// full-surface texture, so a repaint means tiling the wallpaper across
// every pixel, converting every pixel to BGRA and uploading the lot —
// and before this it also allocated and freed the texture each frame.
// A desktop whose chrome is sitting still now costs one quad.
func (r *WebGPURenderer) refreshBaseLayer(
	osWindow *nativeWin,
	list *platform.ChildWindowList,
	renderWindow func(*nativeWin),
) (*baseLayer, error) {
	if r.baseLayers == nil {
		r.baseLayers = make(map[uint32]*baseLayer)
	}
	base := r.baseLayers[osWindow.id]

	sig := paintSignature{
		revision:    list.BaseRevision,
		hasRevision: list.HasBaseRevision,
		fontSize:    osWindow.backend.FontSize(),
		metrics:     osWindow.backend.Metrics(),
	}
	if img := osWindow.backend.Image(); img != nil {
		sig.widthPx, sig.heightPx = img.Bounds().Dx(), img.Bounds().Dy()
	}

	now := time.Now()
	stale := base == nil || base.texture == nil ||
		base.paintedBackend != osWindow.backend ||
		needsRepaint(base.painted, sig, now.Sub(base.paintedAt),
			heartbeatInterval(osWindow.id), false, compositorAlwaysRepaint)
	if !stale {
		r.frameSkipped++
		return base, nil
	}

	// Paint the chrome into the OS window's backend. This is the callback
	// that reaches SurfaceHandler.FrameBase.
	renderWindow(osWindow)

	img := osWindow.backend.Image()
	if img == nil {
		return nil, fmt.Errorf("OS window backend has no image for the base layer")
	}
	// The backend can have been resized by the paint above, so take the
	// size that was actually produced.
	sig.widthPx, sig.heightPx = img.Bounds().Dx(), img.Bounds().Dy()
	w, h := uint32(sig.widthPx), uint32(sig.heightPx)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("base layer is %dx%d", w, h)
	}

	// Rebuild the GPU objects only when the surface changed size;
	// otherwise the existing texture is written in place.
	if base == nil || base.texture == nil || base.widthPx != w || base.heightPx != h {
		if base != nil {
			base.release()
		}
		texture, textureView, bindGroup, err := r.createBoundTexture(img)
		if err != nil {
			delete(r.baseLayers, osWindow.id)
			return nil, fmt.Errorf("failed to create the base layer texture: %w", err)
		}
		base = &baseLayer{
			texture:     texture,
			textureView: textureView,
			bindGroup:   bindGroup,
			widthPx:     w,
			heightPx:    h,
		}
		r.baseLayers[osWindow.id] = base
	} else {
		r.uploadPixels(base.texture, img)
	}

	base.painted = sig
	base.paintedAt = now
	base.paintedBackend = osWindow.backend
	r.framePainted++
	return base, nil
}

// refreshPresentTexture returns the cached full-surface texture for a
// window presented WITHOUT compositing (a torn-off window, or any
// surface whose handler exposes no child windows), re-uploading its
// pixels only when the platform actually repainted them.
//
// It shares the baseLayer cache with the compositor: both are "this OS
// window's full-surface texture", and a window only ever takes one path
// or the other in a given frame.
func (r *WebGPURenderer) refreshPresentTexture(w *nativeWin, backend *raster.Backend) (*baseLayer, error) {
	img := backend.Image()
	if img == nil {
		return nil, fmt.Errorf("backend image is nil")
	}
	pxW, pxH := uint32(img.Bounds().Dx()), uint32(img.Bounds().Dy())
	if pxW == 0 || pxH == 0 {
		return nil, fmt.Errorf("backend is %dx%d", pxW, pxH)
	}

	if r.baseLayers == nil {
		r.baseLayers = make(map[uint32]*baseLayer)
	}
	layer := r.baseLayers[w.id]

	// A different backend means a resize or font zoom handed us fresh
	// zero-filled memory; its pixels have to reach the GPU whatever the
	// dirty flag says.
	if layer == nil || layer.texture == nil ||
		layer.widthPx != pxW || layer.heightPx != pxH ||
		layer.paintedBackend != backend {
		if layer != nil {
			layer.release()
		}
		texture, textureView, bindGroup, err := r.createBoundTexture(img)
		if err != nil {
			delete(r.baseLayers, w.id)
			return nil, err
		}
		layer = &baseLayer{
			texture:     texture,
			textureView: textureView,
			bindGroup:   bindGroup,
			widthPx:     pxW,
			heightPx:    pxH,
		}
		r.baseLayers[w.id] = layer
	} else if w.pixelsDirty {
		r.uploadPixels(layer.texture, img)
	}

	layer.paintedBackend = backend
	w.pixelsDirty = false
	return layer, nil
}

// releaseBaseLayer drops an OS window's cached base layer (window
// closed, or the renderer shutting down).
func (r *WebGPURenderer) releaseBaseLayer(id uint32) {
	if base, ok := r.baseLayers[id]; ok {
		base.release()
		delete(r.baseLayers, id)
	}
}

// subtreeRepaintRevision reads a child window's repaint counter. ok is
// false for anything that does not report one, which makes the caller
// repaint every frame — the behaviour before this cache existed.
func subtreeRepaintRevision(child interface{}) (rev uint64, ok bool) {
	tracker, isTracker := child.(core.SubtreeRepaintTracker)
	if !isTracker {
		return 0, false
	}
	return tracker.SubtreeRepaintRevision(), true
}

// compositorStats prints how much of the per-window work each second of
// frames actually did, under KITTYTK_COMPOSITOR_STATS.
var compositorStats = os.Getenv("KITTYTK_COMPOSITOR_STATS") != ""

// reportCompositorStats accumulates this frame's repaint tally and emits
// a line about once a second. The tally counts the base layer alongside
// the windows — it is one more cached texture, and the biggest.
func (r *WebGPURenderer) reportCompositorStats(windows int) {
	painted, skipped := r.framePainted, r.frameSkipped
	r.framePainted, r.frameSkipped = 0, 0
	if !compositorStats {
		return
	}

	r.statsFrames++
	r.statsPainted += painted
	r.statsSkipped += skipped
	if r.statsLastEmit.IsZero() {
		r.statsLastEmit = time.Now()
		return
	}
	elapsed := time.Since(r.statsLastEmit)
	if elapsed < time.Second {
		return
	}

	total := r.statsPainted + r.statsSkipped
	pct := 0.0
	if total > 0 {
		pct = 100 * float64(r.statsSkipped) / float64(total)
	}
	fmt.Fprintf(os.Stderr,
		"kittytk-compositor: %d frames in %v, %d windows: %d layer-paints, %d skipped (%.0f%% cached)\n",
		r.statsFrames, elapsed.Round(time.Millisecond), windows, r.statsPainted, r.statsSkipped, pct)
	r.statsFrames, r.statsPainted, r.statsSkipped = 0, 0, 0
	r.statsLastEmit = time.Now()
}

// shadowDebugFlag is 1 under KITTYTK_SHADOW_DEBUG, which paints every
// shadow's footprint opaque red and logs its geometry — the fastest way
// to tell a draw that never lands from one whose output is simply too
// faint to see.
var shadowDebugFlag = func() float32 {
	if os.Getenv("KITTYTK_SHADOW_DEBUG") != "" {
		return 1
	}
	return 0
}()

// shadowDebugCount bounds the debug chatter to the first few shadows.
var shadowDebugCount int

// drawOverlay renders one overlay layer (popup or menu dropdown) into a
// transient texture and records its quad into the render pass. The
// returned cleanup releases the layer's GPU resources and MUST run only
// after the pass's commands are submitted.
func (r *WebGPURenderer) drawOverlay(
	renderPass *wgpu.RenderPassEncoder,
	osWindow *nativeWin,
	bounds core.UnitRect,
	paint func(*core.Painter),
	scale int,
) (func(), core.TextCaret, error) {
	backendImg := osWindow.backend.Image()
	if backendImg == nil {
		return nil, core.TextCaret{}, fmt.Errorf("OS window backend has no image")
	}
	backendSize := osWindow.backend.Size()
	metrics := osWindow.backend.Metrics()

	backendBounds := backendImg.Bounds()
	// A host dragged to near-zero height (or width) has a unit extent that
	// rounds to 0; float64(n)/0 is +Inf, which math.Round carries into int() as
	// MaxInt64 and blows up the overlay texture. Nothing to draw an overlay onto
	// at zero area — bail so the caller skips it this frame.
	if backendSize.Width <= 0 || backendSize.Height <= 0 {
		return nil, core.TextCaret{}, fmt.Errorf("host backend has zero unit area %dx%d", backendSize.Width, backendSize.Height)
	}
	pixelsPerUnitW := float64(backendBounds.Dx()) / float64(backendSize.Width)
	pixelsPerUnitH := float64(backendBounds.Dy()) / float64(backendSize.Height)

	// Pad the texture so outer strokes drawn just outside the nominal
	// bounds survive, with the padding converted at the surface density
	// so texture pixels match the outset on-screen quad exactly.
	widthPx, heightPx, _, _ := overlayTexturePx(bounds, pixelsPerUnitW, pixelsPerUnitH, overlayStrokeOffset)
	if widthPx <= 0 || heightPx <= 0 {
		return nil, core.TextCaret{}, fmt.Errorf("overlay pixel size %dx%d invalid", widthPx, heightPx)
	}

	// Some GPU backends (Windows/DX12 in particular) blit a too-small overlay
	// texture as fully transparent: a narrow dropdown — the ≡ system menu, a
	// Help menu, or ANY menu once the font is shrunk — showed its drop shadow
	// but no body, while wider menus painted fine. Pad the texture up to a safe
	// minimum in each axis. The content still paints at the top-left (the offset
	// below is unchanged), so the extra pixels are transparent and fall to the
	// bottom-right, OFF the menu; the on-screen quad grows to match so the
	// texture maps 1:1 and nothing distorts. The shadow uses the real bounds, so
	// the visible menu keeps its exact position and size.
	texW, texH := widthPx, heightPx
	if texW < minOverlayTexPx {
		texW = minOverlayTexPx
	}
	if texH < minOverlayTexPx {
		texH = minOverlayTexPx
	}

	overlayBackend, err := raster.NewScaled(texW, texH, scale)
	if err != nil {
		return nil, core.TextCaret{}, err
	}
	overlayBackend.SetCellMetrics(metrics)
	overlayBackend.SetFontSize(osWindow.backend.FontSize())

	// The overlay's Paint expects a painter at screen origin and offsets
	// itself to its bounds; our backend covers only the overlay (plus
	// stroke padding), so a negative offset cancels that placement.
	overlayBackend.BeginFrame()
	painter := core.NewPainter(overlayBackend)
	painter.ResetTextCaretRequest()
	paint(painter.WithOffset(-bounds.X+overlayStrokeOffset, -bounds.Y+overlayStrokeOffset))
	overlayBackend.EndFrame()

	// The paint function offsets itself to the overlay's bounds, so a
	// caret request from inside it is already in surface coordinates.
	caret := painter.TextCaretRequest()

	img := overlayBackend.Image()
	if img == nil {
		return nil, core.TextCaret{}, fmt.Errorf("overlay backend returned no image")
	}

	texture, textureView, bindGroup, err := r.createBoundTexture(img)
	if err != nil {
		return nil, core.TextCaret{}, err
	}

	// The on-screen quad grows by the same padding the texture gained. When the
	// texture was padded up to the minimum above, grow the quad's unit extent to
	// the padded pixel size too, so the texture still maps 1:1 (no stretch) and
	// the transparent padding simply extends off the bottom-right of the menu.
	aspect := float32(1)
	if backendBounds.Dy() > 0 {
		aspect = float32(backendBounds.Dx()) / float32(backendBounds.Dy())
	}
	quad := outsetBounds(bounds, overlayStrokeOffset)
	if texW != widthPx {
		quad.Width = core.Unit(math.Round(float64(texW) / pixelsPerUnitW))
	}
	if texH != heightPx {
		quad.Height = core.Unit(math.Round(float64(texH) / pixelsPerUnitH))
	}
	uniformBuffer, uniformBindGroup, err := r.createWindowUniformBuffer(
		quad, backendSize, aspect)
	if err != nil {
		bindGroup.Release()
		textureView.Release()
		texture.Release()
		return nil, core.TextCaret{}, err
	}

	renderPass.SetBindGroup(0, bindGroup, nil)
	renderPass.SetBindGroup(1, uniformBindGroup, nil)
	renderPass.Draw(6, 1, 0, 0)

	cleanup := func() {
		uniformBindGroup.Release()
		uniformBuffer.Release()
		bindGroup.Release()
		textureView.Release()
		texture.Release()
	}
	return cleanup, caret, nil
}
