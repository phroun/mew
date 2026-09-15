# Writing a sort and a filter

> **Status: settled.** The spelling below is decided, and all three client
> libraries read it into a structure (`hosting-a-query.md`).
>
> **What a sort and a filter MEAN is not here.** The ranks, the collations, what
> each operator answers, and the rule that a sequence refuses to open rather
> than approximate — those are serval's, and are written down in that library's
> `docs/ordering.md`. They are a data question: two holders of one sequence have
> to compute the same order and the same membership without conferring, and that
> is true whether or not either of them ever speaks to the other over a wire.
>
> This document is the other half: how those two things are WRITTEN in the
> KittyTK Wire Language, so that what crosses reads back as what was meant.

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

**A field name may be an index.** A record's field names belong to the data, and
a positional member's name is its position: `sort={ 0 natural }` orders by the
first member. A block is a list of statements everywhere else in this language,
so this is the one place a statement's head is not a word — and the digits stand
as they are written, `007` and `7` being two different names.

A filter names its field in ARGUMENT position rather than in the head, where a
bare `0` is the number zero. So an index reaches a sort and does not yet reach a
filter; changing that means changing how a predicate decides which of its
operands is the field.

## A field list

The names a query asks for, and the ones it does not want, are written two ways.

A **block** carries names and values together, which is what a record is:

```
record={ name "src/parser.go"; size 1024 }
```

A **string** carries names alone, separated by commas, which is what a query
narrowing a record is:

```
fields=".name, .size"   exclude="blob"   fields="0, 1"
```

Comma because that is what a list is separated by. `;` is where a statement
ends, one level up, and would be doing a second job inside the quotes.

Both are read and the block is what is written back, so a name holding a comma —
which the string form cannot spell — still crosses. An empty list is a list of
nothing; an empty *name* is refused, because a stray comma is a typo and
dropping it would narrow a query by one field and say nothing.

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

A view's sort and filter are stated once, when it is opened, and an end that
cannot honour them exactly refuses. That rule and its reasoning belong with the
comparison itself — see serval's `docs/ordering.md`. What matters here is only
that a refusal is a statement like any other and says what is wrong.
