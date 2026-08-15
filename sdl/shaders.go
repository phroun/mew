package sdl

import "unsafe"

// The GPU compositor's shader sources and the uniform block they share.
// Deliberately free of build tags: WGSL text and a memory layout are
// pure data, so tests can check them on any machine with no SDL library
// and no GPU. The invariants they encode (both stages declaring the same
// block, and group 0 holding no buffer bindings) are ones a compiler
// cannot catch and a bad value merely draws wrong.

// WGSL shaders for blitting the UI texture to the screen
const blitVertexShader = `
// Combined uniforms in group 1 (since group 2 doesn't work)
struct CombinedUniforms {
    angle: f32,      // rotation (from effects)
    enabled: f32,    // effects enabled
    scale: f32,      // effects scale (2.0 = scene at 50%)
    aspect: f32,     // surface width/height, for undistorted rotation
    pos_x: f32,      // window x in NDC
    pos_y: f32,      // window y in NDC
    size_w: f32,     // window width in NDC
    size_h: f32,     // window height in NDC

    // Shadow mode. A layer draws its texture when mode is 0; when it is
    // 1 the fragment stage draws an analytic drop shadow instead and
    // the bound texture is ignored. Both go through THIS pipeline: a
    // second pipeline switched mid render pass did not draw at all.
    mode: f32,
    blur_px: f32,
    shadow_alpha: f32,
    radius_px: f32,
    quad_px: vec2<f32>,   // quad size in pixels
    rect1_min: vec2<f32>, // caster, cast-shifted, in quad pixels
    rect1_max: vec2<f32>,
    rect2_min: vec2<f32>, // anchor, cast-shifted (empty when max <= min)
    rect2_max: vec2<f32>,
    punch_min: vec2<f32>, // anchor UNshifted: the hole
    punch_max: vec2<f32>,
    debug: f32,

    // How the layer's texture maps across its quad. An ordinary layer
    // samples once: tile (1,1), offset (0,0), repeat (1,1).
    //
    // The wallpaper sets tile = surface/drawn so the sampler repeats a
    // small image over the whole desktop, offset to the anchored copy's
    // origin, and repeat 0 on an axis that must NOT tile — there the
    // fragment stage draws nothing outside a single copy rather than
    // letting the sampler wrap it.
    tile: vec2<f32>,
    tile_offset: vec2<f32>,
    tile_repeat: vec2<f32>,

    // Trailing padding. WGSL sizes the fields above at 136 bytes; padding
    // to 144 keeps the Go side a round [36]float32 and the binding size a
    // multiple of 16, which every backend's uniform rules are happy with.
    pad0: f32,
    pad1: f32,
}

@group(1) @binding(0) var<uniform> uniforms: CombinedUniforms;

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) texCoord: vec2<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) vertex_index: u32) -> VertexOutput {
    // Generate a proper quad (2 triangles = 6 vertices)
    var pos = array<vec2<f32>, 6>(
        vec2<f32>(0.0, 0.0),  // Triangle 1: bottom-left
        vec2<f32>(1.0, 0.0),  //            bottom-right
        vec2<f32>(0.0, 1.0),  //            top-left
        vec2<f32>(1.0, 0.0),  // Triangle 2: bottom-right
        vec2<f32>(1.0, 1.0),  //            top-right
        vec2<f32>(0.0, 1.0)   //            top-left
    );

    var texCoords = array<vec2<f32>, 6>(
        vec2<f32>(0.0, 1.0),  // Flipped Y for texture
        vec2<f32>(1.0, 1.0),
        vec2<f32>(0.0, 0.0),
        vec2<f32>(1.0, 1.0),
        vec2<f32>(1.0, 0.0),
        vec2<f32>(0.0, 0.0)
    );

    // Apply position and size from uniforms
    let scaled_pos = pos[vertex_index] * vec2<f32>(uniforms.size_w, uniforms.size_h);
    var final_pos = vec2<f32>(uniforms.pos_x, uniforms.pos_y) + scaled_pos;

    // Rotation demo: rigidly rotate and shrink the whole scene around the
    // surface center. Every layer (desktop base, windows, menus, popups)
    // carries the same effect values, so the composited scene turns as
    // one. Rotation happens in aspect-corrected space so circles stay
    // circles on non-square surfaces.
    if (uniforms.enabled > 0.5) {
        let inv = 1.0 / max(uniforms.scale, 0.001);
        let aspect = max(uniforms.aspect, 0.001);
        var q = vec2<f32>(final_pos.x * aspect, final_pos.y) * inv;
        let ang = -uniforms.angle;
        let c = cos(ang);
        let s = sin(ang);
        q = vec2<f32>(q.x * c - q.y * s, q.x * s + q.y * c);
        final_pos = vec2<f32>(q.x / aspect, q.y);
    }

    var output: VertexOutput;
    output.position = vec4<f32>(final_pos, 0.0, 1.0);
    output.texCoord = texCoords[vertex_index];
    return output;
}
`

const blitFragmentShader = `
struct CombinedUniforms {
    angle: f32,      // rotation (from effects)
    enabled: f32,    // effects enabled
    scale: f32,      // effects scale (2.0 = scene at 50%)
    aspect: f32,     // surface width/height, for undistorted rotation
    pos_x: f32,      // window x in NDC
    pos_y: f32,      // window y in NDC
    size_w: f32,     // window width in NDC
    size_h: f32,     // window height in NDC

    // Shadow mode. A layer draws its texture when mode is 0; when it is
    // 1 the fragment stage draws an analytic drop shadow instead and
    // the bound texture is ignored. Both go through THIS pipeline: a
    // second pipeline switched mid render pass did not draw at all.
    mode: f32,
    blur_px: f32,
    shadow_alpha: f32,
    radius_px: f32,
    quad_px: vec2<f32>,   // quad size in pixels
    rect1_min: vec2<f32>, // caster, cast-shifted, in quad pixels
    rect1_max: vec2<f32>,
    rect2_min: vec2<f32>, // anchor, cast-shifted (empty when max <= min)
    rect2_max: vec2<f32>,
    punch_min: vec2<f32>, // anchor UNshifted: the hole
    punch_max: vec2<f32>,
    debug: f32,

    // How the layer's texture maps across its quad. An ordinary layer
    // samples once: tile (1,1), offset (0,0), repeat (1,1).
    //
    // The wallpaper sets tile = surface/drawn so the sampler repeats a
    // small image over the whole desktop, offset to the anchored copy's
    // origin, and repeat 0 on an axis that must NOT tile — there the
    // fragment stage draws nothing outside a single copy rather than
    // letting the sampler wrap it.
    tile: vec2<f32>,
    tile_offset: vec2<f32>,
    tile_repeat: vec2<f32>,

    // Trailing padding. WGSL sizes the fields above at 136 bytes; padding
    // to 144 keeps the Go side a round [36]float32 and the binding size a
    // multiple of 16, which every backend's uniform rules are happy with.
    pad0: f32,
    pad1: f32,
}

@group(1) @binding(0) var<uniform> uniforms: CombinedUniforms;
@group(0) @binding(0) var ui_texture: texture_2d<f32>;
@group(0) @binding(1) var ui_sampler: sampler;

fn sd_round_rect(p: vec2<f32>, mn: vec2<f32>, mx: vec2<f32>, r: f32) -> f32 {
    let c = (mn + mx) * 0.5;
    let h = max((mx - mn) * 0.5 - vec2<f32>(r, r), vec2<f32>(0.0, 0.0));
    let q = abs(p - c) - h;
    return length(max(q, vec2<f32>(0.0, 0.0))) + min(max(q.x, q.y), 0.0) - r;
}

fn is_rect(mn: vec2<f32>, mx: vec2<f32>) -> bool {
    return mx.x > mn.x && mx.y > mn.y;
}

@fragment
fn fs_main(@builtin(position) fragPos: vec4<f32>, @location(0) texCoord: vec2<f32>) -> @location(0) vec4<f32> {
    // Sampled unconditionally: textureSample takes implicit derivatives,
    // which want uniform control flow. mode is uniform so a branch would
    // be legal, but sampling first sidesteps the question entirely and
    // costs one fetch on the handful of shadow quads per frame.
    let uv = texCoord * uniforms.tile - uniforms.tile_offset;
    let tex = textureSample(ui_texture, ui_sampler, uv);
    if (uniforms.mode < 0.5) {
        // An axis that does not repeat shows ONE copy and nothing
        // beyond it. The sampler wraps whatever it is handed, so the
        // rejection has to happen here.
        if (uniforms.tile_repeat.x < 0.5 && (uv.x < 0.0 || uv.x > 1.0)) {
            return vec4<f32>(0.0, 0.0, 0.0, 0.0);
        }
        if (uniforms.tile_repeat.y < 0.5 && (uv.y < 0.0 || uv.y > 1.0)) {
            return vec4<f32>(0.0, 0.0, 0.0, 0.0);
        }
        return tex;
    }

    // Drop shadow: the signed distance to the caster's rounded rect,
    // unioned with the opening control's, faded across the blur.
    let p = texCoord * uniforms.quad_px;

    var d = sd_round_rect(p, uniforms.rect1_min, uniforms.rect1_max, uniforms.radius_px);
    if (is_rect(uniforms.rect2_min, uniforms.rect2_max)) {
        d = min(d, sd_round_rect(p, uniforms.rect2_min, uniforms.rect2_max, uniforms.radius_px));
    }

    var a = uniforms.shadow_alpha * (1.0 - smoothstep(-uniforms.blur_px, uniforms.blur_px, d));

    // The opening control sits on a layer BELOW and is never redrawn
    // above the shadow, so punch it out or the shadow darkens the very
    // control it belongs to. Hard-edged on purpose: the hole's edges
    // coincide with the control's.
    if (is_rect(uniforms.punch_min, uniforms.punch_max)) {
        if (all(p >= uniforms.punch_min) && all(p <= uniforms.punch_max)) {
            a = 0.0;
        }
    }

    // KITTYTK_SHADOW_DEBUG paints the footprint opaque red instead, so a
    // screenshot separates "nothing drew" from "it drew, too faint".
    if (uniforms.debug > 0.5) {
        if (a > 0.002) {
            return vec4<f32>(1.0, 0.0, 0.0, 1.0);
        }
        return vec4<f32>(0.0, 0.0, 0.0, 0.0);
    }

    // Black, with the shape carried entirely in alpha. The blit blend is
    // SrcAlpha/OneMinusSrcAlpha, and a zero colour is unchanged by it.
    return vec4<f32>(0.0, 0.0, 0.0, a);
}
`

// 3D cube shaders
const cubeVertexShader = `
struct CubeUniforms {
    mvp: mat4x4<f32>,  // Model-View-Projection matrix
}

@group(1) @binding(0) var<uniform> cube_uniforms: CubeUniforms;

struct VertexInput {
    @location(0) position: vec3<f32>,
    @location(1) texCoord: vec2<f32>,
}

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) texCoord: vec2<f32>,
}

@vertex
fn vs_main(in: VertexInput) -> VertexOutput {
    var out: VertexOutput;
    out.position = cube_uniforms.mvp * vec4<f32>(in.position, 1.0);
    out.texCoord = in.texCoord;
    return out;
}
`

const cubeFragmentShader = `
@group(0) @binding(0) var cube_texture: texture_2d<f32>;
@group(0) @binding(1) var cube_sampler: sampler;

@fragment
fn fs_main(@location(0) texCoord: vec2<f32>) -> @location(0) vec4<f32> {
    return textureSample(cube_texture, cube_sampler, texCoord);
}
`

// The uniform block every blit draw binds: the effect state, the quad's
// NDC placement, and the shadow parameters that switch the fragment
// stage into its drop-shadow branch. WGSL sizes the declared fields at
// 112 bytes; the block pads to 128 so the Go side stays a round
// [32]float32 and the binding size a multiple of 16.
const (
	combinedUniformFloats = 36
	combinedUniformSize   = combinedUniformFloats * 4
)

// combinedUniformTileWord is where the block's `tile` vec2 starts, with
// tile_offset and tile_repeat following. A vec2 needs 8-byte alignment,
// so WGSL slips an unnamed pad word in after `debug` — hence 28 rather
// than 27.
//
// Every layer MUST set these: the fragment stage multiplies its texture
// coordinates by tile, so a zeroed block collapses the layer onto one
// texel and then rejects it for lying outside a copy.
const combinedUniformTileWord = 28

type combinedUniformData [combinedUniformFloats]float32

// setTiling writes the whole texture mapping: how many copies span the
// quad, where the reference copy starts (in copies), and whether each
// axis repeats.
func (d *combinedUniformData) setTiling(tileX, tileY, offX, offY, repeatX, repeatY float32) {
	d[combinedUniformTileWord+0] = tileX
	d[combinedUniformTileWord+1] = tileY
	d[combinedUniformTileWord+2] = offX
	d[combinedUniformTileWord+3] = offY
	d[combinedUniformTileWord+4] = repeatX
	d[combinedUniformTileWord+5] = repeatY
}

// setNoTiling is the ordinary layer's mapping: sample the texture once
// across the quad, with no rejection.
func (d *combinedUniformData) setNoTiling() { d.setTiling(1, 1, 0, 0, 1, 1) }

// combinedUniformBytes views the block as raw bytes for WriteBuffer.
func combinedUniformBytes(d *combinedUniformData) []byte {
	return (*[combinedUniformSize]byte)(unsafe.Pointer(d))[:]
}

// bindingKind is the resource type of one shader binding. Metal indexes
// buffers, textures and samplers in three SEPARATE sequential spaces, so
// the kinds — not just the counts — decide which slot a binding lands in.
type bindingKind int

const (
	bindingBuffer bindingKind = iota
	bindingTexture
	bindingSampler
)

// blitBindGroups describes the blit pipeline's bind group layout, group
// by group, in binding order. It is the single source of truth:
// initBlitPipeline builds its layouts from it, and the shader tests
// check the WGSL declarations against it.
//
// Group 0 must hold NO buffer, and that is not arbitrary. The WGSL→MSL
// translator numbers a module's bindings sequentially per type over the
// globals THAT MODULE declares, while the command encoder numbers them
// cumulatively across the whole pipeline layout. The vertex module
// declares only the group 1 uniform, so it names buffer 0 — which is
// where the encoder puts it exactly while no earlier group binds a
// buffer. Break that and the vertex stage silently reads some other
// buffer as its position uniforms and throws every quad off screen,
// with no validation error anywhere. This is why drop shadows draw
// through this pipeline (switched by the block's mode field) instead of
// through one of their own.
var blitBindGroups = [][]bindingKind{
	0: {bindingTexture, bindingSampler},
	1: {bindingBuffer},
}
