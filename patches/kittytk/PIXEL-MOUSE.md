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

- **TUI host** — mew's mouse routes through PurfecTerm, so the handshake
  succeeds; the *source* is the outer real terminal. Originally the tui backend
  read it via `?1006` (cells) only, mapping cell → `Unit` at the cell's left
  edge (sub-cell residual always 0), so nearest-edge was a silent no-op. The
  **TUI outer-`?1016`** work below lifts that ceiling.

### TUI outer-`?1016` — IMPLEMENTED in the vendored tree (`tui-outer-pixel-mouse.patch`)

To make the TUI host itself sub-cell precise, the outer terminal's own pixel
resolution is carried through. The pleasant surprise on implementation: this
needs NO direct-key-handler change. dkh **v0.3.11** already surfaces the two
probe replies as distinct pseudo-keys — `DECRPM:Ps;Pm` (the DECRQM answer) and
`WinOp:Ps;…` (the XTWINOPS answer) — and already passes SGR mouse coordinates
through **unclamped** (`parseMouseSGR` → `Mouse@%d,%d` straight from the decoded
integers), so a `?1016` report's pixel-magnitude numbers arrive intact. A
`?1016` report is byte-identical to a `?1006` one; only the enabled mode says
whether the numbers are cells or pixels, and the backend is what enabled the
mode, so the backend alone knows how to read them.

So the whole feature is one file, `backend/tui/tui.go`:

1. **Probe** (in `Init`, after the keyboard reader starts, since the replies
   are asynchronous): send DECRQM `CSI ? 1016 $ p` and XTWINOPS `CSI 16 t`.
2. **Consume the replies** in `handleKey`: `handleDECRPM` records that `?1016`
   is settable (Pm ∈ {1,2,3}); `handleWinOp` records the outer cell pixel size
   (Ps=6, height;width). `maybeEnablePixelMouse` enables `?1016` on the outer
   terminal once BOTH have arrived (they race in either order) and flips the
   backend to pixel interpretation.
3. **Convert** in `outerToUnitsX/Y`: the raw 1-based report is a cell column in
   the default mode (→ left edge, unchanged) or an outer pixel under `?1016` —
   `cell = (px-1) / outerCellW`, and the remainder scales into a fraction of
   this backend's cell width, `(px-1) % outerCellW * CellWidth / outerCellW`:
   the finer coordinate the gfx/PurfecTerm path already forwards. The pending
   position and the drag-embedded position share the helper.
4. **Cleanup**: `?1016l` leads the mouse-mode reset in `RestoreTerminal`.

It degrades gracefully — a terminal that ignores either probe keeps cell
resolution, exactly as before — and `pixelmouse_test.go` locks the probe gate
(enable only when both replies land; refuse on Pm 0/4), the pixel↔unit
conversion, and the cell-mode fallback. Independent of the SDL path and mew-free
(pure KittyTK tui backend), so it is a clean upstream candidate.
