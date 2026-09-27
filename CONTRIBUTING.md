# Contributing to mew

mew is **source-available**, not open source (see [`LICENSE`](LICENSE)). During
this pre-release period there is one mew — the standard mew — and forks or
modified redistributions are not permitted. That said, help making the standard
mew better is very welcome.

## What's welcome

- **Bug reports.** Clear, reproducible reports are hugely valuable.
- **Pull requests that fix bugs.** Focused fixes for defects, with a short note
  on what was broken and how the change addresses it.

For larger changes or new features, please open an issue to discuss first —
direction is easier to agree before code than after.

## Contribution terms

By submitting a contribution (a pull request, patch, or other material) for
inclusion in mew, you agree to the contribution terms in Section 4 of the
[`LICENSE`](LICENSE): you grant the copyright holder a perpetual, worldwide,
irrevocable, royalty-free license to use, modify, distribute, sublicense, and
relicense your contribution as part of mew under any terms, and you confirm you
have the right to grant that license.

This is what lets fixes be merged into the single standard mew and lets mew move
to a more permissive license (MIT- or FSF-style) later without having to track
down every contributor for permission.

## Vocabulary

Some words are house words across PawScript, mew, KittyTK and serval, and code
that reaches for the ordinary synonym puts a little more distance between what
the code says and what we say.

- **Prior and Next**, never `prev` — that abbreviation is never the right
  answer, because it hides which of two different relations is meant:
  - **Prior** is a POSITION in an ordering: the trinket before this one in the
    focus chain, the line above, the tab to the left, the sample at the index
    before. The thing it names is a different thing, and both exist at once.
  - **Previous** is the SAME thing at an earlier moment: the previous frame's
    glyph cache, the window that was active before the menu bar took focus, the
    origin a call is about to restore. Here *previous* is the right word and
    *prior* would be the wrong one.
  - **Preceding and Following** belong to Unicode text — cluster bases, joining
    runs, a mark riding the cell in front of it. That is the script's own
    vocabulary, not ours, and reaching for *prior* there would be importing our
    word into somebody else's domain.

  The test: could the two things be pointed at simultaneously? Then it is
  prior. Is one of them gone? Then it is previous.
- **PSL is not PawScript.** PSL is PawScript Serialized Lists, a data format,
  and it is no more PawScript than JSON is JavaScript. Say which you mean.
- **The KittyTK Wire Language** is the language; `kittytk/wire/` is the package
  that reads and writes it. "The wire" alone is ambiguous between them, so name
  the one you mean where both are in play.

## Naming test files

Test files are named for the source file they test, so that `ls 0_buffer*`
answers "what tests `buffer.go`?" and a source with no test beside it shows up
as a gap in a listing you were already reading. The rule, its traps, and how it
meets the `kittytk/` fork boundary are in [`TEST-NAMING.md`](TEST-NAMING.md).

## The syntax grammars are different

The grammar files under [`internal/editor/syntax/`](internal/editor/syntax/) are
separately **MIT licensed** (see the `LICENSE` in that directory). Contributions
to those `.jsf` files are made under the MIT License, and you're free to reuse
them under MIT independently of mew.
