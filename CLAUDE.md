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

- **Prior and Next**, never `prev`. *Prior* is a position in an ordering (the
  line above, the tab to the left) — two things that exist at once. *Previous*
  is the same thing at an earlier moment (the previous frame's cache, the
  window active before the menu bar) and is right there. *Preceding* and
  *Following* are Unicode's words for text — cluster bases, joining runs, a
  mark riding the cell in front — and stay Unicode's. The test: could both be
  pointed at simultaneously? Then prior. Is one of them gone? Then previous.
- **A DataSetDescriptor describes a data set** — which one we are talking
  about. `Open` takes one and hands back the DataSet it names, and `dataSetKey`
  derives that set's identity from it. Never abbreviated to `Desc`: `desc` is
  already a sort keyword on the wire (`sort={ name desc }`), so the short form
  would read as *descending* in the one domain where that is a real word.
  `descriptor` is the parameter everywhere.
- **PSL is not PawScript.** PSL is PawScript Serialized Lists, a data format,
  and is no more PawScript than JSON is JavaScript.
- **The KittyTK Wire Language** is the language; `kittytk/wire/` is the package
  that reads and writes it. Name which you mean when both are in play.
- **A View reads a DataSet; it does not hold one.** ListView and TreeView are
  views: they draw POSITIONS, the DataSet answers in identities, and the view
  owns neither the records nor the order. It is why the two are the same shape —
  a tree view is a list view that takes its indent from one field and its twisty
  from another.
- **A placeholder record** is a record a view knows is THERE and knows nothing
  else about yet. Not an error and not a failure to draw: it is drawn in its
  place, with its banding and its selection bar and no values, and it is what
  lets a thumb drag stay smooth while the records catch up. The code spells it
  `placeholderRow` and `placeholderTreeRow`; `blank` elsewhere means an empty
  cell, an unused width or a bare caption, and is not this.
- **An Extent is a stretch of a DataSet that somebody HOLDS**, and never a
  window — a window is a KittyTK UI object, and the display's word has no
  business in the data layer. `serval/coverage.go` owns the type: named by its
  ends, both INCLUSIVE, both records actually held. It is not a **Scope**: a
  scope is the ASK — where to start, which way to walk, how many, where the
  asker's own knowledge picks up again — and its ends are exclusive because they
  say where to start and stop walking. A scope asks; what is held afterwards is
  an extent. A view's spine holds extents; so does a flattening; so does a cache,
  which is what `Covers` hands out and what a staleness notice is stated against.
- **A Trouble is one thing that was wrong, carried as a VALUE.** Nothing is
  thrown. It travels the path the answer would have taken — `Complete.Error`, an
  `Open` refusal, the `trouble` statement — and is held where the answer would
  have been held. The word means one thing at three layers: `wire.Trouble` is the
  statement, `display.Trouble` a bundle load's complaint, `trinkets.Trouble` what
  a view holds and draws.

## Concepts the code names

Longer than a word each, and each has a file that owns the full account. Here so
that the next reader meets them by name rather than deducing them.

- **The spine** (`objects/trinkets/spine.go`) is what a view knows about where
  its records are, standing between the positions it draws and the identities a
  DataSet answers in. It is NOT a copy: it holds a few extents — stretches
  somebody was actually told about, each anchored by the position of its first
  record — plus how long the DataSet is, capped at `spineKept`. Anything further from the
  reader is dropped, a record nobody is looking at being one that can be asked
  for again. Each held record carries a depth, dead weight for a list and the two
  answers a tree needs: how far to indent, and where a subtree ENDS.
- **`reckon`** (`serval/reckon.go`) is how long a flattening is, worked out
  rather than walked: the top level counts itself, and every open node adds its
  children out of the census — one question per node TYPE, not per row. It
  refuses, and floors instead, for an expand-all, for more than one kind of row,
  for a kind chosen per row, for `SaysChildren`, and for a loop. The floor is
  still *at least the top level*, which is what stops a thumb lurching.
- **`walks`** (`objects/trinkets/listsource.go`) is a view's note that this
  DataSet will not jump to a position. Told, never assumed: it asked to begin at
  one and the answer said it began somewhere else. The view then carries on from
  the nearest record it holds BELOW the place it wants, covering the gap in one
  ask, rather than asking the same unanswerable question for ever. It only ever
  becomes true, and a source that honours `from` is never made to walk.
- **`first=`** is an answer saying where it BEGAN, and it is what makes
  `Scope.From` safe to be best effort. Everything downstream reads that one
  number: a view's `walks`, and a tree's descent deciding whether the rows it
  was handed are the ones it skipped to.

## Expansion is not invalidation

Opening a node makes no record anywhere untrue. Every level of a tree is a data
set of its own, opened with its own descriptor and cached on its own, so opening
changes only which levels the walk visits — and therefore what POSITION each row
below the mark stands at. Nothing above the mark moves at all.

So a view told the delta shifts by it and keeps what it holds (`spine.grew` and
`shrank`), and one that is not told forgets from the mark DOWN, which costs a
re-ask and not a re-fetch: the levels' own caches still hold every record either
way.

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
- **A wire grammar change is four things**, not one: Go, C, Python, and the
  corpus all three answer. Three corpora now: `query.wire` covers specs and
  scopes, `answer.wire` what an `ask` is answered with, and `stale.wire` what a
  source says has stopped being true. Result statements are covered by the
  `wire/` tests and the three conformance harnesses instead.
- Go, C and Python conformance runs are driven from Go: `go test ./c/...` and
  `go test ./python/...` inside `kittytk/`, plus
  `python3 -m unittest discover -s tests` in `kittytk/python/`.
