# ifitfits — improvement notes

Notes gathered while integrating **ifitfits v0.1.0** into mew's main-area window
tiling. These are gaps and rough edges we hit in practice — some we worked
around host-side, but fixing them in the library would help us and any other
host, especially cell/grid renderers (terminals). Ordered roughly by impact.

## 1. Clone metrics on `New` / `Split`, not just the ref

`New` and `Split` clone the origin tile's **ref**, but not its **metrics**
(min/natural sizes). A freshly created tile gets the library default minimum
(`minW = 90`), so on a modest workspace it is omitted from the layout
immediately — a split "does nothing visible" until the host stamps metrics on
the new tile.

We now call `SetMetrics(newTile, …)` after **every** `New`/`Split`. Since the
ref is already cloned, cloning the origin's metrics too would be the least
surprising behavior — or let `New`/`Split` take metrics inline.

## 2. Integer / cell-grid support (edge-rounding)

Rects are `float64`, which is right for a renderer-agnostic engine, but a
cell/grid host has to snap them to integer cells. Truncating each tile's `X`
and `W` **independently** leaves a one-cell gap at fractional splits — e.g. an
81-column area halved into `40.5 + 40.5` truncates to `40 + 40`, one column
short, so the tiled region comes up narrower than the surrounding chrome
(inconsistently, only at widths where the split lands on a half-cell).

The fix is to round each tile's **left/right (and top/bottom) edges** and take
the span, so adjacent tiles stay flush and the last tile reaches the workspace
edge. It's a small recipe, but every grid host needs it. Worth either
**documenting** the edge-rounding pattern or offering a helper (e.g. a variant
of `Tiles()` returning integer-snapped, contiguous rects).

## 3. `GetMetrics` for read-back symmetry

`SetMetrics` / `SetPriority` are write-only. `Box` reflects `Priority` and the
*resolved* `Rect`, but not the min/natural hints you set — so there is no way to
read a tile's current metrics. A `GetMetrics(tile) (minW, minH, natW, natH,
ok)` would round out the surface (and should travel to the JS port for parity).

## 4. A root-level split / append primitive

"Add a tile at the outermost edge of the workspace" has no direct primitive:
there is no root handle, and `New`/`Split` operate on a *given* tile. We
approximate "new tile to the right" with `New(activeTile, Right, ref)`, which
busts out to the root's axis — workable, but it depends on already having a tile
to anchor from and on the root's current orientation. An explicit
`SplitRoot(dir, ref)` / `AppendRoot(dir, ref)` (or an accessor for the root)
would make the common "open another pane alongside" case unambiguous.

## 5. Concurrency discipline

The package documents "not safe for concurrent use." In a real host that is
easy to violate accidentally: we drive the tiler from a focus hook *and* from
the render loop, and had to make sure both stay on one goroutine (the focus hook
runs synchronously, never off an async event). A one-line note on the expected
single-threaded discipline — "serialize all calls; don't touch it from an async
event handler" — would preempt a class of subtle races.

## Already addressed during the port

`Stacks()` and `Reveal()` (tab-strip info including hidden tabs, plus
click-to-reveal a tab) were added during the Go↔JS parity work, so a host can
actually draw tabbed stacks. Noting here so they are not re-flagged.

---

*Nothing above blocks the current integration — these are quality-of-life and
gap-filling items for a future ifitfits release.*
