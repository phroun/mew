# Sorting and filtering

> **Status: settled.** The comparison core, the sort spec and the filter
> grammar below are decided and meant to be implemented against. The protocol
> that carries them — how a view is opened, filled and folded — is still being
> designed; see `data-sources-and-bundles.md` for that conversation.

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

An ordered run of levels. Each level names a field, and may say `desc` and,
for strings, a collation:

```
sort=( (name, fold), (size, desc), (modified, desc) )
```

The first level that separates two records decides. A level settles only what
the levels above it left equal, so "by department, then by salary, highest
first" is two levels and the answer never depends on what the records were
sorted by before.

**The record's key is always an implicit final level**, ascending, compared as
the key's own type. It is not decoration: without a total order, "the record
after this point" names more than one place, two fills of a window can overlap
or skip, and a fold of two ordered runs is not reproducible. The key level is
what makes a position mean exactly one record.

## A filter

A tree in prefix form, written as a PSL list. No infix, no precedence, nothing
to parse beyond PSL itself — an implementation is a switch on the first item:

```
filter=( and,
         ( eq, kind, folder ),
         ( ge, size, 1024 ),
         ( not, ( starts, name, "." ) ) )
```

| | |
|---|---|
| `eq` `ne` `lt` `le` `gt` `ge` | comparison, by the core above |
| `contains` `starts` `ends` | strings only, collation-aware |
| `and` `or` `not` | any number of operands |

`eq kind folder` and `eq kind "folder"` are different questions, because a
symbol and a string are different values. The data says which it is.

**There is no presence operator.** `undefined` is a value with a rank, so
`( eq, thumbnail, undefined )` already asks whether a record has the field —
one less thing to specify and one less thing to learn.

For the same reason there is no `nulls first` knob: `undefined` sits at the
bottom of the rank, and `desc` lifts it to the top along with everything else.

## Agreement is settled at open, not discovered later

A view's sort and filter are stated once, when it is opened. An end that cannot
honour them exactly — a field it does not have, an op it does not implement, a
collation it does not carry — **refuses to open the view**.

A refusal is recoverable and says what is wrong. An ordering that is quietly a
little different corrupts every answer after it, and looks like data.
