# Graphical Mode Plan — Decision Record

Status: **planning — no implementation yet.**
This document records the decisions made for adding true graphical (GUI)
rendering to KittyTK, alongside the open questions still to be settled. It
builds on the analysis in `adding-true-gui-rendering.md` and the
architecture in `multi-app-desktop-plan.md`.

## Goals

1. Any window can optionally be created as a real graphical (OS-native)
   window when running under an OS that supports it.
2. The same application code runs as either a graphical or a text (TUI)
   version of the app — one codebase, two presentations.
3. Mixed use is supported: terminal-style containers and cell-grid
   rendering remain available *inside* a graphical window (PurfecTerm
   depends on this; it is also a product feature in its own right).
4. In graphical mode, the app integrates with the user's real desktop
   where possible — native OS windows, and native system/desktop menus
   when the platform allows — rather than living inside a single-window
   "desktop environment" as the current TUI demo does.

## Decisions

### D1 — Trinkets are mode-aware and own both renderings  *(decided 2026-07-05)*

Every trinket knows whether it is painting to a graphical or a text-mode
target and behaves accordingly. The **programmer-facing interface of a
trinket is identical in both modes**; only its rendering activity differs.

- All current cell-idiom visuals — TabTrinket's trapezoid tabs built from
  `/ \ _ < >` runes, Button's `▄ ▀` half-block shadows, `░`/`█` rune
  scrollbars, per-cell selection restyling, the overline→underline
  attribute hack in the TUI backend's `EndFrame` — are **TUI-specific
  rendering material**. In graphical mode they dissolve into a different
  implementation that the trinket itself fosters (vector borders, real
  shadows, pixel scrollbars, highlight rectangles, real overlines).
- This supersedes both options sketched in `adding-true-gui-rendering.md`
  Decision 1: rendering is neither pushed down into the backend as
  per-trinket draw calls, nor split into backend-specific trinket classes.
  The trinket hosts both paint paths and selects by target mode.

Implications:

- The `Painter` (or backend) must expose the rendering mode so a trinket's
  `Paint` can branch (e.g. a `Mode()`/`IsGraphical()` query).
- The `Painter`/backend needs graphical primitives alongside the cell
  primitives: color fills, lines/strokes, rectangles (incl. rounded),
  real text runs with real fonts. Cell primitives (`DrawCell`, rune
  fills, box-drawing borders) remain for TUI mode and for cell-grid
  containers inside graphical windows.
- Layout and measurement logic must be shared between the two paths, so
  text measurement has to come from the backend (see groundwork G1) —
  a trinket cannot assume 1 char = 1 cell = 8×16 units in graphical mode.

### D2 — Trinket-level client/server protocol: apps compile independent of the renderer  *(decided 2026-07-05)*

Applications are compilable independent of the rendering environment
and talk to a rendering/desktop process over a boundary (unix socket,
IP, or ssh-forwarded), similar in spirit to the X Window System. A
running TUI (or graphical) desktop becomes a real desktop-environment
binary; separately compiled apps connect to it and request their
rendering needs. In-process operation remains supported: the same
app-facing API is implemented either directly (current single-binary
mode) or by a client library speaking the wire protocol.

Chosen boundary: a **trinket-level protocol**. The server (desktop
process) owns trinket instances, layout, rendering (mode-aware per D1),
and hit-testing. Apps manipulate trinkets through proxy objects exposing
the same programmer interface as today (satisfying D1's "identical
API" guarantee), and receive **semantic events** (clicked, text
changed, selection changed, window closed) rather than raw input.

Why trinket-level (supersedes the earlier primitive-level proposal):

- Best possible remote latency: hover, scrolling, text-edit echo,
  drag feedback, menu navigation all happen server-side with zero
  round-trips. Only meaningful state changes cross the wire.
- Apps are freed from layout and text measurement entirely — those
  live with the renderer, where the fonts are. G1's "mirrorable
  metrics" constraint dissolves for apps (it still applies *inside*
  the server between trinkets and render backends).
- Matches the vision: the desktop is an environment that serves apps,
  not a dumb framebuffer.

Costs accepted, with mitigations:

- **The trinket API becomes wire contract.** Every trinket's properties
  and events are protocol surface. Requires versioning + capability
  negotiation at connect, and API-design discipline (additive changes).
- **Custom trinkets need an escape hatch.** Apps that draw things the
  server has no trinket for get a client-rendered surface trinket:
  (a) a cell-grid surface (app streams cell diffs — also the natural
  transport for terminal content), and (b) later, a pixel surface for
  graphical custom rendering. These are the "canvas" trinkets of the
  protocol.
- **State ownership must be explicit.** The server-side trinket owns
  interactive state (text buffer contents, scroll position, selection,
  checked state) and emits change events; the client library keeps a
  replicated cache so app-side property *reads* stay synchronous-
  looking. App-side *writes* are async messages. In-process and remote
  modes must be behaviorally identical, so the API is designed against
  the cached-replica model from the start.

Design constraints this imposes on the groundwork (cheap to honor now,
expensive to retrofit):

- **IDs, not pointers; data + events, not closures.** Trinkets, windows,
  menus, popups, dock entries get stable IDs across the seam. Menu
  items dispatch as "item ID triggered" events (extends G6's command-ID
  requirement). Callbacks (`OnClick`, `OnTriggered`, `PopupOverlay`'s
  `Paint func`) become event subscriptions keyed by ID.
- **No synchronous app→server queries** in any hot path; reads come
  from the replicated client cache, updated by server events.
- **Flow control and lifecycle:** back-pressure so a stalled client
  cannot wedge the server; the server cleans up all trinkets/windows of
  a disconnected client (app crash safety — a side benefit no
  in-process design offers); reconnection semantics defined.

Sub-decisions still open (see O6).

### D3 — Unified key nomenclature everywhere  *(decided 2026-07-05)*

The direct-key-handler key-naming scheme (`^N`, `M-x`, `S-Tab`, `F10`,
and so on) is retained across **all** implementations — TUI, graphical,
and the display protocol — for now. It is the single internal
representation for key events, shortcut definitions, and shortcut
matching, and it is presented to the programmer, and to the user as far
as practical, unchanged on every platform.

Rationale: this is a specialized system — a technology demo and a
programming learning environment, deliberately dabbling in
WordStar-like heritage. The nomenclature carries real complexity and is
part of the project's identity; unification outweighs platform
convention for now.

Boundary rule: if native system menus (NSMenu key equivalents, Win32
accelerators) require per-platform normalized forms to be implemented,
translation happens **only at that boundary**, as a display/registration
concern inside the platform integration layer — never as a change to the
internal or app-facing representation. This may be revisited later; any
such revisit is a new decision, not an erosion of this one.

Effect on G6: the "shortcut translation" snag listed there is scoped to
a one-way mapping (key-string → native key equivalent) living in the
native menu module; wire protocol (D2) and trinket APIs carry key-strings
verbatim.

### D4 — X-direction rendezvous; sessions are separate from connections  *(decided 2026-07-05)*

Connection topology for the D2 protocol follows the X model: the
**display service (desktop) listens on a well-known endpoint** (env
var, unix socket by default) and **apps dial in**. Rationale: the
ephemeral party must announce itself to the durable, well-known party —
discovery is one env var, launching from the desktop is fork/exec with
connect-back, cleanup on socket drop is unambiguous, one socket to
secure with peer credentials, and ssh forwarding works like `ssh -X`.

**Protocol invariant adopted:** a *session* (an app's entire UI state)
is a first-class protocol object distinct from the *connection* that
carries it. Under the trinket-level protocol this is cheap — the app-side
client library already replicates the trinket tree — and it buys
tmux-grade capabilities in the X-style topology: reattach after a
display-service restart, attach to a different display service
(replay the tree, resubscribe), and potentially multiple simultaneous
viewers later (input ownership then becomes a policy question).

Also adopted:

- **Naming discipline:** the desktop process is the *display service*
  (or desktop); apps are *apps*. Client/server language is reserved for
  describing individual connections, avoiding X's naming confusion.
- **Reverse attachment is a possible later mode, not the foundation:**
  daemon-style apps that listen for a display service to dial in can be
  added later — the post-handshake protocol is identical, only the
  connector differs (as with LSP transports / gdbserver reverse
  connections). Nothing in the wire format depends on who dialed.

This resolves the reconnection item in O6 in principle (v1 may still
ship terminate-on-disconnect, but IDs/handshake are designed for
session reattach from the start).

### D5 — Dual graphical substrates: Gio and SDL, neutrally  *(decided 2026-07-05)*

The graphical Platform (G2) gets **two substrate implementations, Gio
and SDL**, behind one substrate-neutral interface — the same discipline
PurfecTerm already applies to GTK/Qt. A third implementation of the
Platform interface (alongside the TUI Platform) is what keeps the
boundary honest and fossil-free, and it insures against substrate risk
(Gio API churn; SDL cgo dependency).

**Condition that makes this sound — the shared text engine:** text
shaping, measurement, and rasterization are pulled *out* of the
substrates into one KittyTK-owned font module (go-text/typesetting is the
leading candidate), used identically by all graphical backends. Gio's
built-in text stack goes deliberately unused. This is mandatory, not
stylistic: under D2, layout is server-side and must be deterministic —
text measuring differently on two substrates would be a correctness
bug. The shared engine doubles as G1's server-side `TextMeasurer`, so
measurement and painting can never disagree.

The substrate contract is correspondingly small:

- window/surface creation and lifecycle, DPI scale factor
- input events, translated into D3 key nomenclature at the boundary
- vector primitives: fills, strokes, clips; glyph and image blitting
- clipboard; capability flags (IME, etc.)

Threading rule: Gio runs an event loop per window (goroutine each); SDL
has one main-thread global queue. The Platform delivers all events into
a **single KittyTK dispatch goroutine** (channel fan-in), keeping trinket
code single-threaded on both substrates (pins down G3's model).

Sequencing rule: substrates are brought up serially — define the
interface, land one substrate, then land the second **before the
interface is declared stable**, as a validation pass. Which substrate
goes first remains open (see O1): SDL is the easy glyph-grid bring-up
target; Gio is the nicer pure-Go distribution story.

Each substrate sits behind its own build tag (interacts with O3).

### D6 — Pango-class text is an available capability, at shaped-paragraph altitude  *(decided 2026-07-05, scope clarified same day)*

The shared text engine (D5) must make the full modern text model
**available to any trinket that needs it** — full Unicode, OpenType
shaping (ligatures via GSUB, combining-mark positioning via GPOS — e.g.
Hebrew niqqud), bidirectional text (UAX #9, e.g. mixed Hebrew/Latin),
font fallback, and standard line/grapheme segmentation (UAX #14/#29).
We are building the architectural role Pango plays; a text model that
*cannot* express this is explicitly rejected.

**Scope clarification — capability, not mandate:**

- Not all UI text must go through the full pipeline. The engine exposes
  tiers behind one roof: a **fast simple path** (single-font,
  single-direction glyph runs — button labels, menu items, titles) and
  the **full shaped-paragraph path**, chosen per trinket need. Same
  engine, same fonts, same metrics source, so D5's substrate-
  independence and D2's layout determinism hold on both tiers.
- **Terminal-style regions are a carve-out: PurfecTerm keeps its own
  text handling.** PurfecTerm's graphical text rendering is already
  sophisticated, customized, and proven in its GTK/Qt frontends, and it
  is retained for all terminal-style regions KittyTK incorporates. The
  shared engine has no jurisdiction inside those regions; the boundary
  is the trinket border. A terminal region's external layout contract
  (columns × cell size) is trivially deterministic, satisfying D2/D5
  without touching the shared engine.

**The protective decision is the interface altitude.** The engine's
contract is the shaped paragraph, not the measured string:

- Input: attributed text (font/style spans), available width,
  paragraph direction.
- Output: lines of shaped glyph runs — positioned glyphs with a bidi
  level per run — plus the **cluster map** (byte-range ↔ glyph-range),
  which is what makes caret movement, selection, and hit-testing
  correct in RTL text and inside ligatures.

Trinkets' graphical paint paths (D1) consume shaped runs and cluster
maps — never per-rune arithmetic. With this contract the implementation
is swappable without touching trinkets, layout, or protocol.

Implementation direction: **go-text/typesetting** as the reference
implementation (a Go transliteration of HarfBuzz's shaper — real
GSUB/GPOS execution; used by Gio but standalone), with
`x/text/unicode/bidi` (UAX #9), go-text's segmenter, and
`go-text/fontscan` for fontconfig-style discovery/fallback. A cgo
HarfBuzz/FreeType (or Pango) backend remains possible behind the same
interface if fidelity gaps appear; known soft spot in pure Go is
rasterization hinting quality (a swappable back-end concern, not
architectural).

Consequences recorded:

- **D2 synergy:** shaping lives entirely in the display service, where
  layout already is. Apps send logical text and never shape; cluster
  maps never cross the wire. (A primitive-level protocol would have
  forced both.)
- **Accepted asymmetry — TUI mode is constrained by the terminal.** A
  character grid cannot position niqqud or render ligatures; the TUI
  paint path does what terminals can (grapheme clusters, wide chars,
  the terminal's own bidi behavior). Same trinket, same stored text,
  same API; full fidelity appears in the graphical path. This is D1
  working as intended, not a defect.

### D7 — A Canvas trinket is the pixel escape hatch; development deferred  *(decided 2026-07-05)*

There will be a trinket akin to HTML5's canvas: the escape hatch for
apps with image and drawing needs the stock trinket set cannot express.
It follows the PurfecTerm pattern — app-owned content streaming into a
server-composited region, with input events forwarded raw — but for
pixels/drawing instead of character cells. **Development is deferred**
to a future to-be-developed trinket; the groundwork only needs to keep
the slot open (it is one more trinket type in the D2 protocol, so
nothing structural depends on its internals).

Design questions to answer when it is built (noted now, not decided):

- **Command-based vs pixel-buffer, or both** (HTML5 canvas is
  command-based with `putImageData` bolted on). Command-based remotes
  well — compact, and the display service can redraw on expose/resize
  without app round-trips. Pixel-buffer is the truly universal hatch
  but is bandwidth-heavy remotely and wants a shared-memory fast path
  locally. Likely both modes, command-based first.
- Coordinate space and DPI behavior (ties to O2).
- Input forwarding contract and frame synchronization/damage.
- Behavior in TUI mode (unavailable? degraded cell rendering? app's
  choice?).

### D9 — Height-for-width protocol; text-flow tiers; chrome vs text  *(decided 2026-07-05)*

A `core.HeightForWidther` optional interface (HasHeightForWidth /
HeightForWidth) lets trinkets whose height depends on allocated width
(wrapped text) report their real height during layout, when widths are
known. `SizeHint` remains the width-independent preference. BoxLayout
consults it; Panel propagates it upward; ScrollArea/Splitter/Window are
absorbers where propagation stops. (A WidthForHeight transpose is
acknowledged but not built — nothing needs it.)

Text-flow tiers (which trinkets flow text is a toolkit design decision,
and under D2 it is protocol surface):

- **Label** — wrap is core purpose; wraps + height-for-width.
- **Checkbox / RadioButton** — wrap is **opt-in** (`SetWordWrap`),
  default single-line. **The `[x]`/`(*)` indicator is CHROME, not
  text**: it stays anchored to the top line, and wrapped lines hang
  under the text column, never under the indicator.
- **Button, tabs, list rows, menu items** — deliberately single-line
  for now; overflow handled by other means (ellipsis, scrolling).
- **DockRow** — already a hand-rolled height-for-width trinket
  (RequiredHeight); deliberately NOT migrated to the interface —
  it is specific for other reasons and will be considered separately
  rather than replaced on confidence alone.
- **MessageBox** — future adopter (wrap message via Label, auto-size
  height); pending, ties to the dialog.go:179 C-site.

### Context: PurfecTerm is already multi-frontend  *(noted 2026-07-05)*

PurfecTerm predates KittyTK as an independent project and already has
**three working frontends in a single codebase: this TUI implementation,
GTK, and Qt.** Consequences for this plan:

- D1's mode-aware-trinket pattern is already proven in production by
  PurfecTerm; porting the trinket into the new system is expected to be
  easy.
- Its GTK/Qt renderers are existing graphical cell-grid renderers —
  directly relevant to Goal 3 (terminal containers inside graphical
  windows) and to the D2 cell-grid surface trinket.
- Its GTK/Qt experience is an input to the substrate choice (O1).

**Frontend audit + first graphical port (2026-07-06).** The published
module's `gtk/` and `qt/` frontends were audited: each is a thin
`Terminal` (PTY + process wiring) around a ~3,300-line `Trinket`
adapter owning the toolkit-agnostic `Buffer`+`Parser`. What a Trinket
consumes from its host toolkit: a paint callback with rect fills,
clip, scaling, and **glyph-image blitting** (each frontend keeps its
own glyph LRU keyed by `purfecterm.GlyphCacheKey` and only needs
"rasterize this glyph once"); font family/size selection with
metrics and per-rune fallback; blink/auto-scroll timers; key events
with modifiers (it builds its own escape sequences); mouse events
with pixel→cell mapping; clipboard; scrollbars + context menu
composed around the drawing area. The render contract against the
buffer is ~40 read methods, all in core. Our side now provides:
`core.ImageDrawer` / `Painter.DrawImage` (device-pixel composite,
alpha honored — the sprite/custom-glyph carrier); the text engine
covers fonts, fallback, metrics, and cached glyph rendering.
**Landed:** `trinkets.PurfecTerm` gained its D1 graphical paint path —
terminal-font cell grid (`SetTerminalFont`: the terminal's own
family/size, independent of toolkit cells; sizing measured through
the render target so text mode is untouched), run-batched
backgrounds, glyphs through the engine's cached shaped-text path
with real bold/italic faces, reverse resolved to concrete colors,
combining marks appended, and buffer cursor shapes (block/underline/
bar — bar via DrawCaret) gated on the active window chain. **Full port landed
2026-07-06:** sprites (fine-positioned via device-pixel image
composition, crop rects, flips, scaling, Z-order behind/front),
screen splits (scanline renderer with fine scroll and per-split
clipping), custom glyphs (GlyphCacheKey-cached images, seam
extension, palette resolution), screen scale and crop, selection
painting with drag + edge autoscroll (50ms timer, speed-capped),
blink animation (bobbing wave + cursor blink at gtk cadences),
cursor states including the unfocused hollow-box form, overlay
scrollbars (vertical scrollback + horizontal content, gtk adjustment
math), the right-click context menu (Copy/Paste/Select All/mouse
reporting toggle), xterm mouse reporting with the Shift bypass
(mouse events now carry keyboard modifiers), and DECDWL/DECDHL
double-width/height lines. Groundwork added: ImageDrawer.DrawImagePx
+ Painter.DrawImageOffset (device-pixel anchoring), Painter.
DeviceScale, raster local clipboard.

## Groundwork required regardless of open decisions

These follow from the code survey (2026-07-05) and are needed no matter
how the open decisions land. All are refactors verifiable against the
existing TUI demo with pixel-identical output.

### G1 — Metrics hygiene: two concepts, two sources  *(model decided 2026-07-05; full call-site audit in `g1-metrics-audit.md`)*

There are two distinct measurement concepts, which coincide numerically
in default TUI mode and diverge everywhere else. G1's job is giving each
its proper source and stopping them impersonating each other:

- **Grid metrics** (CellMetrics) — a deliberate **layout vocabulary**,
  not a TUI artifact: the application decides how many native units a
  virtual row/column occupies per container, for placement detail and
  density — including in true graphical windows, where lines/columns
  remain available for aesthetic layout decoupled from the proportional
  font. **Clarified: metrics are a coordinate denomination, like DPI —
  they change what unit values mean, never how big things look.**
  Row/column-denominated sizes are visually invariant under
  re-denomination; only explicit numeric unit values reinterpret.
  Requires denomination scaling at container boundaries in the
  paint/input path (see `g1-metrics-audit.md`, "Denomination model"). **Decided model:** grid metrics are *inherited from the
  container chain* (mirroring the existing `FontProvider`/
  `FindEffectiveFont` pattern), overridable per window/container in all
  modes including TUI, rooted at the display service's default — which
  derives from the system default font size in its settings. The
  hygiene defect is that ~133 call sites grab the global
  `DefaultCellMetrics()` constructor instead of their container's
  definition (no container metrics storage exists yet — the inheritance
  mechanism must be built; fonts provide the blueprint).
- **Text metrics** — "how much space does this text in this font occupy
  on this render target?" Target-dependent: the same text has different
  correct widths on a TUI surface and a graphical window coexisting in
  one display service. `Font.MeasureText` as a static function cannot
  answer this; measurement routes through the render context
  (TextMeasurer per target; the TUI measurer returns today's
  cell-quantized answers, the graphical one delegates to the D6 text
  engine). The Tuesday font exists precisely as a proportional test
  case (letters/digits 16 units, punctuation 8) and already exposes
  text-questions-answered-with-grid-math (Label `wrapText`,
  tab-bar width, menu clock width).

Audit summary (see `g1-metrics-audit.md` for the per-site checklist):
133 sites — 102 grid questions needing the container walk, 14 text
questions in disguise, 10 legitimate root-default sites (backend init +
Desktop chrome, to be re-rooted on a stored desktop default), 6
PurfecTerm cell-grid sites, 1 trivial painter swap, and one structural
dead-end (`NewSpacer` sizing in its constructor). Verified by
byte-identical demo rendering, except deliberate bug fixes at the
text-in-disguise sites (reproducible via the Selection-tab wrap row
under Tuesday).

### G2 — Split the backend into Platform + Surface

`core.RenderBackend` models exactly one drawing target and one event
queue. Native mode needs:

- **Platform** — owns the OS event loop, window creation, clipboard,
  screens/DPI, native menus, native dialogs.
- **Surface** — a per-window render target with per-window size, frames,
  damage/invalidations, and input events.

The TUI backend becomes a Platform with exactly one Surface (the
terminal). Multi-monitor awareness lives at the Platform level.

### G3 — Event-loop inversion

`Desktop.Run()`'s poll → dispatch → full-frame-render loop cannot drive
native toolkits, which own their main loop, require main-thread affinity
(`runtime.LockOSThread`), and deliver events per-window via callbacks.
Control inverts to `platform.Run(app)` calling back into KittyTK dispatch.
Rendering becomes per-window and damage-driven rather than every-iteration
full-frame. The TUI backend adapts easily (its input is already a
goroutine feeding a channel).

### G4 — Dual-mode Window

`Window` gains an explicit mode:

- **Native top-level** — the OS owns chrome, coordinates, move/resize,
  minimize, activation; `Window.Paint` renders content only; `SetBounds`
  maps to native window geometry.
- **In-surface child** — current behavior: self-drawn chrome, drag,
  keyboard move/resize, desktop/MDI coordinates.

The in-surface path is permanent, not legacy: MDIPane embeds child
windows with the current chrome even inside a fully graphical app, and
the TUI desktop always uses it. `WindowManager` scopes down to managing
in-surface windows (TUI desktop, MDI panes); in native mode the OS window
server replaces its z-order/drag/cascade/maximize/minimize/M-Tab roles,
with native activation callbacks driving `activeWindow` and the
active-app/menu-bar switch. The dock likewise yields to the OS taskbar
for native windows but remains for TUI/MDI.

### G5 — Popups become real windows in native mode

Menu dropdowns and combobox popups are currently overlays painted onto
the single desktop surface (`PopupController`/`PopupOverlay`/
`MapToScreen`). With native windows they must become borderless popup OS
windows (or native menu APIs), positioned in screen coordinates. Modal
dialogs port most cleanly — they are already `Window`s and map to native
dialogs/sheets.

### G6 — Menu data model ports; menu presentation is per-platform

The `Menu`/`MenuItem` tree (title, items, shortcut, checked/enabled,
submenu, callback) maps nearly 1:1 to `NSMenu`/`HMENU`, and the Desktop's
single global bar with per-active-app content swap is already the macOS
model. Known porting snags:

- Shortcuts use key-strings (`"^N"`, `"M-x"`) which per D3 remain the
  internal and app-facing representation; native menus get a one-way
  key-string → native-key-equivalent mapping inside the platform menu
  module only.
- Items dispatch by closure with no stable command ID (Win32 `WM_COMMAND`
  wants IDs).
- Windows/Linux use per-window menu bars, so the active app's bar must be
  replicated onto each native window there.
- The Ψ system menu needs a mapping convention per platform (e.g. the
  macOS application menu).

## Open decisions

### O1 — Substrate bring-up order  *(narrowed by D5)*

The substrate question itself is resolved by D5 (both Gio and SDL,
neutrally, serially). Remaining open: **which substrate lands first.**
SDL is the trivially easy target for glyph-grid bring-up (cell grid →
texture-atlas blit); Gio is the nicer pure-Go distribution story and
exercises more of the interface sooner.

Still applicable regardless: no portable layer provides native menus,
so a `PlatformIntegration` capability interface (menus, dialogs) falls
back to the existing rendered `MenuBar` when no native implementation
exists, with per-OS native menu modules (Cocoa/Win32) added over time.

### O2 — Unit semantics in graphical mode

What is 1 unit in a graphical window? Working proposal: 1 unit = 1
device-independent pixel, with HiDPI scaling handled by the Surface.
Note: today no trinket generates sub-cell coordinates (everything is a
multiple of 8/16 units), so graphical layouts will initially sit on a
chunky grid until measurement (G1) and mode-aware painting (D1) land.

### O3 — Backend selection mechanism

Build tags vs runtime selection. Lean: graphical backends behind build
tags so TUI-only builds stay cgo-free and trivially cross-compilable;
within a graphics-enabled build, mode/window kind is a runtime choice.

### O4 — Bring-up sequencing of graphical rendering

Whether to use a whole-window glyph-grid presenter (existing cell
rendering rastered through a monospace font into a pixel window) as an
interim bring-up milestone before trinkets' graphical paint paths exist.
The glyph-grid renderer is needed permanently either way — for PurfecTerm
and for terminal-style containers inside graphical windows (Goal 3) — the
open question is only whether it also serves as the first end-to-end
milestone for whole windows.

### O6 — Display-protocol sub-decisions (under D2)

- **Wire format — direction set by D10 (2026-07-05):** self-describing
  **named-property records** (nothing positional), text-oriented, with
  **sender-declared alias dictionaries** for wire efficiency. Sketch
  (syntax illustrative, not final):

  ```
  new button caption="Caption Here" action="action_id_here"
  alias c="caption" a="action"
  new button c/Caption Here/ a/other_action/ some_float_prop=4.2
  ```

  Design notes recorded with it: alias tables are
  **connection-scoped, not session-scoped** (encoding state resets on
  reattach; session replay stays purely semantic, per D4), and
  **independent per direction** (each sender declares its own
  outbound aliases). Named properties + additive-only evolution +
  capability advertisement is the versioning story. Likely needs a
  length-prefixed bulk/binary escape within the text framing for
  cell-diff streams (PurfecTerm) — TBD. Remaining open: exact syntax,
  framing, quoting/escaping, the bulk escape, negotiation handshake.
- **Protocol versioning discipline:** how trinket properties/events are
  declared and evolved (additive-only? feature flags per trinket?), so
  server and app binaries of different vintages interoperate.
- **Transports for v1:** unix socket with peer credentials is the
  default. For remote use, lean on ssh forwarding (the X11 answer)
  rather than building TLS+auth immediately? Direct IP+TLS later.
- **Where terminal emulation lives:** for a remote app hosting a PTY,
  does the app stream raw bytes to a server-side PurfecTerm trinket
  (thin client, server does emulation), or run the emulator app-side
  and stream cell diffs to a cell-grid surface trinket? Both are
  possible with the existing purfecterm library; pick the v1 shape.
- **Pixel escape-hatch timing:** resolved by D7 — a Canvas trinket will
  exist and is explicitly deferred; the cell-grid surface trinket is
  still needed early.
- **Reconnection semantics:** resolved in principle by D4 (sessions are
  first-class and separable from connections). Remaining detail: does
  v1 ship terminate-on-disconnect with reattach-ready IDs/handshake, or
  implement session reattach immediately?
- **App launching:** does the desktop server spawn client apps (menu of
  installed apps), or are apps launched externally and connect? Both
  eventually; which first?

### O5 — Scope deferrals to confirm

- Native accessibility bridging (NSAccessibility/UIA) — the existing
  accessibility-label system is the seed, but the native bridge is a
  large separate effort. Propose: explicitly deferred, tracked.
- IME/composition input for graphical text entry — required for real
  GUI text input eventually; decide which phase.
- Drag-and-drop with the host desktop — propose deferred.
- Real clipboard support (TUI backend's is a stub) — needed early in
  graphical mode; cheap via the substrate.

## Proposed phase order (draft, pending O1–O4)

1. ✅ **G1** metrics/measurement hygiene — **complete 2026-07-05**
   (inheritance chain, denomination exchange with visual invariance,
   full call-site sweep, C-site resolution; residuals tracked in
   `g1-metrics-audit.md`). Delivered beyond original scope: the
   denomination model (D8′) implemented and live-verified, plus the
   height-for-width protocol (D9) and four layout-contract fixes.
2. **D2 API shape** — restructure the app-facing API against the
   proxy/replica model (stable IDs, event subscriptions instead of
   closures, cached reads, async writes), still entirely in-process.
   This is the protocol's dress rehearsal with no serialization yet.
3. ✅ **G2 + G3** core — **done 2026-07-05** (D21). The `platform`
   package defines Platform (Run/Post/PostAfter/Quit/CreateSurface/
   clipboard) and Surface (size, metrics, damage-driven Invalidate,
   handler callbacks: Frame/Event/Resized), all callbacks on the
   OS-locked platform thread. The TUI runs as a one-surface Platform
   (`platform.NewPolling` over any RenderBackend — no backend-package
   coupling); `Desktop.Run()` wraps `RunOn(platform)`; the old
   poll→dispatch→render loop is gone (dispatch is `dispatchEvent`
   per event, paint is the Frame callback, timers self-tick via
   PostAfter). Contract test + headless end-to-end desktop test.
   Remaining under G2 for the substrate phase: screens/DPI
   enumeration, multi-surface creation, native dialogs/menus hooks
   (G5/G6), and per-damage-region frames when a substrate can use
   them.
4. ✅ **G4** seam — **done 2026-07-05**. Dual-mode Window:
   `window.SurfaceHost` runs one Window as the entire content of one
   Surface (chrome suppressed — the OS provides it; window tracks
   surface size; input forwards 1:1; damage flows through
   Surface.Invalidate). `Window.NativeRequested` (+ wire `native`
   flag) records the app's REQUEST; granting it is host policy —
   single-surface platforms keep windows in-surface. WindowManager
   explicitly scoped to compositing within one surface. Tested
   against a fake surface (bounds/flags/layout, paint, click
   routing, resize tracking, invalidation). Desktop-side granting
   policy activates when a multi-surface platform exists.
   **Granting landed 2026-07-07**: the SDL platform is multi-surface
   (per-window renderer/texture/raster backend, events routed by
   window ID, mouse captured during drags so coordinates keep
   reporting past the edge). New optional capabilities:
   `platform.MultiSurfacePlatform`, `platform.GlobalPointerPlatform`,
   `platform.NativeSurface` (screen-px position + Close), and
   `SurfaceOptions` pixel geometry + Borderless. `window.TearOffHost`
   hosts one window per surface with KittyTK chrome KEPT (borderless OS
   window - torn windows look identical to docked ones); its title
   drag moves the OS window via the global pointer. Desktop
   choreography: a WindowManager title drag crossing the surface edge
   tears the window off into its own OS window (the desktop keeps the
   capture and drives the torn surface until release); crossing back
   over the desktop re-docks it with the drag still armed, in either
   phase. Headless choreography tests against a fake multi-surface
   platform.
5. ✅ **D2 transport** core — **done 2026-07-05** (D22). The wire IS
   the language: `protocol.Scanner` frames statements off the socket
   by brace/string awareness; batches end with `end`; replies and
   errors are statements (`reply wcb=19`, `error text="…"`);
   `hello`/`welcome` handshake carries app name and a server-assigned
   session id (reattach-ready). `display.Serve` makes any desktop a
   display service: per connection — session, BindContext, a FULL
   Application (windows/menubar/statusbar adopted from batches),
   socket reader Posting batches onto the UI thread (D21), writer
   goroutine so a slow client can never stall the display, teardown
   on disconnect. `client.Dial` gives apps the same Conn surface as
   in-process (events on a dedicated goroutine; Target() nil);
   command dispatch unified as command events on both transports.
   The demo desktop serves `$XDG_RUNTIME_DIR/KittyTK/display-0.sock`;
   `examples/remoteapp` is a rendering-free app binary that dials
   in. End-to-end socket test (build, set, events both ways, command
   dispatch, disconnect teardown), race-detector clean. Remaining:
   D4 reattach, O6 bulk frames, TCP/SSH endpoints, per-connection
   auth beyond socket perms.
6. ✅ **Shared text engine** (D5/D6) — **core landed 2026-07-06**.
   The `text` package: `Engine` over go-text/typesetting (HarfBuzz
   shaping, bidi + script + face segmentation, UAX #14 wrapping) with
   embedded default fonts ("Go" sans + "Go Mono"; TUI-era names
   "Monday"/"Tuesday" aliased; `RegisterFont` extends the fallback
   chain — registered fonts only, never the host system, so layout
   stays deterministic per D5). Both D6 tiers: fast path
   (`Measure`/`ShapeRun`) and full shaped-paragraph path
   (`ShapeParagraph`: attributed spans, base direction with UAX #9
   auto-detection, wrapping). Contract altitude held: the shaping
   library appears nowhere in the API; runs carry logical rune
   ranges + resolved direction, and the cluster map is exposed as
   operations (`Line.CaretX`, `Line.RuneForX` — ligature/combining
   snapping, RTL-correct). `text.Render` rasterizes shaped output at
   any integer scale (crisp outlines, not upsampling) via
   go-text/render. Tested: proportional vs mono metrics, cross-engine
   determinism, wrapping invariants, bidi run structure (LTR/RTL/LTR
   and RTL-base), caret round-trips both directions, cluster
   snapping, span splitting, render ink at 1x/2x. **Adopted
   2026-07-06:** measurement comes from the render target
   (`core.TextMeasurer`, installed by `Desktop.SetBackend`; the
   text-based system keeps its exact cell arithmetic — one character
   = one cell of layout units); the raster backend's `DrawText`/
   `DrawTextAligned` is ONE path — fully shaped and proportional,
   with monospace-by-choice giving the grid look — while `DrawCell`
   stays the cell primitive for terminal regions (D23 carve-out).
   Size denomination: a font's line height is `Size × 4/3` units
   (12pt = 16 = one default cell row); the em size is derived per
   face from that budget, so text always fits its chrome. `"ui-text"`
   is the internal UI font name: text mode maps it to Monday, the
   graphical engine maps it to "Go" (swappable later). Remaining in
   this phase: D1 trinket rollout onto shaped paragraphs (Label
   bidi/wrap via ShapeParagraph, TextInput selection via cluster
   maps), height-for-width via shaping, optional system-font
   discovery.
7. First graphical substrate — **SDL core landed 2026-07-05** (D23).
   The `raster` package is the pixel implementation of the rendering
   primitives (real TTF glyphs at Monday-matching advance, real
   lines for borders, shade-rune alpha blends; cgo-free, headless-
   testable, PNG-provable). The `sdl` package (build tag `sdl`)
   wraps it: SDL2 window, streaming-texture blit, D21 loop, D3 key
   translation; `cmd/kittytk-sdl` is the graphical display
   service serving the same socket (selection-by-binary per O3).
   Verified headless via SDL's dummy driver incl. a full-stack smoke
   (remoteapp connected to the SDL desktop). Remaining in this
   phase: on-screen verification by the owner, HiDPI scale, cursor,
   multi-surface (G4 granting), then Gio as the second substrate
   before the interface is declared stable.
8. **G5 + G6** native popups and `PlatformIntegration` for menus/dialogs/
   clipboard with rendered fallback; native macOS menus first.
9. **D1 rollout** trinket-by-trinket graphical paint paths with real fonts.

## Decision log

| # | Date | Decision |
|---|------|----------|
| D1 | 2026-07-05 | Trinkets are mode-aware; same API, per-mode rendering owned by the trinket. TUI cell idioms are TUI-only rendering material. |
| D2 | 2026-07-05 | Apps compile independent of the renderer and talk to a desktop/render server over a socket (X-style). Boundary = **trinket-level protocol**: server owns trinkets/layout/rendering/hit-testing, apps drive proxies with the same API and receive semantic events. In-process stays as a direct implementation. Cell-grid + (later) pixel surface trinkets are the custom-rendering escape hatch. |
| — | 2026-07-05 | Context: PurfecTerm is an independent pre-existing project with TUI, GTK, and Qt frontends in one codebase — proof of the D1 pattern, source of graphical cell-grid rendering, input to O1. |
| D7 | 2026-07-05 | A Canvas trinket (HTML5-canvas-like: PurfecTerm pattern, but for images/drawing) is the pixel escape hatch. Committed to exist; development deferred to a future trinket. Likely command-based + pixel-buffer modes, command-based first. |
| D8 | 2026-07-05 | Grid-metrics model: CellMetrics is a per-container layout vocabulary (app chooses units per virtual row/column for placement density), inherited through the container chain like fonts, overridable per window in all modes including TUI, rooted at the display service's default derived from its system default font size. Text measurement is a separate, per-render-target question. G1 implements this model; call-site audit in `g1-metrics-audit.md`. |
| D9 | 2026-07-05 | Height-for-width: `core.HeightForWidther` optional interface, consulted by layouts at layout time, propagated by containers, absorbed by ScrollArea/Splitter/Window. Text-flow tiers: Label wraps; Checkbox/RadioButton wrap opt-in with the indicator as top-line-anchored chrome and lines hanging under the text; buttons/tabs/list rows/menu items stay single-line; DockRow deliberately not migrated (considered separately); MessageBox a future adopter. Implemented same day (slices 1+2). |
| D10 | 2026-07-05 | Wire discipline: **nothing positional — every value travels under a property name**, with sender-declared, connection-scoped alias dictionaries for efficiency (HPACK-style, but explicit). Text-oriented spirit; exact syntax deliberately open. Consequence: property/event names formalized during the D2 API-shape phase ARE wire vocabulary — maintained deliberately from slice 3 onward (draft registry: `property-vocabulary.md`). |
| D11 | 2026-07-05 | Creation IDs via **request-scoped correlation keys**: `key1=new button …` returns `key1=<server-assigned id>`. Keys are meaningful only within their request; creates batch freely and only the ones needing IDs carry keys — pipelined creation with server-side ID authority. Proposed extension pending review: later lines in a batch may reference earlier keys (forward references — whole UI trees in one burst). Button↔action linkage clarified as OPTIONAL, not required. |
| D12 | 2026-07-05 | Boolean **flags**: presence = true (`wrap`), `!name` = false (`!enabled`), absence = unsaid — default at creation, unchanged on update. Long forms `name=true/false` accepted on input; flag form canonical. Applies only to pure booleans (tri-states are enums); no bitsets on the wire (window flags become individual flag properties). Composes with D10 aliases. |
| D13 | 2026-07-05 | **Inline children blocks**: `new panel children={new button; new button}` builds subtrees structurally, complementing `parent=` (which remains for later additions/reparenting). Block order = layout/z order; correlation keys (D11) remain flat per request at any nesting depth. Unifies list encoding: combo items, tabs, tree nodes, and menu trees are all children blocks of typed items — one recursive construct, not four ad-hoc encodings. |
| D14 | 2026-07-05 | **Templates are macros, not classes**: `template MyBtn=button align=right caption="Click Me"` then `new MyBtn caption="Other"`. Sender-declared, connection-scoped (like aliases); expanded at parse time; instances retain no template linkage — changing a template never restyles live trinkets (live theming stays the schemes system's job). Template properties apply first, instance properties override (D12 flags can explicitly un-set: `!visible`). Transitive templates allowed with cycle guard. Templates MAY contain children (component definitions). Child addressing: resolved by D15. Convention: builtins lowercase, templates CamelCase. |
| D15 | 2026-07-05 | **Hierarchical key scoping with explicit surfacing** (supersedes D13's flat-key note): a key inside a `children={}` block is local to the block and externally addressable as a path through the enclosing key — `k1=new thing children={sk1=new subthing}` → `k1.sk1`. Surfacing is explicit (`mine=k1.sk1`); the reply reports surfaced names + top-level keys only, so reply size is app-controlled. Resolves template-child addressing: instance key namespaces template-body keys (`k1.input`, `k2.input` — collision-free). Unkeyed parents make child keys externally unreachable (intra-block use only) — intentional. Syntax-phase item parked in O6: distinguishing key paths from dotted string values in value position (type-directed vs sigil). |
| D16 | 2026-07-05 | **`?name` = asserted indeterminate**, extending D12 flags to three-valued logic: `name` / `!name` / `?name`, with absence still meaning *unsaid*. Indeterminate is a VALUE (deliberately set); absence never is. Grammar admits `?` on any flag; each property declares whether indeterminate is meaningful (checkbox `checked` yes; `visible` no — rejected under the standard invalid-property policy). Checkbox `checked` returns to being a flag (the off/on/mixed enum is deleted); `tristate` governs UI cycling only. Long form `name=mixed` accepted on input. `?` reserved alongside `!`. |
| D17 | 2026-07-05 | **The wire type system is six types**: `flag`, `enum`, `numeric` (int or float lexically; property declares domain), `identifier` (unquoted reference: object IDs, key paths, command IDs, template names), `{}` (collection, per D13), and `"string"` (**quotes required** — a bare token is never a string). Quoting alone separates references from text (`parent=k1.sk1` vs `caption="k1.sk1"`), closing the D15 parked sigil question. Tokenizer is schema-free; typing is per-property. Command IDs become unquoted (`action=file.open`); D3 key strings stay quoted (`shortcut="^N"`); color literal form TBD. Addendum: alias targets are strings (lexical macros — nothing to validate against); template targets are identifiers. |
| D18 | 2026-07-05 | **Case namespaces**: system names (properties, trinket types, verbs, enums) begin lowercase; **user-defined templates and aliases MUST begin uppercase** (upgrades D14's convention to a rule). The namespaces are disjoint by construction — the system vocabulary can grow without ever colliding with client definitions (HTML custom-elements precedent). `new X…` dispatch: uppercase → template table, lowercase → builtin. Correlation keys unconstrained (different syntactic positions). Enforced by the interpreter; parser stays schema-free. |
| D19 | 2026-07-05 | **Verb inventory**: `new`, `set`, `destroy`, `sub`, `unsub` (plus declaration forms `alias`, `template`). Later verbs reference their target as a **key path or bare numeric ObjectID** (`set root.status caption="…"` / `set 1042 …`) — the one place a bare number is legal (D10 stays intact for properties). Consequence: **correlation keys become session-persistent** (D11 refined: replies still report only their own request's keys; re-registering shadows); surfacing (`wcb=root.cb`) also registers the short name as a key. `destroy` detaches the object and releases every key referencing it. `set` accepts everything `new` accepts, including `children={}` (append). |
| D20 | 2026-07-05 | **Event flow is default-closed except `command`**: state events (`change`, `toggle`, …) are delivered only where a `sub` exists (`sub <target>\|all [events…]`; no events listed = all of that target; `unsub` symmetric, `unsub all` clears). `command` events and registry dispatch always flow — a button with `action=` works with zero subscriptions. **Wire-initiated mutations never echo**: property application during `new` and `set` is suppressed at the connection — no state events, no action firing — killing construction echo and set-feedback loops by construction. |
| D23 | 2026-07-05 | **O1–O4 resolved.** (O1) **SDL first**, Gio second before the substrate interface is declared stable (D5). (O4) **There is no glyph-grid bring-up stage**: the graphical backend implements the SAME rendering primitives with pixels — `DrawText` rasterizes real font glyphs, `DrawRect` draws actual lines instead of box runes, fills are pixels — so the whole desktop renders graphically through the existing trinket paint paths from day one. The cell-grid machinery's permanent home is terminal-style regions only (PurfecTerm, Goal 3). D1's trinket-by-trinket rollout is mode-aware REFINEMENT (real check glyphs, proportional text), not bring-up. (O2) **Dissolved by D8′**: units stay abstract; a graphical surface reports its size in units and root CellMetrics derived from the default font, exactly as the TUI reports 8×16; unit↔device-pixel is the surface's own scale (a DPI-like denomination, HiDPI included). Bring-up default: unit = pixel at scale 1; finer per-surface subdivision remains an open per-surface option the machinery already permits. (O3) **Dissolved by the display-protocol split**: apps never link renderers; selection is WHICH DISPLAY SERVICE BINARY RUNS (TUI desktop vs SDL desktop), dialed via `KITTYTK_DISPLAY` — the X11 model. Build tags are merely how service binaries compile (TUI stays cgo-free); nothing app-facing selects backends. |
| D22 | 2026-07-05 | **Transport shape.** (1) **The wire IS the language**: the socket carries protocol text in both directions — commands/`sub`/`set` inbound, `event`/`reply`/`error` statements outbound; framing is the parser's own brace-awareness (no length prefixes; the O6 bulk frame arrives later as a statement announcing raw bytes). A display service is drivable by hand with socat. (2) **Batches end with an explicit `end` statement** — one reply per batch (D11); blank lines stay insignificant so scripts move to the wire verbatim. (3) **Disconnect = teardown now, reattach-ready**: v1 destroys the connection's UI, but the `hello` handshake carries app identity and a server-assigned session ID from day one, so D4 detach/reattach lands later without a protocol break. (4) **Every connection is a full Application**: its own ApplicationProvider — menu bar content, status bar, dock presence, command identity — peers of in-process apps via the desktop's existing multi-app machinery. Rendezvous: `KITTYTK_DISPLAY` env, default `$XDG_RUNTIME_DIR/KittyTK/display-0.sock`. |
| D21 | 2026-07-05 | **G2/G3 execution model — single-threaded UI on the platform main thread.** The platform loop invokes KittyTK dispatch/layout/paint via callbacks on its (OS-locked) main thread; `Platform.Post(func())` is the ONLY cross-thread door (+ `PostAfter` for timers). Rationale: matches every substrate's real contract (SDL/AppKit/GTK/Qt main-thread rules; Gio adapts), keeps event→layout→paint synchronous with no tree-locking discipline, and the process running Platform/Surface is the display service — app logic lives in other processes after transport, so the classic "app blocks the UI thread" risk is architecturally evicted; the socket reader simply Posts decoded statements. Channel-pumped dispatch was rejected: paint marshaling forces whole-tree snapshots or deep locking, a permanent per-substrate bridge tax. Rendering is **damage-driven** (`Surface.Invalidate` → scheduled frame callback); the TUI platform v1 maps any invalidation to a full repaint (visual parity). `Desktop.Run()` stays as a wrapper over the inverted loop. PurfecTerm relationship clarified: KittyTK becomes another HOST toolkit PurfecTerm is ported onto (like Qt/GTK) — the platform layers stay independent. |
| D8′ | 2026-07-05 | **D8 clarified:** CellMetrics is a coordinate *denomination* (units per row/column, like DPI), not a spacing knob. Row/column-denominated sizes are visually invariant under re-denomination; only explicit numeric unit values reinterpret. Implies denomination scaling at container boundaries (paint + input; `Transform.ScaleX/Y` was built for this) and container-denominated text metrics (font.go's 8/16 are DefaultCellMetrics in disguise). Demo's grid toggle is the acceptance test: must become a visual no-op for row-denominated content. |
| D3 | 2026-07-05 | The direct-key-handler key nomenclature (`^N`, `M-x`, `S-Tab`, …) stays the unified internal, app-facing, and (as far as practical) user-facing key representation on all platforms and in the wire protocol. Native-menu key equivalents, if required, are a one-way mapping at the platform-integration boundary only. Revisitable later as a new decision. |
| D4 | 2026-07-05 | X-direction rendezvous: the display service listens on a well-known endpoint, apps dial in. Sessions are first-class protocol objects separable from connections (enables reattach/multi-viewer without inverting topology). Reverse attachment is a possible later mode. Naming: "display service" and "apps". |
| D5 | 2026-07-05 | Two graphical substrates, Gio and SDL, behind one neutral Platform interface (PurfecTerm-style discipline). Mandatory condition: one shared KittyTK-owned text engine (shaping/measurement/rasterization) outside the substrates, so layout is substrate-independent; it doubles as the server-side TextMeasurer. Substrates land serially; second lands before the interface is declared stable. |
| D6 | 2026-07-05 | Pango-class text (full Unicode, OpenType shaping incl. ligatures and niqqud mark positioning, bidi, fallback, UAX segmentation) is an **available capability**, not a universal mandate: the engine offers a fast simple-run tier and a full shaped-paragraph tier, chosen per trinket need. Interface at shaped-paragraph altitude (attributed text in → shaped runs + cluster maps out); go-text/typesetting reference, cgo HarfBuzz/Pango swappable. Terminal-style regions are a carve-out: PurfecTerm keeps its own proven graphical text handling. TUI-mode fidelity limits remain an accepted asymmetry. |
