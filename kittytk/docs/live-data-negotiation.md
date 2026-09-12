# Live data negotiation

> **Status: a plan, with its first piece built.** The comparison core it stands
> on is settled (`sort-and-filter.md`), the reverse direction it needs is
> decided (`app-hosted-objects.md`), and the filling half — the query a display
> opens against an application, and the windows it serves — is implemented in
> all three client libraries (`hosting-a-query.md`). Coverage and invalidation
> are not built, and the open questions at the end are open.

## The pieces, and where each one lives

**A store** holds bundles. It exists and is built.

**A data source** is server-side, and is the top of the abstraction: a stack of
bundle layers, whatever ephemeral material the server holds itself, and — if
this source has an application behind it — material cached from corresponding
with that application.

**A view** is server-side: one query against one data source, like a cursor.

**A query** is the application's side of that correspondence: an equivalent
sequence, the same filter and the same sort, with far less management. It
exists only when a data source has an application component, and its interface
is deliberately much smaller than a view's.

The display opens a **view** and manages position, generation, watermark and
coverage; it also opens the **query** on the application's side, because only
it knows one is wanted and what sort and filter it carries. The application
holds that query, names it, and answers windows of it. Nothing about views
reaches the application; it hears about its query and nothing else.

## The query

A query is a source, the columns to include, the columns to exclude, a filter
and a sort. It says *what sequence this view is*, and it is stated **once**,
when the view is opened; everything after that is positional. That is what
keeps the wire quiet.

Four properties matter:

- **It names exactly one sequence.** The record key is always the sort's last
  level, so no two records tie and "the record after this point" means one
  place. Without that, two fills of a window can overlap or skip.
- **Both ends compute that sequence independently**, from the rules in
  `sort-and-filter.md`. Neither confers with the other about order.
- **Changing it is a new generation, not a new view.** Windows, watermark and
  counts held against the old spec are dropped; the view's identity persists.
- **It can be refused.** An end that cannot honour a query exactly says so when
  it is asked. A refusal is recoverable; an ordering that is quietly a little
  different corrupts everything after it and looks like data.

## A query is deduplicated, and positionless

**Positionless is what makes dedup safe.** Every fill request carries its own
boundaries, so a query holds no cursor of its own — nothing about where anybody
is reading. The watermark belongs to the *view*. Two views scrolled to
different places can therefore share one query without interfering. If a query
ever grew a position, dedup would break the same day.

**Dedup by the query as sent.** The server renders a view's filter and sort
into text to put them on the wire, and renders the same spec the same way every
time, so the application can key its table on that text — a string compare, not
a structural walk of two filter trees. That matters most where structural
comparison is real work and string comparison is not.

**The application counts.** If it hands the same query back to two requests, it
knows when the last of them goes and drops it at zero.

**Invalidation drops the table entry**, not just the data: the next request
with that same query text gets a fresh one rather than matching a stale one.

## Filling: one interface, and a minimal implementation of it

There are not two interfaces. There is one request, and the least an
implementation can do with it is trivial.

A fill request carries hints: where the server is, what it already has, how
many rows it needs, which columns, what to leave out. **A minimal query ignores
all of them**, sends every record it has, and says it is exhausted. That answer
is correct — the server asked for a window and got a superset.

So the application that onboards in an afternoon implements one thing —
enumerate my records — and never learns what a boundary, a watermark, a column
list or a collation is.

**That is not a toy.** Once the server holds the whole layer and an
*exhausted*, it caches it and never asks again until something invalidates it:
no watermarks, no boundary arithmetic, no round trip per scroll. Below whatever
size the author is comfortable shipping, the simple implementation is the
*fastest* one. Choosing it is an honest promise about **size** — my data fits
in a message stream and I accept that all of it crosses — not about effort.

What a more capable query takes over, in order:

1. **Filter** — send only matching records
2. **Sort** — send them in the query's order
3. **Window** — send only the stretch asked for, with a watermark
4. **Project** — send only the columns asked for, minus the exclusions
5. **Count** — say how many there are without sending them

**3 requires 2**: a window is defined by the order, so nothing can answer a
window without ordering. The rest are independent. A query backed by SQL takes
all five, because they are a `WHERE`, an `ORDER BY`, a `LIMIT`, a `SELECT` list
and a `COUNT(*)`.

Two rules make ignoring hints safe:

- **The server must accept more than it asked for.**
- **The answer must say whether it is in order.** This is the one hint that
  cannot be silently ignored, because it changes what the server does with what
  arrived: unordered means the server sorts it, ordered means it merges it
  as-is. A minimal query simply never sets the flag.

Everything else — columns, exclusions, boundaries, counts — can be ignored with
no flag at all, because ignoring them can only produce more data than was asked
for, never wrong data.

## The fill itself

The server states where it is, how far its own knowledge runs, how much of the
window it can fill itself, and how many rows the window holds:

```
from=<boundary>  to=<boundary>  have=<n>  need=<n>
```

The query's whole job:

```
emit every record of mine in (from..to]        -> k of them
if have + k < need:
    keep going past `to` until have + k = need
watermark = the furthest point swept
```

It never needs to know where the server's own records sit. Inside `(from..to]`
it sends all of its own — which the server needs anyway for that range to be
*correct* — and past `to` it sends exactly the shortfall, because out there the
server contributes nothing, so every row is one of the query's.

**The window is covered, and in one round trip.** The server is complete over
`(from..to]` because the query sent everything of its own there, and complete
from `to` to the watermark because the query swept it. Merged row `need` is at
or before the watermark by construction.

**The watermark is a completeness guarantee, not a position**: *there is
nothing of mine between your `from` and this point that you do not now have.*
That is what turns some records into a correct merged prefix, and what makes
the next several questions unnecessary — shrink the window, grow it back,
scroll within it, and nothing is asked at all.

## What goes stale, and how independently

Four things can go stale on their own, and most changes touch exactly one:

| | what it is | what breaks it |
|---|---|---|
| **count** | the total, for the thumb | any insert or delete passing the filter, anywhere |
| **positions** | the index of the rows on screen | inserts and deletes **above the anchor** only |
| **membership and order** | which records are in, and where | a change to a field the filter or the sort names |
| **content** | the cached values of records already held | a change to any field, anywhere in what is cached |

A record appended to a log past the watermark touches **count and nothing
else**: the thumb shrinks, no row repaints, no fill is issued, and the watermark
stays true because it never claimed that far.

A record inserted *above* the anchor touches count and positions but not
membership, order or content. The server adds one to the anchor's index and
everything on screen stays where it is. **That works only because the anchor is
a record, not an index**, which is reason enough to keep it that way.

A field that takes part in neither the sort nor the filter changes, and only
that record's cached content is stale: if it is on screen it repaints, and if
it is not, nothing happens at all.

And the common one: **the user re-sorts or re-filters.** That invalidates count,
positions, membership, order and the watermark — but **not content**, which is
keyed by record identity rather than by position. So re-sorting what is already
held costs no data transfer.

## Coverage: what the server says it depends on

The application cannot decide what matters without knowing what is being
watched, and telling it the filter and sort would not be enough — it would still
have to report every change and let the server sift, which is the traffic we are
avoiding. So the server states its concerns, and the application answers only
against those.

**Coverage is a standing statement, revised — not a log of past fills.** A fill
is how data arrives; it is not a claim the application has to remember. The
server says what it currently depends on and revises it as things move, so the
application's bookkeeping is bounded by the number of live queries rather than by
scroll history.

**A statement is a small set of concerns, each with a handle**:

- the **row extents** currently depended on, with the fields they cover
- the **count**, a standing concern with no extent at all
- a short list of **individually pinned keys** — a selection, an anchor, a row
  being edited

Separate handles are what let the application say *the count is stale* without
saying *the rows are stale*, which is the whole of the log case.

**The handle survives a revision: stable id, changing extent.** A fresh handle
per scroll would throw away the application's evidence every time and rebuild it
from nothing, which is the opposite of the point. The application keeps what
still applies to the overlap and computes only for what is newly covered.

**Coverage is the union of everything sharing that query.** One view's extent
falling inside another's collapses to one by construction, and their drifting
apart grows a second extent again — no special rule, and the application never
hears the word *view*. Extents coalesce when the gap between two is small
relative to their size.

**Coverage follows the cache, not the viewport.** If the server keeps rows
1–5000 because the user keeps sweeping over them, it depends on 1–5000, so it
states 1–5000 and the application watches all of it. That is the honest price of
not evicting, and often the right trade: server memory and application
watch-effort against reload traffic.

**One region is stated as several extents, split by how much each matters.**
A server holding 5000 rows with 10 of them on screen does not state one extent
from 1 to 5000. It states the visible ten as an extent of their own and the rest
as bulk, because a single extent forces the application to answer at the
resolution of the whole thing: sixteen scattered updates, one near the top and
one near the bottom, coarsen to *everything between them is stale* — which is
almost the entire region, and most of it did not change.

**A chunk boundary is a "do not merge across" mark.** Coarsening is always safe,
but what it costs depends on where it lands. Inside the bulk chunk the server
just forgets a stretch nobody is looking at. Across the visible chunk it drops
what is on screen and refills it. So the application may coarsen freely *within*
a chunk and should avoid spanning two, and splitting the chunks is how the server
tells it where that line is.

**The split is most of the signal.** Stating a small extent separately already
says *this one matters*; nothing further has to be spelled. A word naming a
chunk's hotness may be worth adding later, but it is decoration on top of the
shape, and an application is free to ignore it.

**Widen eagerly, shrink lazily.** Widening must happen *before* the server
depends on the new region or it has a silent hole. Shrinking has no correctness
deadline, so it can ride a heartbeat or piggyback on the next message out; the
only cost of shrinking late is that the application watches things nobody needs.

**The rule that has to hold: the stated coverage is a superset of what the
server actually depends on.** Stating more is safe — the application reports
staleness for something already dropped and the server ignores it. Stating less
is a missed invalidation: silent, and permanent.

**Revision replaces release.** There is no separate "I have dropped that region"
message; the next statement simply does not cover it.

**Fields are grouped by the role they play** — sort, filter, detail — and they
move rarely, where extents move constantly. Keep them separable in the message
so a scroll costs a boundary pair rather than a field list.

## Invalidation

The application reports **a handle, an extent, and a type**.

**The extent is what preserves the watermark.** *Handle 3 is stale* costs the
whole region; *handle 3, between these two boundaries* costs only that slice,
and the completeness claim survives on both sides of it. Same over-and-under
rule as everywhere: too wide is safe, too narrow is silent corruption.

**The type is a field-name lookup, not an understanding.** Because the coverage
statement groups its fields by role, the application classifies a change by
asking which group the changed field is in:

| what changed | what it costs |
|---|---|
| a **detail** field | content, for those records; nothing structural moves |
| a **filter** field | membership, so count and positions are in doubt for that extent |
| a **sort** field | order, so the same |
| a record **added or removed** | count, at a point in the order |

An add or a remove is the log case with no special handling: the extent is a
single point, and the server decides from where it falls — beyond the coverage,
just the count; above the anchor, count and a position shift; inside the visible
range, a refill.

The application has done nothing but look up a field name. It never evaluates
the filter, never compares boundaries semantically, never learns what a
collation is.

**The evidence never crosses the wire.** A handle is a peg the application hangs
whatever it has on: a hash of a directory's entries, an mtime, a row version, an
ETag, a generation counter. It re-hashes on suspicion, compares, and reports the
handle. The protocol only ever sees *handle 17 is stale*, which is what lets
this work over backends that have nothing in common.

**Over-invalidating is always safe; under-invalidating never is.** So an
application may coarsen freely — mark ten handles together, or all of them, if
tracking them separately is more bookkeeping than it wants. That makes the blunt
implementation a legal one rather than a failure.

**Points coalesce into a range by the same rule extents do**: merge when the gap
between them is small relative to the span they cover. Dense scatter becomes one
range, sparse scatter stays a list of points, and sixteen changes are reported as
however many pieces their spacing actually warrants. Both ends applying the same
heuristic is the point of writing it down — neither is then surprised by the
other's granularity.

**Invalidation costs nothing until someone looks.** The application says stale,
the server drops that region and pulls its watermark back — and issues no fill.
Whether a replacement is ever requested is the server's decision: a stretch
scrolled out of view an hour ago may never be read again, while something on
screen refreshes at once. Invalidation causes *forgetting*, not traffic.

**Volunteering values.** If the changed record is inside covered **detail**,
sending the new values along with the invalidation saves the round trip and
keeps the watermark whole. If it is outside, the application says stale and
sends nothing. Coverage is exactly what makes that a judgement the application
can make rather than a guess — and the rule that goes with it is: **never
volunteer values for fields outside the stated coverage.** That is the door
unwanted traffic comes through.

**Notices and fills share one ordered stream**, so a notice arriving after a
fill describes the world after that fill. Free, since the connection is already
ordered, but it has to be said or an implementation will answer notices on a
side channel and reintroduce the race.

## Quantization: both ends are free to be late

Neither end has to speak at the rate its own state changes, and both need that
freedom for the same reason — the underlying thing moves far faster than anyone
can use.

**The application accumulates.** An inode watch on a busy directory can fire
hundreds of times a second; nobody needs to hear about it more than about once.
So an application may hold notices for a while and submit them together, merging
what is close enough to merge by the rule above. What it is promising is
staleness, not error: the server's rows were right when they were sent and are
some fraction of a second behind now, which is what every row on a screen already
is.

One rule keeps that safe: **a fill answer is always from current state, and
supersedes any queued notice about the records it delivers.** Without it, an
application that answered a fill at full accuracy would then deliver a
second-old notice saying those very rows are stale, and the server would refill
what it had just been told the truth about — forever, at the accumulation
interval. With it, answering a fill discharges the pending notices it covers.

**The server does not re-designate at frame rate.** While a scrollbar thumb is
flying, the rows it sweeps past are painted and dropped, not cached — so nothing
in them is depended on, and the rule that coverage must widen *before* the server
depends on a region means there is nothing to say. When the thumb lands, the
server caches what is under it and revises once. Thirty frames, one revision, and
it is correct rather than merely throttled: the rate limit falls out of a rule
that is already there instead of being policy bolted on beside it.

The same applies to shrinking, which already has no correctness deadline, and to
the visible chunk, whose boundaries are worth restating once the view has
settled rather than while it is in motion.

## Eviction: flesh before skeleton

The sort and filter fields are the **skeleton**; everything else is **flesh**.

Positions, counts, membership, order and the watermark are all built out of the
skeleton. Lose it for a region and the server cannot place anything there — it
must re-read the whole stretch and re-establish its completeness claim. Lose the
flesh and nothing structural moves: the count is fine, the positions are fine,
the thumb does not twitch, and re-acquiring it is a projection-only fill for the
rows actually on screen.

So flesh is cheap to lose and cheap to regain, and skeleton is neither. **Evict
flesh first.**

It narrows what the application has to watch, too, which may matter more:
skeleton fields are the ones a backend can usually watch cheaply — the indexed
columns, the mtime, the thing a trigger fires on. Flesh is the blob you would
have to read in order to know it had changed.

**Which makes the steady state two concerns**: skeleton over a wide extent —
everything cached — and all requested fields over a narrow one, what is on
screen. Two handles, two extents, two field sets. Small enough that even a
careful application has almost nothing to track, and it lines up with what a
real backend can actually watch.

## What stays policy

*When* to evict — age, memory pressure, how much hysteresis before a shrink — is
server policy, not protocol. Stating it as policy lets two implementations
differ without either being wrong.

So are the quantization intervals on both ends, and where the gap-to-span line
falls when points coalesce. What the protocol fixes is that coarsening is safe
and that a chunk boundary is the one place not to coarsen across; how coarsely
either end chooses to speak is its own.

## Open questions

- **Whether a handle can cover fields with no extent** — "any change to `size`,
  anywhere". Strictly weaker, but it may be the difference between an
  application reporting precisely and falling back to invalidating everything.
- **Counts as deltas or absolutes.** `COUNT(*)` on a large table is not free, so
  a live log wants *+1* rather than a recount per append — with an occasional
  absolute to resynchronise against drift.
- **Scrolling backwards**, which is symmetric but inverts `from` and `to` and
  wants a low watermark.
- **The jump into an uncovered middle**, when the user drags the thumb to
  nowhere in particular and there is no watermark to stand on.
