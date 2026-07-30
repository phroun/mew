# purfecterm: `IsCombiningMark` is wrong in both directions

> **Resolved upstream in purfecterm v0.2.29.** The release makes the predicate
> agree with Unicode in both directions: every Mn/Me codepoint is recognised
> (0 missed, was 1345) and no Mc codepoint is claimed (0, was 11), while the
> non-Mn/Me riders the explicit ranges exist for — ZWJ/ZWNJ, variation
> selectors, Jamo fillers — still return `true`. KittyTK adopted it and
> deleted its compensating category test; `cellRuneWidth` asks purfecterm
> alone again. The patch below is kept for the record.

**Patch:** `0003-purfecterm-combining-marks.patch` (against v0.2.28, `cell.go`)

## Symptom

Two opposite drifts, both a column per mark.

**Missed marks.** A line containing a combining mark from almost any script outside Latin,
Hebrew, Arabic, Thai or Devanagari renders one column too wide *per mark*.
Everything after the mark on that row is shifted right, and the row's tail is
left unpainted — showing the terminal's own background rather than the app's.
The drift is per-row, so a screen of such text develops a ragged right edge
whose depth varies line by line with the number of marks.

It also renders *inconsistently*: an initial paint and a repaint after
scrolling can disagree, because the two take different paths through the
diffing back buffer.

**Over-claimed marks.** In the other direction, the Devanagari ranges span
eleven SPACING marks — the visible matras `U+0903 U+093B U+093E U+093F U+0940
U+0949 U+094A U+094B U+094C U+094E U+094F` — and report them zero-width. Those
occupy a column, so Hindi/Sanskrit text comes out a column SHORT per matra.

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

…add a category test in place of the final `return false`:

```go
return unicode.In(r, unicode.Mn, unicode.Me)
```

…and guard the whole function with an early `Mc` rejection, since the ranges
below it mix Mn and Mc freely (the Devanagari block especially):

```go
if unicode.Is(unicode.Mc, r) {
    return false
}
```

`Mc` is excluded because those are *spacing* combining marks: they occupy a
cell. The guard has to come first — putting the category test only at the end
would leave the eleven Devanagari matras claimed by the explicit ranges.

## Verification

Applied to v0.2.28, the root package builds and:

- no Mn/Me codepoint in `U+0000–U+2FFFF` is missed (was 1345)
- no Mc codepoint is claimed (was 11)
- `U+07ED`, `U+0F71`, `U+102E`, `U+1B34`, `U+1885`, `U+09BC` are recognised
- the non-Mn/Me riders the explicit ranges exist for still return `true`:
  ZWJ/ZWNJ `U+200C`/`U+200D`, variation selectors `U+FE00`/`U+FE0F`, Jamo
  fillers `U+1160`/`U+11FF`
- ordinary runes are unaffected: `a`, space, `9`, `U+65E5` (CJK), `U+25CC`
  (dotted circle), `U+05D0` (Hebrew alef), `U+0F40` (Tibetan KA) all still
  return `false`

## Local workaround (removed)

While this was outstanding, `kittytk/backend/tui/tui.go`'s `cellRuneWidth`
applied both halves of the rule itself — Mc winning over anything the list
claimed, with Mn/Me ORed in alongside it. v0.2.29 made that unnecessary and it
was removed. KittyTK's `TestNonSpacingMarksAreZeroWidth` and
`TestSpacingMarksKeepTheirCell` now sweep the whole range against the upstream
predicate, so a regression there would fail KittyTK's own suite.
