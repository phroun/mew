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

## The syntax grammars are different

The grammar files under [`internal/editor/syntax/`](internal/editor/syntax/) are
separately **MIT licensed** (see the `LICENSE` in that directory). Contributions
to those `.jsf` files are made under the MIT License, and you're free to reuse
them under MIT independently of mew.
