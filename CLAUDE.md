# Working in this repository

Notes that are true across sessions, so they need not be rediscovered or
re-corrected. `CONTRIBUTING.md` is the human-facing version of the vocabulary
section; the rest is orientation.

## What lives where

- **`.`** — the mew editor library.
- **`app/`** — the shippable applications.
- **`kittytk/`** — a vendored fork of the KittyTK UI toolkit. It holds the
  **KittyTK Wire Language** (`wire/`), the three client libraries (Go, Python,
  C) and `ApplicationSource`.
- **`github.com/phroun/serval`** — a **separate repository**, not vendored. It
  is the data layer: sources, sequences, scopes, ordering, matching and the
  caches. `kittytk/go.mod` pins it. Changing serval means pushing there and
  bumping the pin here; a pseudo-version works, no tag required.

A `go.work` ties the co-located modules together, so most `go` commands inside
`kittytk/` want `GOWORK=off` to see that module on its own.

## Vocabulary

House words. Reaching for the ordinary synonym puts distance between what the
code says and what we say.

- **Prior and Next**, never *Previous* or `prev`. The exception is a genuinely
  temporal sense — *the previous frame* — where *previous* is right.
- **PSL is not PawScript.** PSL is PawScript Serialized Lists, a data format,
  and is no more PawScript than JSON is JavaScript.
- **The KittyTK Wire Language** is the language; `kittytk/wire/` is the package
  that reads and writes it. Name which you mean when both are in play.

## Invalidation is told, never decided

Nothing here works out on its own that a held record has gone stale — there is
no poll, no expiry and no generation compared. A **source says so**, through
`CachedSource.Stale`, and serval's `invalidate.go` is what saying so costs. A
cache nobody tells holds what it holds.

So "but what if the data changed" is answered by asking what notice the source
would send and what that notice costs, and not by adding a check anywhere. The
reasons are `Added`, `Removed`, `Replaced` and `Altered`; `Removed` and
`Replaced` are deliberately different prices, and `docs/live-data-negotiation.md`
in `kittytk/` is the long version.

The wire half — handles, coverage statements travelling between a display and an
application — is still a plan. And we are not at the display at all yet: this is
a data engine.

## Testing

- **`go test -count=1`.** Cached results hide a test that only passes because
  it ran before.
- **Mutation-sweep new logic.** Change a condition, drop a line, invert a
  comparison, and check a test dies. A surviving mutant is either a test gap or
  an equivalent mutant — say which, rather than leaving it unexplained.
- **A wire grammar change is four things**, not one: Go, C, Python, and
  `kittytk/testdata/query.wire`, the corpus all three answer. The corpus covers
  specs and scopes; result statements are covered by the `wire/` tests and the
  three conformance harnesses.
- Go, C and Python conformance runs are driven from Go: `go test ./c/...` and
  `go test ./python/...` inside `kittytk/`, plus
  `python3 -m unittest discover -s tests` in `kittytk/python/`.
