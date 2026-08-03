# Pixel mouse in the KittyTK hosts (SGR-Pixels, ?1016)

**STATUS: LANDED upstream in KittyTK `v0.1.7-alpha`** (PR
[#16](https://github.com/phroun/kittytk/pull/16) "Graphical PurfecTerm:
pixel-precise mouse (?1016), script-aware Arabic, embedded focus, OSC-52";
the purfecterm half landed in `purfecterm v0.2.30`). mew's vendored `./kittytk`
is re-synced to v0.1.7, so this note and `pixel-mouse.patch` are kept only as
the development record — the code now lives upstream. The mew-side pieces
(`internal/editor/pixelmouse.go`, the `?1016` gate, the trinket wiring) are in
mew proper. The "further TUI outer-`?1016`" work described at the end remains a
future item.

mew's pixel-precise mouse — the nearest-edge caret in insert mode — works on
any real terminal that speaks `?1016` (the plain build gets it for free). This
brings it to the KittyTK **hosts**, where mew runs as the PurfecTerm-backed
editor trinket and talks to PurfecTerm as its terminal.

## How the pieces fit

Mouse for the hosted mew flows: backend (SDL window / outer terminal) →
KittyTK core `MouseEvent{X,Y Unit}` → the trinket's `gfxMouse*` → PurfecTerm
re-encodes to terminal bytes → mew's input reader. mew's `?1016` handshake
therefore talks to **PurfecTerm** in *both* the SDL and TUI hosts — the same
surface — so one PurfecTerm change lights up both. What differs is only how
much sub-cell resolution the backend can feed in (see the TUI note below).

The feature needs a coordinated chain across three repos:

1. **purfecterm** — `?1016` parse/encode, DECRQM, `CSI 16 t`, and cell-pixel
   size state. See `patches/purfecterm/pixel-mouse.patch` (against v0.2.29).
   *Must land + be released first* (say v0.2.30).

2. **mew core** — a virtualized terminal that answers queries must be allowed
   to probe. `TerminalIO.Interactive` gates the `?1016` handshake on
   `realTerminal || Interactive`, so a live emulator surface probes while a
   dead test buffer never does. **Already in the mew tree** (inert until
   purfecterm answers, so safe to ship ahead): `internal/editor/editor.go`
   (`TerminalIO.Interactive`, `Editor.probeCapable`) and
   `internal/editor/pixelmouse.go` (gate). Test: `TestProbeCapableGate`.

3. **kittytk** — `pixel-mouse.patch` here. The gfx trinket:
   - `editor_mew.go`: sets `Interactive: true` on the hosted mew's terminal.
   - `purfecterm_gfx.go`: `reportMouseGfx` replaces the cell-only
     `sendMouseEventGfx`, reporting a position on a **synthetic pixel grid**
     when the app selected `?1016` and the visual cell otherwise. Device pixels
     can't be reported directly: a cell's painted advance is fractional and its
     boundaries land at `round(col*advance)` (a per-cell rounding), so a hosted
     app dividing by one integer cell size would drift up to a full cell by the
     far edge of the screen. Instead each cell is a fixed `gfxCellSubUnits`
     "pixels" wide (declared via `SetCellPixelSize`/`CSI 16 t`); the report
     puts the exact cell index — from the SAME `cellBoundaryPx` walk the paint
     and `screenToVisualCellGfx` use — in the high digits and the sub-cell
     fraction in the low digits (`pixelReportAxis`). mew's `pixelToCell` then
     divides by `gfxCellSubUnits` and recovers exactly the cell the paint drew,
     drift-free, with the remainder as the sub-cell position. `TestPixelReport*`
     lock the no-drift invariant.

### Apply order

```
# 1. in the purfecterm repo:
patch -p1 < mew/patches/purfecterm/pixel-mouse.patch
cp     mew/patches/purfecterm/_src/pixelmouse_test.go .
go test ./... ./cli/...            # then tag & release, e.g. v0.2.30

# 2. bump the dependency (mew tree):
#    kittytk/go.mod  (and root + app go.mod indirect) → purfecterm v0.2.30

# 3. in the kittytk tree (./kittytk):
git apply patches/kittytk/pixel-mouse.patch
go build ./objects/trinkets/ && go build -tags mew ./objects/trinkets/
go test ./objects/trinkets/
```

Verified in development against a patched purfecterm via a `go.work` replace:
`objects/trinkets` builds under both the default and `-tags mew` tag sets and
its tests pass; the hosted mew then completes the handshake and reports pixels
in the SDL host.

## SDL vs TUI: the precision ceiling

- **SDL host** — the window delivers true sub-cell `Unit` coordinates, so the
  above gives full nearest-edge precision. This is the main event.

- **TUI host** — mew's mouse still routes through PurfecTerm, so the handshake
  succeeds and behaves correctly, but the *source* is the outer real terminal,
  which the tui backend reads via `?1006` (cells): `handleMouseAction` maps
  cell → `Unit` with `CellToUnitsX(col) = col * CellWidth` — the cell's left
  edge, sub-cell residual always 0. So `reportMouseGfx` emits pixels that are
  always the cell's leading edge, and nearest-edge is a silent no-op (harmless).

### TUI outer-`?1016` (a further, separable change — NOT in this patch)

To make the TUI host itself sub-cell precise, the outer terminal's own pixel
resolution has to be carried through. On inspection this reaches past the tui
backend into **direct-key-handler** (the SGR mouse decoder), because a `?1016`
report is byte-identical to a `?1006` one — only the enabled mode says whether
`CSI < b ; x ; y M` carries cells or pixels. The work:

1. **kittytk tui backend** (`backend/tui/tui.go`): at startup, if the outer
   terminal answers `CSI 16 t`, enable `?1016` on it (alongside the existing
   `?1000/1002/1006`) and remember the outer cell pixel size. In
   `handleMouseAction`, when pixel mode is active, treat the decoded numbers as
   pixels: `col = px / outerCellW`, and the sub-cell residual becomes the `Unit`
   fraction `(px % outerCellW) * CellWidth / outerCellW` — the finer coordinate
   the gfx path already knows how to forward.

2. **direct-key-handler**: surface `?1016` reports distinctly from `?1006`
   (either a pixel-mode flag on the decoded mouse action, or a raw pass-through
   the backend scales), and do not clamp pixel-magnitude coordinates to the
   cell grid. Ship as a tagged release, bump, then wire the backend.

It degrades gracefully — a terminal that ignores the outer `CSI 16 t` / `?1016`
simply keeps cell resolution, exactly as the plain build does today — and only
benefits users whose outer terminal supports pixel mouse. Lower priority than
the SDL path, and independent of it.
