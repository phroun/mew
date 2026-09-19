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

    def test_an_answers_arguments_are_read_by_kind(self):
        """The readers answer for an argument of the kind asked for, and nothing
        else.

        "It is there, in another kind" is not an answer. An asker reading
        `size=` wants a size, and a word where a number was expected is a
        question answered wrongly rather than a number to salvage -- so each
        reader turns it away the way an event's does, which is what lets a
        question move off events without its answer being read differently."""
        script = protocol.parse(
            'answer to=q1 size=1024 under=-3 key="notes" type=txt last !cached '
            r'data="\x00\x01\xff" complete')
        a = query.parse_answer(script.statements[0].args)

        self.assertEqual(a.int_("size"), 1024)
        self.assertEqual(a.uint("size"), 1024)
        # A negative number is an int and is NOT a uint: a reader that took it
        # would hand back an enormous count for a figure below zero.
        self.assertEqual(a.int_("under"), -3)
        self.assertIsNone(a.uint("under"), "a negative number read as unsigned")
        self.assertEqual(a.text("key"), "notes")
        self.assertEqual(a.word("type"), "txt")
        self.assertEqual(a.flag("last"), protocol.FlagState.TRUE)
        # A NEGATED flag is not an asserted one, which is the whole reason a
        # flag has three states rather than being present or absent.
        self.assertEqual(a.flag("cached"), protocol.FlagState.FALSE)
        # Bytes come back as bytes, NULs and all.
        self.assertEqual(a.blob("data"), b"\x00\x01\xff")

        # And each reader turns away the kinds that are not its own.
        self.assertIsNone(a.int_("key"), "a string read as a number")
        self.assertIsNone(a.text("type"), "a word read as a string")
        self.assertIsNone(a.word("key"), "a string read as a word")
        self.assertIsNone(a.text("last"), "a flag read as a string")
        self.assertEqual(a.flag("size"), protocol.FlagState.NONE,
                         "an argument carrying a value read as a flag")

        # An argument the answer has not got reads as absent, not as empty.
        self.assertIsNone(a.text("hash"))
        self.assertEqual(a.flag("hash"), protocol.FlagState.NONE)

        # The envelope is not among them: `to` and `complete` are the wire's, so
        # a question whose own vocabulary used those words could not be answered.
        self.assertIsNone(a.word(query.TO_ARG))
        self.assertEqual(a.flag(query.RESULT_COMPLETE), protocol.FlagState.NONE)

    def test_an_answer_with_no_record_says_so(self):
        script = protocol.parse("answer to=q1 count=2 complete")
        a = query.parse_answer(script.statements[0].args)
        _, _, _, got = a.record()
        self.assertFalse(got, "an answer with no record claims to carry one")
        self.assertEqual(a.arg("count").value.number, 2)
        self.assertIsNone(a.arg("nothing"), "it found an argument it has not got")


if __name__ == "__main__":
    unittest.main()
