"""The comparison core, against the corpus every implementation of it answers.

The corpus is read with this client's own wire parser, so Python is answering
the same questions from the same file as Go and C.

    python3 -m unittest discover -s python/tests
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from kittytk import protocol  # noqa: E402
from kittytk.protocol import ValueKind  # noqa: E402

CORPUS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata", "compare.wire")


def arg(stmt, name):
    for a in stmt.args:
        if a.name == name:
            return a.value
    return None


class TestCorpus(unittest.TestCase):
    def test_every_case_answers_the_same_way(self):
        with open(CORPUS, "r", encoding="utf-8") as f:
            script = protocol.parse(f.read())

        cases = 0
        for i, stmt in enumerate(script.statements):
            if stmt.verb == "" and not stmt.args:
                continue  # a blank line or a comment
            self.assertEqual(stmt.verb, "case", "statement %d is not a case" % (i + 1))
            cases += 1

            a, b = arg(stmt, "a"), arg(stmt, "b")
            want = arg(stmt, "want")
            self.assertIsNotNone(want, "case %d has no want" % (i + 1))
            want = int(want.number)
            collate = arg(stmt, "collate")
            collation = collate.word if collate else protocol.COLLATE_EXACT

            self.assertEqual(
                protocol.compare(a, b, collation), want,
                "case %d: %r against %r under %s" % (i + 1, a, b, collation))
            # Every case is its own mirror.
            self.assertEqual(
                protocol.compare(b, a, collation), -want,
                "case %d reversed: %r against %r under %s" % (i + 1, b, a, collation))

        self.assertGreaterEqual(cases, 50, "the corpus answered too few cases")


class TestRanks(unittest.TestCase):
    def test_a_value_is_equal_to_itself(self):
        script = protocol.parse('\n'.join([
            'case a=undefined', 'case a=nil', 'case a=true', 'case a=false',
            'case a=0', 'case a=-17', 'case a=9007199254740993', 'case a=1.5',
            'case a=apple', 'case a="apple"', 'case a=""', 'case a={ inner }',
        ]))
        for collation in (protocol.COLLATE_EXACT, protocol.COLLATE_FOLD,
                          protocol.COLLATE_NATURAL):
            for stmt in script.statements:
                v = arg(stmt, "a")
                self.assertEqual(protocol.compare(v, v, collation), 0)

    def test_bytes_are_not_a_string(self):
        text = protocol.Value(kind=ValueKind.STRING, str="ab")
        blob = protocol.Value(kind=ValueKind.STRING, str="ab", blob=True)
        self.assertEqual(protocol.compare(text, blob), -1)
        self.assertEqual(protocol.compare(blob, blob), 0)
        low = protocol.Value(kind=ValueKind.STRING, str="\x01", blob=True)
        high = protocol.Value(kind=ValueKind.STRING, str="\xff", blob=True)
        self.assertEqual(protocol.compare(low, high), -1)

    def test_a_level_settles_only_what_the_ones_above_it_left_equal(self):
        sales = protocol.Value(kind=ValueKind.WORD, word="sales")
        small = protocol.Value(kind=ValueKind.NUMBER, number=40000, is_int=True)
        large = protocol.Value(kind=ValueKind.NUMBER, number=120000, is_int=True)
        ascending = [(False, ""), (False, "")]
        self.assertEqual(protocol.compare_levels([sales, small], [sales, large], ascending), -1)
        descending = [(False, ""), (True, "")]
        self.assertEqual(protocol.compare_levels([sales, small], [sales, large], descending), 1)


if __name__ == "__main__":
    unittest.main()
