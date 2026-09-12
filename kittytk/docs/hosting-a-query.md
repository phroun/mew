# Hosting a query

> **Status: built in the clients, not yet in the display.** The statements
> below are what the three client libraries speak today, and
> `testdata/query.wire` is the corpus all three answer. The display side that
> asks these questions is still to come; `live-data-negotiation.md` is the plan
> it will be built to, and `sort-and-filter.md` the comparison both ends stand
> on.

A **query** is an application's side of a data source: one filter and one sort
over its own records, which a display fills windows out of as somebody scrolls.
The application makes it, holds it, and answers what is asked of it.

It is the first thing an application hosts, so it is the first traffic that
runs the other way — see `app-hosted-objects.md` for why the verbs go both
ways and what stays the same when they do.

## What an author writes

One function. The statement is taken apart before it arrives, so nothing in it
parses anything, and the answer is written into a sink that goes out in batches
as it fills.

```go
q, err := conn.HostQuery(&wire.Spec{
    Source: "files",
    Sort:   []wire.SortLevel{{Field: "name", Level: wire.Level{Collation: wire.CollateNatural}}},
}, func(f *client.Fill) {
    f.Ordered()
    for _, rec := range myRecords(f.From, f.To, f.Need-f.Have) {
        f.Record(rec.ID, wire.Named("name", rec.Name), wire.Named("size", rec.Size))
    }
    f.Done(watermark)
})
```

Python is the same shape (`conn.host_query(spec, fill)`, `f.record(key,
name=...)`), and C is the same shape with the spec written out as text
(`kt_host_query(c, "source=\"files\" sort={ name natural }", fill, NULL)`),
because C has no comfortable way to build a nested tree as a literal.

**The least an implementation can do is real.** Ignore every hint, send every
record, say `Exhausted`. The display then holds the whole layer and asks
nothing again until something invalidates it — below whatever size the author
is comfortable shipping, that is the *fastest* implementation, not a toy one.

## The statements

**The application announces the query.** It says `new`, as it does for
everything else it makes, and the display surfaces an id for it — there is one
allocator of ids and it is the display, so nothing has to be partitioned and
neither end ever wonders whose number it is holding.

```
q=new query source="files" fields={ name; size } filter={ ge size 1024 } sort={ name natural; size desc }
```

**The display asks for a window.**

```
ask <q> fill tag=4 from={ name "README.md"; key 17 } to={ name "build.sh"; key 42 } have=30 need=50
```

| | |
|---|---|
| `tag` | what the answer is stamped with, so a late one is still placeable. The client library copies it back; an application never sees it |
| `from` `to` | boundaries. Absent or empty is the start of the sequence |
| `have` | how much of the window the display can fill from what it already holds |
| `need` | how many rows the window is |
| `fields` | the fields wanted for *this* window, where they are fewer than the query's — the skeleton of a wide stretch rather than everything |

The whole of what the application does with that: emit every record of its own
in `(from..to]`, and if that does not make up the shortfall, keep going past
`to` until it does.

**The application answers**, in as many messages as it likes:

```
event query_record query=<q> tag=4 fields={ key 17; name "src/parser.go"; size 1024 }
event query_record query=<q> tag=4 fields={ key 42; name "src/window.go"; size 2048 }
event query_filled query=<q> tag=4 ordered watermark={ name "src/window.go"; key 42 }
```

One terminator ends it, and there are three:

| | |
|---|---|
| `watermark={…}` | there is nothing of mine between where you asked from and this point that you do not now have |
| `exhausted` | everything there is. No watermark, because there is nothing past the end to be complete up to |
| `error="…"` | a refusal, which is an answer: the display carries on with what it has |

`ordered` says the records are in the query's own order. It is the one hint
that cannot be left unsaid and assumed, because it changes what the display
does with what arrived — ordered, it merges; unordered, it sorts first. Saying
nothing means unordered, which is always safe.

**The display restates the sequence** when the user re-sorts or re-filters.
That is a property change on the query that exists, not a new query:

```
set <q> sort={ size desc; name fold } filter={ ge size 1024 }
```

Everything the application cached against the old spec that was keyed by
*position* is stale; what was keyed by *record identity* is not. A query that
ignores the restatement is still correct, because the next fill carries the new
spec with it.

**And the display lets it go** with `destroy <q>`.

## A field bag

One shape does three jobs, and it is a block of one statement per field: the
name first, and its value, if it has one, after.

```
{ name "src/parser.go"; size 1024; key 17 }     a record's fields
{ name "build.sh"; key 42 }                     a boundary: where it stands
{ name; size }                                  a bare list of fields
```

`key` is reserved: the record key, which is the sort's implicit final level and
what makes a position mean exactly one record. Every record carries it and so
does every boundary.

A boundary is named rather than positional, so neither end has to agree on the
order the sort levels were written in to read one. A field's value may be any
kind the wire has — a word, a string, a number, and `undefined` for a field a
record does not have.

## The filter and the sort

Both are as `sort-and-filter.md` defines them, and both arrive as a structure
rather than as text. A filter block is an AND, so the top of a parsed filter
tree always is — one shape to walk, whether it held one predicate or twenty.

A set may be written either way, and both read to the same structure:

```
in kind { folder; disk }      a block holds bare names
in name "a" "b" 3             operands hold any value
```

## Where a larger library plugs in

The client libraries understand `fill`, the restatement and the drop. Anything
else the display addresses to a query reaches the application whole:

```go
q.OnStatement(func(stmt *wire.Statement) { … })
```

Coverage and invalidation — the rest of `live-data-negotiation.md` — travel
this way, and neither is implemented in the thin libraries. A fuller one adds
them without the thin one having to grow, which is the point of the seam.

## Two things about numbers

**An integer keeps its digits.** The wire reads a whole number as an integer
and holds it exactly, so an id or a nanosecond timestamp past 2^53 compares as
itself rather than as the nearest float.

**A whole float stays a float.** `3` is an integer and `3.0` is not; a value
that changed type between the two ends would be comparing different things, so
a float is never written in a spelling that would come back an integer.
