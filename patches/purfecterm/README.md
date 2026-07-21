# PurfecTerm patches

Two verified patches for the purfecterm repo, developed from the mew/KittyTK
integration work:

1. **Standard-by-default protocol** — see `PROTOCOL.md` (the contract),
   `_src/visualprotocol.go` + `_src/arabicshape.go` (drop-in root-package files; the `_src/` name keeps the Go tool from compiling them inside this repo),
   `standard-default.patch` (unified diff for parser.go + buffer_output.go,
   verified: patched tree builds and `visualprotocol_test.go` passes against
   v0.2.22), and `_src/visualprotocol_test.go` (drop-in test locking the
   contract).
2. **Arabic contextual joining** — below.

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
