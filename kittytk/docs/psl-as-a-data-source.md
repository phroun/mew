# A PSL list as a data source

> **Status: built, and flat.** One PSL list, read as records, with windows drawn
> out of it. Nothing here follows an include, applies an amendment or resolves a
> hash — layering and shadowing are the next thing and are built on this rather
> than into it. `data-sources-and-bundles.md` is the conversation those belong
> to, `hosting-a-query.md` is the wire spelling, and `sort-and-filter.md` is the
> comparison both ends stand on.

A display sometimes holds the records itself, and then there is nobody to ask.
It is the same question either way — this filter, this sort, this window — so
`source.Source` is one interface with an application behind it or a body of
static data sitting here.

```go
type Source interface{ Open(spec *wire.Spec) (View, error) }

type View interface {
    Fill(f *wire.Fill, out Sink) (Complete, error)
    Close()
}

type Sink interface{ Record(key *wire.Value, fields wire.Fields) error }
```

The shapes are the wire's own. A `Spec` and a `Fill` arrive exactly as
`wire/query.go` takes them off a statement, and `Complete` is the three things
`result <id> complete` can carry — `ordered`, a watermark, `exhausted`. So a
source backed by an application is a relay rather than a translation.

A view does not change. A different sort or a different filter is a different
view, opened alongside the one it replaces and closed after it — which is what
keeps the source in use while the reader moves across.

## Two spaces, one sequence

A PSL list holds two independent collections: an ordered sequence of items, and
a keyed map outside that sequence entirely. **Both are records.** An item's key
is its index and a keyed member's key is its name, and because an integer ranks
below a string in the comparison core, the two fall into one total order with no
rule of their own — the items first, in their order, then the names.

```
(
  ("README.md", size: 2048),
  ("build.sh", size: 310),
  notes: "a bare string, with no members at all",
  _bundle: (key: "figaro", author: "Jeffrey R. Day")
)
```

Four records, keyed `0`, `1`, `"notes"` and `"_bundle"`. `_bundle` is a record
like any other here: it is a key in a PSL list, and only the layer above this
one knows to look at it.

## Naming what is in a record

Records are not all the same shape. Some are lists of named fields, some carry a
leading label, and some are a bare string with no members at all. Two readings
name their contents, and the source is told which when it is made.

**`Whole`** exposes the record entire.

| | |
|---|---|
| `key` | its key: the index it stands at, or the name it is filed under |
| `value` | its value entire, whatever kind that is |
| `.size` | the member called size |
| `.0` | the item at position 0 |

Nothing can shadow anything. A member called `key` — which the bundle format's
own `_bundle: (key: "figaro")` has — is `.key`, and the record's key is `key`.
Within the dot, digits mean a position and anything else means a name, which is
the rule the bundle addressing grammar already reads its last segment by.

**`Members`** exposes the members alone, under their own names: `size`, not
`.size`. It is the shorter reading and it suits a source whose records are all
lists of named fields, which is most of them. It is deliberately not complete:
the record's key and its value cannot be named, positions cannot be named at
all, and a member called `key` is neither reachable nor sent.

**The dot is a word character now.** A field name is a statement's verb wherever
one is written — a sort level, a filter predicate, a field bag — and the wire
grammar read a leading dot as nothing at all. One predicate in each of the three
parsers; nothing was taken away, a number never having started with a dot
either.

```
sort={ key desc; .name natural; value }
filter={ eq .key "figaro" }
```

## What a record's values can be

| in the PSL | on the wire |
|---|---|
| a string | a string |
| a whole number | an integer, exact, however large |
| a fractional number | a float |
| `true`, `false`, `nil` | the words |
| a bare word | a symbol, bare or bracketed |
| a nested list | a block of its own members, which is the unordered rank |
| absent | nothing is sent, and an absent field reads as `undefined` |

A nested list's contents are written the `Whole` way whatever the reading,
because a position inside one has no other spelling.

**A bare word is a symbol.** PSL writes `kind: text` and `kind: "text"`
differently and so does the wire, so the two reach a filter as the two
different questions they were written as: `eq .kind text` asks about the
identifier and `eq .kind "text"` asks about the four characters.

A bare token in the wire is a number or a symbol, and which it is depends on
what the token says rather than on the character it starts with — so
`2026-09-13` is a date, `kebab-case` is a name, `1x` is an identifier, and
`1e+21` is a number. The numeric form is written out rather than handed to each
language's own number parser:

```
[+-]? digits ( "." digits )? ( [eE] [+-]? digits )?
```

A token with a leading sign is a number and nothing else, so one that does not
measure up is refused rather than quietly becoming a symbol.

A symbol with no bare spelling is **bracketed**, not turned into something
else, so every symbol crosses as a symbol:

```
{ .addr (objectLibrary/figaro/3); .dashed kebab-case; .starred (*star) }
```

Parentheses because that is what they already mean. PawScript evaluates a block
written in braces and preserves what is written in parentheses — literal
content, held unparsed — and the wire's block is braces too, so both languages
say the same thing with the same brackets. There are no escapes inside and none
are needed: a symbol cannot hold a closing parenthesis in PawScript either.
`wire.IsWord` decides which spelling is written, and a bundle address stays an
address rather than becoming a string.

`undefined` says the same thing in both languages, so it crosses as the word —
which makes `thumbnail: undefined` in the document and a record with no
thumbnail at all the same answer to `eq .thumbnail undefined`, as they should
be.

## Records that are not uniform

Most of what a mixed source needs is already in the comparison core, and needs
nothing added:

- **A field one record has and another has not** is `undefined`, which is a
  value with a rank rather than an error. `eq .thumbnail undefined` is the
  presence test, which is why there is no presence operator.
- **A field that holds a different type per record** is grouped by rank —
  `undefined < nil < false < true < number < symbol < string < bytes <
  unordered` — and `lt` and `gt` still answer.
- **A text predicate against a value that is not text** is `false`, not an
  error.

One consequence is worth knowing before it surprises somebody: **`undefined`
sits below every number**, so `filter={ lt .size 1000 }` holds every record that
has no size at all. A filter that means *has a size, and it is under a thousand*
is two predicates and says both.

## Drawing a window

Stating the sequence runs the filter over every record and sorts what survives,
once. A window is then a binary search for the boundary and a walk forward as
far as the window is long, so reaching the last screenful of a long list costs
what reaching the first one costs.

The sort tuples are kept beside the rows rather than recomputed, because
extracting a field is a map lookup and a conversion, and a sort that did it per
comparison would read the data `n log n` times instead of once. Orderings are
cached on the spec that names them, so two views of one sequence share the work
and going back to a column somebody clicked before is free.

A window emits every record in `(from..to]` and then carries on past `to` only
while it is still short of `need` — which is what the far end has to merge
against. What ends it is a watermark, or `exhausted` where the sequence ran out.

## What it costs

Measured on 100,000 records, each a list of three named members, over 200
iterations (`0_psl_bench_test.go`):

| | |
|---|---|
| reading the file | **2.5 s** — `pawscript.ParsePSL`, once |
| stating the sequence | **58 ms** — one filter pass and one sort, once per spec |
| a window of 30 at the start | **14.0 µs** |
| a window of 30 at the end | **21.8 µs** |

The last two are the pair that matters: **a window at the far end of a hundred
thousand records costs about half again what one at the near end costs** — not
three thousand times more, which is what walking to the boundary would have
cost. What separates them is the seventeen comparisons the search makes, each
of them a natural collation over a string, and they are the whole of the
difference between the two ends of the sequence.

The parse dominates everything else by a factor of forty, and none of it is
here — it is PawScript reading the text. A source large enough for that to
matter is a source worth holding parsed, which is what a frozen bundle with a
hash on it is for.

## Running one

The same query file that goes to an application through the relay can be
answered here instead, with nothing dialled and nobody asked:

```
kittytk-queryrun -psl objects.psl query.txt
kittytk-queryrun -psl objects.psl -reading members query.txt
```

```
q=new query source="objects" filter={ not { starts .0 "." } } sort={ .0 natural } have=0 need=5
query q from={ .0 "src/file10.go"; key 6 } have=0 need=5
r=new query source="objects" sort={ .size desc } have=0 need=5
destroy q
```

The file holds `new`, `query` and `destroy`, which are the statements a display
would have sent — and a different sequence is another `new query`, opened
before the one it replaces is let go. `-raw` prints them as they would have
crossed rather than as a table.

## What is not here

Layers, shadowing, the merge, nested bundles, `_amendments`, `_hash`, includes
and version expressions. A source that combines a large static bundle with a
small run-time delta has to be careful about which ranges it asks for, how many,
and when — and has to hold the shadow across the lag while it waits. None of
that is built, and all of it is built on a flat reading rather than instead of
one.
