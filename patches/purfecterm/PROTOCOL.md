# PurfecTerm protocol contract: standard by default, flex by opt-in

This is the written contract for PurfecTerm's two protocol modes, plus the
patch that makes the DEFAULT mode behave like a standard terminal. It exists
because the costliest bug class in this ecosystem has been *unwritten
contracts between components* (three width models disagreeing across
mew/purfecterm/KittyTK). Companion file: `visualprotocol.go` (drop into the
purfecterm root package).

## The idea of the system

The logical grid — one cell per character, width as a per-cell attribute — is
PurfecTerm's machine model, and it is the right substrate for the graphics
stack (sprites in sub-cell units, splits, fractional widths). It stays.

What changes is the DEFAULT **protocol surface**: the dialect a hosted
application speaks. Like the 8-bit machines this system channels, the default
must be boring and standard — a naive program, a piped `ls`, a stock TUI all
behave exactly as on xterm — and the magic (flex widths, logical addressing,
OSC 7000-series graphics) is behind explicit opt-ins, the POKEs of the
system.

## Contract A — STANDARD (default, flex mode off)

The universal wcwidth contract:

- Printing a character advances the cursor by its East Asian Width in
  COLUMNS: wide/fullwidth = 2, else 1; ambiguous follows 2029/2030
  (narrow unless `AmbiguousWidthWide`). The width is stored in the cell's
  `CellWidth` so rendering, wrap, and addressing agree.
- CUP/HVP/CHA columns are VISUAL columns. A column landing on the trailing
  half of a wide glyph clamps onto that glyph (the cursor never sits inside
  a character).
- CUF/CUB move by columns; HT advances to 8-column VISUAL tab stops;
  BS moves one column (landing on a wide glyph's cell when stepping onto
  its trailing half).
- Auto-wrap triggers when the accumulated VISUAL width would exceed the
  line (a row of CJK holds cols/2 characters).
- OVERWRITE GEOMETRY: writes never move other cells' columns. Narrow over a
  wide cell leaves an orphaned space in the vacated column; a wide write
  swallows the following column (a half-swallowed wide neighbor becomes an
  orphan space). This is what makes span-granular screen updates safe, and
  it is exactly xterm's behavior.
- CPR (DSR 6), when implemented, MUST report visual columns in this mode.
- Mouse positions delivered to the application are visual columns (renderers
  that draw by accumulated width already produce visual columns from pixels;
  buffer-side consumers translate with `VisualColToLogical`).
- DECDWL/DECDHL rows address per doubled cell (max cols/2), like hardware.

## Contract B — FLEX (DECSET ?2027h, opt-in)

Unchanged from today: one logical cell per character; `CellWidth` may be
0.5/1.0/1.5/2.0; ambiguous auto-matching; visual-width wrap only under
?2028; CUP/CHA/CUF/CUB/HT address LOGICAL cells; overwrites are logical (the
application owns row geometry — mew handles this with whole-row rewrites when
a row's width profile changes). An application that sends ?2027h declares it
speaks this contract.

Note for the future: the terminal-wg is standardizing mode 2027 with
*different* semantics (grapheme clustering over visual columns). Before the
ecosystem grows, consider moving flex to a private number (e.g. ?7027) and
letting ?2027 mean only "cluster combining sequences", which Contract A
already effectively provides. `visualprotocol.go` is agnostic to the number.

## The patch

### 1. Drop in `_src/visualprotocol.go` (root package)

Provides: `standardCharWidth`, `visualToLogicalLocked` /
`logicalToVisualLocked` (+ exported `VisualColToLogical` /
`LogicalToVisualCol`), `SetCursorVisual`, `MoveCursorForwardVisual` /
`MoveCursorBackwardVisual`, `TabVisual`, `standardOverwriteFixup`. Every
entry point degrades to the raw logical behavior when `flexWidthMode` is on,
so the parser calls them unconditionally.

### 2. parser.go — route cursor movement through the visual entry points

```go
case 'C': // CUF
    p.buffer.MoveCursorForwardVisual(p.getParam(0, 1))
case 'D': // CUB
    p.buffer.MoveCursorBackwardVisual(p.getParam(0, 1))
case 'G': // CHA
    x := p.getParam(0, 1) - 1
    _, y := p.buffer.GetCursor()
    p.buffer.SetCursorVisual(x, y)
case 'H', 'f': // CUP/HVP
    row := p.getParam(0, 1) - 1
    col := p.getParam(1, 1) - 1
    p.buffer.SetCursorVisual(col, row)
```
and `case 0x09: p.buffer.TabVisual()` (was `Tab()`).

### 3. buffer_output.go — standard width, wrap, and geometry

In `writeCharInternal`:

a. Width assignment — the non-flex branch (`charWidth = 1.0`) becomes:

```go
} else {
    charWidth = b.standardCharWidth(ch)
}
```

b. Wrap check — standard mode wraps on accumulated visual width (flex keeps
its ?2028 opt-in):

```go
if (b.visualWidthWrap && b.currentFlexWidth) || !b.currentFlexWidth {
    currentVisualWidth := b.getLineVisualWidth(b.cursorY, b.cursorX)
    shouldWrap = (currentVisualWidth + charWidth) > float64(effectiveCols)
} else {
    shouldWrap = b.cursorX >= effectiveCols
}
```

c. Geometry preservation — immediately after `ensureLineLength` and before
`b.screen[b.cursorY][b.cursorX] = cell`:

```go
if !b.currentFlexWidth {
    b.standardOverwriteFixup(b.cursorY, b.cursorX, charWidth)
}
```

d. `Backspace()` — one COLUMN back in standard mode:

```go
if b.cursorX > 0 {
    if b.flexWidthMode {
        b.cursorX--
    } else {
        v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
        b.cursorX = b.visualToLogicalLocked(b.cursorY, v-1)
    }
}
```

### 4. Renderers — width gate becomes `CellWidth > 0`

Everywhere a renderer computes a cell's visual width
(`gtk/widget.go`, `qt/widget.go`, and KittyTK's `purfecterm_gfx.go` /
trinket TUI paint — the KittyTK sites are already updated in this repo):

```go
cellVisualWidth := 1.0
if cell.CellWidth > 0 {              // was: cell.FlexWidth && cell.CellWidth > 0
    cellVisualWidth = cell.CellWidth
}
```

Standard-mode cells now carry real widths, so wide glyphs get wide boxes in
every renderer with no other changes. `getLineVisualWidth` already follows
this rule. Scrollback ANSI re-emission keys `?2027h` runs off `FlexWidth`,
which standard cells do not set — unchanged.

### 5. Mouse / selection sweep (gtk & qt widgets)

Pixel→column division by cell width yields a VISUAL column once rendering
accumulates widths. Before using such a column as a buffer index (selection,
split hit-testing), pass it through `buf.VisualColToLogical(row, vcol)`;
columns reported to the hosted application stay visual. (KittyTK's trinket
already reports screen columns, which are visual.)

## What this buys, concretely

- Any stock program renders and addresses wide content correctly by default;
  purfecterm becomes drop-in xterm-compatible for the boring cases.
- mew can eventually STOP sending ?2027h and delete its flex adaptations
  (`WithFlexTerminal`, logical-CUP translation, width-profile row
  escalation) — those remain correct meanwhile, since ?2027h keeps
  selecting Contract B. Suggested order: land this patch, release
  purfecterm, then simplify mew against it.
- The Arabic presentation-form shaper (see README.md in this directory) is
  orthogonal and applies to both contracts.

## Compatibility notes

- Rows written before this patch (scrollback) have `CellWidth` 1.0 from the
  old non-flex path — translation and rendering treat them exactly as
  before. No stored-state migration.
- Mixed rows (mode toggled mid-session) translate per-cell from stored
  widths, so both contracts remain coherent on the same screen.
- `getPreviousCellWidth` auto-matching stays flex-only; standard mode is
  deterministic by design.
