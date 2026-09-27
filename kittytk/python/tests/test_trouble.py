"""A complaint that did not stop anything, read by this client.

A refusal is what a batch is answered WITH, in place of the reply that would have
named what it made. A trouble is not that: the statements ran, the objects exist,
and something along the way is worth the author knowing. testdata/trouble.wire is
the text three client libraries read, so a part one of them drops is a part
missing here.
"""

import os
import unittest

from kittytk import protocol

CORPUS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata",
                      "trouble.wire")


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
            if verb == protocol.TROUBLE_VERB:
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


def trouble_of(text):
    """One corpus line, taken apart and written back from the structure."""
    script = protocol.parse(text)
    if len(script.statements) != 1:
        raise ValueError("not one statement")
    t = protocol.decode_trouble(script.statements[0])
    return protocol.encode_trouble(t)[len(protocol.TROUBLE_VERB) + 1:]


class TroubleCorpusTest(unittest.TestCase):
    def test_every_trouble_case_answers_the_same_way(self):
        cases = read_corpus()
        self.assertGreaterEqual(len(cases), 12,
                                "the corpus holds fewer cases than it did")
        for n, text, want, bad in cases:
            with self.subTest(line=n, case=text):
                if bad:
                    with self.assertRaises((ValueError, protocol.ParseError)):
                        trouble_of(text)
                    continue
                self.assertEqual(trouble_of(text), want)
