# Follow-up: real TUI fonts (multi-cell / block glyphs)

Status: **idea / not started.** Recorded so it isn't lost.

## Today

The text (TUI) backend has no real fonts. Font names collapse to two
behaviors in `backend/tui/tui.go`:

- **Monday** — the normal fixed-width cell (one grapheme per cell, wide
  glyphs take two). Every name that isn't "Tuesday" resolves here, including
  `ui-term`, `ui-text`, and any graphical family name.
- **Tuesday** — the double-width design-aid pseudo-font: a space is inserted
  after each alphanumeric so letters render two cells wide. A testing/design
  aid, invoked by using the font name "Tuesday" (e.g.
  `desktop.SetFont(core.FontTuesday12)`).

So in the TUI, `ui-term` and `ui-text` are Monday by default, and Tuesday is
the one special case — the intended design.

## The idea

We can add *actual* TUI fonts — glyphs drawn from a grid of terminal cells
(block/box characters, shading), so a "font" becomes a small bitmap rendered
across NxM cells. This is how figlet/TOIlet-style banner fonts and pixel-cell
fonts work, but driven from a real font's outlines rather than hand-authored.

Methodology to follow: the cell/pixel font-conversion approach demonstrated at
<https://texteditor.com/font-converter/> — rasterize a source font glyph to a
small bitmap and map filled pixels to block glyphs (full/half/quadrant blocks,
shades) at the chosen cell resolution.

## Sketch of the work (when we pick it up)

- A new pseudo-font kind in the text backend beyond Monday/Tuesday: a
  cell-bitmap font with a per-glyph NxM cell footprint.
- A converter (offline tool or build step) that turns a source TTF/OTF at a
  target cell resolution into a glyph→cell-bitmap table, using the block-glyph
  mapping above. Could reuse the embedded Noto faces + the existing
  `text.Engine` rasterizer to produce coverage bitmaps, then quantize to block
  glyphs.
- Width/advance handling in `tui.go` so multi-cell glyphs place and clip
  correctly (the Tuesday spacing logic is the seed of this).
- A name/registration path so a cell-bitmap font is selectable like Monday /
  Tuesday, and can back `ui-term` / `ui-text` in the TUI when desired.

This pairs naturally with the ANSI-art editor and the retro aesthetic of the
system: a terminal that can render a banner/display font out of block glyphs.
