"""A query, in structured form (Python port of wire/query.go).

An application hosts a query: one filter and one sort over its own records,
which a display fills scopes out of as somebody scrolls. The wire carries that
as text, and every client library would otherwise make its author walk a
statement tree to find out what was being asked. So the walking happens once,
here, and what reaches an application is an object.

docs/hosting-a-query.md is the spelling this implements, and
testdata/query.wire is the corpus every implementation of it answers.
serval's docs/ordering.md defines the comparison the sort and filter stand on;
docs/sort-and-filter.md is how the two are written down here.
"""

from __future__ import annotations

import dataclasses
from dataclasses import dataclass
from typing import List, Optional

from .protocol import (
    Arg,
    COLLATE_EXACT,
    COLLATE_FOLD,
    COLLATE_NATURAL,
    FlagState,
    Script,
    Statement,
    Value,
    ValueKind,
    encode_statement,
    encode_value,
    new_string,
    new_word,
    quote,
)

# The verb a display opens and refills a query with, and the verb the
# application answers it with.
#
# Three pairs, and nothing carries two of them: `query` is answered by
# `result`, `ask` by `answer`, and `sub` -- or an object's mere existence -- by
# `event`. So a record arriving for a list can never be mistaken for something
# a subscription raised.
QUERY_VERB = "query"      # `new query source="files" sort={ name } count=30`
RESULT_VERB = "result"    # `result 9 id=42 record={ ... }`

# ID_ARG carries a record's identity, beside its fields rather than among them.
#
# An identity is not a field. A record can hold a field called `key` and that
# field is data like any other -- it sorts, it filters, it is shown in a column
# -- while what names the record travels here.
ID_ARG = "id"

# What a result carries, and how much of the record it is.
#
# `record` is every field the record has; `fields` is some of them. The
# difference is worth a word because a whole record answers any question about
# that record, and a subset answers only the one that asked for it -- which is
# what lets an answer be kept and reused rather than asked for again.
RECORD_ARG = "record"
FIELDS_ARG = "fields"

# RESULT_COMPLETE ends a scope: everything for it has been sent.
#
# It can ride on the statement carrying the last record, and `ordered` on the
# one carrying the first, so a scope of a single record crosses as a single
# line. Both forms are read.
RESULT_COMPLETE = "complete"
WATERMARK_ARG = "watermark"
ORDERED_ARG = "ordered"
ERROR_ARG = "error"

# Why a scope ended, which the asker cannot work out for itself: a scope that
# filled and one that ran out of records look identical from the far end.
STOP_FILLED = "filled"        # the count was reached; there is more past it
STOP_JOINED = "joined"        # the walk reached `until`, joining two runs
STOP_EXHAUSTED = "exhausted"  # no more records this way, so no watermark

# The operators a filter is built from.
OP_AND = "and"
OP_OR = "or"
OP_NOT = "not"
OP_EQ = "eq"
OP_NE = "ne"
OP_LT = "lt"
OP_LE = "le"
OP_GT = "gt"
OP_GE = "ge"
OP_IN = "in"
OP_CONTAINS = "contains"
OP_STARTS = "starts"
OP_ENDS = "ends"

# OP_HAS and OP_LACKS ask whether a record carries a field at all, and take no
# value. Every other operator compares one, and a field holding something with
# no order of its own -- a nested list -- cannot be compared, so presence needs
# an operator that does not try.
OP_HAS = "has"
OP_LACKS = "lacks"

# OP_ID matches a record's identity against a set of them, the way `in` matches
# a field against a set of values. It names no field, because an identity is not
# one: it travels beside a record's fields rather than among them, and a field
# called `key` is a field like any other.
#
#     filter={ id (left/1) (left/note) }
OP_ID = "id"

_GROUPS = (OP_AND, OP_OR, OP_NOT)
_PREDICATES = (OP_EQ, OP_NE, OP_LT, OP_LE, OP_GT, OP_GE, OP_IN,
               OP_CONTAINS, OP_STARTS, OP_ENDS, OP_HAS, OP_LACKS)


class QueryError(ValueError):
    """A query, or part of one, that will not be read."""


class Fields(list):
    """A bag of named values, and one shape serves three jobs: the fields a
    record carries, the position a boundary stands at, and -- with the values
    left out -- the bare list of fields a query asks for.

    It is written as a block of one statement per field, the field name first
    and its value, if it has one, after: `{ name "src/parser.go"; size 1024 }`.
    """

    def get(self, name: str) -> Optional[Value]:
        """The value under a name, or None where the bag does not name it. A
        field present with no value reads as None too: a bare name is a name,
        not a value of its own."""
        for a in self:
            if a.name == name:
                return a.value
        return None

    def has(self, name: str) -> bool:
        """Whether the bag names a field at all, valued or not."""
        return any(a.name == name for a in self)

    def names(self) -> List[str]:
        """The fields, in the order they were written."""
        return [a.name for a in self]

    def block(self) -> Value:
        """The bag as a block value, for a statement to carry."""
        script = Script()
        for a in self:
            st = Statement(verb=a.name)
            if a.value is not None:
                st.args = [Arg(value=a.value)]
            script.statements.append(st)
        return Value(kind=ValueKind.BLOCK, block=script)

    def encode(self) -> str:
        """The text of that block."""
        return encode_value(self.block())


@dataclass
class Filter:
    """One node of the tree a filter block parses to: a predicate over one
    field, or an and/or/not over other nodes.

    A block is an AND, so the top of a parsed filter is always an OP_AND -- one
    shape to walk, whether the filter held one predicate or twenty."""

    op: str = OP_AND
    field: str = ""                             # predicates: the field tested
    values: List[Value] = dataclasses.field(default_factory=list)  # more than one only for `in`
    collate: str = ""                           # text predicates; "" is the default
    children: List["Filter"] = dataclasses.field(default_factory=list)

    def value(self) -> Optional[Value]:
        """The single operand of a comparison, and None where there is none."""
        return self.values[0] if self.values else None

    def encode(self) -> str:
        """A filter node as wire text: a block for the top of a tree."""
        if self.op in _GROUPS:
            return self._encode_block()
        return "{ " + self._encode_statement() + " }"

    def _encode_block(self) -> str:
        parts = [c._encode_statement() for c in self.children]
        return "{ " + "; ".join(parts) + " }" if parts else "{}"

    def _encode_statement(self) -> str:
        if self.op in _GROUPS:
            return self.op + " " + self._encode_block()
        if self.op == OP_ID:
            # Every other operator names the field it tests. This one tests an
            # identity, which is not a field and has no name to write.
            return self.op + ''.join(" " + encode_value(v) for v in self.values)
        out = [self.op, " ", self.field]
        for v in self.values:
            out.append(" " + encode_value(v))
        if self.collate:
            out.append(" collate=" + self.collate)
        return ''.join(out)


@dataclass
class SortLevel:
    """One level of a sort: which field, which way, and -- for strings -- under
    which collation."""

    field: str = ""
    descending: bool = False
    collation: str = ""


@dataclass
class Spec:
    """What sequence a query names: which records, in which order.

    It is stated once, when the query is made, and never again. A query is the
    sequence it was opened with and nothing restates it -- a different filter
    or a different sort is a different sequence, which is a different query,
    opened alongside this one and taking its place."""

    source: str = ""
    fields: Fields = dataclasses.field(default_factory=Fields)
    exclude: Fields = dataclasses.field(default_factory=Fields)
    filter: Optional[Filter] = None
    sort: List[SortLevel] = dataclasses.field(default_factory=list)

    def encode(self) -> str:
        """The spec as the arguments of the statement that carries it."""
        parts = []
        if self.source:
            parts.append("source=" + quote(self.source))
        if self.fields:
            parts.append("fields=" + self.fields.encode())
        if self.exclude:
            parts.append("exclude=" + self.exclude.encode())
        if self.filter is not None:
            parts.append("filter=" + self.filter.encode())
        if self.sort:
            parts.append("sort=" + encode_sort(self.sort))
        return " ".join(parts)


@dataclass
class Scope:
    """The run of records a query asks for: where to start, which way to walk,
    how many, and where the asker's own knowledge picks up again.

    It is not a filter and it names no field. The sequence is already decided
    by the spec, and a scope only says which part of it to read -- so a source
    prepares one ordering and serves every scope of it cheaply, rather than
    preparing a new one because the reader scrolled.

    after and until are identities, not positions. An identity means something
    only to the source that issued it, which is why a source made of several
    others never passes one down: it hands each of them that one's own.

    reversed walks the sequence from its end rather than its beginning. Every
    level turns over, the one the sort does not write included -- an identity
    settles what the named levels leave equal, and a sequence read backwards
    settles it backwards too. It belongs to the scope rather than the sequence
    because it costs nothing: one prepared ordering is read either way."""

    after: Optional[Value] = None
    until: Optional[Value] = None
    count: int = 0
    reversed: bool = False

    def encode(self) -> str:
        """The scope as the arguments that carry it."""
        parts = []
        if self.after is not None:
            parts.append("after=" + encode_value(self.after))
        if self.until is not None:
            parts.append("until=" + encode_value(self.until))
        parts.append("count=%d" % self.count)
        if self.reversed:
            parts.append("reversed")
        return " ".join(parts)


@dataclass
class Complete:
    """What ends a scope: which of the three ways it ended, and how far the
    answer is complete.

    watermark says there is nothing between where the scope was asked from and
    that record that the asker does not now have. STOP_EXHAUSTED carries none,
    because there is no point past the end to be complete up to."""

    watermark: Optional[Value] = None
    stop: str = ""
    error: str = ""


@dataclass
class Result:
    """One `result` statement taken apart: the order declaration, a record, and
    the terminator, any of which may be absent.

    All three can ride on one statement. `ordered` is worth saying only before
    the first record, and the terminator only after the last, so an answer of
    one record carries all three and crosses as a single line. A reader takes
    them in that order -- order, then record, then end -- whichever statement
    they arrived on."""

    ordered: bool = False
    id: Optional[Value] = None   # nil where no record rides here
    fields: Fields = dataclasses.field(default_factory=Fields)
    whole: bool = False          # `record=` rather than `fields=`
    complete: Optional[Complete] = None

    def args(self) -> List[Arg]:
        """The result as the arguments after the query id."""
        out: List[Arg] = []
        if self.ordered:
            out.append(Arg(name=ORDERED_ARG, flag=FlagState.TRUE))
        if self.id is not None:
            out.append(Arg(name=ID_ARG, value=self.id))
            what = RECORD_ARG if self.whole else FIELDS_ARG
            out.append(Arg(name=what, value=self.fields.block()))
        c = self.complete
        if c is not None:
            out.append(Arg(name=RESULT_COMPLETE, flag=FlagState.TRUE))
            if c.watermark is not None:
                out.append(Arg(name=WATERMARK_ARG, value=c.watermark))
            if c.stop:
                out.append(Arg(name=c.stop, flag=FlagState.TRUE))
            if c.error:
                out.append(Arg(name=ERROR_ARG, value=new_string(c.error)))
        return out


def parse_fields(v: Optional[Value]) -> Fields:
    """A field bag, from a block value."""
    if v is None or v.kind != ValueKind.BLOCK:
        raise QueryError("expected a block of fields")
    out = Fields()
    for st in v.block.statements:
        if not st.verb:
            raise QueryError("a field is a name, and %r is not one" % encode_statement(st))
        if not st.args:
            out.append(Arg(name=st.verb, flag=FlagState.TRUE))
        elif len(st.args) == 1:
            out.append(Arg(name=st.verb, value=_operand_value(st.verb, st.args[0])))
        else:
            raise QueryError("%s: a field carries one value, not %d" % (st.verb, len(st.args)))
    return out


def _operand_value(what: str, a: Arg) -> Value:
    """One operand as a value.

    A bare word in argument position is a flag as far as the grammar is
    concerned -- that is what `wrap` and `!enabled` are -- so a word written
    where a value belongs arrives as a flag carrying its own name, and this is
    where it becomes the word it was written as. `!` and `?` say something a
    value cannot, so they are refused rather than quietly read as words."""
    if a.value is not None:
        if a.name:
            raise QueryError("%s: no argument called %r" % (what, a.name))
        return a.value
    if a.flag != FlagState.TRUE:
        raise QueryError("%s: %r is asserted, not valued" % (what, a.name))
    return new_word(a.name)


def parse_spec(args: List[Arg]) -> Spec:
    """A query spec, from the arguments of the statement carrying it."""
    s = Spec()
    for a in args:
        if a.name == "source":
            if a.value is None:
                raise QueryError("source: expected a name")
            if a.value.kind == ValueKind.STRING:
                s.source = a.value.str
            elif a.value.kind == ValueKind.WORD:
                s.source = a.value.word
            else:
                raise QueryError("source: expected a name")
        elif a.name in ("fields", "exclude"):
            try:
                bag = parse_fields(a.value)
            except QueryError as e:
                raise QueryError("%s: %s" % (a.name, e))
            setattr(s, a.name, bag)
        elif a.name == "filter":
            try:
                s.filter = parse_filter(a.value)
            except QueryError as e:
                raise QueryError("filter: %s" % e)
        elif a.name == "sort":
            try:
                s.sort = parse_sort(a.value)
            except QueryError as e:
                raise QueryError("sort: %s" % e)
    return s


def parse_scope(args: List[Arg]) -> Scope:
    """The scope, from the same arguments the spec was read from.

    The two travel together -- `new query` states the sequence and asks for a
    run of it in one statement -- and they are read apart because they are
    different things: the spec is what the query is, and the scope is what this
    one question wanted."""
    s = Scope()
    for a in args:
        if a.name == "count":
            if a.value is None or a.value.kind != ValueKind.NUMBER or not a.value.is_int:
                raise QueryError("count: expected a whole number")
            if a.value.number < 0:
                raise QueryError(
                    "count: %d records is not a number of records" % a.value.number)
            s.count = int(a.value.number)
        elif a.name in ("after", "until"):
            if a.value is None:
                raise QueryError("%s: expected an identity" % a.name)
            if a.value.kind == ValueKind.BLOCK:
                raise QueryError("%s: an identity is a value, not a block" % a.name)
            setattr(s, a.name, a.value)
        elif a.name == "reversed":
            if a.value is not None:
                raise QueryError("reversed: it takes no value")
            s.reversed = a.flag == FlagState.TRUE
    return s


def parse_result(args: List[Arg]) -> Result:
    """A result, from the arguments after the query id."""
    r = Result()
    done = Complete()
    ended = False
    for a in args:
        if a.name == ORDERED_ARG:
            r.ordered = True
        elif a.name == ID_ARG:
            if a.value is None or a.value.kind == ValueKind.BLOCK:
                raise QueryError("id: expected an identity")
            r.id = a.value
        elif a.name in (RECORD_ARG, FIELDS_ARG):
            try:
                r.fields = parse_fields(a.value)
            except QueryError as e:
                raise QueryError("%s: %s" % (a.name, e))
            r.whole = a.name == RECORD_ARG
        elif a.name == RESULT_COMPLETE:
            ended = True
        elif a.name == WATERMARK_ARG:
            if a.value is None or a.value.kind == ValueKind.BLOCK:
                raise QueryError("watermark: expected an identity")
            done.watermark = a.value
            ended = True
        elif a.name == ERROR_ARG:
            if a.value is None or a.value.kind != ValueKind.STRING:
                raise QueryError("error: expected a message")
            done.error = a.value.str
            ended = True
        elif a.name in (STOP_FILLED, STOP_JOINED, STOP_EXHAUSTED):
            done.stop = a.name
            ended = True
    if r.fields and r.id is None:
        raise QueryError("a record carries an identity; this one has none")
    if ended:
        r.complete = done
    return r


def parse_filter(v: Optional[Value]) -> Filter:
    """A filter tree, from a block value. A block is an AND."""
    if v is None or v.kind != ValueKind.BLOCK:
        raise QueryError("expected a block")
    return _parse_filter_block(v.block, OP_AND)


def _parse_filter_block(script: Script, op: str) -> Filter:
    node = Filter(op=op)
    for st in script.statements:
        node.children.append(_parse_predicate(st))
    return node


def _parse_id(st: Statement) -> Filter:
    """`id <identity> <identity> ...`.

    Every argument is a value, with no field in front of them: an identity is
    not a field, so there is nothing to name. That is also what keeps it apart
    from `in key ...`, which is a question about a field that happens to be
    called key."""
    f = Filter(op=OP_ID, children=[])
    for a in st.args:
        if a.value is None:
            raise QueryError(
                "id: %r names no identity; write the identities as values" % a.name)
        if a.name:
            raise QueryError("id: takes identities, not %s=" % a.name)
        if a.value.kind == ValueKind.BLOCK:
            raise QueryError("id: an identity is a value, not a block")
        f.values.append(a.value)
    if not f.values:
        raise QueryError("id: takes at least one identity")
    return f


def _parse_predicate(st: Statement) -> Filter:
    if st.verb in _GROUPS:
        if len(st.args) != 1 or st.args[0].value is None \
                or st.args[0].value.kind != ValueKind.BLOCK:
            raise QueryError("%s: takes one block" % st.verb)
        inner = _parse_filter_block(st.args[0].value.block, st.verb)
        if st.verb == OP_NOT and not inner.children:
            raise QueryError("not: takes something to negate")
        return inner
    if st.verb == OP_ID:
        return _parse_id(st)
    if st.verb not in _PREDICATES:
        raise QueryError("no filter operator called %r" % st.verb)

    f = Filter(op=st.verb, children=[])
    for i, a in enumerate(st.args):
        if a.name == "collate":
            if a.value is None or a.value.kind != ValueKind.WORD:
                raise QueryError("%s: collate= expects a word" % st.verb)
            f.collate = a.value.word
        elif i == 0:
            # The field, written bare, which is how a filter reads as a filter
            # rather than naming an argument for every operand.
            if a.value is not None or a.flag != FlagState.TRUE:
                raise QueryError("%s: names no field" % st.verb)
            f.field = a.name
        elif a.value is not None and a.value.kind == ValueKind.BLOCK and f.op != OP_IN:
            # A comparison takes a simple value. A block is a set, and a set is
            # only something `in` can be asked about.
            raise QueryError("%s %s: compares against a value, not a block"
                             % (st.verb, f.field))
        elif a.value is not None and a.value.kind == ValueKind.BLOCK and f.op == OP_IN:
            # A set of words, which is what a block can hold: every statement
            # in it is one bare name.
            for item in a.value.block.statements:
                if not item.verb or item.args:
                    raise QueryError(
                        "in: a set holds bare names; write other values after the field")
                f.values.append(new_word(item.verb))
        else:
            f.values.append(_operand_value(st.verb, a))
    if not f.field:
        raise QueryError("%s: names no field" % st.verb)
    if f.op in (OP_HAS, OP_LACKS):
        if f.values:
            raise QueryError("%s %s: asks whether the field is there, and takes no value"
                             % (f.op, f.field))
        return f
    if not f.values:
        raise QueryError("%s %s: nothing to compare against" % (st.verb, f.field))
    if f.op != OP_IN and len(f.values) > 1:
        raise QueryError("%s %s: compares against one value, not %d"
                         % (st.verb, f.field, len(f.values)))
    return f


def parse_sort(v: Optional[Value]) -> List[SortLevel]:
    """Sort levels, from a block value: one statement per level, naming a field
    and saying which way and under which collation."""
    if v is None or v.kind != ValueKind.BLOCK:
        raise QueryError("expected a block")
    out: List[SortLevel] = []
    for st in v.block.statements:
        if not st.verb:
            raise QueryError("a level names a field")
        level = SortLevel(field=st.verb)
        for a in st.args:
            if a.name == "collate" and a.value is not None \
                    and a.value.kind == ValueKind.WORD:
                level.collation = a.value.word
            elif a.value is not None:
                raise QueryError("%s: no argument called %r" % (st.verb, a.name))
            elif a.name == "desc":
                level.descending = a.flag == FlagState.TRUE
            elif a.name == "asc":
                level.descending = a.flag != FlagState.TRUE
            elif a.name in (COLLATE_EXACT, COLLATE_FOLD, COLLATE_NATURAL):
                level.collation = a.name
            else:
                raise QueryError("%s: %r says nothing about a sort level"
                                 % (st.verb, a.name))
        out.append(level)
    return out


def encode_sort(levels: List[SortLevel]) -> str:
    """Sort levels as a block."""
    parts = []
    for l in levels:
        s = l.field
        if l.collation:
            s += " " + l.collation
        if l.descending:
            s += " desc"
        parts.append(s)
    return "{ " + "; ".join(parts) + " }" if parts else "{}"
