# Where a display's records come from

> **Status: built.** A display reads records out of a document here, out of an
> application across the wire, or out of both at once, and does not know which
> it got.
>
> **The source model itself is not here. It is serval's**, and that library's
> `docs/sources.md` is what a source is, what a format loads into one, and how
> several of them compose; `docs/ordering.md` is the comparison and membership
> rules both ends of a wire compute the same way. This document is the other
> half: what KittyTK adds to them, and how a record crosses.

A display sometimes holds the records itself, and then there is nobody to ask.
It is the same question either way — this filter, this sort, this scope — so
`serval.Source` is one interface with an application behind it or a body of
static data sitting here:

| | |
|---|---|
| `serval.ListSource` | records here, whatever format loaded them |
| `serval.AmendedSource` | any other kind, with replacements and deletions over it |
| `serval.ComposedSource` | several other kinds at once, under names of their own |
| `serval.CachedSource` | any other kind, answered out of what the process holds |
| `source.ApplicationSource` | **KittyTK's own**: records an application's, asked for with `query` and answered with `result` |

Only the last is here. It is the fifth implementation serval's model leaves
room for, and it holds no records: it turns an `Open` into a `query` statement,
a `Read` into a scope on the wire, and every `result` that comes back into
records on a sink.

## A relay, not a translation

**The shapes are the wire's own.** A `serval.Spec` and a `serval.Scope` arrive
exactly as `wire/query.go` takes them off a statement, and `serval.Complete` is
the three things `result <id> complete` can carry — `ordered`, a watermark,
`exhausted`. So `ApplicationSource` passes a question down and answers back up
without a structure of its own in between.

It makes no claims either. A source here says whether it sent the record entire
or only the fields that were asked for; `ApplicationSource` says whatever the
application said, and relays the `map=` and `len=` counts with it. See
`hosting-a-query.md` for the statements and `live-data-negotiation.md` for what
travels beside them.

**Nothing waits.** A scope is asked for and returns; the records reach the sink
as they are produced — at once for records that are here, and as they arrive for
records that are an application's. The error is for a request that could not be
started, never for one that has not finished.

## How a record crosses

`hosting-a-query.md` is the grammar. What matters where a PSL document is the
thing being read is that every kind survives the crossing:

| in the PSL | on the wire |
|---|---|
| a string | a string |
| a whole number | an integer, exact, however large |
| a fractional number | a float |
| `true`, `false`, `nil` | the words |
| a bare word | a symbol, bare or bracketed |
| a nested list | a block of its own members, which is the unordered rank |
| absent | a name with nothing under it, which is a guarantee rather than a silence |

**A bare word is a symbol.** PSL writes `kind: text` and `kind: "text"`
differently and so does the wire, so the two reach a filter as the two different
questions they were written as: `eq .kind text` asks about the identifier and
`eq .kind "text"` asks about the four characters.

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

**A field name may begin with a dot**, which is how a name says it is a member
of the record rather than a word in its own right. The wire grammar read a
leading dot as nothing at all, so it is a word character now — one predicate in
each of the three parsers, and nothing was taken away, a number never having
started with a dot either. `sort-and-filter.md` is the spelling and serval's
`docs/sources.md` is the reading it names.

```
sort={ key desc; .name natural; value }
filter={ eq .key "figaro" }
```

## Running one

The same query file that goes to an application through the relay can be
answered here instead, with nothing dialled and nobody asked:

```
kittytk-queryrun -psl objects.psl query.txt
kittytk-queryrun -psl objects.psl -reading members query.txt
```

```
q=new query source="objects" filter={ not { starts .0 "." } } sort={ .0 natural } count=5
n=new query source="objects" filter={ not { starts .0 "." } } sort={ .0 natural } after=6 count=5
r=new query source="objects" sort={ .size desc } count=5
destroy q
```

The file holds `new` and `destroy`, which are the statements a display would
have sent. A query is asked once and answered once, so the second scope is a
query of its own naming the record to carry on past — and a different sequence
is another `new query` again, opened before the one it replaces is let go.
`-raw` prints them as they would have crossed rather than as a table.

## Naming one from the wire language

A trinket written down in the KittyTK Wire Language says what it reads by NAME,
because a statement cannot carry a Go value and the thing it names may not exist
yet when the statement is read.

```
new listview source="source:mail.incoming"
new listview source="bundle:objectLibrary@2.1.0" display=".caption" value=".id"
```

The two namespaces are the two a bundle's own includes use, so the language says
this one way rather than two: `source:` is a live source somebody registered,
`bundle:` — or the bare form — is a bundle to be found and assembled, and a
version may ride after an `@`.

`trinkets.RegisterSource` files a live one. Bundles resolve through a loader the
**display** installs (`trinkets.SetBundleLoader`), because assembling one means
reaching a store and a store is the display's; the trinket package holds the
names and not the knowledge. With no loader installed a `bundle:` name says so
plainly, rather than failing as though the bundle were missing.

**`display` and `value` are one field until somebody says otherwise.** A plain
list of strings has nothing to tell them apart. A list over a delimited file
has: the column a reader sees and the column the program hands back need not be
the same one, and `ValueAt` is what asks for the second.

**A bundle's fields wear a dot**, and this is where it bites. A bundle is read
under serval's `Whole`, so a record that is a list names its members `.caption`,
`.id`, `.0` — the dot being how a name says it is a member of the record rather
than a word in its own right. The bare `display` and `value` a list assumes are
the names a *made* source writes; a source of somebody else's making has its
own, and naming them is what `display=` and `value=` are for.

A bundle record written `("some text")` is a list with one positional member, so
what it shows is `.0` and not `value` — `value` being the whole of a record that
is *not* a list. That catches people, this author included.

### A tree says the same word

```
new treeview source="source:hosts.tree" showheader
```

**The nesting is the SOURCE's**, so the statement does not describe it. A
`serval.TreeSource` answers its visible rows flat and in pre-order with a depth,
a kind and a child count on each, and the view rebuilds the parentage from the
depth — which is why `Level()`, `IsLeaf()` and `Expanded` go on being asked the
same way over rows that were never written down. A source that is *not* a
hierarchy is read as one flat level, which is a tree of one generation and is
ordinary rather than an error.

A tree has columns where a list has one field, and which of a kind's fields fills
which column is `SetKindMap` — Go, for now. The wire language can say where the
rows come from and not yet what each kind puts where, so a tree named from a
statement gets the **identity** mapping: a column called `size` is the field
`size`, and the caption is `value`. `treemap.go` is the rungs above that.

## The layer above

A PSL document read here is flat: `_bundle` is a member like any other, and
serval resolves no name and no hash. What reads such a member and assembles the
composition and the amendment it declares is the **bundle loader**, which is
KittyTK's and is in `display`. `bundle-format.md` is what a bundle looks like
and what it becomes; `bundles.md` is the reasoning underneath.
