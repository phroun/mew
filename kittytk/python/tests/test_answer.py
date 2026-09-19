"""The third pair, read by this client.

`ask` is answered by `answer`. testdata/answer.wire is the text three client
libraries read, and what is agreed there is the ENVELOPE: which question is being
answered, whether this is the last piece, whether it is a refusal. The payload is
the question's own and nothing here reads it -- whoever asked knows what the
answer to that question looks like -- so what is checked about it is that it
survives untouched and in order.
"""

import os
import unittest

from kittytk import protocol, query

CORPUS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata", "answer.wire")


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
            if verb == query.ANSWER_VERB:
                assert pending is None, "line %d: a case with no answer" % i
                pending = (i, text)
            elif verb in ("want", "bad"):
                assert pending is not None, "line %d: an answer with no case" % i
                n, case = pending
                out.append((n, case, rest, verb == "bad"))
                pending = None
            else:
                raise AssertionError("line %d: %r is not a corpus line" % (i, verb))
    assert pending is None, "a case with no answer"
    return out


def answer_of(text):
    """One corpus line, taken apart and written back from the structure."""
    script = protocol.parse(text)
    if len(script.statements) != 1:
        raise query.QueryError("not one statement")
    a = query.parse_answer(script.statements[0].args)
    return " ".join(protocol.encode_arg(arg) for arg in a.args())


class AnswerCorpusTest(unittest.TestCase):
    def test_every_answer_case_answers_the_same_way(self):
        cases = read_corpus()
        self.assertGreaterEqual(len(cases), 10,
                                "the corpus holds fewer cases than it did")
        for n, text, want, bad in cases:
            with self.subTest(line=n, case=text):
                if bad:
                    with self.assertRaises((query.QueryError, protocol.ParseError),
                                           msg="line %d: %s was not refused" % (n, text)):
                        answer_of(text)
                else:
                    self.assertEqual(answer_of(text), want, "line %d: %s" % (n, text))

    def test_the_canonical_spelling_is_stable(self):
        """What comes out goes back in unchanged."""
        for n, text, want, bad in read_corpus():
            if bad:
                continue
            with self.subTest(line=n, case=want):
                self.assertEqual(answer_of(query.ANSWER_VERB + " " + want), want)


class AnswerShapeTest(unittest.TestCase):
    def test_a_record_is_spelled_as_a_results_is(self):
        """`record=` is every member, `fields=` is some of them -- and a caller
        writing an amendment out has to know which it is holding."""
        for text, whole in (
            ('answer to=q1 id=7 record={ kind "Archive" }', True),
            ('answer to=q1 id=7 fields={ kind "Archive" }', False),
        ):
            with self.subTest(case=text):
                script = protocol.parse(text)
                a = query.parse_answer(script.statements[0].args)
                rid, fields, got_whole, got = a.record()
                self.assertTrue(got, "no record was read")
                self.assertEqual(got_whole, whole)
                self.assertEqual(rid.number, 7)
                self.assertEqual(fields.get("kind").str, "Archive")

    def test_an_answer_with_no_record_says_so(self):
        script = protocol.parse("answer to=q1 count=2 complete")
        a = query.parse_answer(script.statements[0].args)
        _, _, _, got = a.record()
        self.assertFalse(got, "an answer with no record claims to carry one")
        self.assertEqual(a.arg("count").value.number, 2)
        self.assertIsNone(a.arg("nothing"), "it found an argument it has not got")


if __name__ == "__main__":
    unittest.main()
