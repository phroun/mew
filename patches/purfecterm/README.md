# PurfecTerm patches

**STATUS: LANDED upstream in purfecterm v0.2.23** (standard-by-default
protocol, ?7027 renumber with ?2027 accepted as inherently satisfied, CLI
renderer visual emission, and the exported ShapeArabicCellVisual shaper —
which KittyTK now consumes directly, its private copy retired). This
directory remains as the development record; PROTOCOL.md's contract now
lives with the purfecterm repo.

**v0.2.24** landed the font-slot **machine model** (item 3 below):
`Cell.Font`, the buffer slot API, SGR 10-20, and OSC 7004. **v0.2.25** then
landed the **renderer wiring** (item 4): the gtk/qt widgets now honor
`cell.Font`. Both are upstream; the KittyTK gfx/SDL renderer honors slots in
the kittytk tree. This directory keeps the patch artifacts as the development
record.

Verified patches for the purfecterm repo, developed from the mew/KittyTK
integration work:

1. **Standard-by-default protocol** — see `PROTOCOL.md` (the contract),
   `_src/visualprotocol.go` + `_src/arabicshape.go` (drop-in root-package files; the `_src/` name keeps the Go tool from compiling them inside this repo),
   `standard-default.patch` (unified diff for parser.go, buffer_output.go, and cli/renderer.go,
   verified: patched tree builds and `visualprotocol_test.go` passes against
   v0.2.22), and `_src/visualprotocol_test.go` (drop-in test locking the
   contract). **LANDED in v0.2.23.**
2. **Arabic contextual joining** — below. **LANDED in v0.2.23**
   (`ShapeArabicCellVisual` exported).
3. **Font slots — machine model (SGR 10-20 + OSC 7004)** — per-cell font
   selection. See the "Font slots" section of `PROTOCOL.md` (wire protocol +
   machine model), `font-slots.patch` (unified diff for cell.go, buffer.go,
   buffer_output.go, parser.go, cli/terminal.go against v0.2.23), and
   `_src/fontslot_test.go` + `_src/cli_fontslot_test.go` (drop-in tests).
   **LANDED in v0.2.24** (`Cell.Font`, `Buffer.SetFont/GetFont/SetFontSlot/
   GetFontSlot`, SGR 10-20, OSC 7004, `cli.RenderedCell.Font`).
4. **Font slots — GTK + Qt renderers honor `cell.Font`** — the per-cell
   renderers still paint every glyph in the primary face; this teaches them to
   read the slot. `font-slots-renderers.patch` (unified diff for gtk/widget.go
   and qt/widget.go **against v0.2.24**) adds a `cellFontFamily(cell, primary)`
   helper to each widget and routes the per-cell font through it: slot 0 (and
   any unconfigured slot) stays the primary family; a configured slot names its
   own family, still subject to the existing per-character CJK/Unicode fallback.
   Both the main paint and the split-view paint are covered. Apply with
   `patch -p1 < font-slots-renderers.patch` from the purfecterm root. **LANDED
   in v0.2.25.** (The KittyTK gfx trinket — the SDL path — honors `cell.Font`
   in the kittytk tree; this patch brought gtk/qt to parity.)

   *Compile note:* gtk/qt need their system toolkits (pango/gdk, Qt) present to
   build, so these two hunks were verified by patch-applies-clean + gofmt +
   review rather than a local `go build`; the change mirrors the existing
   `getFontForCharacter(cell.Char, fontFamily, …)` idiom, substituting the
   slot-resolved family for the bare primary.
5. **Script-class fonts (OSC 7005)** — the *automatic* per-script counterpart
   to the app-selected font slots: a per-terminal map from a script class
   (`hebrew`/`arabic`/`cjk`) to the family a renderer uses when the primary
   can't cover a glyph of that script, so the standalone gtk/qt builds render
   RTL + CJK reliably (as the KittyTK/SDL renderer already does via its engine).
   See the "Script-class fonts" section of `PROTOCOL.md`,
   `font-scriptclass.patch` (buffer.go, scriptclass.go [new], parser.go,
   gtk/widget.go, qt/widget.go **against v0.2.25**), and
   `_src/scriptfont_test.go`. Adds `Buffer.SetScriptFont/GetScriptFont/
   ClearScriptFonts`, the exported `ScriptClass(rune)`, OSC 7005 (`s`/`sd`/`sda`),
   and a script-class branch in each widget's `getFontForCharacter`. Verified:
   patched v0.2.25 root + cli suites pass; gtk/qt by patch-applies-clean + gofmt
   + review (same toolkit caveat as item 4). **Not yet landed upstream.**

---

# PurfecTerm patch: Arabic contextual joining for the per-cell renderers

## The bug

Every PurfecTerm graphical renderer draws **one cell at a time** and hands the
text engine only that cell's `cell.String()`:

- `gtk/widget.go` ~line 1903: `charStr := cell.String()` → `pango_render_text`
- `qt/widget.go` ~line 1728: `charStr := cell.String()` → `painter.DrawText3`
- (KittyTK's gfx trinket did the same at `purfecterm_gfx.go` `drawCellText`;
  already fixed in the kittytk tree with a private copy of this shaper.)

A shaping engine given a single Arabic letter has no joining context, so it
always produces the ISOLATED form — even though Pango/Qt/go-text are all
capable of joining. Empirically (2026-07 screenshots) only the KittyTK/SDL
path renders visibly broken-apart Arabic; the gtk and qt builds look joined
because their fallback fonts' isolated forms happen to carry connective
strokes. The architecture is the same in all three, so gtk/qt work by font
luck: apply this patch there for typographically correct contextual forms and
a real lam-alef ligature, or skip it while the current fonts satisfy.

## The fix

`arabicshape.go` (drop into the purfecterm root package): a table of Unicode
Arabic Presentation Forms plus

```go
func ShapeArabicCellVisual(left, c, right rune) (glyph rune, suppress bool)
```

For each cell, pass the base characters of the visually-left and
visually-right neighbor cells; draw the returned glyph instead of `cell.Char`.
`suppress` marks the alef half of a lam-alef pair (the mandatory ligature is
drawn in the lam's cell): paint the background, skip the glyph.

Cells are assumed to hold VISUAL order (RTL runs reversed into left-to-right
cells), which is what bidi-implementing applications like mew emit: the right
neighbor is the logically previous letter, the left neighbor the logically
next. Non-Arabic characters pass through unchanged, so the call is safe on
every cell.

## gtk/widget.go hunk (the cell character render, ~line 1893)

```go
			// Draw character (skip if traditional blink mode and currently invisible)
			if cell.Char != ' ' && cell.Char != 0 && blinkVisible {
				// Check for custom glyph first
				if w.renderCustomGlyph(cr, &cell, cellX, cellY, cellW, cellH, x, blinkPhase, scheme.BlinkMode, lineAttr) {
					goto afterCharRender
				}

				// >>> ADD: Arabic contextual joining from the neighbor cells.
				var leftCh, rightCh rune
				if x > 0 {
					leftCh = w.buffer.GetVisibleCell(x-1, y).Char
				}
				if x+1 < effectiveCols {
					rightCh = w.buffer.GetVisibleCell(x+1, y).Char
				}
				shapedChar, suppress := purfecterm.ShapeArabicCellVisual(leftCh, cell.Char, rightCh)
				if suppress {
					goto afterCharRender // alef of a lam-alef: ligature lives in the lam's cell
				}
				// <<< ADD

				// Determine which font to use for this character (with fallback for Unicode/CJK)
				charFont := w.getFontForCharacter(shapedChar, fontFamily, fontSize) // was: cell.Char

				// Get character string including any combining marks
				charStr := string(shapedChar) + cell.Combining // was: cell.String()
```

## qt/widget.go hunk (~line 1728) — optional

Empirically the Qt build already renders joined-looking Arabic (font-level
connective strokes), so patch only if you want identical shaping:

```go
				// Measure actual character width
				metrics := qt.NewQFontMetrics(drawFont)
				// >>> ADD (mirror the gtk hunk: neighbors via GetVisibleCell,
				// suppress -> skip the draw, then):
				charStr := string(shapedChar) + cell.Combining // was: cell.String()
				// <<< ADD
				actualWidth := metrics.HorizontalAdvance(charStr)
```

The split-view path (`qt/widget.go` ~line 1473, `gtk` split equivalent) draws
through the same per-cell idiom; apply the identical neighbor+shape+suppress
treatment there, using `GetCellForSplit(col±1, ...)` for the neighbors.

## Font note

The glyphs come from Arabic Presentation Forms-A/B (U+FB50–FEFF). Any font
with real Arabic coverage includes them; Pango/Qt fall back automatically.
