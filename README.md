# mew - mew edits words



# History

This began life as a TypeScript project in another repo, my first attempt to
experiment with AI coding assistance:

commit d63f17caac57e3f27c248f56d7b25cab6020423a
Author: Jeff R. Day <phroun@gmail.com>
Date:   Sun May 18 02:42:41 2025 -0700

    Initial Commit

Eventually, the desire to handle large files led to the development of
Garland, and the desire for a Macro Language led to the development of PawScript.

PawScript eventually led to the development of KittyTK, and KittyTK needed a multi-line
editor Trinket, so now we are back.

# License

mew is **source-available** (not open source, for now): the source is published
to read, build, and run, and you may redistribute verbatim copies and link the
**unmodified** editor into your own programs — including commercial ones — but
you may not modify or fork it. See [`LICENSE`](LICENSE) for the exact terms, and
[`CONTRIBUTING.md`](CONTRIBUTING.md) — bug reports and bug-fix pull requests are
welcome. The license is provisional; a move to MIT- or FSF-style terms is on the
table as the project matures.

Two carve-outs:

- The syntax grammars under [`internal/editor/syntax/`](internal/editor/syntax/)
  are separately **MIT licensed** (original works, not derived from JOE's GPL
  grammars) — reuse them freely under MIT.
- Bundled third-party dependencies (all MIT / BSD-3-Clause) keep their own
  licenses; see [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).

