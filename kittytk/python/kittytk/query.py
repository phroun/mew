"""A query, in structured form (Python port of wire/query.go).

An application hosts a query: one filter and one sort over its own records,
which a display fills windows out of as somebody scrolls. The wire carries that
as text, and every client library would otherwise make its author walk a
statement tree to find out what was being asked. So the walking happens once,
here, and what reaches an application is an object.

docs/hosting-a-query.md is the spelling this implements, and
testdata/query.wire is the corpus every implementation of it answers.
docs/sort-and-filter.md defines the comparison the sort and filter stand on.
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
    new_word,
    quote,
)

# The verb an application announces a query with, and the question the display
# puts to it afterwards.
QUERY_TYPE = "query"
ASK_FILL = "fill"
KEY_FIELD = "key"

# The events a query answers with. Both name the query they belong to and carry
# back the tag of the fill they answer.
EVENT_QUERY_RECORD = "query_record"
EVENT_QUERY_FILLED = "query_filled"

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

_GROUPS = (OP_AND, OP_OR, OP_NOT)
_PREDICATES = (OP_EQ, OP_NE, OP_LT, OP_LE, OP_GT, OP_GE, OP_IN,
               OP_CONTAINS, OP_STARTS, OP_ENDS)


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

    def key(self) -> Optional[Value]:
        """The record key: the field every record and every boundary carries,
        because it is the sort's implicit last level and what makes a position
        mean exactly one record."""
        return self.get(KEY_FIELD)

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
    """What sequence a query names. It is stated when the query is announced
    and restated when the display changes it, which is a new generation of the
    same query rather than a new query."""

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
class Fill:
    """One window of the sequence, asked for.

    from_ and to are boundaries: where the display's own knowledge starts and
    how far it runs. Both are empty at the beginning of the sequence. have is
    how much of the window the display can fill from what it already holds, and
    need is how many rows the window is."""

    tag: int = 0
    from_: Fields = dataclasses.field(default_factory=Fields)
    to: Fields = dataclasses.field(default_factory=Fields)
    have: int = 0
    need: int = 0
    fields: Fields = dataclasses.field(default_factory=Fields)

    def encode(self) -> str:
        """The fill as the arguments after the question word."""
        parts = ["tag=%d" % self.tag]
        if self.from_:
            parts.append("from=" + self.from_.encode())
        if self.to:
            parts.append("to=" + self.to.encode())
        parts.append("have=%d" % self.have)
        parts.append("need=%d" % self.need)
        if self.fields:
            parts.append("fields=" + self.fields.encode())
        return " ".join(parts)


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


def parse_fill(args: List[Arg]) -> Fill:
    """A fill request, from the arguments after the question word."""
    f = Fill()
    for a in args:
        if a.name in ("tag", "have", "need"):
            if a.value is None or a.value.kind != ValueKind.NUMBER or not a.value.is_int:
                raise QueryError("%s: expected a whole number" % a.name)
            setattr(f, a.name, int(a.value.number))
        elif a.name in ("from", "to", "fields"):
            try:
                bag = parse_fields(a.value)
            except QueryError as e:
                raise QueryError("%s: %s" % (a.name, e))
            setattr(f, "from_" if a.name == "from" else a.name, bag)
    return f


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


def _parse_predicate(st: Statement) -> Filter:
    if st.verb in _GROUPS:
        if len(st.args) != 1 or st.args[0].value is None \
                or st.args[0].value.kind != ValueKind.BLOCK:
            raise QueryError("%s: takes one block" % st.verb)
        inner = _parse_filter_block(st.args[0].value.block, st.verb)
        if st.verb == OP_NOT and not inner.children:
            raise QueryError("not: takes something to negate")
        return inner
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
