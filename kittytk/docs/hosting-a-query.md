# Serving a query

> **Status: built in the clients, not yet in the display.** The statements
> below are what the three client libraries speak today, and
> `testdata/query.wire` is the corpus all three answer. The display side that
> asks these questions is still to come; `live-data-negotiation.md` is the plan
> it will be built to, and `sort-and-filter.md` the comparison both ends stand
> on.

A **query** is a sequence of an application's own records that a display is
reading: one filter, one sort, and a window asked for at a time.

**The display opens it.** Only the display knows a query is wanted and what it
is — the sort comes from the column header somebody clicked, the filter from
the filter box, the window from the scroll position. The application is the end
that holds the records, so it serves it.

## Three pairs, and nothing carries two of them

| the request | what answers it | what gates it |
|---|---|---|
| `query` | `result` | the request |
| `ask` | `answer` | the request |
| `sub`, or an object's mere existence | `event` | the subscription |

That is what keeps a request for records off the event line, so nobody is
tempted to hand-roll what the library already does for them.

## Running one

No display opens queries yet, so there is a stand-in that does — it listens,
does the handshake, opens one query against a named source, and prints the
statements as they cross:

```
go run ./cmd/kittytk-queryprobe &
KITTYTK_DISPLAY=/tmp/kittytk-queryprobe.sock go run ./examples/queryapp
```

`examples/queryapp` serves two sources, at the two ends of how much work an
author wants to do: `colours` sends everything and says `exhausted`, and
`files` honours the boundary, the sort and the window and answers with a
watermark. The probe's flags drive the rest — `-filter`, `-sort`, `-resort`,
`-more` — and what it prints is this:

```
<- hello version=1 app="queryapp"
-> welcome version=1 session=1
-> init app=1 store=2 host=3
-> q=new query source="files" filter={ not { starts name "." } } sort={ name natural } have=0 need=3
<- reply q=1
<- end
<- result 1 fields={ key 2; name "build.sh"; size 310 }
<- result 1 fields={ key 3; name "go.mod"; size 96 }
<- result 1 fields={ key 1; name "README.md"; size 2048 }
<- result 1 complete ordered watermark={ name "README.md"; key 1 }
-> query 1 from={ key 1; name "README.md"; size 2048 } have=0 need=3
<- reply
<- end
<- result 1 fields={ key 6; name "src/file2.go"; size 1200 }
<- result 1 fields={ key 7; name "src/file10.go"; size 880 }
<- result 1 fields={ key 4; name "src/parser.go"; size 14022 }
<- result 1 complete ordered watermark={ name "src/parser.go"; key 4 }
-> set 1 sort={ size desc }
<- reply
<- end
-> query 1 have=0 need=3
<- reply
<- end
<- result 1 fields={ key 4; name "src/parser.go"; size 14022 }
<- result 1 fields={ key 5; name "src/window.go"; size 9310 }
<- result 1 fields={ key 8; name "testdata/query.wire"; size 6100 }
<- result 1 complete ordered watermark={ size 6100; key 8 }
-> destroy 1
<- reply
<- end
```

**A boundary is the sort fields and the key, not the key alone.** The second
window asks `from={ key 1; name "README.md"; size 2048 }`; a boundary carrying
only the key has nothing to compare against the levels, and lands at the start
of the sequence rather than where it meant to. Fields the sort does not mention
are ignored at the far end, so sending the whole record is the simplest thing
that is right.

## Putting one through a real display

The probe is a stand-in. To put a query to an application that is connected to
a **real** display, the display carries it -- it reads none of it, it hands the
statements over and sends back whatever comes:

```
KITTYTK_DEBUG_RELAY=1 kittytk-tui &
kittytk-queryrun -to queryapp query.txt
```

where `query.txt` is the wire text and nothing else:

```
q=new query source="files" filter={ not { starts name "." } } sort={ name natural } have=0 need=5
```

```
key  name             size
---  ---------------  ----
2    "build.sh"       310
3    "go.mod"         96
1    "README.md"      2048
6    "src/file2.go"   1200
7    "src/file10.go"  880

complete up to { name "src/file10.go"; key 7 }
```

`-raw` prints the statements instead of the table. Under the tool it is one
question on the display's own host object:

```
ask host relay to="queryapp" text="<the file>"
```

and every statement the application says back arrives as a `relay` event
carrying it. **The relay is shut unless the display opens it** -- an
application that can relay can address another application's objects, which is
the display's business and nobody else's. `KITTYTK_DEBUG_RELAY` opens it, or a
host with a surface of its own calls `SetRelayEnabled`.

## What an author writes

One function. The statement is taken apart before it arrives, so nothing in it
parses anything, and the answer is written into a sink that goes out in batches
as it fills.

```go
src, _ := conn.HostSource("files", func(f *client.Fill) {
    f.Ordered()
    for _, rec := range myRecords(f.From, f.To, f.Need-f.Have) {
        f.Record(rec.ID, wire.Named("name", rec.Name), wire.Named("size", rec.Size))
    }
    f.Done(watermark)
})
```

Registering a source says nothing on the wire: **a source is a name, not an
object**. The application tells whatever trinket is to show it `data="files"`,
and the display opens queries against that name.

Python is the same shape (`conn.host_source(name, fill)`, `f.record(key,
name=...)`), and so is C (`kt_host_source(c, "files", fill, NULL)`).

**The least an implementation can do is real.** Ignore every hint, send every
record, say `Exhausted`. The display then holds the whole layer and asks
nothing again until something invalidates it — below whatever size the author
is comfortable shipping, that is the *fastest* implementation, not a toy one.

## The statements

**The display opens a query and asks for its first window in one statement**,
because it never wants a sequence without wanting rows of it.

```
DISPLAY → APP   q=new query source="files" filter={ ge size 1024 } sort={ name natural }
                  have=0 need=30
                end
APP → DISPLAY   reply q=9
                end
APP → DISPLAY   result 9 fields={ key 17; name "src/parser.go"; size 1024 }
                result 9 fields={ key 42; name "src/window.go"; size 2048 }
                result 9 complete ordered watermark={ name "src/window.go"; key 42 }
```

**The application names it.** Each end mints ids in its own space and the
direction a statement travelled says whose space it is in, so nothing collides
and no range is reserved anywhere. The display has no id to offer for something
the application holds, so the reply carries the application's.

The reply goes out before any result, and that ordering is not policy: the
application mints the id, so it writes it before anything that carries it. A
display need not wait for it, though — it may address a query by the key it
opened it under, in the same batch:

```
DISPLAY → APP   q=new query source="files" have=0 need=5
                query q have=5 need=10
                end
```

**Every window after that is the same, minus the making:**

```
DISPLAY → APP   query 9 from={ name "build.sh"; key 42 } have=25 need=30
                end
APP → DISPLAY   reply
                end
APP → DISPLAY   result 9 fields={ … }
                result 9 complete ordered exhausted
```

| on a window request | |
|---|---|
| `from` `to` | boundaries. Absent or empty is the start of the sequence |
| `have` | how much of the window the display can fill from what it already holds |
| `need` | how many rows the window is |
| `fields` | the fields wanted for *this* window, where they are fewer than the query's — the skeleton of a wide stretch rather than everything |

The whole of what the application does with that: emit every record of its own
in `(from..to]`, and if that does not make up the shortfall, keep going past
`to` until it does.

**A result carries a record, or ends the window.** One `complete` ends it, and
three things can ride on it:

| | |
|---|---|
| `watermark={…}` | there is nothing of mine between where you asked from and this point that you do not now have |
| `exhausted` | everything there is. No watermark, because there is nothing past the end to be complete up to |
| `error="…"` | a refusal, which is an answer: the display carries on with what it has |

`ordered` says the records are in the query's own order. It is the one hint
that cannot be left unsaid and assumed, because it changes what the display
does with what arrived — ordered, it merges; unordered, it sorts first. Saying
nothing means unordered, which is always safe.

**Nothing is stamped**, because nothing needs to be. The application's replies
and its results travel one ordered stream, so a window's results are the ones
between the reply that accepted it and the result that completes it — and that
same boundary is what separates the generation before a re-sort from the one
after it.

**The display restates the sequence** when the user re-sorts or re-filters.
That is a property change on the query that exists, not a new query:

```
DISPLAY → APP   set 9 sort={ size desc; name fold } filter={ ge size 1024 }
                end
```

Everything the application cached against the old spec that was keyed by
*position* is stale; what was keyed by *record identity* is not. A query that
ignores the restatement is still correct, because the next window carries the
new spec with it.

**And the display lets it go** with `destroy 9`, which is how the application
learns it may drop the records it was holding. That is why a query is an object
with a lifetime rather than a standing arrangement: without an ending, nothing
ever tells the application to let go.

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

The client libraries understand the open, the window, the restatement and the
drop. Anything else the display addresses to a query reaches the application
whole:

```go
src.OnStatement(func(q *client.Query, stmt *wire.Statement) { … })
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

## What this costs a client library

Both ends now send replies, because both ends now receive batches. The reply
path in each client is a few lines, but it comes with two disciplines.

**An end waiting for a reply must keep reading**, or two ends waiting on each
other deadlock. The client libraries answer on a thread of their own for that
reason, and a display has to do the same.

**An answer is not a batch.** A request is terminated by `end` and answered; a
reply, an error and a result are bare statements, exactly like the events going
the other way. A reader that waits for an `end` after an answer waits forever,
and a reader that blocks handing on a reply nobody asked for goes deaf without
saying so -- both of which happened on the way to this working.
