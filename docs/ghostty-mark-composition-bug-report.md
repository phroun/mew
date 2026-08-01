# Ghostty: RTL combining marks mispositioned; presentation forms decomposed under a trailing mark

## Summary

Ghostty mispositions certain right-to-left combining marks, shifting them one
cell to the right (onto the previous letter). It also decomposes a precomposed
Hebrew presentation form when a combining mark follows it, which re-exposes the
same misplacement even for text that was pre-composed to avoid it.

Observed in Ghostty with `TERM=xterm-ghostty`, reproducible with `printf` alone
(no application involved).

## Repro

Run in a Ghostty shell:

```
printf 'bare:        שׁ  שׂ  תּ\n'   # base + combining mark
printf 'precomposed: שׁ        שׂ        תּ\n'          # single presentation-form glyphs
printf 'form+mark:   שְׁ\n'                               # presentation form + a trailing vowel
```

Observed:

- **bare** — the combining mark is shifted one cell to the right (e.g. the
  shin-dot `U+05C1` lands on the letter before the shin). At least `U+05C1`
  is affected; `U+05C2` renders in place, so it is mark-specific.
- **precomposed** — all three render correctly (the dot/dagesh is in place).
- **form+mark** — the dot drifts again: adding the trailing vowel makes Ghostty
  decompose `U+FB2A` back to `U+05E9 U+05C1`, so the bare-cluster bug returns.

Expected: the mark sits on its own base in every case; a presentation form is
not decomposed by a following combining mark.

## Impact

Vocalized Hebrew (a pointed consonant that also carries a vowel) cannot be
rendered correctly by any single-cell byte sequence:

- `shin + shin-dot + vowel` → the dot drifts.
- `FB2A (shin-with-dot) + vowel` → decomposed, then the dot drifts.

The only workaround is to place the vowel in a separate cell so no cell holds a
form plus a trailing mark — which shifts the burden onto the application.

## Notes

- `U+05C1` shin-dot drifts; `U+05C2` sin-dot does not — so it is per-mark, not a
  blanket RTL issue.
- Likely two related defects: (1) RTL combining-mark placement offset by one
  cell, and (2) decomposition of Arabic/Hebrew presentation forms when a
  combining mark follows.
