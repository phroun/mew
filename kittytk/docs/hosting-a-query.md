# Serving a query

> **Status: built in the clients, not yet in the display.** The statements
> below are what the three client libraries speak today, and
> `testdata/query.wire` is the corpus all three answer. The display side that
> asks these questions is still to come; `live-data-negotiation.md` is the plan
> it will be built to, and serval's `docs/ordering.md` the comparison both ends stand
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
`files` honours the sort and the scope and answers with a watermark. The probe's flags drive the rest — `-filter`, `-sort`, `-more`, and
`-resort`, which opens a second query on another sort and drops the first —
and what it prints is this:

```
<- hello version=1 app="queryapp"
-> welcome version=1 session=1
-> init app=1 store=2 host=3
-> q=new query source="files" filter={ not { starts name "." } } sort={ name natural } count=3
<- reply q=1
<- result 1 ordered id=2 record={ name "build.sh"; size 310 }
<- result 1 id=3 record={ name "go.mod"; size 96 }
<- result 1 id=1 record={ name "README.md"; size 2048 } complete watermark=1 filled
-> n=new query source="files" filter={ not { starts name "." } } sort={ name natural } after=1 count=3
<- reply n=2
<- result 2 ordered id=6 record={ name "src/file2.go"; size 1200 }
<- result 2 id=7 record={ name "src/file10.go"; size 880 }
<- result 2 id=4 record={ name "src/parser.go"; size 14022 } complete watermark=4 filled
-> destroy 1
<- reply
-> r=new query source="files" sort={ size desc } count=3
<- reply r=3
<- result 3 ordered id=4 record={ name "src/parser.go"; size 14022 }
<- result 3 id=5 record={ name "src/window.go"; size 9310 }
<- result 3 id=8 record={ name "testdata/query.wire"; size 6100 } complete watermark=8 filled
-> destroy 2
-> destroy 3
```

**A query is asked once and answered once.** Nothing addresses one that already
exists: `complete` ends the answer, `destroy` ends the interest, and neither is
a way to ask for more. So the second scope above is a second query, stating the
same sequence again and saying which record to carry on past -- and the first
is let go once the records it answered with are held.

**A scope names its ends by identity, and nothing else.** `after=1` is the whole
of "carry on past README.md". Where a record stands in a sequence is the
business of whoever put it there, so the asker has nothing to say about it and
does not try: it names the record, and the source finds the place.

**`ordered` rides on the first record and the terminator on the last.** Both may
stand alone -- a source that does not yet know it has reached the end says so on
a line of its own -- and a reader takes order, then record, then end, whichever
statement they arrived on. An answer of one record is one line.

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
q=new query source="files" filter={ not { starts name "." } } sort={ name natural } count=5
```

```
key  name             size
---  ---------------  ----
2    "build.sh"       310
3    "go.mod"         96
1    "README.md"      2048
6    "src/file2.go"   1200
7    "src/file10.go"  880

complete up to 7
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
    for _, rec := range myRecords(f.After, f.Until, f.Count) {
        f.Record(rec.ID, wire.Named("name", rec.Name), wire.Named("size", rec.Size))
    }
    f.Filled(last.ID)
})
```

Registering a source says nothing on the wire: **a source is a name, not an
object**. The application tells whatever trinket is to show it `data="files"`,
and the display opens queries against that name.

`Record` says these are all the fields there are. `Subset` — `f.Subset(id, has,
…)`, `f.subset(id, named=…, …)`, `kt_fill_subset` — says they are the ones this
query asked for, and says how many the record has altogether. It crosses as
`fields={…} map=5`; see [How much was left out](#how-much-was-left-out).

**The identity is the first argument, not a field.** A record is free to carry a
field called `key` of its own, and that field is data like any other: it sorts,
it filters, it fills a column. What names the record travels beside the bag, and
crosses as `id=`.

Python is the same shape (`conn.provide_source(name, fill)`, `f.record(id,
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
                  count=30
                end
APP → DISPLAY   reply q=9
                end
APP → DISPLAY   result 9 ordered id=17 record={ name "src/parser.go"; size 1024 }
                result 9 id=42 record={ name "src/window.go"; size 2048 }
                  complete watermark=42 filled
```

**The application names it.** Each end mints ids in its own space and the
direction a statement travelled says whose space it is in, so nothing collides
and no range is reserved anywhere. The display has no id to offer for something
the application holds, so the reply carries the application's.

The reply goes out before any result, and that ordering is not policy: the
application mints the id, so it writes it before anything that carries it.

**Every scope after that is a query of its own**, stating the same sequence
again and naming the record to carry on past:

```
DISPLAY → APP   n=new query source="files" filter={ ge size 1024 } sort={ name natural }
                  after=42 count=30
                end
APP → DISPLAY   reply n=10
                end
APP → DISPLAY   result 10 ordered id=51 record={ … }
                result 10 complete exhausted
DISPLAY → APP   destroy 9
                end
```

A query is asked when it is made and answered once. Nothing addresses one that
already exists except `destroy`, which is how the application learns it may let
those records go.

| naming the sequence | |
|---|---|
| `source` | the name the application serves these records under |
| `filter` `sort` | which records are in it, and in what order |
| `reversed` | that sequence, walked from its end |
| `fields` `exclude` | which of a record's fields the display wants |

| naming the scope of it | |
|---|---|
| `after` | the identity to start past — the display holds that one. Absent is the start of the sequence |
| `until` | the identity to stop before — the display holds that one and everything beyond it. Absent walks to the count or to the end |
| `count` | how many records are wanted |
| `from` | a POSITION to begin near, where the display has a place in mind and no identity there. Best effort; nought is the start |

The whole of what the application does with that: start past `after`, send
`count` records, and stop early if it reaches `until`.

| ending the scope | |
|---|---|
| `filled` | the count was reached. There is more past the watermark |
| `joined` | `until` was reached, so the display's two runs are now one |
| `exhausted` | there is nothing more this way, and so no watermark |
| `error` | a refusal, which is an answer |
| `total` | how many records the whole SEQUENCE has — at least this many |
| `exact` | and that figure is all there are, rather than a floor |

The same completion under the `place` verb ends the **order** instead — see
[Places](#places).

## What may be sent, and what may be claimed

These are two different things, and only one of them is a promise.

**What is sent may be any superset of the answer.** An application that filters
on the one predicate it can index cheaply and ignores the rest is correct. So
is one that uses a looser comparison than the core's, or sends the whole source
every time. The display rejects what it did not ask for, so extra records cost
bandwidth and nothing else — and it may keep them, because a record it has is a
record it need not ask for later.

## Identity is not a field

Every record has an identity — it is what a scope's ends name, what a watermark
names, and the sort's implicit last level. It travels **beside** a record's
fields, never among them, and `id` is how a filter asks about it:

```
filter={ id (left/1) (left/note) }
```

It matches against a set of them, the way `in` matches a field against a set of
values, and it names no field because an identity is not one. A record that
carries a field called `key` is carrying data like any other — `eq key 1` is an
ordinary question about an ordinary field, and says nothing about which record
it is.

## Reversed

`reversed` walks the stated sequence from its end:

```
DISPLAY → APP   q=new query source="files" sort={ size } reversed count=30
```

**Every level turns over with it, the one the sort does not write included.**
An identity settles what the named levels leave equal, and a sequence read
backwards settles it backwards too — which is what makes this the exact mirror.
`sort={ size desc }` is a third sequence again: it turns one level over and
leaves the records it ties facing the way they already were.

It names no field, and that is the point of it. A record's identity is not
something a query can name at all — an application that exposes a record's fields and
not its identity has no way to write `sort={ key desc }` at all — so the one
way to turn a sequence over that every implementation can answer is the one
that asks for no field by name.

**It is the one hint that cannot be quietly dropped**, along with the sort
itself. Every other — the boundaries, the field list, the exclusions, the count
— can only make an answer bigger when it is ignored, never wrong. Ignoring this
one produces the sequence backwards, and `ordered` would then be a lie. An
application that will not reverse simply does not say `ordered`, and the
display sorts what arrives.

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

## A place, and where the answer really began

Everything above names a RECORD. A display dragging a scroll thumb has no
record to name: it knows it is sixty percent of the way down a sequence it has
mostly never seen, and the identity there is exactly what it is trying to find
out.

`from` is that question, and `first` is the answer to it.

```
DISPLAY → APP   q=new query source="files" sort={ name } from=600 count=20
APP → DISPLAY   result q ordered
APP → DISPLAY   result q id=… record={ … }
                …
APP → DISPLAY   result q complete filled watermark=… first=600 total=9000 exact
```

**`from` is best effort and never a promise.** An application that can place a
position lands on it; one that cannot starts where it would have started anyway.
Either way `first` says where the answer ACTUALLY began, so a display that meant
somewhere else asks again from what it learned, and converges. Nothing has to be
negotiated, because a display that asks for a different place and is told the
same figure has learned that this application does not seek.

**`first` crosses only when it is known, and what crosses is exact.** A count
can honestly be a floor — part of a sequence seen is at least that many — but a
position cannot be reckoned the same way, and *at least the six hundredth* is
not something a display can put a thumb on. So there is no weak form, `exact`
does not apply to it, and silence means the display stays where it was rather
than believing a figure nobody sent.

An unhonoured `from` answered in silence would be read as *you are at the top*,
and the display would paint the first rows of the sequence as though they were
the six hundredth. So an application that ignores `from` and starts at the
beginning says `first=0` — which is true, and is how the display finds out.

Beside `total`, the two are a scroll thumb: how long the sequence is, and where
in it this answer sits.

**A record is not a position**, so `after` and `from` on one scope is refused
rather than answered from one of them. A display holding the record it wants to
carry on past knows something better than a place.

## What a result carries

**A result carries a record, or ends the scope.**

A record crosses under one of two words, and the difference is how much of the
record is there:

```
result 9 id=17 record={ name "src/parser.go"; size 1024 }
result 9 id=17 fields={ name "src/parser.go" } map=5
result 9 id=17 fields={ .0 "a"; .name "x" } map=5 len=3
```

| | |
|---|---|
| `id=` | what names the record, beside its fields rather than among them |
| `record={…}` | every field the record has |
| `fields={…}` | some of them — the ones this query asked for |
| `map=` | how many members the record has **by name**, sent or not |
| `len=` | how many it has **by position** |

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

### How much was left out

The two counts are what keep a subset worth more than the one question it
answered. `map=5` with two fields sent says three are missing; send the other
three later and whoever asked knows they now hold the lot, **without anyone
deciding that** — it is what the counts say. `len=3` says the members standing
by position are `0`, `1` and `2` and nothing else, so `3` is answered without
anyone being asked, an ordered member being named by where it stands.

`map` and `len` because those are already the two halves of a PSL node, which is
what a record is read out of: its items and its keyed members.

**Count members only, the way `serval.Tally` does.** A count that said a record
had no positional members while carrying one called `.0` would have the far end
answer *not there* about a member that is, which is worse than saying nothing.

**Either count may be zero, and a zero is not written.** Most records have no
members standing by position, so most subsets carry `map=` alone.

**A whole record states neither**, and a `record=` carrying one is refused:
what a whole record carries *is* all of them, so a count beside it either says
that again or contradicts it.

**A field the record has not got is worth sending** as a name with nothing under
it — `fields={ .name "a"; .thumbnail }` — rather than leaving out. Left out it
reads as a field nobody asked about, and the next query naming it asks all over
again; sent, it is a guarantee that the record has not got it. It is not
counted, being knowledge about the record rather than a member of it.

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

## Places

> **Status: built.** The `place` verb, the order's completion, `total=` and
> `extend` are in the KittyTK Wire Language and in all three clients, and all
> three conformance harnesses answer them. Below the wire, serval's `Placing` is
> a sink with somewhere to put a place, and `CachedSource` answers a stretch
> whose order it holds and whose values it does not by placing the rest.
>
> **Nothing an author writes changes with `extend`.** They place what they have
> and send the record when they have it; the leaving-out happens in the library,
> which is the only place it can happen safely — an author eliding by hand would
> have to know what the far end asked for.

An answer can be worth starting on before it is finished, and what a reader
needs first is almost never the values — it is **where the rows are**. A view
with the order can lay out its rows, size its bar and stay reactive under a
scrub while the contents arrive behind it. A merging source with the order can
begin placing records against another source's. Both of those are blocked today
by an answer that arrives all at once or not at all.

So an application may send a **place** for any row whose position it knows
before its contents, alongside the results, in the one answer:

```
APP → DISPLAY   result 9 ordered
                place 9 id=17 fields={ name "src/parser.go" }
                result 9 id=17 record={ name "src/parser.go"; size 1024 }
                place 9 id=42 fields={ name "src/window.go" }
                place 9 id=55 fields={ name "src/wire.go" }
                place 9 complete watermark=55 filled
                result 9 id=42 record={ name "src/window.go"; size 2048 }
                result 9 id=55 record={ name "src/wire.go"; size 640 }
                  complete watermark=55 filled
```

**Everything goes out the moment it is known.** A row whose values were to hand
completes immediately; the places carry on behind it. There is no phase here and
nothing is queued to keep the two kinds apart.

A place says: **a record stands here, this is what I have of it so far, and I
am claiming nothing about how much that is.** It is the third degree of
knowledge, and the set closes up:

| | |
|---|---|
| `record={…}` | every field the record has |
| `fields={…}` with `map=`/`len=` | some of them, **and how many there are** |
| `place` with `fields={…}` | some of them, **and no claim about how many** |

**A place carries no `map=` or `len=`, and `record=` on one is refused.** Both
are claims about how much of the record there is, and not making that claim is
the entire difference between a place and a result. Which also means a place
never needs a spelling for *I do not know how many members this record has* —
the question does not arise, because a place was never asked.

**If you can send the whole record, send a result.** A place is for a row whose
position you know sooner than its contents; sending one for a record you could
have completed just costs a second statement.

### When the order is settled

An answer completes **two different things**, and they need not finish at the
same time: the order is settled once every row has been named, and the scope is
done once every one of them has been filled in. So `complete` says which of the
two it is ending by the verb it rides.

This is not decoration. A reader cannot present a sequence — not even a sequence
of placeholders — until it knows it has all the rows, because another may still
turn up between two it already holds. Painting the first row early is fine;
knowing the shape of the block is not. If the only completion arrived at the end
of the answer, the reader would learn the row set was final at exactly the moment
it no longer needed to know, and places would buy nothing at all.

**A result with no place before it is both.** It settles where that row stands
and what it holds at once, so an application with a whole record in hand simply
sends it — there is no obligation to place every row, and nothing whatever to be
gained by placing one you are about to complete. What the order's completion
closes over is the rows sent under *either* verb.

**And the scope's own `complete` completes the order too**, if nothing has
already. So the order's completion is worth sending only when the order settles
*earlier* than the answer; where the two moments are the same there is nothing
to send, and an answer that never mentions places is today's answer exactly —
one completion, settling everything at the end.

Which leaves one rule about where it falls, and only if it is sent at all:
**after the last place, and before the scope's.** Places and results interleave
however the application likes either side of it.

Both of these are the same principle. Nothing here has to be said twice: the
stronger statement carries the weaker one wherever the weaker was never made.

**Where both are sent they carry the same watermark and the same ending word**,
and they agree. `filled`, `joined` and `exhausted` say where the walk stopped,
which is a fact about the order, so they belong to the first — and the second
says them again for a reader that skipped the places and is seeing exactly
today's answer.

### Why a verb of its own

Because that is what makes ignoring it safe, with nothing to negotiate.

An answer's place statements are **additional**, not substitutional: every
record still arrives as a result, and the scope's own `complete` still ends the
answer. So a reader that does not know the verb, or knows it and does not want
it, skips both the places and the completion that rides one, and is left with
exactly the answer it gets today — every record, in order, watermark true. It
cannot be misled about completeness, because it never saw them.

It is the mirror of how a hint works, pointing the other way. A hint travels
with the **question** and the answerer may drop it, safely, because dropping it
can only produce more data than was asked for. A place travels with the
**answer** and the asker may drop it, safely, because it was never part of what
the answer claimed.

That is the test for whether anything in this protocol needs opting into:
**does dropping the statement still leave the answer true?**

### Extend and replace

There is one thing that fails that test, and it is worth having anyway.

If a place already carried what the row needs, the result for it repeats every
field. Under **extend**, it need not: the result carries only what the place did
not, and in the limit carries no fields at all — just the counts, which is the
claim only a result can make. Whoever asked holds two fields and is now told
there are two, so it holds the lot, *without anyone deciding that*. The counts
already work this way; nothing new is needed to say it.

Which means extend-mode results are subset-shaped — they state counts, so they
cannot state `record=`. And since a zero count is not written, a record with no
members at all confirms as a bare `result 9 id=17`: no fields, no counts, and
nothing left for either to say.

**Replace is the default**, and extend is asked for by the display when it opens
the query, because the display is the end that has to hold the places for it to
lean on:

```
DISPLAY → APP   q=new query source="files" sort={ name natural } extend count=30
```

It is the display's declaration and not the application's choice: under extend a
reader that dropped the places would silently lose fields, so only the reader
can say it is safe.

**Extend obliges nobody to place anything.** A result with no place before it
has nothing to lean on, so it carries the lot — which is the same rule, not an
exception to it. An application under extend still sends a whole record outright
whenever it has one.

### Forwarding

Anything in the middle — a cache, an overlay, a client library re-emitting —
may pass places straight through and follow with its own result when it has
one. The mode relays; it does not have to be unwound.

The rule is: **forward a place as long as you can honestly put it where you are
putting it.**

- A **pass-through** keeps a place's position by construction, so it forwards
  freely. An overlay that deletes that record simply does not forward it.
- A **merging** source cannot, in general. Interleaving records from several
  includes needs their sort values, and a place carrying none cannot be put
  anywhere in the merged order.

Which is why the natural cut for what a place carries is the fields that
**decide** the sequence — its sort and filter fields. That is not a convenience
for the renderer, it is what makes a place survive a merge. A place carrying its
sort values travels; a place carrying nothing dies at the first merge.

And there is no failure mode in it, only a missed optimisation: whoever cannot
use a place drops it, and that record arrives as an ordinary result later.

### What this is worth to an application

`SELECT id ORDER BY name WHERE …` is cheap and `SELECT id, thumbnail …` is not,
and an application in that position currently has to either pay for the
expensive columns or claim nothing at all. With places it can say exactly what
it knows cheaply, when it knows it.

It is not a new capability to take over — it sits beside the ladder in
`live-data-negotiation.md` rather than on it, as a way to answer *scope* and
*project* in the order they get cheap.

### One hazard for an implementer

**A place's fields are true, and its silence is not.** The values on a place can
be believed, rendered and kept — correctness is not what is being withheld,
completeness is. But a holder that files them as though they were a record's
whole contents will read that record as complete, which is the strongest claim
in the system arriving from the weakest statement. Whatever holds records needs
to be able to say *some fields, count unknown*, even though the wire never has
to.

## A query does not change

There is no re-sort. A query is the sequence it was opened with, and a
different sort or a different filter is a **different query** — `set` addressed
to one is refused.

```
DISPLAY → APP   r=new query source="files" sort={ size desc } count=30
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
{ name "src/parser.go"; size 1024 }             a record's fields
{ name; size }                                  a bare list of fields
```

**Nothing is reserved in it.** A record may carry a field called `key`, and
that field is data like any other: `eq key 1` is an ordinary question about an
ordinary field, and says nothing about which record it is. What names the
record travels beside the bag as `id=`, and no filter reaches it except `id`.

A field's value may be any kind the wire has — a word, a string, a number, and
`undefined` for a field a record does not have.

## The filter and the sort

Both are spelled as `sort-and-filter.md` defines and mean what serval's
`docs/ordering.md` says, and both arrive as a structure
rather than as text. A filter block is an AND, so the top of a parsed filter
tree always is — one shape to walk, whether it held one predicate or twenty.

Which cuts the other way when a filter is *written out*: only an `and` may have
its children put straight into the outer block. An `or` and a `not` are written
as the statement they are, `filter={ or { … } }`, because a block already means
and — writing their children into it would read back as a conjunction, which for
an `or` asks for the rows satisfying every branch at once. It does not fail; it
answers wrongly and quietly, and a tree hint with no root spelled out is the case
that found it.

A set may be written either way, and both read to the same structure:

```
in kind { folder; disk }      a block holds bare names
in name "a" "b" 3             operands hold any value
```

**A field name may begin with a dot**, which is how a name says it is a member
of the record rather than a word in its own right: `.size` is the member called
size, `size` is the word size. An application whose records carry their own
contents needs the distinction; one that has a fixed set of columns never writes
a dot. serval's `docs/sources.md` is where it is put to work.

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

Both are built *below* the wire already: serval's `CachedSource` states what it
depends on and is told what has stopped being true. What has not been settled is
how either is spelled between two processes.

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
