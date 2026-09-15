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

## Invalidation is not being built yet

Every held record is treated as an answer about an **unchanging** body of data.
Nothing decides that a held value is stale, and nothing should start to: the
cache carries a `gen` per record for it, and that is all the groundwork there
is. Reasoning that reaches for "but what if the data changed" is out of scope
until invalidation is a thing we are doing on purpose.

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
