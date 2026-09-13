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
            'result 1 fields={ key 17; name "src/parser.go"; size 1024 }',
            'result 1 fields={ key 42; name "src/window.go"; size 2048 }',
            'result 1 complete ordered watermark={ name "src/window.go"; key 42 }',
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
        self.assertEqual(len(lines), records + 1)
        for i in range(records):
            self.assertIn("key %d;" % i, lines[i])
        self.assertTrue(lines[records].startswith("result 1 complete"))

    def test_the_display_can_restate_the_sequence(self):
        told, at_window = [], []
        c, s = serve_one(lambda f: (at_window.append(f.spec), f.exhausted()))
        s.on_respec(lambda q, spec: told.append(spec))

        send(c, 'q=new query source="files" sort={ name natural } have=0 need=1')
        send(c, "set 1 sort={ size desc; name fold } filter={ ge size 1024 }")
        self.assertEqual(len(told), 1, "the application was not told")
        self.assertEqual([(l.field, l.descending) for l in told[0].sort],
                         [("size", True), ("name", False)])
        self.assertEqual(told[0].filter.children[0].op, query.OP_GE)

        send(c, "query 1 have=0 need=1")
        self.assertEqual(len(at_window[-1].sort), 2,
                         "the window carried the old spec")

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
