# GPU Compositor — Status

Updated 2026-08-03 after the bug-fix + hardening session. The original
August 2 handoff described four open bugs and a half-finished renderer
split; all four are fixed, the renderer abstraction is now real, and the
behavior is locked in by headless tests.

## Layering contract (design of record)

Per OS window, bottom to top:

1. **Base layer** — the surface's own content: desktop background, menu
   bar, status bar, dock (a torn-off window draws its own chrome, menu
   bar included, on its base layer).
2. **Child windows** — each on its own GPU texture; MDI children
   composite *within* their parent window's texture because the window
   paints its whole subtree.
3. **Menu dropdowns** — the menu bar's open dropdown.
4. **Popups** — combo box lists, context menus. Topmost, so a popup
   opened from a menu paints above the menu that spawned it.

Torn-off windows currently present as plain single surfaces (base layer
only, everything painted into it — correct visually). Giving each its
own child-window/popup layer stack later only requires its surface
handler to implement `platform.WindowProvider` + `BaseLayerPainter`;
the platform plumbing is already per-surface.

## How presents work now

`Platform.presentWindow` (sdl/platform_sdl.go) is the ONE path to the
screen, used by the main loop, live resize, and font zoom alike:

- Compositing renderer + handler with child windows →
  `RenderFrameWithChildWindows` (the 4-layer pipeline above). The base
  layer is painted through `platform.BaseLayerPainter.FrameBase`
  (chrome only).
- Otherwise → `paintAndPresent`: full-scene `Frame` into the backend,
  then `Renderer.Present` (software: SDL streaming texture; the
  legacy WebGPU blit path also remains for GPU windows without child
  windows).

`SurfaceHandler.Frame` ALWAYS paints the complete scene (windows
included). Only the compositor asks for the chrome-only `FrameBase`.
This split is what fixed windows vanishing during live resize and the
blank-window software path.

## Fixed this session (regression tests in parentheses)

- **Black triangle after resize** — the legacy present drew 3 vertices
  against the 6-vertex quad shader and wrote only 12 of the 32 uniform
  bytes; every resize present painted half the window. Fixed; resize
  path also funnels through `presentWindow`
  (`TestSDLLiveResizeRepaintsFully`).
- **Windows disappeared during desktop resize** — live resize used the
  non-compositing present while `Frame` painted chrome-only. Frame
  paints everything again; resize composites
  (`TestDesktopFramePaintsWindowsFrameBaseDoesNot`).
- **Wrong window scale after desktop resize** — per-window NDC uniforms
  were baked at texture creation against the then-current surface size.
  Uniforms are now rewritten every frame from current bounds + surface
  size (`TestWindowNDCTracksSurfaceResize`); window moves no longer
  recreate GPU resources at all. Child backends also inherit the parent
  backend's font size, so content density matches at any `font_size`.
- **Menu scroll buttons let clicks fall through** — title hit-testing
  had no right boundary, so titles extending beneath `[<][>]`/the clock
  still activated on press, hover, and drag
  (`TestMenuBarScrollButtonsConsumeMouse`).
- **Menu/popup z-order** — dropdowns now render below popups (shared
  `drawOverlay` helper replaced the two duplicated reflection blobs).
- **Elided menu title drew monospace** — the last partially-visible
  title now measures AND paints in the bar's proportional font
  (`TestElidedTitlePrefixMeasuresProportionally`).
- **Software renderer path restored** — createWindow/sizeFramebuffer/
  paintAndPresent no longer hard-require the WebGPU chain; the headless
  dummy-driver test runs the whole loop again
  (`TestSDLPlatformHeadless`).
- **Linux build** — `getMetalLayer` is behind a darwin seam;
  X11/Windows handles come from SDL WM info (`sdl/gpusurface_*.go`).
- **Leaks** — closed child windows now release their compositor
  surfaces (eviction each frame + on shutdown).
- **CLI renderer override** — `--webgpu`, `--software` (alias `--sdl`),
  `--renderer=NAME` override `kittytk.ini` in the host binary, parsed
  with `github.com/phroun/argwild` (host app only, not the library).

- **Rotation demo rewritten for the compositor** — the `r` key works in
  both GPU modes again. The blit vertex shader rigidly rotates/shrinks
  every layer's quad around the surface center (aspect-corrected, so the
  scene doesn't skew), and the spinning content-textured cube draws over
  the composited scene. The cube pipeline and all effect easing now live
  in WebGPURenderer (Platform borrows them via exposeWebGPUObjects); the
  Platform keeps only its animation clock for mouse-coordinate rotation
  compensation.

## Drop shadows on the GPU (solved, and why it was hard)

Shadows are now **analytic**: the fragment stage evaluates the signed
distance to the caster's rounded rect — unioned with its anchor, so a
menu title and its dropdown cast one shape — and fades it across the
blur. Nothing is rasterized on the CPU and nothing is cached, so a
window that is moving or resizing costs exactly what a still one does,
and the falloff is exact at any density instead of resampled from an
image. The anchor's own rect is punched out of the result, because it
lives on a layer BELOW the shadow and is never redrawn above it.

They draw through the **same pipeline** as every other layer, switched
by a `mode` field in the shared uniform block. That is the whole fix,
and the reason is worth writing down because nothing reports it:

- `msl.Compile` translates each shader module **in isolation**. It sorts
  that module's own `@group/@binding` declarations and numbers them
  sequentially **per resource type** — buffers, textures and samplers in
  three separate counters.
- The command encoder numbers the same resources from the **pipeline
  layout**, cumulatively across all groups.
- These agree only while no earlier group contributes a resource of the
  same kind. The blit layout is group 0 = {texture, sampler}, group 1 =
  {uniform}, so the uniform is buffer 0 under both rules.

The first attempt gave shadows their own pipeline with group 0 =
{shadow params}, group 1 = {shared position block}. The vertex module
declares only the group 1 uniform, so it compiled to buffer **0** while
the encoder bound it at buffer **1**: the vertex stage read the shadow
parameters as its NDC position and threw every quad off screen. No
error, no validation warning, correct geometry in every log — just no
shadows, and not even red ones under `KITTYTK_SHADOW_DEBUG`.

### Where each shadow comes from

A shadow needs something below it to fall on, so which mechanism draws it
follows from what the caster IS:

| Caster | Mechanism |
| --- | --- |
| Desktop window, menu dropdown, popup | compositor layer → analytic SDF in the blit shader |
| Torn-off window's dropdown and popups | same, on that window's own surface (`TearOffHost` is a compositor host) |
| MDI child | painted into its parent window's surface by `core.Painter.DropShadow` |

An MDI child has no layer of its own — it lives inside the texture its
parent window occupies — so nothing the compositor draws could get
underneath it. `MDIPane.Paint` lays its shadow immediately before the
child, interleaved in z-order, which is also what makes a stack of
children shade each other correctly: each shadow lands on everything
below it and is covered by everything above.

Both read `core.WindowDropShadow` / `core.OverlayDropShadow`, so the two
paths cannot drift apart in look (`TestShadowSpecsTrackCoreStyles`), and
the raster implementation evaluates the same rounded-rect distance field
the shader does.

`sdl/shaders.go` is untagged on purpose: WGSL text and a memory layout
are pure data, so `sdl/shaders_test.go` checks them with no SDL library
and no GPU. It holds `blitBindGroups` (which the pipeline is built from,
not merely compared against) and enforces that both stages declare the
same 32-word uniform block, that every binding lands on the slot the
encoder binds it to, and — running the two numbering rules against the
retired shadow layout — that the checker can actually catch the bug it
exists for.

## Per-window damage (the compositor's texture cache)

The compositor keeps a texture per child window and repaints one only
when something about it changed. A window nobody touched costs nothing:
no CPU paint, no BGRA conversion, no upload.

What counts as "changed" is `paintSignature` (`sdl/compositor_math.go`):
the window's subtree repaint revision, its pixel size, font size and cell
metrics. **Position is deliberately absent** — a window's placement lives
in its uniform buffer, which is rewritten every frame regardless, so
dragging a window repaints nothing at all.

The revision comes from `core.SubtreeRepaintTracker`, which `Window`
implements. `Update()` walks from the changed trinket to the root and
notifies **every** tracker on the way, not just the nearest: an MDI child
paints into its ancestor's surface, so stopping early would leave the
ancestor thinking it was clean.

Two distinctions matter and are easy to get backwards:

- **A move is not a content change.** A trinket paints in its own local
  coordinates, so its pixels are identical at a new position. `SetPos`
  and a position-only `SetBounds` notify the trinket's *ancestors* (their
  pixels include it at its new place) but not the trinket itself.
- **The flag is not the signal.** Every mutation that sets `needsRepaint`
  must also call `notifyAncestorsOfRepaint`/`OfMove`. The flag records
  local intent; the notification is what a container caching pixels can
  actually see. `SetResizeHoverRects` was the one setter that reported
  its change to the caller instead of announcing it.

**Staleness is bounded, not prevented.** Before this cache, code that
changed a window's look without signalling was masked by the desktop's
own ~1s heartbeat repainting everything, so the bug showed as a moment's
lag. Cached, the same miss would freeze that window's pixels for good.
`compositorHeartbeat` restores the old bound by refreshing each texture
about once a second — **staggered** by a hash of the window id, because
every window is first painted in the same frame and a shared interval
would put a full repaint of the whole desk into one frame every second.

### The non-compositing present is cached too

A surface presented WITHOUT compositing — a torn-off window with no popup
open — went through `paintAndPresent`, which repainted the whole window
and had `Present` allocate, fill and free a full-surface texture every
frame. Dragging one stuttered while the composited desktop stayed smooth.

Both are now skipped when nothing changed, via
`platform.RepaintRevisionProvider` (`TearOffHost` implements it). The
present still happens every frame from the pixels already held, so the
cadence is unchanged and nothing can go stale on an expose; only the
paint, the conversion and the upload are skipped.

The reason a *drag* needs this at all: `TearOffHost.Event` invalidates
the whole surface after **every** input event — a deliberate parity
contract with the terminal, where trinkets do not invalidate precisely.
A move is input, so it asked for a full repaint of a picture identical to
the one on screen. Rather than unpick that contract, the revision lets
the host notice the picture did not change.

And it presents nothing when nothing changed — the window still shows
the frame it last presented. A present WAITS for vsync, and the desktop's
tick invalidates every torn-off host whenever anything wants a repaint,
so an idle torn window was paying a refresh-rate stall per tick to
redisplay an identical picture. The rotation demo is the exception: it
animates through the uniform buffer rather than the pixels, so its frames
have nothing dirty and present anyway.

**There is no frame-rate floor in the present path.** There used to be a
`time.Sleep` filling out the remainder of 16ms "to prevent event
starvation" — which the main loop already guards with its own `Delay`
every iteration, and which slept on the PLATFORM THREAD, stalling event
handling and every other window along with it. It was also asymmetric:
the compositing present returns before reaching `paintAndPresent`, so
the desktop never paid it and only torn-off windows did. Caching the
repaint made it worse rather than better — with the paint skipped there
was nothing left to fill the budget, so it slept nearly the whole 16ms
every frame.

Diagnostics:

```
KITTYTK_COMPOSITOR_REPAINT=always   # restore the unconditional repaint
KITTYTK_COMPOSITOR_STATS=1          # per-second painted/skipped tally
KITTYTK_FRAME_DEBUG=1               # report presents over 30ms
```

If a window ever shows stale content, one run with `=always` settles
whether this cache is the cause; the fix is then to find the mutation
that changes pixels without notifying.

The **base layer** (desktop chrome) is cached the same way. Its signal is
the harder half: windows are the desktop's own trinket children, so a
keystroke in a window bumps the desktop's counter too. `GetChildWindows`
therefore reports `BaseRevision = desktop's revision − Σ(the revisions of
the windows it is handing over as layers)`. Those changes bumped both by
one, so they cancel exactly, leaving a number that moves only for what
`FrameBase` paints. It nets out nesting too: a change in an MDI child
bumps the child, its parent window and the desktop, and the parent's term
cancels the desktop's.

## The wallpaper is a tiled texture

The desktop background is **one tile, repeated by the GPU's sampler** —
not a fill across every pixel. A 16×16 pattern and a 1024×1024 photograph
are the same single quad; only the upload differs, and that happens once
per revision rather than once per frame.

- `wallpaperSampler` addresses **Repeat** with **Nearest** filtering. The
  quad maps the tile 1:1 to pixels, so there is nothing to interpolate
  and linear would only soften a hard-edged pattern.
- The uniform block's `tile` vec2 is `surfacePx / tilePx`; the fragment
  stage multiplies its texture coordinates by it. **Every layer must
  write (1,1)** — a zeroed tile collapses the layer onto one texel.
- The built-in 8×8 two-color pattern is *rendered into a tile*
  (`Desktop.WallpaperTile`), so the default wallpaper takes the same path
  a custom image does. Its revision folds in the pattern bits, the chunk
  size and the fill colors, so a theme switch re-uploads without anyone
  hooking a setter.
- `FrameBase` **clears to transparent** and `Paint` skips the background
  fill, so the tiled quad underneath shows through wherever the chrome
  does not paint. A surface with no alpha (or any host that does not take
  the wallpaper as a layer) falls back to the opaque clear and
  `Painter.TileImage` on the CPU, so the software renderer looks the same.

Setting one, quickest first:

```
KITTYTK_WALLPAPER=~/pictures/weave.png ./bin/kittytk-sdl --webgpu
./bin/kittytk-sdl --wallpaper=/path/tile.png     # --wallpaper- for the built-in
[window] wallpaper = /path/tile.png              # kittytk.ini
desktop.SetWallpaperFile(path)                   # or SetWallpaperImage(*image.RGBA)
```

PNG, JPEG and GIF decode; a bad path is reported and the current
wallpaper is left alone, so a typo cannot blank the desktop.

### How it covers the desktop

`core.WallpaperLayout` answers four separate questions, and keeping them
separate is what makes them compose:

| Key | Values |
| --- | --- |
| `wallpaper_mode` | `natural` (default), `fit_both`, `fit_width`, `fit_height`, `cover`, `stretch` |
| `wallpaper_scale` | a multiplier on whatever size the mode arrived at |
| `wallpaper_tile` | `both` (default), `none`, `horizontal`, `vertical` |
| `wallpaper_align` | `center` (default), `top left`, `bottom`, … or fractions `0.25 0.75` |
| `wallpaper_filter` | `crisp` (default) or `smooth` |

Notes worth having in one place:

- **Sizing and repetition are orthogonal.** "Tile at natural size" is
  `natural` + `both`; "one copy stretched to fill" is `stretch` + `none`.
  There is deliberately no mode called `tile`.
- **Alignment is a fraction per axis**, 0 left/top through 1
  right/bottom, so the in-between values come free. On an axis that
  TILES it sets the repetition's PHASE rather than confining it — one
  rule either way, instead of a special case that surfaces only when
  someone turns tiling off.
- **`wallpaper_filter`, not `wallpaper_scaling`.** The alias is accepted
  because it is the obvious name, but `wallpaper_scale` is a number and
  the two would read as typos for each other.
- A name that does not parse is **reported**, and the default kept.
  Silently papering the old way leaves no way to tell a typo from a
  setting that does not do what it sounds like.

The GPU scales in the sampler, so the texture is always the image's own
size — a 4K photograph set to `stretch` uploads once, at 4K. The software
renderer resamples once into a cached scaled copy (keyed on image, size
and filter) and then lays it down with row-length memmoves, rather than
sampling per destination pixel every frame.

## The platform caret, and the IME

A trinket asks for the platform caret while it holds focus
(`Painter.RequestTextCaret`), last request of a frame wins. In a
single-surface frame the handler applies the winner itself. **Under the
compositor it must not**: every child window, menu and popup paints into
a texture of its own, so their requests never reach the base layer's
painter, and applying the chrome-only request there would hide a focused
window's caret for the rest of the frame.

So the compositor gathers them — seeded from the base layer, then each
window in z-order (`caretInSurface` shifts a request out of its layer's
local coordinates), then overlays — and the platform applies the single
winner to the surface. A window whose paint is skipped by the texture
cache keeps the caret it last asked for, cached alongside its texture.

On a graphical surface there is **no OS-drawn caret** — trinkets paint
their own — so all three caret methods are no-ops there. Where text is
being typed is a *different question*, answered by the separate
`platform.TextInputAreaSetter`: `SDL_SetTextInputArea` anchors the CJK
candidate list, macOS's press-and-hold accent picker and the emoji
picker under the insertion point instead of at a default corner.

Keeping them separate is the point. `RequestTextCaret` means "draw the
platform's caret here" and only the terminal wants it — a terminal's
cursor IS the platform's. A text field paints its own caret (a blinking
bar, or a reverse-video block on a cell surface) and asking for the
platform's too would paint two. It calls `RequestTextInputArea`
instead, which reports the insertion point and nothing else. A caret
request implies an input area as well, since a drawn caret is also where
typing goes; `TextCaret.Requested()` is the "asked for anything" test the
compositor folds on.

`KITTYTK_IME_DEBUG=1` logs every area update with the rect in units and
pixels, whether text input is active on that window, and any error. An
input method that ignores the area and a host that never sets one look
identical on screen — both put the candidate window in a corner — so
watching the calls is the only way to tell them apart.

### Preedit

The other half: `SDL_EVENT_TEXT_EDITING` carries the characters an input
method is still composing — typed, shown, but not yet the document's.
`core.TextEditingEvent` delivers them and `core.PreeditFrom` normalizes
them, which is the one place that interprets SDL's `Start`/`Length` (the
selected clause) — units the SDL3 documentation leaves open, so they are
read as rune offsets and clamped rather than trusted.

Routing deliberately does **not** reuse the key path. A composition is
not a key: it goes straight to the focused trinket, skipping the menu
bar, the shortcut resolver, Alt+F4, the cycle keys and the window
manager's desktop fallback, because none of them have anything to say
about characters still being composed and a composition containing "m"
is not Cmd+M. `HandleTextEditing` mirrors `HandleKeyPress` at each layer
(FocusManager, Window, WindowManager, MDIPane) minus all of that policy,
and has no Tab fallback — a composition never moves focus.

TextInput keeps the preedit apart from `text` and splices it into the
painted run at the caret, so it shapes with its neighbours exactly as it
will once it commits. It is marked twice: underlined (the universal
"provisional" convention) and drawn in the **caret's** color rather than
the text color, since the input method is still holding it the way it is
still holding the caret. A reported clause gets a second, thicker rule.
Committing clears it — the commit arrives as ordinary typed characters,
and clearing there rather than waiting for the empty `TEXT_EDITING` that
usually follows means the two never briefly paint at once, whichever
order the platform sends them in. Focus loss clears it too. Password
fields mask the composition, since masking only what was already
committed would show the next word in the clear for as long as it took
to compose.

While composing, `RequestTextInputArea` reports the **start** of the
composition rather than the caret inside it, so the candidate list sits
under the text it is offering candidates for instead of walking rightward
with every keystroke.

## Known gaps (not regressions)
- **MDI children are not compositor layers.** They paint into their
  parent window's texture and shadow themselves there. Hoisting them
  would need each child's bounds and paint closure mapped up through the
  trinket tree — `MapTrinketToScreen` already does that arithmetic — and
  a resolution for the one thing that genuinely paints above a pane:
  `PaintModalDim`. The window's own menu dropdown is NOT a conflict, as
  it already has a compositor layer of its own.
- **`windowSurfaces` keys** truncate the child-window pointer to
  uint32; collision is unlikely but a stable window ID would be nicer.
- **Linux `-tags "sdl webgpu"` final link** currently fails in goffi
  (`unhandled relocation … SDYNIMPORT`) — packages compile and vet
  clean; macOS unaffected. Upstream (go-webgpu/goffi) issue.

## Rounded transparent corners (solved, and why it was hard)

Torn-off windows compositing their rounded corners was a long-standing
want that had NEVER worked. Four things had to be true at once, and
each failure looked like the others:

1. **The pixels must carry the shape.** `punchRoundedCorners` clears
   the corner pixels of every painted frame, premultiplied so all four
   channels go to zero (an alpha-only clear leaves an additive black
   fringe). This was correct well before it was visible: a screenshot
   showing a black square exactly corner-radius sized is the punch
   working while its alpha is discarded downstream.
2. **SDL's shaped windows are not available.** SDL3 removed the API,
   and the runtime here is SDL3 (directly now; formerly sdl2-compat
   under SDL2 headers), so `SetShape` reported NONSHAPEABLE for BOTH
   renderers — which is why the software path stayed square too.
3. **Core Animation cannot round it either.** A CAMetalLayer's drawable
   goes to the window server rather than through the layer's own mask,
   so `cornerRadius`/`masksToBounds` on it are ignored; the debug dump
   confirmed the layer was found, non-opaque, and rounded, with square
   corners still on screen.
4. **The window must be created transparent.** `SDL_WINDOW_TRANSPARENT`
   is what gives the framebuffer's alpha somewhere to go, and it can
   only be set AT CREATION. This is the piece SDL2 could not express at
   all, and the reason the SDL3 migration was the fix rather than a
   nice-to-have.

`KITTYTK_WINDOW_SHAPE` still selects the mechanism (`layer`, `shape`,
`perpixel`) and `KITTYTK_ALPHA_DEBUG=1` still dumps the window/view/
layer opacity chain, both of which earned their place during the
diagnosis and are worth keeping for the next platform surprise.

## Build & test matrix

```
make sdl          # software renderer, system SDL3
make webgpu       # + WebGPU compositor, system SDL3
make standalone   # + WebGPU, SDL3 embedded: runs with nothing installed

go build ./...                    # core, TUI, tools
go build -tags sdl ./...          # SDL host, software renderer
go vet  -tags "sdl webgpu" ./sdl  # WebGPU renderer type-checks
go test ./...                     # includes untagged compositor math tests
go test -tags sdl ./...           # + event-translation and headless tests
```

The host is on SDL3 via github.com/Zyko0/go-sdl3, bound through the
adapter in `sdl/sdl3`. Two things about that binding are worth
knowing before touching it:

- It is **purego**: nothing links at build time and no SDL headers are
  needed, but libSDL3 is opened at RUN time. `Init` does that (widening
  the search past dyld's defaults, since Homebrew's prefixes are not on
  it); `-tags sdlembed` embeds SDL instead.
- Some SDL2 calls became **per-window** in SDL3, and default to OFF.
  `SDL_StartTextInput()` was global and on by default; SDL3's takes a
  window and starts disabled. A port that keeps the single call gets a
  first window that types and later windows that do not — and because
  key events are a separate, always-on stream, Tab and the arrows keep
  working, so it reads as a focus bug rather than a setup one. Anything
  else moved this way (`SDL_SetTextInputArea`, formerly the global
  `SDL_SetTextInputRect`) belongs in `createWindow` for the same reason.
- Some of its functions are **declared but panic("not implemented")** —
  a failure the compiler cannot warn about. Three were in the host's
  path (`Metal_CreateView`, `PointerProperty`, `Metal_GetLayer`) and are
  bound directly in `gapfill.go`. `grep -rn 'panic("not implemented")'`
  in the binding lists the rest; each is ~3 lines to bind.
