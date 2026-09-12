"""What an application has to write to host a query, and what it never has to.

It never parses. The statement the display sent is taken apart before the
handler sees it, so the handler reads attributes off an object. And it never
has to hold the whole answer: records go out in batches as they accumulate.

The Go side of this is client/00_a_query_reaches_the_app_taken_apart_test.go,
and the two are meant to stay recognisably the same test.

    python3 -m unittest discover -s python/tests
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from kittytk import client, protocol, query  # noqa: E402


class Recorder:
    """A connection that answers `new` with an id and keeps what was sent,
    standing in for the socket."""

    def __init__(self):
        self.sent = []
        self._lock = __import__("threading").Lock()
        self._hosted = {}

    def exec(self, src):
        self.sent.append(src)
        if src.startswith("q=new "):
            return {"q": 7}
        return {}

    # The three the query machinery calls on a connection.
    host_query = client.Conn.host_query
    hosted = client.Conn.hosted
    inbound = client.Conn.inbound

    def since(self, n):
        return list(self.sent[n:])


def host_one(fill):
    c = Recorder()
    spec = query.Spec(source="files",
                      sort=[query.SortLevel(field="name", collation="natural")])
    return c, c.host_query(spec, fill)


def send(c, src):
    for stmt in protocol.parse(src).statements:
        c.inbound(stmt)


class HostedQueryTest(unittest.TestCase):
    def test_a_query_is_announced_with_its_spec(self):
        c, q = host_one(lambda f: None)
        self.assertEqual(q.id(), 7)
        self.assertEqual(c.sent[0], 'q=new query source="files" sort={ name natural }')

    def test_a_fill_arrives_taken_apart(self):
        got = []
        c, _ = host_one(lambda f: (got.append(f), f.exhausted()))
        send(c, 'ask 7 fill tag=4 from={ name "README.md"; key 17 }'
                ' to={ name "build.sh"; key 42 } have=30 need=50 fields={ name; size }')
        f = got[0]
        self.assertEqual(f.tag, 4)
        self.assertEqual((f.have, f.need), (30, 50))
        self.assertEqual(f.from_.get("name").str, "README.md")
        self.assertEqual(f.from_.key().number, 17)
        self.assertEqual(f.to.key().number, 42)
        self.assertEqual(f.fields.names(), ["name", "size"])
        # The sequence comes with it, so the handler need not have kept it.
        self.assertEqual(f.spec.source, "files")

    def test_an_answer_is_records_then_a_terminator(self):
        def fill(f):
            f.ordered()
            f.record(17, name="src/parser.go", size=1024)
            f.record(42, name="src/window.go", size=2048)
            f.done([protocol.named("name", "src/window.go"),
                    protocol.named(query.KEY_FIELD, 42)])

        c, _ = host_one(fill)
        n = len(c.sent)
        send(c, "ask 7 fill tag=9 have=0 need=2")
        self.assertEqual("\n".join(c.since(n)).split("\n"), [
            'event query_record query=7 tag=9 fields={ key 17; name "src/parser.go"; size 1024 }',
            'event query_record query=7 tag=9 fields={ key 42; name "src/window.go"; size 2048 }',
            'event query_filled query=7 tag=9 ordered watermark={ name "src/window.go"; key 42 }',
        ])

    def test_the_simplest_answer_is_everything_and_exhausted(self):
        def fill(f):
            f.record("a")
            f.exhausted()

        c, _ = host_one(fill)
        n = len(c.sent)
        send(c, "ask 7 fill tag=1 have=0 need=10")
        self.assertEqual("\n".join(c.since(n)),
                         'event query_record query=7 tag=1 fields={ key "a" }\n'
                         'event query_filled query=7 tag=1 exhausted')

    def test_a_refusal_is_an_answer(self):
        c, _ = host_one(lambda f: f.fail('no records past "build.sh"'))
        n = len(c.sent)
        send(c, "ask 7 fill tag=2 have=0 need=10")
        self.assertEqual("\n".join(c.since(n)),
                         'event query_filled query=7 tag=2 error="no records past \\"build.sh\\""')

    def test_an_answered_fill_refuses_more(self):
        caught = []

        def fill(f):
            f.exhausted()
            for again in (lambda: f.record(1), lambda: f.done()):
                try:
                    again()
                except RuntimeError as e:
                    caught.append(str(e))

        c, _ = host_one(fill)
        send(c, "ask 7 fill tag=3 have=0 need=1")
        self.assertEqual(len(caught), 2, "a finished fill took more")

    def test_a_long_answer_goes_out_in_batches(self):
        records = 400

        def fill(f):
            f.ordered()
            for i in range(records):
                f.record(i, name="file-%03d-%s" % (i, "x" * 80))
            f.exhausted()

        c, _ = host_one(fill)
        n = len(c.sent)
        send(c, "ask 7 fill tag=5 have=0 need=400")
        batches = c.since(n)
        self.assertGreater(len(batches), 1,
                           "the whole answer went in one message; it was meant to stream")
        lines = "\n".join(batches).split("\n")
        self.assertEqual(len(lines), records + 1)
        for i in range(records):
            self.assertIn("key %d;" % i, lines[i])
        self.assertTrue(lines[records].startswith("event query_filled "))

    def test_the_display_can_restate_the_sequence(self):
        told, at_fill = [], []
        c, q = host_one(lambda f: (at_fill.append(f.spec), f.exhausted()))
        q.on_respec(told.append)

        send(c, "set 7 sort={ size desc; name fold } filter={ ge size 1024 }")
        self.assertEqual(len(told), 1, "the application was not told")
        self.assertEqual([(l.field, l.descending) for l in told[0].sort],
                         [("size", True), ("name", False)])
        self.assertEqual(told[0].filter.children[0].op, query.OP_GE)

        send(c, "ask 7 fill tag=6 have=0 need=1")
        self.assertEqual(len(at_fill[0].sort), 2, "the fill carried the old spec")

    def test_the_display_can_drop_the_query(self):
        state = {"dropped": False, "filled": False}

        def fill(f):
            state["filled"] = True
            f.exhausted()

        c, q = host_one(fill)
        q.on_dropped(lambda: state.__setitem__("dropped", True))
        send(c, "destroy 7")
        self.assertTrue(state["dropped"], "the application was not told")
        send(c, "ask 7 fill tag=1 have=0 need=1")
        self.assertFalse(state["filled"], "a query that was let go answered anyway")

    def test_what_the_library_does_not_know_reaches_the_app(self):
        got = []
        c, q = host_one(lambda f: None)
        q.on_statement(got.append)
        send(c, "do 7 cover handle=3 from={ key 1 } to={ key 200 }")
        self.assertEqual(len(got), 1)
        # Whole means whole: the target, the action word, and its three arguments.
        self.assertEqual(got[0].verb, "do")
        self.assertEqual(len(got[0].args), 5)

    def test_a_statement_for_nothing_hosted_is_dropped(self):
        called = []
        c, _ = host_one(lambda f: called.append(f))
        send(c, "ask 99 fill tag=1 have=0 need=1")
        self.assertEqual(called, [])


if __name__ == "__main__":
    unittest.main()
