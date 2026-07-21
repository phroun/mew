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

### Assigned mode numbers (decided 2026-07)

PurfecTerm's private DECSET block is **7020–7049**, rhyming with the OSC
7000-series graphics protocol: *7000s = PurfecTerm extensions*, in both
namespaces. The width family moves there keeping its last-two-digit
mnemonics:

| old       | new       | meaning                          |
|-----------|-----------|----------------------------------|
| `?2027`   | `?7027`   | flex width (Contract B opt-in)   |
| `?2028`   | `?7028`   | visual-width line wrap           |
| `?2029`   | `?7029`   | ambiguous width: narrow          |
| `?2030`   | `?7030`   | ambiguous width: wide            |

`?2027` is then repurposed to its terminal-wg standards-track meaning
(grapheme cluster processing over visual columns): ACCEPT it and treat it as
inherently satisfied — purfecterm always clusters combining marks and
Contract A supplies visual-column widths — and report it set/always under
DECRQM when implemented. No flex alias is kept on 2027 (a clean break is the
point; both sides of the wire are in this repo family today).

Sweep sites for the renumber: the parser's DECSET cases 2027–2030, and
`buffer_scrollback.go`'s per-run re-emission of `?2027h/l` → `?7027h/l`.

mew consequence: mew's startup `?2027h` (EnableGraphemeWidth) becomes correct
everywhere — real terminals enable standards grapheme clustering, patched
purfecterm acks it as inherent — and only true flex clients ever send
`?7027h`. `visualprotocol.go` is agnostic to the mode number.

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

### 4b. cli/renderer.go — the real-terminal renderer emits visual columns

The CLI adapter writes into a HOST terminal, which is always visual: with
standard-mode wide cells (one logical cell, width 2), its logical-index
column math places everything after a wide glyph one column early and CUPs
into glyph halves. Included in `standard-default.patch`:

- `hostCellWidth(cell)`: the columns a cell occupies on the host (2 when
  `CellWidth >= 1.5`, else 1 — fractional flex widths quantize; a host
  terminal cannot render halves).
- `Render()` and `RenderToString()` track `vx`, the accumulated visual
  column, and address each emitted cell at `vx` instead of its logical
  index x (the clip test in RenderToString also runs on the visual column).
- Both hardware-cursor CUPs map through `buffer.LogicalToVisualCol`.
- `renderedCell` carries `cellWidth` in the diff comparison, so a
  width-attribute change re-emits the cell.

Verified: `_src/cli_visualprotocol_test.go` (drop into `cli/`) feeds
"日abc" through the real parser and asserts 'a' emits at visual column 3,
the wide glyph's right half is never addressed, and the cursor parks at
visual column 6 — and the pre-existing root + cli test suites still pass
with the whole patch applied.

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

---

# Font slots: per-cell font selection (SGR 10-20 + OSC 7004)

Companion patch: `font-slots.patch` (unified diff for cell.go, buffer.go,
buffer_output.go, parser.go, cli/terminal.go against v0.2.23) plus
`_src/fontslot_test.go` and `_src/cli_fontslot_test.go` (drop-in tests).
Developed against v0.2.23; not yet landed upstream.

## The idea

A cell already carries color, weight, and flip attributes; this adds a **font
slot** — a small integer chosen with the standard ANSI font-selection SGRs and
resolved, per terminal, to a family name that a renderer maps through its
shared font engine. It is deliberately just an *index* in the machine model:
purfecterm stores a `uint8` per cell and a slot→family map per terminal, and
stays entirely out of font loading and rasterization (that is the renderer's —
KittyTK's — job). This keeps the GTK and Qt builds working unchanged: they
simply ignore `Cell.Font` until they choose to honor it, exactly as they
ignore any attribute they do not yet draw.

## The wire protocol

### SGR — select the active slot (ANSI-standard 10-20)

| SGR       | slot | meaning                                   |
|-----------|------|-------------------------------------------|
| `10`      | 0    | primary font (also the reset default)     |
| `11`-`19` | 1-9  | alternate fonts                           |
| `20`      | 10   | Fraktur / gothic                          |

These are the historical ECMA-48 font-selection codes (10 = primary, 11-19 =
alternate 1-9, 20 = Fraktur), so a naive terminal that ignores them and a
purfecterm renderer that honors them stay wire-compatible. SGR `0` (reset)
returns to slot 0 along with the other attributes. The active slot is written
into every subsequently painted cell's `Font` field.

### OSC 7004 — configure the slot→family map

```
ESC ] 7004 ; <cmd> BEL
```

| cmd                | effect                                            |
|--------------------|---------------------------------------------------|
| `f;SLOT;FAMILY`    | map slot SLOT (0-10) to family name FAMILY        |
| `fd;SLOT`          | clear slot SLOT (it then inherits slot 0)         |
| `fda`              | clear all slot mappings                           |

`FAMILY` is an opaque name handed to the renderer's font engine (e.g.
`JetBrainsMono`, or a KittyTK alias like `ui-term` / `ui-fraktur`). Slot 0 is
the primary; **any unset slot inherits slot 0's family**, so an application can
configure just the slots it uses and everything else falls back to the primary
face. This mirrors KittyTK's own alias fallback and the "any font not
specified defaults back to the primary" rule.

## The machine model (what the patch adds)

- `Cell.Font uint8` — slot 0..10 stored per cell (cell.go), written from
  `Buffer.currentFont` in `buffer_output.go`.
- `Buffer.currentFont uint8` + `Buffer.fontSlots map[uint8]string`
  (buffer.go): the active slot and the per-terminal slot→family map.
- `Buffer.SetFont(slot)` / `GetFont()` — active slot, clamped to 0..10.
- `Buffer.SetFontSlot(slot, family)` / `GetFontSlot(slot)` — the map;
  empty family clears a slot; `GetFontSlot` returns slot 0's family for any
  unset slot (and "" when even slot 0 is unset → renderer's default primary).
- `ResetAttributes` clears `currentFont` to 0 alongside the other SGR state.
- parser.go: SGR cases 10 / 11-19 / 20 call `SetFont`; OSC dispatch routes
  7004 to `executeOSCFont`, which parses the `f` / `fd` / `fda` commands.
- cli/terminal.go: `RenderedCell.Font` carries the slot out through
  `GetCells` so a CLI consumer can resolve it too.

## Renderer contract (KittyTK side, staged)

A renderer honoring font slots reads `cell.Font`, looks up
`buffer.GetFontSlot(int(cell.Font))` for the family name, and resolves that
name through its shared font engine (KittyTK's `text` engine, where `ui-term`
and `ui-fraktur` are live-redefinable aliases). Because `GetFontSlot` already
folds unset slots onto slot 0, the renderer never has to special-case an
unconfigured slot. A renderer that does nothing with `cell.Font` renders
everything in its primary face — the GTK/Qt builds' pre-wiring behavior — so
the machine model is safe to land (and did, in v0.2.24) ahead of any renderer
honoring it.

**Renderer wiring status.** The KittyTK gfx (SDL) trinket honors `cell.Font`
in the kittytk tree: `drawCellText` resolves the cell's family via a
`cellFamily(buf, cell)` helper — slot 0 → the "ui-term" primary alias, an
unset slot → GetFontSlot's slot-0 fallback, a configured slot → its family —
and threads it through the coverage-mask cache (keyed by family). The GTK and
Qt per-cell renderers are brought to parity by `font-slots-renderers.patch`
(against v0.2.24): each gains a `cellFontFamily(cell, primary)` helper, and the
per-cell font — already computed as
`getFontForCharacter(cell.Char, fontFamily, size)` — takes the slot-resolved
family in place of the bare primary, so the existing per-character CJK/Unicode
fallback still layers on top. Slot 0 stays the widget's `SetFont` family; the
KittyTK "ui-term" alias is a KittyTK-engine concept, not a purfecterm one, so
on the standalone gtk/qt builds slot 0 is simply the configured widget font.

## Verification

`_src/fontslot_test.go` (root package) drives the parser with
`A ESC[11m B ESC[20m C ESC[10m D ESC[0m E` and asserts slots
`[0,1,10,0,0]`; checks the slot map's inherit-slot-0 and clear semantics; and
feeds `ESC]7004;f;2;Comic Mono BEL` then `ESC[12mZ` to confirm OSC config +
SGR select paint slot 2. `_src/cli_fontslot_test.go` (cli package) confirms
`RenderedCell.Font` survives `GetCells`. Patched tree builds and the full root
+ cli suites pass against v0.2.23.

---

# Script-class fonts: automatic per-script fallback (OSC 7005)

Companion patch: `font-scriptclass.patch` (buffer.go, scriptclass.go [new],
parser.go, gtk/widget.go, qt/widget.go against v0.2.25) + `_src/scriptfont_test.go`.
Not yet landed upstream.

## The idea

Font slots (OSC 7004) are *app-selected per cell* (SGR 10-20). Script-class
fonts are the orthogonal *automatic* mechanism: a per-terminal map from a
script class — `hebrew`, `arabic`, `cjk` — to the family a renderer uses when
the primary font can't cover a glyph of that script. The app configures the
map once; the renderer classifies each glyph by its Unicode script and picks
the mapped face. This is what lets the standalone gtk/qt builds render Hebrew,
Arabic, and CJK reliably, the way the KittyTK/SDL renderer already does through
its font engine.

## The wire protocol

```
ESC ] 7005 ; <cmd> BEL
```

| cmd                | effect                                                   |
|--------------------|----------------------------------------------------------|
| `s;CLASS;FAMILY`   | map script class CLASS (hebrew/arabic/cjk) to FAMILY     |
| `sd;CLASS`         | clear CLASS (renderer falls back to its own default)     |
| `sda`              | clear all script-class mappings                          |

`FAMILY` is a concrete family name the renderer's font system resolves
(Pango / Qt on the standalone builds). `CLASS` is lower-cased.

## The machine model

- `Buffer.scriptFonts map[string]string` + `SetScriptFont(class, family)` /
  `GetScriptFont(class)` / `ClearScriptFonts()` (buffer.go). It is terminal
  config, not per-cell state, so `ResetAttributes` does NOT touch it (like the
  font-slot map).
- `ScriptClass(r rune) string` (scriptclass.go, exported root helper): buckets
  a rune into `hebrew` / `arabic` / `cjk` / `""` by Unicode range (letters plus
  the RTL Presentation Forms and the full CJK/Hangul/kana/fullwidth blocks).
- parser.go routes OSC 7005 to `executeOSCScriptFont`.
- gtk/qt `getFontForCharacter`: after the primary-has-glyph check and before
  the generic CJK/Unicode fallback, consult
  `buffer.GetScriptFont(ScriptClass(r))` and use it when set. So an app's
  Hebrew/Arabic/CJK face choice wins over the widget's generic fallbacks, and
  Latin/box-drawing/etc. are unaffected (ScriptClass returns "").

## Relationship to the KittyTK/SDL path

The KittyTK gfx renderer resolves scripts through its own font engine (the
`ui-term-<script>` alias tree), so it does not need OSC 7005 — the two are
parallel mechanisms: OSC 7005 is the portable, per-terminal wire protocol for
renderers without that engine; the alias tree is KittyTK's richer, host-config
mechanism. An app that wants to run identically on gtk/qt and KittyTK can send
OSC 7005 and also configure the engine.

## Verification

`_src/scriptfont_test.go` (root package): `ScriptClass` bucketing; the map's
set / case-insensitive read / clear-one / clear-all; and OSC 7005 `s` / `sd` /
`sda` driving it. Patched v0.2.25 tree builds and the full root + cli suites
pass.
