"""What a source says has stopped being true, read by this client.

testdata/stale.wire is the text three client libraries read, and they have to
arrive at the same structure from the same text or the same notice means three
things. What it agrees is the whole statement: which source, which record, which
of the four things happened, and what an alteration touched.
"""

import os
import unittest

from kittytk import protocol, query

CORPUS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata", "stale.wire")


def read_corpus():
    """The cases: (line, text, want, bad)."""
    out = []
    pending = None
    with open(CORPUS, "r", encoding="utf-8") as f:
        for i, raw in enumerate(f, start=1):
            text = raw.strip()
            if not text or text.startswith("#"):
                continue
            verb, _, rest = text.partition(" ")
            if verb == query.STALE_VERB:
                assert pending is None, "line %d: a case with no answer" % i
                pending = (i, text)
            elif verb in ("want", "bad"):
                assert pending is not None, "line %d: a notice with no case" % i
                n, case = pending
                out.append((n, case, rest, verb == "bad"))
                pending = None
            else:
                raise AssertionError("line %d: %r is not a corpus line" % (i, verb))
    assert pending is None, "a case with no answer"
    return out


def taken(text):
    """One statement's worth of text, as a Stale."""
    script = protocol.parse(text)
    assert len(script.statements) == 1, "%s is %d statements" % (text, len(script.statements))
    assert script.statements[0].verb == query.STALE_VERB
    return query.parse_stale(script.statements[0].args)


class StaleCorpusTest(unittest.TestCase):
    def test_every_stale_case_answers_the_same_way(self):
        cases = read_corpus()
        self.assertTrue(cases, "the corpus holds no cases")
        for line, text, want, bad in cases:
            with self.subTest(line=line, case=text):
                if bad:
                    with self.assertRaises(Exception, msg="%s was read as a notice" % text):
                        taken(text)
                    continue
                got = query.encode_stale(taken(text))
                self.assertEqual(got, query.STALE_VERB + " " + want)

    def test_the_canonical_spelling_is_stable(self):
        """What comes out goes back in unchanged."""
        for line, text, want, bad in read_corpus():
            if bad:
                continue
            with self.subTest(line=line, case=text):
                once = query.encode_stale(taken(text))
                self.assertEqual(query.encode_stale(taken(once)), once)


class StaleShapeTest(unittest.TestCase):
    def test_the_reason_is_what_it_costs(self):
        """A removal is cheaper than a move, so the two must not collapse.

        A record that has LEFT the sequence leaves everything between a run's
        ends still there and the completeness claim survives; one that may have
        MOVED takes the run with it. Same operation on the links, told apart by
        this word alone."""
        gone = taken('stale source="papers" id=1 how=removed')
        moved = taken('stale source="papers" id=1 how=replaced')
        self.assertEqual(gone.how, query.CHANGE_REMOVED)
        self.assertEqual(moved.how, query.CHANGE_REPLACED)
        self.assertNotEqual(gone.how, moved.how)

    def test_what_is_left_out_reads_as_the_widest_thing(self):
        """Saying less than happened is the one mistake that cannot be
        recovered from, so every default leans wide."""
        whole = taken('stale source="papers"')
        self.assertIsNone(whole.id, "a notice naming no record named one")
        self.assertEqual(whole.how, query.CHANGE_REPLACED)
        anyfield = taken('stale source="papers" id=42 how=altered')
        self.assertEqual(anyfield.fields, [], "an alteration naming no field named some")

    def test_a_contradiction_is_refused_rather_than_half_read(self):
        """`fields=` beside any reason but an alteration says the opposite of
        the word next to it, and quietly dropping one half of a contradiction is
        how a source comes to believe it said something it did not."""
        for bad in (
            'stale source="papers" id=42 how=removed fields={ size }',
            'stale source="papers" id=42 how=replaced fields={ size }',
            'stale source="papers" id=42 fields={ size }',
        ):
            with self.subTest(case=bad):
                with self.assertRaises(query.QueryError):
                    taken(bad)

    def test_a_notice_carries_no_values(self):
        """It names fields, never their values: a value would read as an
        assertion about what the record now holds, which a notice never makes."""
        with self.assertRaises(query.QueryError):
            taken('stale source="papers" id=42 how=altered fields={ size 1024 }')


if __name__ == "__main__":
    unittest.main()
