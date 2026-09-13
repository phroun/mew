# Serving a query

> **Status: built in the clients, not yet in the display.** The statements
> below are what the three client libraries speak today, and
> `testdata/query.wire` is the corpus all three answer. The display side that
> asks these questions is still to come; `live-data-negotiation.md` is the plan
> it will be built to, and `sort-and-filter.md` the comparison both ends stand
> on.

A **query** is a sequence of an application's own records that a display is
reading: one filter, one sort, and a scope asked for at a time.

**The display opens it.** Only the display knows a query is wanted and what it
is — the sort comes from the column header somebody clicked, the filter from
the filter box, the scope from the scroll position. The application is the end
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
`files` honours the boundary, the sort and the scope and answers with a
watermark. The probe's flags drive the rest — `-filter`, `-sort`, `-more`, and
`-resort`, which opens a second query on another sort and drops the first —
and what it prints is this:

```
<- hello version=1 app="queryapp"
-> welcome version=1 session=1
-> init app=1 store=2 host=3
-> q=new query source="files" filter={ not { starts name "." } } sort={ name natural } have=0 need=3
<- reply q=1
<- result 1 ordered
<- end
<- result 1 record={ key 2; name "build.sh"; size 310 }
<- result 1 record={ key 3; name "go.mod"; size 96 }
<- result 1 record={ key 1; name "README.md"; size 2048 }
<- result 1 complete watermark={ name "README.md"; key 1 }
-> query 1 from={ key 1; name "README.md"; size 2048 } have=0 need=3
<- reply
<- end
<- result 1 ordered
<- result 1 record={ key 6; name "src/file2.go"; size 1200 }
<- result 1 record={ key 7; name "src/file10.go"; size 880 }
<- result 1 record={ key 4; name "src/parser.go"; size 14022 }
<- result 1 complete watermark={ name "src/parser.go"; key 4 }
-> r=new query source="files" sort={ size desc } have=0 need=3
<- reply r=2
<- end
<- result 2 record={ key 4; name "src/parser.go"; size 14022 }
<- result 2 record={ key 5; name "src/window.go"; size 9310 }
<- result 2 record={ key 8; name "testdata/query.wire"; size 6100 }
<- result 2 complete watermark={ size 6100; key 8 }
-> destroy 1
<- reply
<- end
-> destroy 2
<- reply
<- end
```

**A boundary is the sort fields and the key, not the key alone.** The second
scope asks `from={ key 1; name "README.md"; size 2048 }`; a boundary carrying
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
src, _ := conn.ProvideSource("files", func(f *client.Fill) {
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

`Record` says these are all the fields there are. `Subset` — `f.Subset(id, …)`,
`f.subset(key, …)`, `kt_fill_subset` — says they are the ones this scope asked
for, and crosses as `fields={…}`.

Python is the same shape (`conn.provide_source(name, fill)`, `f.record(key,
name=...)`), and so is C (`kt_provide_source(c, "files", fill, NULL)`).

**The least an implementation can do is real.** Ignore every hint, send every
record, say `Exhausted`. The display then holds the whole layer and asks
nothing again until something invalidates it — below whatever size the author
is comfortable shipping, that is the *fastest* implementation, not a toy one.

## The statements

**The display opens a query and asks for its first scope in one statement**,
because it never wants a sequence without wanting rows of it.

```
DISPLAY → APP   q=new query source="files" filter={ ge size 1024 } sort={ name natural }
                  have=0 need=30
                end
APP → DISPLAY   reply q=9
                end
APP → DISPLAY   result 9 ordered
                result 9 record={ key 17; name "src/parser.go"; size 1024 }
                result 9 record={ key 42; name "src/window.go"; size 2048 }
                result 9 complete watermark={ name "src/window.go"; key 42 }
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

**Every scope after that is the same, minus the making:**

```
DISPLAY → APP   query 9 from={ name "build.sh"; key 42 } have=25 need=30
                end
APP → DISPLAY   reply
                end
APP → DISPLAY   result 9 ordered
                result 9 record={ … }
                result 9 complete exhausted
```

| on a scope request | |
|---|---|
| `from` `to` | boundaries. Absent or empty is the start of the sequence |
| `have` | how much of the scope the display can fill from what it already holds |
| `need` | how many rows the scope is |
| `fields` | the fields wanted for *this* scope, where they are fewer than the query's — the skeleton of a wide scope rather than everything |

The whole of what the application does with that: emit every record of its own
in `(from..to]`, and if that does not make up the shortfall, keep going past
`to` until it does.

## What may be sent, and what may be claimed

These are two different things, and only one of them is a promise.

**What is sent may be any superset of the answer.** An application that filters
on the one predicate it can index cheaply and ignores the rest is correct. So
is one that uses a looser comparison than the core's, or sends the whole source
every time. The display rejects what it did not ask for, so extra records cost
bandwidth and nothing else — and it may keep them, because a record it has is a
record it need not ask for later.

**What is claimed must be true.** The watermark says *there is nothing of mine
between where you asked from and this point that you do not now have*, and
`exhausted` says it of the whole sequence. Those are the only statements the
display takes on trust, because they are the only ones it cannot check.

So the single mistake is **cutting out records that match, and then claiming
the range anyway**. Cutting out records that do not match is the filtering; it
is what the filter is for. An application that drops matching records and stays
quiet about coverage has still corrupted nothing — the display simply never
gets a complete range out of it and goes on asking. It is the claim that makes
the loss a lie.

Which is why an application never has to reproduce the comparison core exactly.
Exactness buys a smaller answer, not a correct one.

**A result carries a record, or ends the scope.**

A record crosses under one of two words, and the difference is how much of the
record is there:

```
result 9 record={ key 17; name "src/parser.go"; size 1024 }
result 9 fields={ key 17; name "src/parser.go" }
```

| | |
|---|---|
| `record={…}` | every field the record has |
| `fields={…}` | some of them — the ones this scope asked for |

A whole record answers **any** question about that record, so whoever asked can
keep it and answer the next query out of it instead of asking again. A subset
answers only the question that asked for it: a later query naming a field it
left out is not answered by it, however many of the same records it names.

Neither end can tell them apart by looking — a record of two fields and two
fields of a record of nine come out the same shape — so the answer says which
it is. Say `record` when these are all the fields there are, and `fields` when
they are the ones somebody asked for. `fields` is the weaker claim and is
therefore always safe; `record` is the one worth making, and worth making only
when it is true.

One `complete` ends the scope, and three things can ride on it:

| | |
|---|---|
| `watermark={…}` | there is nothing of mine between where you asked from and this point that you do not now have |
| `exhausted` | everything there is. No watermark, because there is nothing past the end to be complete up to |
| `error="…"` | a refusal, which is an answer: the display carries on with what it has |

**`ordered` leads the answer**, on its own statement, before any record:

```
result 9 ordered
```

It says the records are in the query's own order, and it is said up front
because that is the only place it is worth anything. It changes what the far
end does with what arrives — ordered, it merges; unordered, it sorts first —
and an end that does not learn which until the records have all gone by can act
on neither. Saying nothing means unordered, which is always safe, and costs no
statement at all.

An application that declares it after a record has gone out is declaring
something that is already not true of what crossed, so the client libraries
drop it rather than send it.

**Nothing is stamped**, because nothing needs to be. The application's replies
and its results travel one ordered stream, so a scope's results are the ones
between the reply that accepted it and the result that completes it.

## A query does not change

There is no re-sort. A query is the sequence it was opened with, and a
different sort or a different filter is a **different query** — `set` addressed
to one is refused.

```
DISPLAY → APP   r=new query source="files" sort={ size desc } have=0 need=30
                end
APP → DISPLAY   reply r=10
                end
                …
DISPLAY → APP   destroy 9
                end
```

That is what makes the id the generation. Results for the sort somebody just
abandoned are still crossing when the results of the one they chose begin, and
nothing has to work out where one generation ended: they are addressed to
different numbers.

**The new one is opened before the old one is destroyed.** Records are held
against the *source* — the application has them because there are queries open
against that name — so opening first is what keeps the source in use while the
reader moves across, instead of letting it fall out of use and be built again a
statement later.

**And the display lets a query go** with `destroy 9`, which is one reader
finishing. That is why a query is an object with a lifetime rather than a
standing arrangement: without an ending, nothing ever tells the application
that the last reader has gone.

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

**A field name may begin with a dot**, which is how a name says it is a member
of the record rather than a word in its own right: `.size` is the member called
size, `size` is the word size. An application whose records carry their own
contents needs the distinction; one that has a fixed set of columns never writes
a dot. `psl-as-a-data-source.md` is where it is put to work.

**The filter does not have to be walked by hand.** `wire.Match(rec, spec.Filter)`
answers it for anything that can produce a field by name:

```go
func (e entry) Field(name string) *wire.Value { ... }
```

An application is still free to do better — a filter it can turn into an index
lookup should be — and one that ignores the filter entirely is still correct,
because the display rejects what it did not ask for.

## Where a larger library plugs in

The client libraries understand the open, the scope and the drop. Anything
else the display addresses to a query reaches the application whole:

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
