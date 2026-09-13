# Sorting and filtering

> **Status: settled.** The comparison core, the sort spec and the filter
> grammar below are decided, and all three client libraries read them into a
> structure (`hosting-a-query.md`). The protocol that carries them — how a view
> is opened, filled and folded — is still being designed; see
> `live-data-negotiation.md` for that.

A view's records can come from more than one place at once: some held here,
some known only to an application at the other end of a connection. Neither end
can hand the other its whole set, so each orders and filters its own records
and the results are folded together.

That only works if both ends compute **the same order** and **the same
membership** from the same spec, without conferring. So the rules have to be
exact, small enough to implement twice, and free of anything whose answer
depends on the machine, the locale or the library version.

## The comparison core

Sorting and filtering are the same comparison rules used two ways. Define
comparison once and both follow.

### Ranks

A value's type decides its rank before anything in it is looked at:

```
undefined < nil < false < true < int|float < symbol < string < bytes < unordered
```

`int` and `float` share one rank and interleave by value, so `2` and `2.0`
sort together rather than into separate groups.

The last rank holds every value with no order of its own — a block, a token.
All of them compare equal, and the sort's final level settles them. Nothing
here pretends a block has a position it does not have.

PawScript's complex types (macro, command, list, struct, structarray, channel,
file, fiber) are not serializable and so cannot be a record's field value at
all; they never reach this comparison.

### Within a rank

| rank | compared by |
|---|---|
| `undefined`, `nil`, `false`, `true` | equal to themselves; the rank is the whole answer |
| `int`, `float` | mathematical value. Integers compare as integers and widen to float only when one side is genuinely a float, so an id or a nanosecond timestamp past 2^53 still compares exactly |
| `symbol` | rune by rune, exact, **no collation**: a symbol is an identifier, and folding its case or reading digit runs inside it would invent meaning it does not have |
| `string` | rune by rune, under the level's collation |
| `bytes` | unsigned byte comparison |

### Collations

A collation belongs to the string rank alone.

| | |
|---|---|
| `exact` | codepoint order, rune by rune. The default, and what PawScript does today |
| `fold` | ASCII `A`–`Z` mapped to `a`–`z`, every other rune untouched, then `exact` |
| `natural` | runs of ASCII digits compared as numbers, everything else `fold` |

`natural` in full, so two implementations agree: walk both strings together; at
a position where both have a run of ASCII digits, take the maximal run from
each and compare them as numbers with leading zeros ignored; if they are equal
in value, the run with fewer leading zeros sorts first; anywhere else compare
one rune against one rune under `fold`. A run too long to hold in an integer is
compared by its length after leading zeros are stripped, then rune by rune —
which is the same answer without needing arbitrary precision.

**Locale-aware collation is deliberately absent.** Turkish dotless i, umlaut
placement, the Unicode collation algorithm: none of it is reproducible in
twenty lines in every language a client might be written in, and a near-miss
does not fail — it produces a wrong fold that nobody notices. An application
that needs its own locale order computes a sort field and sorts on that, which
is exact and needs no agreement at all.

## A sort

An ordered run of levels, written as a block. Each level's statement names a
field, and may say `desc` and, for strings, a collation:

```
sort={ name fold; size desc; modified desc }
```

**A field name may begin with a dot**, which is how a name says it is a member
of the record rather than a word in its own right: `.size` is the member called
size, `size` is the word size. A source whose records carry their own contents
needs the distinction — otherwise a member called `key` and the record key are
the same name — and one that does not need it never writes a dot.
`psl-as-a-data-source.md` is where it is put to work.

**A symbol is a bare token that is not a number**, which is decided by what the
token says rather than by what it starts with: `2026-09-13` is a symbol,
`1e+21` is a number, and a token written with a leading sign is a number and
nothing else. The numeric form is
`[+-]? digits ( "." digits )? ( [eE] [+-]? digits )?`, written out rather than
handed to each language's own number parser — those accept different sets of
extras (infinities, hexadecimal floats, digit separators), and three
implementations have to agree on exactly where a number stops and a symbol
begins.

**A symbol with no bare spelling is bracketed**: `(objectLibrary/figaro/3)`,
`(*star)`. Parentheses because that is what they already mean — PawScript
evaluates a block written in braces and preserves what is written in
parentheses, and the wire's block is braces too, so both languages say the same
thing with the same brackets. There are no escapes inside and none are needed: a
symbol cannot hold a closing parenthesis in PawScript either, and a newline is
refused for the same reason it ends a statement.

The first level that separates two records decides. A level settles only what
the levels above it left equal, so "by department, then by salary, highest
first" is two levels and the answer never depends on what the records were
sorted by before.

**The record's key is always an implicit final level**, ascending, compared as
the key's own type. It is not decoration: without a total order, "the record
after this point" names more than one place, two fills of a scope can overlap
or skip, and a fold of two ordered runs is not reproducible. The key level is
what makes a position mean exactly one record.

## A filter

A block of statements. No infix, no precedence, and nothing to parse that the
wire language does not already parse — an implementation is a switch on the
verb:

```
filter={
  eq kind folder
  ge size 1024
  not { starts name "." }
}
```

**A block is an AND**: every statement in it must hold. That is the common
case, so it costs no nesting, and `or { … }`, `and { … }` and `not { … }` are
statements in their own right when the shape is not a plain conjunction:

```
filter={
  or { eq kind folder; eq kind disk }
  ge size 1024
}
```

A predicate is `<op> <field> <value>`: the operator is the verb, the field is a
bare word, and the value is an operand. Values written with no name are read in
the order they were written, which is what lets a filter read as a filter
instead of inventing an argument name for every operand.

| | |
|---|---|
| `eq` `ne` `lt` `le` `gt` `ge` | comparison, by the core above |
| `in` | one field against a set: `in kind { folder; disk }` |
| `contains` `starts` `ends` | strings only, collation-aware |
| `has` `lacks` | whether the record carries the field at all; no value |
| `and` `or` `not` | a block of predicates |

A text op carries its collation where it differs from the default:
`contains title "report" collate=fold`.

**The text ops are text's alone.** A number, a symbol and a word have no inside
for a string to sit in, so a field that is not the same flavour of string as the
value is `false` rather than being rendered into one to compare — which is the
same decision as `eq kind folder` and `eq kind "folder"` being different
questions. Bytes take no collation, being not text; and under a text op
`natural` reads as `fold`, because a digit run's numeric value says whether one
string sorts before another and nothing at all about whether it sits inside it.

**A block is an AND wherever one appears**, `not { … }` included, so
`not { a; b }` is the negation of `a and b`. With one predicate inside, which is
how a negation is nearly always written, the two readings agree. An operator
given nothing is its own identity: an empty `and` holds and an empty `or` does
not.

`eq kind folder` and `eq kind "folder"` are different questions, because a
symbol and a string are different values. So a filter needs no type
annotations: the value's own spelling says what it is.

**A comparison takes a simple value**, and the last rank is not one. A field
holding something with no order of its own — a nested list — cannot be compared
against anything, so **every comparison naming one is false**.

That is a rule the filter has and the sort does not. The comparison core calls
all such values equal, which is exactly what a sort needs: they tie, and the
record key settles them. A filter inheriting it would answer *yes* to
`eq tags { … }` for any record carrying any tags at all, having looked inside
nothing — a false positive, in the direction a filter should never fail. It
cannot answer, so it does not admit.

A block is refused as an operand for the same reason: `eq tags { a; b }` is not
a question this grammar asks. A block after a field is a **set**, and a set is
only something `in` can be asked about.

**`has` and `lacks` are how presence is asked**, and they take no value. They
work whatever the field holds, which is the point — `eq thumbnail undefined`
still answers for a field holding a simple value, but it cannot serve a field
holding a list, and presence is not a comparison.

There is no `nulls first` knob: `undefined` sits at the bottom of the rank, and
`desc` lifts it to the top along with everything else.

## Agreement is settled at open, not discovered later

A view's sort and filter are stated once, when it is opened. An end that cannot
honour them exactly — a field it does not have, an op it does not implement, a
collation it does not carry — **refuses to open the view**.

A refusal is recoverable and says what is wrong. An ordering that is quietly a
little different corrupts every answer after it, and looks like data.
