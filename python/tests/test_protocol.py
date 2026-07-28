"""Wire-format fidelity tests for the Python kittytk client.

These mirror the Go protocol tests: if the framing, escapes, and value
typing match here, a Python program produces and consumes exactly the bytes
the Go display host does. No server required.

    python3 -m unittest discover -s python/tests
"""

import io
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from kittytk import protocol  # noqa: E402
from kittytk.protocol import FlagState, ValueKind  # noqa: E402


def scan_all(data: str):
    sc = protocol.Scanner(io.BytesIO(data.encode("utf-8")))
    out = []
    while True:
        try:
            out.append(sc.next())
        except EOFError:
            return out


class TestQuote(unittest.TestCase):
    def test_escapes(self):
        self.assertEqual(protocol.quote("hi"), '"hi"')
        self.assertEqual(protocol.quote('a"b\\c'), '"a\\"b\\\\c"')
        self.assertEqual(protocol.quote("line1\nline2\t."), '"line1\\nline2\\t."')
        self.assertEqual(protocol.quote("\x1b[0m"), '"\\e[0m"')
        # Arbitrary control byte -> \xNN (0x07 bell).
        self.assertEqual(protocol.quote("\x07"), '"\\x07"')

    def test_roundtrip_through_parser(self):
        for s in ["plain", 'with "quotes"', "tab\tand\nnewline",
                  "\x1b[2J\x07ok", "unicode: café ✓"]:
            wire = "x=new label caption=" + protocol.quote(s)
            st = protocol.parse(wire).statements[0]
            cap = next(a for a in st.args if a.name == "caption")
            self.assertEqual(cap.value.kind, ValueKind.STRING)
            self.assertEqual(cap.value.str, s)


class TestParser(unittest.TestCase):
    def test_value_types(self):
        st = protocol.parse("new x a=1 b=2.5 c=word d=\"str\" e f={}").statements[0]
        args = {a.name: a for a in st.args if a.name}
        self.assertEqual(st.verb, "new")
        self.assertEqual(st.args[0].name, "x")
        self.assertEqual(st.args[0].flag, FlagState.TRUE)
        self.assertTrue(args["a"].value.is_int)
        self.assertEqual(args["a"].value.number, 1)
        self.assertFalse(args["b"].value.is_int)
        self.assertEqual(args["c"].value.word, "word")
        self.assertEqual(args["d"].value.str, "str")
        self.assertEqual(args["e"].flag, FlagState.TRUE)
        self.assertEqual(args["f"].value.kind, ValueKind.BLOCK)

    def test_flags(self):
        st = protocol.parse("set 5 !enabled ?checked wrap").statements[0]
        self.assertEqual(st.verb, "set")
        # bare number arg (target ref)
        self.assertEqual(st.args[0].value.number, 5)
        self.assertEqual(st.args[1].name, "enabled")
        self.assertEqual(st.args[1].flag, FlagState.FALSE)
        self.assertEqual(st.args[2].name, "checked")
        self.assertEqual(st.args[2].flag, FlagState.INDETERMINATE)
        self.assertEqual(st.args[3].name, "wrap")
        self.assertEqual(st.args[3].flag, FlagState.TRUE)

    def test_key_verb_and_ref(self):
        s1 = protocol.parse("w=new window title=\"T\"").statements[0]
        self.assertEqual(s1.key, "w")
        self.assertEqual(s1.verb, "new")
        s2 = protocol.parse("tabs=w.t").statements[0]
        self.assertEqual(s2.key, "tabs")
        self.assertEqual(s2.ref, "w.t")
        self.assertEqual(s2.verb, "")

    def test_nested_blocks(self):
        st = protocol.parse(
            "new panel children={ new label caption=\"a\"; new button caption=\"b\" }"
        ).statements[0]
        children = next(a for a in st.args if a.name == "children")
        block = children.value.block
        self.assertEqual(len(block.statements), 2)
        btn_cap = next(a for a in block.statements[1].args if a.name == "caption")
        self.assertEqual(btn_cap.value.str, "b")

    def test_x_escape(self):
        st = protocol.parse('set 1 feed="\\x1b[0m\\x07"').statements[0]
        self.assertEqual(st.args[1].value.str, "\x1b[0m\x07")


class TestScanner(unittest.TestCase):
    def test_frames_by_newline_at_depth_zero(self):
        frames = scan_all("a=1\nb=2\n")
        self.assertEqual([f.strip() for f in frames], ["a=1", "b=2"])

    def test_braces_span_lines(self):
        frames = scan_all("new p children={\n  new label\n}\nnext\n")
        self.assertEqual(len(frames), 2)
        self.assertIn("new label", frames[0])
        self.assertEqual(frames[1].strip(), "next")

    def test_string_with_newline_escape_and_braces(self):
        # A brace or newline inside a quoted string must not affect framing.
        frames = scan_all('x=new label caption="a } b" other=1\ndone\n')
        self.assertEqual(len(frames), 2)
        self.assertIn('"a } b"', frames[0])

    def test_comments_and_blank_lines_skipped(self):
        frames = scan_all("# a comment\n\nreal=1\n# trailing\n")
        self.assertEqual([f.strip() for f in frames], ["real=1"])

    def test_eof_terminates_final_statement(self):
        frames = scan_all("last=1")  # no trailing newline
        self.assertEqual([f.strip() for f in frames], ["last=1"])


class TestEvent(unittest.TestCase):
    def test_parse_and_read_fields(self):
        ev = protocol.parse_event('event toggle trinket=17 checked')
        self.assertEqual(ev.type, "toggle")
        self.assertEqual(ev.uint("trinket"), 17)
        self.assertEqual(ev.trinket(), 17)
        self.assertEqual(ev.flag("checked"), FlagState.TRUE)

    def test_window_source_via_window_field(self):
        ev = protocol.parse_event('event window_closed window=42')
        self.assertEqual(ev.trinket(), 42)

    def test_change_text_and_selected(self):
        ev = protocol.parse_event('event change trinket=3 text="hi there" selected=2')
        self.assertEqual(ev.text("text"), "hi there")
        self.assertEqual(ev.int_("selected"), 2)

    def test_command_action(self):
        ev = protocol.parse_event('event command trinket=9 action=demo.hello')
        self.assertEqual(ev.word("action"), "demo.hello")

    def test_encode_roundtrip(self):
        ev = protocol.Event("toggle")
        ev.fields.append(protocol.Arg(name="trinket",
                                      value=protocol.Value(kind=ValueKind.NUMBER, number=17, is_int=True)))
        ev.fields.append(protocol.Arg(name="checked", flag=FlagState.TRUE))
        encoded = ev.encode()
        self.assertEqual(encoded, "event toggle trinket=17 checked")
        back = protocol.parse_event(encoded)
        self.assertEqual(back.uint("trinket"), 17)
        self.assertEqual(back.flag("checked"), FlagState.TRUE)


class TestReply(unittest.TestCase):
    def test_decode(self):
        st = protocol.parse("reply w=17 cb=19").statements[0]
        ids = protocol.decode_reply(st)
        self.assertEqual(ids, {"w": 17, "cb": 19})


if __name__ == "__main__":
    unittest.main()
