"""KittyTK display-protocol wire format (Python port of the Go `protocol`
package).

This module is a faithful reimplementation of the command language and its
byte-stream framing: the same six-value type system, the same string
escapes, the same brace/quote-aware scanner. It is transport-agnostic -
sockets and sessions live in `client`.

The point of this file is to prove the protocol is language-neutral: a
Python program that encodes/decodes exactly what the Go host expects can
drive the identical display service.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass, field
from typing import Dict, List, Optional


class FlagState(enum.IntEnum):
    """A bare-name argument's assertion (D12/D16). Values match the Go
    iota order so the wire meaning is identical."""

    NONE = 0            # carries a value (name=value)
    TRUE = 1            # bare name: `wrap`
    FALSE = 2           # negated name: `!enabled`
    INDETERMINATE = 3   # asserted-indeterminate: `?checked`


class ValueKind(enum.IntEnum):
    WORD = 0     # enum or identifier (bare token)
    NUMBER = 1   # int or float
    STRING = 2   # quotes required on the wire
    BLOCK = 3    # {} collection of statements


@dataclass
class Value:
    kind: ValueKind
    word: str = ""
    number: float = 0.0
    is_int: bool = False
    str: str = ""
    block: Optional["Script"] = None


@dataclass
class Arg:
    name: str = ""
    flag: FlagState = FlagState.NONE
    value: Optional[Value] = None


@dataclass
class Statement:
    key: str = ""
    verb: str = ""
    ref: str = ""
    args: List[Arg] = field(default_factory=list)


@dataclass
class Script:
    statements: List[Statement] = field(default_factory=list)


class ParseError(Exception):
    def __init__(self, line: int, col: int, msg: str):
        super().__init__(f"{line}:{col}: {msg}")
        self.line = line
        self.col = col
        self.msg = msg


# --- String quoting (mirror of quoteString) ------------------------------

def quote(s: str) -> str:
    """Render s as a protocol string literal (quotes + escapes, control
    bytes as \\xNN). Script builders use it to interpolate arbitrary text
    safely."""
    out = ['"']
    for ch in s:
        if ch == '"':
            out.append('\\"')
        elif ch == '\\':
            out.append('\\\\')
        elif ch == '\n':
            out.append('\\n')
        elif ch == '\t':
            out.append('\\t')
        elif ch == '\r':
            out.append('\\r')
        elif ch == '\x1b':
            out.append('\\e')
        else:
            o = ord(ch)
            if o < 0x20 or o == 0x7f:
                out.append('\\x%02x' % o)
            else:
                out.append(ch)
    out.append('"')
    return ''.join(out)


# --- Scanner: frame statements out of a byte stream ----------------------

_SP, _TAB, _CR, _NL = ord(' '), ord('\t'), ord('\r'), ord('\n')
_DQUOTE, _BSLASH, _LBRACE, _RBRACE = ord('"'), ord('\\'), ord('{'), ord('}')
_HASH, _SEMI = ord('#'), ord(';')


class Scanner:
    """Frames complete statements out of a byte stream. "Complete" means a
    newline (or EOF) reached at brace depth zero outside a string - the
    language frames itself; no length prefixes (D22)."""

    def __init__(self, reader):
        # reader: a binary reader with .read(1) -> bytes (e.g. a socket
        # makefile('rb')).
        self._reader = reader

    def _read_byte(self) -> Optional[int]:
        b = self._reader.read(1)
        if not b:
            return None
        return b[0]

    def next(self) -> str:
        """Return the text of the next complete statement, skipping blank
        and comment lines. Raises EOFError when the stream ends cleanly
        between statements."""
        buf = bytearray()
        depth = 0
        in_string = False
        escaped = False
        saw_content = False

        while True:
            ch = self._read_byte()
            if ch is None:  # EOF
                if saw_content and depth == 0 and not in_string:
                    return buf.decode('utf-8')
                if not saw_content:
                    raise EOFError()
                raise IOError("wire: unexpected EOF mid-statement")

            if escaped:
                escaped = False
            elif in_string:
                if ch == _BSLASH:
                    escaped = True
                elif ch == _DQUOTE:
                    in_string = False
                elif ch == _NL:
                    raise ValueError("wire: newline inside string")
            elif ch == _DQUOTE:
                in_string = True
                saw_content = True
            elif ch == _LBRACE:
                depth += 1
                saw_content = True
            elif ch == _RBRACE:
                depth -= 1
                saw_content = True
            elif ch == _HASH:
                # Comment to end of line; the newline still terminates.
                while True:
                    c = self._read_byte()
                    if c is None or c == _NL:
                        break
                if saw_content and depth == 0:
                    buf.append(_NL)
                    return buf.decode('utf-8')
                continue
            elif ch == _NL:
                if depth == 0:
                    if saw_content:
                        buf.append(_NL)
                        return buf.decode('utf-8')
                    continue  # blank line between statements
            elif ch not in (_SP, _TAB, _CR, _SEMI):
                saw_content = True
            buf.append(ch)


# --- Parser (mirror of parser.go) ----------------------------------------

def _is_word_start(ch: str) -> bool:
    return ch == '_' or ('a' <= ch <= 'z') or ('A' <= ch <= 'Z')


def _is_word_rune(ch: str) -> bool:
    return _is_word_start(ch) or ch == '.' or ('0' <= ch <= '9')


def _is_number_start(ch: str) -> bool:
    return ch == '-' or ('0' <= ch <= '9')


def _hex_val(ch: str) -> int:
    if '0' <= ch <= '9':
        return ord(ch) - ord('0')
    if 'a' <= ch <= 'f':
        return ord(ch) - ord('a') + 10
    if 'A' <= ch <= 'F':
        return ord(ch) - ord('A') + 10
    return -1


class _Parser:
    def __init__(self, src: str):
        self.src = src
        self.pos = 0
        self.line = 1
        self.col = 1

    def _errf(self, msg: str) -> ParseError:
        return ParseError(self.line, self.col, msg)

    def eof(self) -> bool:
        return self.pos >= len(self.src)

    def peek(self) -> str:
        if self.eof():
            return '\0'
        return self.src[self.pos]

    def advance(self) -> str:
        ch = self.src[self.pos]
        self.pos += 1
        if ch == '\n':
            self.line += 1
            self.col = 1
        else:
            self.col += 1
        return ch

    def skip_inline(self):
        while not self.eof():
            c = self.peek()
            if c in (' ', '\t', '\r'):
                self.advance()
            elif c == '#':
                while not self.eof() and self.peek() != '\n':
                    self.advance()
            else:
                return

    def skip_separators(self):
        while not self.eof():
            c = self.peek()
            if c in (' ', '\t', '\r', '\n', ';'):
                self.advance()
            elif c == '#':
                while not self.eof() and self.peek() != '\n':
                    self.advance()
            else:
                return

    def at_statement_end(self, in_block: bool) -> bool:
        self.skip_inline()
        if self.eof():
            return True
        c = self.peek()
        if c in ('\n', ';'):
            return True
        if c == '}':
            return in_block
        return False

    def parse_word(self) -> str:
        if self.eof() or not _is_word_start(self.peek()):
            raise self._errf("expected a name")
        out = []
        while not self.eof() and _is_word_rune(self.peek()):
            out.append(self.advance())
        return ''.join(out)

    def parse_string(self) -> str:
        if self.peek() != '"':
            raise self._errf("expected string")
        self.advance()  # opening quote
        out = []
        while True:
            if self.eof():
                raise self._errf("unterminated string")
            ch = self.advance()
            if ch == '"':
                return ''.join(out)
            elif ch == '\\':
                if self.eof():
                    raise self._errf("unterminated escape")
                esc = self.advance()
                if esc == '\\':
                    out.append('\\')
                elif esc == '"':
                    out.append('"')
                elif esc == 'n':
                    out.append('\n')
                elif esc == 't':
                    out.append('\t')
                elif esc == 'r':
                    out.append('\r')
                elif esc == 'e':
                    out.append('\x1b')
                elif esc == 'x':
                    v = 0
                    for _ in range(2):
                        if self.eof():
                            raise self._errf("unterminated \\x escape")
                        d = _hex_val(self.advance())
                        if d < 0:
                            raise self._errf("malformed \\x escape (two hex digits required)")
                        v = (v << 4) | d
                    out.append(chr(v))
                else:
                    raise self._errf("unknown escape \\%s" % esc)
            elif ch == '\n':
                raise self._errf("unterminated string (newline)")
            else:
                out.append(ch)

    def parse_number(self) -> Value:
        out = []
        if self.peek() == '-':
            out.append(self.advance())
        digits = 0
        dot = False
        while not self.eof():
            ch = self.peek()
            if '0' <= ch <= '9':
                digits += 1
                out.append(self.advance())
            elif ch == '.' and not dot:
                dot = True
                out.append(self.advance())
            else:
                break
        if digits == 0:
            raise self._errf("malformed number")
        text = ''.join(out)
        try:
            f = float(text)
        except ValueError:
            raise self._errf("malformed number %r" % text)
        return Value(kind=ValueKind.NUMBER, number=f, is_int=not dot)

    def parse_value(self, in_block: bool) -> Value:
        self.skip_inline()
        if self.eof():
            raise self._errf("expected a value")
        c = self.peek()
        if c == '"':
            return Value(kind=ValueKind.STRING, str=self.parse_string())
        if c == '{':
            self.advance()  # '{'
            block = self.parse_script(False)
            if self.eof() or self.peek() != '}':
                raise self._errf("unterminated block: expected '}'")
            self.advance()  # '}'
            return Value(kind=ValueKind.BLOCK, block=block)
        if _is_number_start(c):
            return self.parse_number()
        if _is_word_start(c):
            return Value(kind=ValueKind.WORD, word=self.parse_word())
        raise self._errf("unexpected character %r in value position" % c)

    def parse_args(self, in_block: bool) -> List[Arg]:
        args: List[Arg] = []
        while not self.at_statement_end(in_block):
            ch = self.peek()
            if ch in ('!', '?'):
                self.advance()
                try:
                    name = self.parse_word()
                except ParseError:
                    raise self._errf("expected flag name after %r" % ch)
                state = FlagState.INDETERMINATE if ch == '?' else FlagState.FALSE
                args.append(Arg(name=name, flag=state))
            elif _is_word_start(ch):
                name = self.parse_word()
                self.skip_inline()
                if not self.eof() and self.peek() == '=':
                    self.advance()  # '='
                    val = self.parse_value(in_block)
                    args.append(Arg(name=name, value=val))
                else:
                    args.append(Arg(name=name, flag=FlagState.TRUE))
            elif _is_number_start(ch):
                val = self.parse_number()
                args.append(Arg(value=val))
            else:
                raise self._errf("unexpected %r: values must be named (name=value)" % ch)
        return args

    def parse_statement(self, in_block: bool) -> Statement:
        first = self.parse_word()
        self.skip_inline()
        if not self.eof() and self.peek() == '=':
            self.advance()  # '='
            self.skip_inline()
            if self.eof() or not _is_word_start(self.peek()):
                raise self._errf("expected command or reference after %r=" % first)
            second = self.parse_word()
            if self.at_statement_end(in_block):
                return Statement(key=first, ref=second)
            args = self.parse_args(in_block)
            return Statement(key=first, verb=second, args=args)
        args = self.parse_args(in_block)
        return Statement(verb=first, args=args)

    def parse_script(self, top_level: bool) -> Script:
        script = Script()
        while True:
            self.skip_separators()
            if self.eof():
                if not top_level:
                    raise self._errf("unterminated block: expected '}'")
                return script
            if self.peek() == '}':
                if top_level:
                    raise self._errf("unexpected '}'")
                return script
            if not _is_word_start(self.peek()):
                raise self._errf("expected a statement, found %r" % self.peek())
            script.statements.append(self.parse_statement(not top_level))


def parse(src: str) -> Script:
    return _Parser(src).parse_script(True)


# --- Reply ---------------------------------------------------------------

def decode_reply(stmt: Statement) -> dict:
    """Parse a `reply` statement into a name -> id dict."""
    if stmt.verb != "reply":
        raise ValueError("not a reply statement: %r" % stmt.verb)
    ids = {}
    for a in stmt.args:
        if a.value is None or a.value.kind != ValueKind.NUMBER or not a.value.is_int:
            raise ValueError("reply %s: expected integer id" % a.name)
        ids[a.name] = int(a.value.number)
    return ids


# --- Introspection / describe (D24) --------------------------------------

@dataclass
class PropInfo:
    """One property in a described vocabulary: its value kind, default,
    a brief (tooltip-length) description, and the enum words if any."""
    name: str
    kind: str = ""
    default: str = ""
    doc: str = ""
    enum: List[str] = field(default_factory=list)


@dataclass
class TypeInfo:
    name: str
    virtual: bool = False
    props: List[PropInfo] = field(default_factory=list)


@dataclass
class Vocabulary:
    common: List[PropInfo] = field(default_factory=list)
    types: List[TypeInfo] = field(default_factory=list)


def _stmt_str(stmt: Statement, name: str) -> str:
    for a in stmt.args:
        if a.name == name and a.value is not None and a.value.kind == ValueKind.STRING:
            return a.value.str
    return ""


def _stmt_flag(stmt: Statement, name: str) -> bool:
    for a in stmt.args:
        if a.name == name and a.value is None:
            return a.flag == FlagState.TRUE
    return False


def _stmt_prop_info(stmt: Statement) -> PropInfo:
    enum_s = _stmt_str(stmt, "enum")
    return PropInfo(
        name=_stmt_str(stmt, "name"),
        kind=_stmt_str(stmt, "kind"),
        default=_stmt_str(stmt, "default"),
        doc=_stmt_str(stmt, "doc"),
        enum=enum_s.split(",") if enum_s else [],
    )


def decode_vocabulary(lines: List[str]) -> Vocabulary:
    """Parse the flat describe stream (proptype/prop/propcommon statements,
    one per line) into a Vocabulary. Unknown lines are ignored."""
    vocab = Vocabulary()
    by_type: Dict[str, int] = {}
    for line in lines:
        if not line.strip():
            continue
        for stmt in parse(line).statements:
            if stmt.verb == "proptype":
                name = _stmt_str(stmt, "name")
                vocab.types.append(TypeInfo(name=name, virtual=_stmt_flag(stmt, "virtual")))
                by_type[name] = len(vocab.types) - 1
            elif stmt.verb == "propcommon":
                vocab.common.append(_stmt_prop_info(stmt))
            elif stmt.verb == "prop":
                of = _stmt_str(stmt, "of")
                if of in by_type:
                    vocab.types[by_type[of]].props.append(_stmt_prop_info(stmt))
    return vocab


# --- Event ---------------------------------------------------------------

class Event:
    """A display-service -> app record: `event <type> field=value ...`."""

    def __init__(self, type_: str, fields: Optional[List[Arg]] = None):
        self.type = type_
        self.fields = fields or []

    def _field(self, name: str) -> Optional[Arg]:
        for a in self.fields:
            if a.name == name:
                return a
        return None

    def uint(self, name: str):
        a = self._field(name)
        if a is None or a.value is None or a.value.kind != ValueKind.NUMBER \
                or not a.value.is_int or a.value.number < 0:
            return None
        return int(a.value.number)

    def int_(self, name: str):
        a = self._field(name)
        if a is None or a.value is None or a.value.kind != ValueKind.NUMBER \
                or not a.value.is_int:
            return None
        return int(a.value.number)

    def text(self, name: str):
        a = self._field(name)
        if a is None or a.value is None or a.value.kind != ValueKind.STRING:
            return None
        return a.value.str

    def word(self, name: str):
        a = self._field(name)
        if a is None or a.value is None or a.value.kind != ValueKind.WORD:
            return None
        return a.value.word

    def flag(self, name: str) -> FlagState:
        a = self._field(name)
        if a is None or a.value is not None:
            return FlagState.NONE
        return a.flag

    def trinket(self):
        v = self.uint("trinket")
        if v is not None:
            return v
        return self.uint("window")

    def encode(self) -> str:
        out = ["event ", self.type]
        for a in self.fields:
            out.append(' ')
            if a.value is None:
                if a.flag == FlagState.FALSE:
                    out.append('!')
                elif a.flag == FlagState.INDETERMINATE:
                    out.append('?')
                out.append(a.name)
                continue
            out.append(a.name)
            out.append('=')
            v = a.value
            if v.kind == ValueKind.WORD:
                out.append(v.word)
            elif v.kind == ValueKind.NUMBER:
                if v.is_int:
                    out.append(str(int(v.number)))
                else:
                    out.append(repr(v.number))
            elif v.kind == ValueKind.STRING:
                out.append(quote(v.str))
        return ''.join(out)


def parse_event(src: str) -> Event:
    script = parse(src)
    if len(script.statements) != 1:
        raise ValueError("expected one event statement, got %d" % len(script.statements))
    st = script.statements[0]
    if st.verb != "event" or st.key != "":
        raise ValueError("not an event statement")
    if not st.args or st.args[0].value is not None or st.args[0].flag != FlagState.TRUE:
        raise ValueError("event: missing type word")
    return Event(st.args[0].name, st.args[1:])
