# purfecterm: `IsCombiningMark` misses most of Unicode's non-spacing marks

**Patch:** `0003-purfecterm-combining-marks.patch` (against v0.2.28, `cell.go`)

## Symptom

A line containing a combining mark from almost any script outside Latin,
Hebrew, Arabic, Thai or Devanagari renders one column too wide *per mark*.
Everything after the mark on that row is shifted right, and the row's tail is
left unpainted — showing the terminal's own background rather than the app's.
The drift is per-row, so a screen of such text develops a ragged right edge
whose depth varies line by line with the number of marks.

It also renders *inconsistently*: an initial paint and a repaint after
scrolling can disagree, because the two take different paths through the
diffing back buffer.

## Cause

`IsCombiningMark` is a hand-written list of codepoint ranges. Its own doc
comment claims to include "Other combining marks (Mn, Mc, Me categories)", but
no category test is performed — the function ends in `return false`.

Measured against Go's Unicode tables, the list recognises **413 of 1758**
non-spacing marks (Mn + Me): it misses **1345, about 76%**. Among the scripts
entirely absent:

| script | marks missed | script | marks missed |
|---|---|---|---|
| SignWriting | 127 | Myanmar | 30 |
| Tai Tham | 29 | Balinese | 21 |
| Brahmi | 20 | Gujarati | 19 |
| Grantha | 16 | Bhaiksuki | 14 |
| Kharoshthi | 13 | Malayalam | 11 |
| Limbu / Khudawadi / Kaithi | 9 each | Kannada / Khojki | 8 each |
| Mongolian / Pahawh Hmong / Nandinagari / Nyiakeng Puachue Hmong | 7 each | Sinhala / Meetei Mayek | 6 each |

NKo (U+07EB–U+07F3) and Tibetan (U+0F71–…) are missing too, and were how this
surfaced in practice.

Callers use the predicate to decide whether a rune rides the previous cell's
`Combining` string or claims a cell of its own, so a false negative directly
becomes a column of drift.

## Fix

Keep every existing range — several are deliberately **not** Mn/Me and must
stay explicit:

- Hangul Jamo fillers `U+1160–U+11FF` are category **Lo**
- ZWJ / ZWNJ `U+200C`, `U+200D` and variation selectors `U+FE00–U+FE0F` are
  category **Cf**

…and add a category test in place of the final `return false`:

```go
return unicode.In(r, unicode.Mn, unicode.Me)
```

`Mc` is deliberately **excluded**: those are *spacing* combining marks and do
occupy a cell, so treating them as zero-width would trade one drift for
another. (Note the current explicit ranges do already claim a few Mc
codepoints — e.g. `U+0903` via the `0x0901–0x0903` range. The patch leaves
that pre-existing behaviour untouched rather than changing membership callers
may depend on.)

The change is purely additive: no rune that returned `true` before returns
`false` after.

## Verification

Applied to v0.2.28, the root package builds and:

- no Mn/Me codepoint in `U+0000–U+2FFFF` is missed (was 1345)
- `U+07ED`, `U+0F71`, `U+102E`, `U+1B34`, `U+1885`, `U+09BC` are recognised
- ordinary runes are unaffected: `a`, space, `9`, `U+65E5` (CJK), `U+25CC`
  (dotted circle), `U+05D0` (Hebrew alef), `U+0F40` (Tibetan KA) all still
  return `false`

## Local workaround in the meantime

`kittytk/backend/tui/tui.go`'s `cellRuneWidth` ORs the same category test
alongside `purfecterm.IsCombiningMark`, so KittyTK is correct against the
current release. That fallback can be dropped once this lands upstream.
