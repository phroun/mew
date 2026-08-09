# Geometry: cells, denomination units, and pixels

This is the authoritative model for how KittyTK turns abstract layout coordinates
into device pixels, and — just as important — **which of two conversions to use
where**. Getting this wrong produces a specific, recurring class of bug: geometry
that is *sized* by one conversion but *painted* by the other, so a window's frame
runs a handful of pixels past its own surface, or a torn-off window shrinks a
little on every undock. If you are about to convert units → pixels for anything,
read this first.

## Terms

- **Unit** (`core.Unit`) — the abstract layout coordinate. All trinket placement,
  sizing, and hit-testing is expressed in units. Units are integers; there is no
  such thing as "half a unit" in a layout.
- **Denomination** (`core.CellMetrics`) — how many units make up one character
  cell. The default is **8 units wide × 16 tall** (`DefaultCellMetrics`). A unit
  is therefore a *sub-cell* quantity (⅛ of a cell's width by default).
- **Denomination can change per subtree.** A trinket may be *re-denominated*
  (`Painter.WithDenomination`) — e.g. an MDI child, or mew's inner PurfecTerm —
  so the *same* unit value means a different physical size in different places.
  This is why anything that must be **physically uniform across windows** (the
  frame border) cannot be stored as a unit count; see [Window border](#window-border).
- **Zoom** — chosen by `[window] scale` (integer device pixels per unit at the
  base font) and `[window] font_size` together. The single derived quantity is
  `pixels-per-unit`.

## The two conversions

Everything below hinges on these two, and on the fact that **they are equal at the
base font size (12) and diverge only when zoomed**.

### 1. Pure ratio — `pixels-per-unit`

```
pixels-per-unit = scale × fontSize / 12
```

Smooth, no rounding. Exposed as `Backend.PxPerUnit()` / `Painter.PxPerUnitF()`
(raster.go). **Use it only for things that are not layout geometry:**

- **Glyph rasterization scale** — the bitmap a glyph is drawn at (`text.Render`).
- **Decorative lengths** — corner radius, stroke/border *weight*, the tabbed
  control's hairline (`pxLen`, `Painter.UnitsToPx`).

These want to grow smoothly with the font and never need to line up with a cell
edge, so the pure ratio is correct for them.

### 2. Hardened cell pitch — the geometry conversion

The cell's pixel size is **hardened once per zoom**, rounded **up**:

```
CELL_PX(denom) = ceil(denom × fontSize / 12) × scale        // raster.go cellPx
```

The `ceil` exists for exactly one reason, documented at `raster.go`:

> *a cell must fully contain its glyph line box, so it can never be shorter than
> the character it holds — otherwise descenders spill below the item's background
> fill.*

That is a **glyph-containment** guarantee. Because of it, `CELL_PX / denom` runs a
hair faster than `pixels-per-unit`, and across a wide window that difference
accumulates (tens of pixels at some font sizes). A unit *length* `W` converts to
pixels as:

```
geometry_px(W) = round(W × CELL_PX / denom)
              = (W/denom)·CELL_PX + round((W%denom)·CELL_PX/denom)     // == snapAxis
```

This is exactly what the raster backend's `snapAxis` / `UnitToPxX/Y` compute, and
it is what **all painting already uses** (`FillRect`, `DrawRoundedRect` window
frames, clips, text origins). Its inverse (pixels → whole units) is `Size()` /
`unSnapAxisFloor`.

**Use the hardened cell pitch for all layout geometry:** trinket and window
placement and sizing, a torn window's OS-surface pixel size and position,
hit-testing edges, and the frame-border reservation. The painter helpers
`UnitSpanPxX/Y` give a hardened span that lines up with painted shapes exactly;
prefer them over `UnitsToPx` whenever the span borders drawn geometry.

## The rule (one line)

> **Layout geometry is sized and placed on the hardened cell pitch. The pure ratio
> is only for glyph bitmaps and decorative lengths. Never size geometry with the
> pure ratio, and never size a surface with one pitch while painting it with the
> other.**

At `fontSize == 12` the two coincide, which is why pitch-mixing bugs are invisible
until someone zooms.

## PurfecTerm surfaces are the exception you *expect*

A PurfecTerm surface (including mew's editor trinket, which paints through
purfecterm) renders its **own** cell geometry, at its **own** denomination, and
deliberately does not re-snap to the host grid (`purfecterm_gfx.go`,
`editor_mew.go`). That is intended: it *is* a cell surface. The cell-pitch
behavior is meant to live in the TUI backend and in PurfecTerm surfaces — not to
leak into the placement of general KittyTK trinkets. (In the graphical host both
paths run through the one raster backend, so the distinction is about *which
conversion a given call site uses*, not about a separate backend.)

## Windows: outer bounds, border, client area, origin

- **Window-local `(0,0)` is the outer top-left corner** — the frame border starts
  there.
- **The outer bounds include the border.** Content is `bounds − 2·b` on the sides
  (`window.go` `contentBounds`, graphical-frames branch). Tile/cascade operate on
  the border-inclusive outer footprint, which is what you want.
- **The client area is a first-class, reported feature.** `ContentBounds()` and
  `ClientAreaOffset()` return the border-and-titlebar-excluded rectangle and its
  offset `(b, top)`; a window's own painter and its inner-control geometry use
  *these*, never the outer bounds.

## Window border

The frame border must be **physically uniform across every window on screen**,
even when windows carry different denominations — you should never see one
window's border render thicker than another's. It must also **scale with zoom**
(a border that stayed a fixed pixel count would look proportionally thinner as you
zoom in).

Therefore the border is **not** stored as a unit count (a unit count is not
physically uniform across denominations). Instead:

- `[window] border_width` is the border thickness **at the base zoom**
  (`pixels-per-unit == 1`, i.e. font 12 / scale 1). Default 2.
- **Once per zoom**, one hardened device-pixel value is computed from the
  desktop's single pitch and applied to every window:

  ```
  border_px = round(border_width × pixels-per-unit)
  ```

  This scales with zoom, is calculated once when the zoom changes, and is the
  same physical thickness on all windows by construction.
- A window reserves this **same physical `border_px`** inside its outer bounds.
  Its border **in that window's units** is `border_px ÷ that window's pitch`,
  computed from the window's own metrics — the physical thickness stays constant;
  only the unit count differs per denomination. The client area is the outer
  bounds minus that reservation.

## Anti-pattern to watch for (the retire-list)

The pure ratio is currently (correctly) consumed by glyph rendering, `pxLen`
(radius/stroke), the tab hairline, text-line device fills, and PurfecTerm's own
cell math. Where it must **not** appear is general geometry. Known leaks to keep
retired:

1. **Torn-window OS-surface size, position, and bounds round-trip** — must use the
   hardened cell pitch, so the surface is exactly as wide as what the frame paints
   into it (mixing pitches here is the "undock shrink" / "lost right edge" bug).
   The torn window's *corner radius* stays on the pure ratio (it's decorative).
2. **Frame-border → units** (`WindowFrameBorderUnits`) — must resolve the
   universal physical `border_px` through the **requesting window's** hardened
   pitch, not the desktop's raw ratio, so a re-denominated window still reserves
   the same physical border.
