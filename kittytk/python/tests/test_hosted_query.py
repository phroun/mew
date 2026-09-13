"""What an application has to write to serve a query, and what it never has to.

It never parses. The statement the display sent is taken apart before the
handler sees it, so the handler reads attributes off an object. It never sees a
request on the event line: `query` is answered by `result`, `ask` by `answer`,
`sub` by `event`, and nothing carries two of them. And it never has to hold the
whole answer: records go out in batches as they accumulate.

The Go side of this is client/00_a_query_reaches_the_app_taken_apart_test.go,
and the two are meant to stay recognisably the same test.

    python3 -m unittest discover -s python/tests
"""

import os
import sys
import threading
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from kittytk import client, protocol, query  # noqa: E402


class Recorder:
    """A connection that keeps everything written through it, standing in for
    the socket."""

    def __init__(self):
        self.sent = []
        self._lock = threading.Lock()
        self._sources = {}
        self._queries = {}
        self._last_hosted_id = 0

    def send(self, src):
        self.sent.append(src)

    def exec(self, src):
        self.sent.append(src)
        return {}

    # What the query machinery calls on a connection.
    host_source = client.Conn.host_source
    query = client.Conn.query
    queries = client.Conn.queries
    _mint_id = client.Conn._mint_id
    inbound_batch = client.Conn.inbound_batch
    _inbound_one = client.Conn._inbound_one
    _open_query = client.Conn._open_query
    _window = client.Conn._window

    def since(self, n):
        return list(self.sent[n:])


def serve_one(fill):
    c = Recorder()
    return c, c.host_source("files", fill)


def send(c, src):
    c.inbound_batch(protocol.parse(src).statements)


class ServingAQueryTest(unittest.TestCase):
    def test_registering_a_source_says_nothing(self):
        c, s = serve_one(lambda f: None)
        self.assertEqual(s.name(), "files")
        self.assertEqual(c.sent, [], "registering a source wrote to the wire")

    def test_the_application_names_the_query(self):
        served = []
        c, _ = serve_one(lambda f: (served.append(f), f.exhausted()))
        send(c, 'q=new query source="files" sort={ name natural } have=0 need=30')
        self.assertEqual(c.sent[0], "reply q=1")
        self.assertEqual(served[0].query.id(), 1)
        self.assertEqual(c.query(1).source().name(), "files")

    def test_the_reply_comes_before_the_records(self):
        def fill(f):
            f.ordered()
            f.record(17, name="src/parser.go", size=1024)
            f.record(42, name="src/window.go", size=2048)
            f.done([protocol.named("name", "src/window.go"),
                    protocol.named(query.KEY_FIELD, 42)])

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" sort={ name natural } have=0 need=30')
        self.assertEqual("\n".join(c.sent), "\n".join([
            "reply q=1",
            # The order is declared before the records rather than after them,
            # which is the only place a far end can act on it.
            "result 1 ordered",
            'result 1 fields={ key 17; name "src/parser.go"; size 1024 }',
            'result 1 fields={ key 42; name "src/window.go"; size 2048 }',
            'result 1 complete watermark={ name "src/window.go"; key 42 }',
        ]))

    def test_a_window_arrives_taken_apart(self):
        got = []
        c, _ = serve_one(lambda f: (got.append(f), f.exhausted()))
        send(c, 'q=new query source="files" sort={ name natural } have=0 need=1')
        send(c, 'query 1 from={ name "README.md"; key 17 }'
                ' to={ name "build.sh"; key 42 } have=30 need=50 fields={ name; size }')
        f = got[-1]
        self.assertEqual((f.have, f.need), (30, 50))
        self.assertEqual(f.from_.get("name").str, "README.md")
        self.assertEqual(f.from_.key().number, 17)
        self.assertEqual(f.to.key().number, 42)
        self.assertEqual(f.fields.names(), ["name", "size"])
        # The sequence comes with it, so the handler need not have kept it.
        self.assertEqual(f.spec.source, "files")

    def test_a_query_is_addressable_in_the_batch_that_made_it(self):
        asked = []
        c, _ = serve_one(lambda f: (asked.append(f.need), f.exhausted()))
        send(c, 'q=new query source="files" have=0 need=5\nquery q have=5 need=10')
        self.assertEqual(asked, [5, 10])

    def test_the_simplest_answer_is_everything_and_exhausted(self):
        def fill(f):
            f.record("a")
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" have=0 need=10')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 fields={ key "a" }\n'
                         'result 1 complete exhausted')

    def test_a_refusal_is_an_answer(self):
        c, _ = serve_one(lambda f: f.fail('no records past "build.sh"'))
        send(c, 'q=new query source="files" have=0 need=10')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 complete error="no records past \\"build.sh\\""')

    def test_an_answered_window_refuses_more(self):
        caught = []

        def fill(f):
            f.exhausted()
            for again in (lambda: f.record(1), lambda: f.done()):
                try:
                    again()
                except RuntimeError as e:
                    caught.append(str(e))

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" have=0 need=1')
        self.assertEqual(len(caught), 2, "a finished window took more")

    def test_a_long_answer_goes_out_in_batches(self):
        records = 400

        def fill(f):
            f.ordered()
            for i in range(records):
                f.record(i, name="file-%03d-%s" % (i, "x" * 80))
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" have=0 need=400')
        batches = c.since(1)
        self.assertGreater(len(batches), 1,
                           "the whole answer went in one message; it was meant to stream")
        lines = "\n".join(batches).split("\n")
        self.assertEqual(len(lines), records + 2,
                         "a declaration, every record once, and a terminator")
        self.assertEqual(lines[0], "result 1 ordered")
        for i in range(records):
            self.assertIn("key %d;" % i, lines[i + 1])
        self.assertTrue(lines[records + 1].startswith("result 1 complete"))

    def test_a_different_sequence_is_a_different_query(self):
        """A query is stated when it is made and does not change, so the id IS
        the generation: results still in flight for the sort somebody just
        abandoned cannot be taken for results of the one they chose. The
        display opens the replacement before destroying what it replaces, which
        keeps the source in use while the reader moves across."""
        specs = []
        c, _ = serve_one(lambda f: (specs.append(f.spec), f.exhausted()))

        send(c, 'q=new query source="files" sort={ name natural } have=0 need=1')
        n = len(c.sent)
        send(c, 'r=new query source="files" sort={ size desc } have=0 need=1')
        answered = "\n".join(c.sent[n:])
        self.assertIn("reply r=2", answered,
                      "the second query was not named in its own right")
        self.assertEqual([s.sort[0].field for s in specs], ["name", "size"])
        self.assertEqual(len(c.queries()), 2)

        send(c, "destroy 1")
        self.assertIsNone(c.query(1))
        self.assertIsNotNone(c.query(2),
                             "destroying the old query took the new one too")

    def test_a_query_cannot_be_restated(self):
        c, _ = serve_one(lambda f: f.exhausted())
        send(c, 'q=new query source="files" sort={ name natural } have=0 need=1')

        n = len(c.sent)
        send(c, "set 1 sort={ size desc }")
        self.assertIn("error", "\n".join(c.sent[n:]),
                      "a restatement was accepted")
        self.assertEqual(c.query(1).spec().sort[0].field, "name",
                         "the refused restatement changed the query anyway")

    def test_the_display_can_drop_the_query(self):
        dropped, served = [], []
        c, s = serve_one(lambda f: (served.append(f), f.exhausted()))
        s.on_dropped(dropped.append)

        send(c, 'q=new query source="files" have=0 need=1')
        send(c, "destroy 1")
        self.assertEqual(len(dropped), 1, "the application was not told")
        self.assertIsNone(c.query(1))
        send(c, "query 1 have=0 need=1")
        self.assertEqual(len(served), 1, "a query that was let go was served again")

    def test_what_the_library_does_not_know_reaches_the_app(self):
        got = []
        c, s = serve_one(lambda f: f.exhausted())
        s.on_statement(lambda q, stmt: got.append(stmt))

        send(c, 'q=new query source="files" have=0 need=1')
        send(c, "do 1 cover handle=3 from={ key 1 } to={ key 200 }")
        self.assertEqual(len(got), 1)
        # Whole means whole: the target, the action word, and its three arguments.
        self.assertEqual(got[0].verb, "do")
        self.assertEqual(len(got[0].args), 5)

    def test_an_unknown_source_is_refused(self):
        c, _ = serve_one(lambda f: None)
        send(c, 'q=new query source="ledgers" have=0 need=1')
        self.assertTrue(c.sent[0].startswith("error text="), c.sent[0])
        self.assertIn("ledgers", c.sent[0])
        self.assertEqual(c.queries(), [])


if __name__ == "__main__":
    unittest.main()


class OrderIsDeclaredUpFrontTest(unittest.TestCase):
    """The order is declared before the records or not at all. One declared
    after a record has gone out is too late to be true of what has already
    crossed, so it is dropped rather than sent."""

    def test_a_late_declaration_is_not_sent(self):
        def fill(f):
            f.record(1, name="alpha")
            f.ordered()  # too late
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" have=0 need=2')
        self.assertNotIn("ordered", "\n".join(c.sent))

    def test_it_is_declared_once(self):
        def fill(f):
            f.ordered()
            f.ordered()
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" have=0 need=2')
        self.assertEqual("\n".join(c.sent).count("ordered"), 1)
