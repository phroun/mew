"""The query structure, against the corpus every implementation of it answers.

The corpus is read with this client's own wire parser, so Python is taking the
same text apart as Go and C, and has to arrive at the same structure.

    python3 -m unittest discover -s python/tests
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from kittytk import protocol, query  # noqa: E402

CORPUS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata", "query.wire")


def read_corpus():
    """The shared file as (line, kind, text, want, bad) cases."""
    cases = []
    pending = None
    with open(CORPUS, encoding="utf-8") as f:
        for n, raw in enumerate(f, start=1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            verb, _, rest = line.partition(" ")
            if verb in ("spec", "fill"):
                assert pending is None, "line %d: a case with no answer" % n
                pending = [n, verb, rest, "", False]
            elif verb == "want":
                assert pending is not None, "line %d: an answer with no case" % n
                pending[3] = rest
                cases.append(tuple(pending))
                pending = None
            elif verb == "bad":
                assert pending is not None, "line %d: an answer with no case" % n
                pending[4] = True
                cases.append(tuple(pending))
                pending = None
            else:
                raise AssertionError("line %d: %r is not a corpus line" % (n, verb))
    assert pending is None, "a case with no answer"
    return cases


def answer(kind, text):
    """Parse one case and render the structure back out."""
    script = protocol.parse(kind + " " + text)
    args = script.statements[0].args
    if kind == "spec":
        return query.parse_spec(args).encode()
    return query.parse_fill(args).encode()


class QueryCorpusTest(unittest.TestCase):
    def test_every_query_case_answers_the_same_way(self):
        cases = read_corpus()
        self.assertGreaterEqual(len(cases), 40,
                                "the corpus holds fewer cases than it did")
        for n, kind, text, want, bad in cases:
            with self.subTest(line=n, case=text):
                if bad:
                    with self.assertRaises((query.QueryError, protocol.ParseError),
                                           msg="line %d: %s %s was not refused"
                                               % (n, kind, text)):
                        answer(kind, text)
                else:
                    self.assertEqual(answer(kind, text), want,
                                     "line %d: %s %s" % (n, kind, text))

    def test_the_canonical_spelling_is_stable(self):
        """What comes out goes back in unchanged."""
        for n, kind, text, want, bad in read_corpus():
            if bad:
                continue
            with self.subTest(line=n, case=want):
                self.assertEqual(answer(kind, want), want)


class FieldBagTest(unittest.TestCase):
    def test_a_bag_finds_its_fields_by_name(self):
        bag = query.parse_fields(
            protocol.parse('x f={ name "a"; size 12; key 7 }').statements[0].args[0].value)
        self.assertEqual(bag.names(), ["name", "size", "key"])
        self.assertEqual(bag.get("name").str, "a")
        self.assertEqual(bag.key().number, 7)
        self.assertIsNone(bag.get("absent"))
        self.assertTrue(bag.has("size"))
        self.assertFalse(bag.has("absent"))

    def test_a_named_field_with_no_value_is_still_named(self):
        bag = query.parse_fields(
            protocol.parse("x f={ name; size }").statements[0].args[0].value)
        self.assertTrue(bag.has("name"))
        self.assertIsNone(bag.get("name"))


class FilterShapeTest(unittest.TestCase):
    def test_a_block_is_an_and(self):
        f = query.parse_filter(
            protocol.parse("x f={ eq kind folder }").statements[0].args[0].value)
        self.assertEqual(f.op, query.OP_AND)
        self.assertEqual(len(f.children), 1)
        self.assertEqual(f.children[0].op, query.OP_EQ)
        self.assertEqual(f.children[0].field, "kind")
        self.assertEqual(f.children[0].value().word, "folder")

    def test_a_symbol_and_a_string_are_different_questions(self):
        symbol = query.parse_filter(
            protocol.parse("x f={ eq kind folder }").statements[0].args[0].value)
        text = query.parse_filter(
            protocol.parse('x f={ eq kind "folder" }').statements[0].args[0].value)
        self.assertEqual(symbol.children[0].value().kind, protocol.ValueKind.WORD)
        self.assertEqual(text.children[0].value().kind, protocol.ValueKind.STRING)


if __name__ == "__main__":
    unittest.main()
