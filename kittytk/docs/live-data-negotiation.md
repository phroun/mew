# Live data negotiation

> **Status: a plan, with its server half built.** The comparison core it stands
> on is settled (serval's `docs/ordering.md`), the reverse direction it needs is
> decided (`app-hosted-objects.md`), and the filling half — the query a display
> opens against an application, and the scopes it serves — is implemented in all
> three client libraries (`hosting-a-query.md`).
>
> **Coverage and invalidation are now built as a library rather than as a
> protocol.** serval's `CachedSource` holds what has been read, states what it
> depends on (`Covers`), and is told what has stopped being true (`Stale`), with
> tests for each. None of it is on the wire: the handles, the chunking and the
> evidence-free reporting below are still a plan.
>
> **This is a data engine. There is no view here yet**, so everything about a
> viewport, a thumb or a trinket is describing what will ask, not what does.

## Two questions that look like one

An earlier draft of this document ran two things together under *coverage*, and
they are not one question. Different owners, different rates, and — the part
that matters — **different failure modes**.

**What is held** is a correctness question. Whatever has been read and kept can
be handed back without asking anybody, so anything that stops being true about
it has to be forgotten. State it too narrowly and a stale value is served as
though it were fresh: silently, and for as long as it is held. It is not a
preference. It is derivable, it is exactly what is in the cache, and the holder
is the only one who can state it.

**What is wanted live** is a latency question. A visible trinket says *I am
looking at this, tell me when it moves* — an assertion about attention rather
than about memory. It comes from the view, it changes as fast as a viewport
does, and stating it too narrowly costs a round trip when something changes off
the edge of what was asked for. Nothing goes wrong. It is also the only one of
the two that has a **level** — how hot, how soon, how finely — because it is the
one somebody is choosing.

The first is built and is serval's. The second is not built and is not serval's:
a hot range is asserted by the view against the same data sets the queries are
answered from, and travels back down the source graph to whatever leaf actually
holds the data. A composed source already keeps the per-include cursor vector
that translating a range downwards will need.

Each section below says which of the two it belongs to.

## The pieces, and where each one lives

**A store** holds bundles. It exists and is built.

**A data source** is server-side, and is the top of the abstraction: a stack of
bundle layers, whatever ephemeral material the server holds itself, and — if
this source has an application behind it — material cached from corresponding
with that application.

**A data set** is server-side: one stated sequence over one data source — this
source, this sort, this filter — prepared once and drawn from. Those three name
it and nothing else does, so two queries naming the same three are reading one
data set, and whatever was worked out for either holds for both. It holds no
position of its own, so readers at different places share one. It is
`source.DataSet`, and it is built (`psl-as-a-data-source.md`).

**A cache** is server-side, shared by every data set over one source, and is a
wrapper rather than a policy — a source that should not be cached simply is not
wrapped. There are two of them, holding the two different things a held answer
is made of; see *Eviction*, below. `serval.CachedSource`, built.

**A query** is the application's side of that correspondence: an equivalent
sequence, the same filter and the same sort, with far less management. It
exists only when a data source has an application component, and its interface
is deliberately much smaller than the server's own.

**What a reader holds** — where it is scrolled to, which sequence it is
reading, how far its watermark runs, what it depends on — is per-reader, and it
is neither of the two above: several readers at different positions share one
result set and one query. It has no name yet; the last open question at the
bottom is that it needs one.

The display opens a result set and keeps that bookkeeping against it; it also
opens the **query** on the application's side, because only it knows one is
wanted and what sort and filter it carries. The application holds that query,
names it, and answers scopes of it. None of the server's own bookkeeping
reaches the application; it hears about its query and nothing else.

## The query

A query is a source, the columns to include, the columns to exclude, a filter
and a sort. It says *what sequence this is*, and it is stated **once**, when the
query is opened; everything after that is positional. That is what keeps the
wire quiet.

Four properties matter:

- **It names exactly one sequence.** The record key is always the sort's last
  level, so no two records tie and "the record after this point" means one
  place. Without that, two fills of a scope can overlap or skip.
- **Both ends compute that sequence independently**, from the rules in
  serval's `docs/ordering.md`. Neither confers with the other about order.
- **It cannot be changed.** A different sort or a different filter is a
  different query, so **the id IS the generation** — results still in flight for
  the old sequence are told apart from the new ones by the number they are
  addressed to rather than by where they fall in a stream. `set` addressed to a
  query is refused. The display opens the replacement *before* it destroys what
  it is replacing, which is what keeps the source in use while the reader moves
  across; records are held against the source's name, not against any one query.
- **It can be refused.** An end that cannot honour a query exactly says so when
  it is asked. A refusal is recoverable; an ordering that is quietly a little
  different corrupts everything after it and looks like data.

## A query is deduplicated, and positionless

**Positionless is what makes dedup safe.** Every fill request carries its own
boundaries, so a query holds no cursor of its own — nothing about where anybody
is reading. The watermark belongs to the reader. Two readers scrolled to
different places can therefore share one query without interfering. If a query
ever grew a position, dedup would break the same day.

**Dedup by the query as sent.** The server renders a result set's filter and
sort into text to put them on the wire, and renders the same spec the same way
every time, so the application can key its table on that text — a string
compare, not a structural walk of two filter trees. That matters most where
structural comparison is real work and string comparison is not.

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
is correct — the server asked for a scope and got a superset.

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
3. **Scope** — send only the records asked for, with a watermark
4. **Project** — send only the columns asked for, minus the exclusions
5. **Count** — say how many there are without sending them

**3 requires 2**: a scope is defined by the order, so nothing can answer a
scope without ordering. The rest are independent. A query backed by SQL takes
all five, because they are a `WHERE`, an `ORDER BY`, a `LIMIT`, a `SELECT` list
and a `COUNT(*)`.

**Beside the ladder rather than on it: answering in two passes.** An
application may know where the rows are long before it knows what is in them —
`SELECT id ORDER BY name WHERE …` is cheap and `SELECT id, thumbnail …` is not
— and today it must either pay for the expensive columns or claim nothing. It
can instead send **places** first and results behind them, in the same answer,
so that a reader can lay out rows and a merging source can begin placing them
while the values arrive. It is not a sixth thing to take over; it is a way to
answer 3 and 4 in the order they get cheap. Designed and not built — see
`hosting-a-query.md`.

Two rules make ignoring hints safe:

- **The server must accept more than it asked for.**
- **The answer must say whether it is in order.** This is the one hint that
  cannot be silently ignored, because it changes what the server does with what
  arrived: unordered means the server sorts it, ordered means it merges it
  as-is. A minimal query simply never sets the flag.

Everything else — columns, exclusions, the scope's ends, the count — can be
ignored with no flag at all, because ignoring them can only produce more data than was asked
for, never wrong data.

## The scope itself

The server names the record to carry on past, the record its own knowledge
picks up at, and how many rows it wants:

```
after=<identity>  until=<identity>  count=<n>
```

Both ends are identities, not positions. Where a record stands in a sequence is
the business of whoever put it there, so the asker names the record and the
source finds the place.

The query's whole job:

```
start past `after`
send records until `count` of them have gone
   or until `until` is reached, which says the asker's two runs are now one
watermark = the furthest point swept
```

It never needs to know where the server's own records sit, and it is told
nothing about them: `until` says only where to stop, because out there the
server already holds everything.

**The scope is covered, and in one round trip**, and the answer says which of
three things ended it — `filled`, `joined` or `exhausted`. Those are different
facts to the server and it cannot work out which from the records alone: a
scope that filled and one that ran out look identical.

**The watermark is a completeness guarantee, not a position**: *there is
nothing of mine between your `from` and this point that you do not now have.*
That is what turns some records into a correct merged prefix, and what makes
the next several questions unnecessary — shrink the scope, grow it back,
scroll within it, and nothing is asked at all.

**Backwards is the same scope with its ends swapped**, and it is built: in the
wire's `reversed`, in all four source implementations, and in the cache. There
is no low watermark as a separate idea — a held run names the record it is
guaranteed FROM and the one it is guaranteed TO, and a backwards answer fills
those in the other order. Which is why a run carries two ends rather than a
direction: a stretch read backwards and the stretch below it read forwards meet
and become one run, and neither remembers which way it was walked.

## What goes stale, and how independently

*(This is the "what is held" question.)*

Four things can go stale on their own, and most changes touch exactly one:

| | what it is | what breaks it |
|---|---|---|
| **count** | the total, for the thumb | any insert or delete passing the filter, anywhere |
| **positions** | the index of the rows on screen | inserts and deletes **above the anchor** only |
| **membership and order** | which records are in, and where | a change to a field the filter or the sort names |
| **content** | the cached values of records already held | a change to any field, anywhere in what is cached |

A record appended to a log past the watermark touches **count and nothing
else**: the thumb shrinks, no row repaints, and no fill is issued. In the cache
it costs no places at all — a run that had read to the end of the sequence
gives up the claim that the sequence ends there, and keeps every record of it.

A record inserted *above* the anchor touches count and positions but not
membership, order or content. The server adds one to the anchor's index and
everything on screen stays where it is. **That works only because the anchor is
a record, not an index**, which is reason enough to keep it that way.

A field that takes part in neither the sort nor the filter changes, and only
that record's cached content is stale: if it is on screen it repaints, and if
it is not, nothing happens at all.

And the common one: **the user re-sorts or re-filters.** That invalidates count,
positions, membership, order and the watermark — but **not content**, which is
keyed by record identity rather than by position. So re-sorting costs no data
transfer for the records held **whole**. One held as a subset answers only the
question that asked for it, which is why a result carries `record={…}` or
`fields={…}` and says which (`hosting-a-query.md`).

That last paragraph is now a property of the code rather than a hope: the values
are held per SOURCE and the order per SEQUENCE, so opening a second sort over a
source finds every value it needs already here. See *Eviction*.

## Coverage: what is held

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

**A statement is a small set of concerns**:

- the **row extents** currently depended on — built: `Covers(spec)` returns them,
  taken from the runs themselves, both ends inclusive and by identity
- the **fields**, grouped by the part each plays — built: `Spec.Roles()` returns
  sort, filter and detail, and `Whole` for a query that asked for whole records
- the **count**, a standing concern with no extent at all — built:
  `RecordCount` is a figure, a floor, or nothing, and it is keyed by the filter
  rather than the sequence, so a re-sort keeps it
- a short list of **individually pinned keys** — a selection, an anchor, a row
  being edited — not built

Separate concerns are what let the application say *the count is stale* without
saying *the rows are stale*, which is the whole of the log case.

**Roles and extents move at completely different rates**, which is why they are
two things and not one. Which fields decide a sequence is settled when the
sequence is opened and does not change while it is read; the extents move every
scroll. Kept separable, a scroll costs a pair of boundaries rather than a field
list.

**Coverage is the union of everything sharing that query.** One reader's extent
falling inside another's collapses to one by construction, and their drifting
apart grows a second extent again — no special rule, and the application never
hears that there were two readers. In the cache this is not a rule either: there
is one run per stretch of a sequence, two answered back to back join where their
ends meet, and `Covers` reports what is there.

**Coverage follows the cache, not the viewport.** If the server keeps rows
1–5000 because the user keeps sweeping over them, it depends on 1–5000, so it
states 1–5000 and the application watches all of it. That is the honest price of
not evicting, and often the right trade: server memory and application
watch-effort against reload traffic. Which way to lean is eviction policy — see
*What stays policy* — and coverage simply reports where it landed.

**Widen eagerly, shrink lazily.** Widening must happen *before* the server
depends on the new region or it has a silent hole. Shrinking has no correctness
deadline, so it can ride a heartbeat or piggyback on the next message out; the
only cost of shrinking late is that the application watches things nobody needs.

**The rule that has to hold: the stated coverage is a superset of what is
actually depended on.** Stating more is safe — the application reports
staleness for something already dropped and it is ignored. Stating less is a
missed invalidation: silent, and permanent. `Covers` satisfies it with nothing
to spare, because it is read off what is held rather than tracked beside it.

**Revision replaces release.** There is no separate "I have dropped that region"
message; the next statement simply does not cover it.

**The handle is the wire's half and is not built.** A handle is a stable id for
one concern across revisions, so that the application keeps whatever evidence
still applies to the overlap and computes only for what is newly covered — a
fresh handle per scroll would throw that away every time. Within one process
there is nothing to peg: `Covers` is asked and answers.

## Subscription: what is wanted live

*(Not built, and not serval's. This section is a plan.)*

A hot range is a view saying **I am watching this, and at this level**. It is
asserted against the same data sets the queries are answered from, and passed
back down the source graph to whatever leaf is really providing the data.

**One region is stated as several ranges, split by how much each matters.**
A reader holding 5000 rows with 10 of them on screen does not assert one range
from 1 to 5000. It asserts the visible ten as a range of their own and the rest
as bulk, because a single range forces the application to answer at the
resolution of the whole thing: sixteen scattered updates, one near the top and
one near the bottom, coarsen to *everything between them is stale* — which is
almost the entire region, and most of it did not change.

**A chunk boundary is a "do not merge across" mark.** Coarsening is always safe,
but what it costs depends on where it lands. Inside the bulk chunk the server
just forgets a stretch nobody is looking at. Across the visible chunk it drops
what is on screen and refills it. So the application may coarsen freely *within*
a chunk and should avoid spanning two, and splitting the chunks is how the
asker tells it where that line is.

**The split is most of the signal.** Stating a small range separately already
says *this one matters*; nothing further has to be spelled. A word naming a
chunk's hotness may be worth adding later, but it is decoration on top of the
shape, and an application is free to ignore it.

**Volunteering values.** If a changed record is inside a hot range that covers
the changed field, sending the new values along with the notice saves the round
trip and keeps the watermark whole. If it is outside, the application says stale
and sends nothing. The hot range is exactly what makes that a judgement rather
than a guess — and the rule that goes with it is: **never volunteer values for
fields nobody asserted an interest in.** That is the door unwanted traffic comes
through.

This is why it is not the same statement as coverage. A cache holding 5000 rows
*depends* on all 5000 and must be told when any of them stops being true; it
does not want the new values for 4990 of them pushed at it. Coverage says what
must be forgotten; a hot range says what is worth sending unasked.

## Invalidation

*(Built, in serval's `invalidate.go`, as everything below the wire. The handle
and the evidence are the wire's half and are not.)*

**A notice says where and why**, and the why is what decides what it costs. A
bare "forget this" throws away the one thing that settles whether the order
moved or only the values did, and those are two caches with two prices.

**The extent is what preserves the watermark.** *This sequence is stale* costs
the whole of it; *this sequence, between these two records* costs only that
slice, and the completeness claim survives on both sides of it. Too wide is
safe, too narrow is silent corruption — the same rule as everywhere here.

**The reason is a field-name lookup, not an understanding.** Because the
coverage statement groups its fields by role, a change is classified by asking
which group the changed field is in — `Roles.Decides` is that question. Four
reasons, and what each costs:

| notice | the values | the order |
|---|---|---|
| **added** | nothing is known of it | the claim across that place is cut, or an open one pulled back |
| **removed** | forgotten | unlinked, **and the claim survives** |
| **replaced** | forgotten | the run goes: it may have moved |
| **altered**, naming fields | those fields forgotten | the run goes where a named field decides the sequence, and nothing where none does |

**A deletion is cheaper than a move**, and it is what most repays telling the
two apart. A record taken out of the *middle* of a run normally breaks the claim
across it — which is why eviction only ever takes from an end: the record is
still in the sequence, so a run that had dropped it from the middle would be
claiming across a gap. A record that has *left* the sequence is different:
everything still in the sequence between the run's ends is still here, so the
claim is as true as it was and the run stays whole. The same operation on the
links, told apart by the reason.

**Everything else is conservative.** A record that may have moved takes its run
with it rather than the run being cut around it: a cut would leave a boundary
anchored to a record that is no longer where it says, and a boundary that lies
is worse than a run that is gone.

**One notice, one sequence.** A stretch is a stretch *of an order* — "between
these two records" means nothing without one, and a stretch of the by-name
sequence is not a stretch of the by-size sequence over the same records. What it
*costs*, though, is not per sequence: `.size` changing moves a record in the
by-size order and repaints it in the by-name one, so the same notice is a lost
run here and nothing at all there. The values are forgotten once, for the
source; the order is forgotten per sequence, or not at all.

**A stretch the holder cannot walk is answered bluntly.** What falls between two
identities is the sequence's own answer, and a run is that answer written down;
a stretch whose first record is not placed, or whose run stops before the last,
names records that cannot be named. Answering for the ones that can be would
leave the rest held and wrong, so instead the sequence's order goes and every
record held of that source with it. It is a real price, and it is the source's
to avoid by naming stretches the reader is actually holding — which is what
coverage is for.

**Over-invalidating is always safe; under-invalidating never is.** So a source
may coarsen freely — name a stretch, or name no extent at all, which is the
whole sequence — and the blunt implementation is a legal one rather than a
failure.

**Points coalesce into a range by the same rule extents do**: merge when the gap
between them is small relative to the span they cover. Dense scatter becomes one
range, sparse scatter stays a list of points, and sixteen changes are reported as
however many pieces their spacing actually warrants. Both ends applying the same
heuristic is the point of writing it down — neither is then surprised by the
other's granularity.

**Invalidation costs nothing until someone looks.** What has stopped being true
is let go of, the guarantee is pulled back to what is still true, and no fill is
issued. Whether a replacement is ever requested is somebody else's decision: a
stretch scrolled out of view an hour ago may never be read again, while
something on screen refreshes at once. Invalidation causes *forgetting*, not
traffic.

A consequence worth naming, because it removed code: **a held value is never
replaced, only forgotten.** Two answers about one record of one source do not
contradict each other, so filing an answer only ever adds to what is known —
and a volunteered value is a forget followed by a file, not an overwrite.

**The evidence never crosses the wire.** A handle is a peg the application hangs
whatever it has on: a hash of a directory's entries, an mtime, a row version, an
ETag, a generation counter. It re-hashes on suspicion, compares, and reports the
handle. The protocol only ever sees *handle 17 is stale*, which is what lets
this work over backends that have nothing in common.

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
the visible chunk, whose boundaries are worth restating once the scrolling has
settled rather than while it is in motion.

## Eviction: the order and the values

This document used to say the **skeleton** was the sort and filter fields and
the **flesh** everything else, which is a split of one record's field list. What
got built is a different cut, and the names survived it.

**The skeleton is the ORDER**: runs of places, each one a record's identity and
what stands either side of it, with no field values in them at all. **The flesh
is what records hold**, kept once per SOURCE and read by every sequence over it.

The improvement is that the order does not need the values that decided it. Once
a stretch has been read, which record comes after which is a fact; the `.name`
it was sorted by is only how that fact was arrived at. So a place costs an
identity and two links rather than however many fields, and a quarter of the
cache's room buys a great deal of sequence — which is the right share, because a
long sequence is what a reader scrolling has and what re-asking for is most
expensive.

The old section's conclusions survive it, and so does the reasoning:

- **The flesh outlives the order.** Close one sort and open another and every
  value the new order needs is still here, keyed by identity. That is the
  re-sort case: no data transfer for anything held whole.
- **The order outlives the flesh.** A run whose records have been evicted still
  knows what comes after what, so the fields can be asked for again for exactly
  those records rather than the stretch being walked from the start. That is the
  projection-only fill this document predicted.
- So neither pins the other. Two caches, two limits, two sets of books, two
  evictions — and a place holds an identity rather than a pointer, or the values
  would stay on the heap after the cache had let go of them, which is the one
  thing a cost coming off the books is supposed to mean.

**Evict flesh first** still holds, for the reason it always did: losing the
order means a stretch has to be re-read and its completeness claim
re-established, and losing the values means a projection-only fill for the rows
somebody is actually looking at.

Where the old wording still applies exactly is to **watching**. The fields that
DECIDE a sequence are the ones a backend can usually watch cheaply — the indexed
columns, the mtime, the thing a trigger fires on — and the rest is the blob you
would have to read in order to know it had changed. That is a statement about
which fields, and it is `Roles.Decides`; it is not a statement about which cache
holds what.

**Which makes the steady state two concerns**: the order over a wide extent —
everything cached — and all requested fields over a narrow one, what is on
screen. Two extents, two field sets. Small enough that even a careful
application has almost nothing to track, and it lines up with what a real
backend can actually watch.

## What stays policy

*When* to evict — age, memory pressure, how much hysteresis before a shrink — is
server policy, not protocol. Stating it as policy lets two implementations
differ without either being wrong.

So are the quantization intervals on both ends, and where the gap-to-span line
falls when points coalesce. What the protocol fixes is that coarsening is safe
and that a chunk boundary is the one place not to coarsen across; how coarsely
either end chooses to speak is its own.

## Answered since this was written

- **Scrolling backwards.** Built, and it needed no low watermark of its own — see
  *The scope itself*.
- **Whether a handle can cover fields with no extent** — "any change to `size`,
  anywhere". Yes, and it is the cheap case rather than the weak one: a notice
  with no extent is the whole sequence, and where the named field decides
  nothing it costs the order nothing and the values one field per record. An
  application that can say *something about size changed and I cannot say where*
  is better off saying that than invalidating everything.
- **Counts as deltas or absolutes.** Neither, and there was nothing to invent:
  the NOTICES are the deltas. A source reporting an append has already said
  *+1* by saying `added`, and one reporting a deletion has said *-1*, so there
  is no second channel and nothing to resynchronise — the figure only ever moves
  on a statement that was being sent anyway. What needed deciding instead was
  what a *doubtful* notice costs, and the answer is that it lowers a floor
  rather than discarding the figure. Which is why a count is exact-or-at-least
  rather than exact-or-unknown.

## Open questions

- **The jump into an uncovered middle**, when the user drags the thumb to
  nowhere in particular and there is no watermark to stand on. The library half
  is settled — a scope starting at a record no run places is simply a miss, and
  the source is asked — but what the display does while it waits is not.
- **How a hot range translates down a composed source.** A range is asserted
  against a data set and has to reach the leaf that really holds the records.
  Identities can be back-traced up a composite graph, and a composed source
  already keeps the per-include cursor vector, so the machinery is there; how it
  is spelled is not.
- **A name for what a reader holds.** Position, the sequence being read, the
  watermark and the coverage are per-reader, and the thing that carries them is
  not the result set and not the query — several readers share one of each. It
  needs a word of its own before anything can be written about it precisely.
