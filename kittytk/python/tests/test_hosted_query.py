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
    provide_source = client.Conn.provide_source
    query = client.Conn.query
    queries = client.Conn.queries
    _mint_id = client.Conn._mint_id
    inbound_batch = client.Conn.inbound_batch
    _inbound_one = client.Conn._inbound_one
    _open_query = client.Conn._open_query

    def since(self, n):
        return list(self.sent[n:])


def serve_one(fill):
    c = Recorder()
    return c, c.provide_source("files", fill)


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
        send(c, 'q=new query source="files" sort={ name natural } count=30')
        self.assertEqual(c.sent[0], "reply q=1")
        self.assertEqual(served[0].query.id(), 1)
        self.assertEqual(c.query(1).source().name(), "files")

    def test_the_reply_comes_before_the_records(self):
        def fill(f):
            f.ordered()
            f.record(17, name="src/parser.go", size=1024)
            f.record(42, name="src/window.go", size=2048)
            f.filled(42)

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" sort={ name natural } count=30')
        self.assertEqual("\n".join(c.sent), "\n".join([
            "reply q=1",
            # The order is declared before the records rather than after them,
            # which is the only place a far end can act on it -- so it rides on
            # the first of them, and the terminator rides on the last.
            'result 1 ordered id=17 record={ name "src/parser.go"; size 1024 }',
            'result 1 id=42 record={ name "src/window.go"; size 2048 }'
            ' complete watermark=42 filled',
        ]))

    def test_a_scope_arrives_taken_apart(self):
        got = []
        c, _ = serve_one(lambda f: (got.append(f), f.exhausted()))
        send(c, 'q=new query source="files" sort={ name natural }'
                ' fields={ name; size } after=17 until=42 count=50 reversed')
        f = got[-1]
        self.assertEqual(f.count, 50)
        self.assertEqual(f.after.number, 17)
        self.assertEqual(f.until.number, 42)
        self.assertTrue(f.reversed)
        # The sequence comes with it, so the handler need not have kept it.
        self.assertEqual(f.descriptor.source, "files")
        self.assertEqual(f.descriptor.fields.names(), ["name", "size"])

    def test_each_scope_is_a_query_of_its_own(self):
        asked = []
        c, _ = serve_one(lambda f: (asked.append(f.count), f.exhausted()))
        send(c, 'q=new query source="files" count=5\n'
                'r=new query source="files" after=4 count=10')
        self.assertEqual(asked, [5, 10])

    def test_the_simplest_answer_is_everything_and_exhausted(self):
        def fill(f):
            f.record("a")
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 id="a" record={} complete exhausted')

    def test_a_whole_record_and_a_subset_cross_under_different_words(self):
        # A whole record answers any question about that record, so whoever
        # asked can keep it and answer the next query out of it; a subset
        # answers the one question that asked for it. Neither end can work that
        # out from the fields alone, so the answer says which it is.
        def fill(f):
            f.record(17, name="src/parser.go", size=1024)
            f.subset(42, named=3, name="src/window.go")
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10 fields={ name }')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 id=17 record={ name "src/parser.go"; size 1024 }\n'
                         'result 1 id=42 fields={ name "src/window.go" } map=3'
                         ' complete exhausted')

    def test_places_lead_the_order_settles_and_the_total_ends(self):
        # A place says where a record stands and whatever is known of it, under
        # a verb of its own. Places are ADDITIONAL: every record still arrives
        # as a result, so a reader that does not know the verb skips them and
        # is left with the same answer.
        #
        # The completion riding a place ends the ORDER and not the scope --
        # every record has now been named, and no further one will turn up
        # between two already sent -- and the total rides the terminator.
        def fill(f):
            f.ordered()
            f.place(17, name="src/parser.go")
            f.place(42)
            f.placed(query.STOP_EXHAUSTED)
            f.record(17, name="src/parser.go", size=1024)
            f.record(42, name="src/window.go", size=2048)
            f.total(2, exact=True)
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10')
        self.assertEqual("\n".join(c.since(1)),
                         'place 1 ordered id=17 fields={ name "src/parser.go" }\n'
                         'place 1 id=42 fields={}\n'
                         'place 1 complete exhausted\n'
                         'result 1 id=17 record={ name "src/parser.go"; size 1024 }\n'
                         'result 1 id=42 record={ name "src/window.go"; size 2048 }'
                         ' complete exhausted total=2 exact')

    def test_an_answer_says_where_it_began(self):
        # **`first` is what answers `from`.** A scope carrying a position asks to
        # begin NEAR somewhere, and an application walking its own body may honour
        # that not at all -- so an application that DOES honour it says where it
        # began, and one that does not says nothing and is read as having started
        # at the beginning.
        #
        # It has no weak form, unlike a count: either the position is here or
        # nothing is. And nought is a position like any other, so saying it crosses.
        def fill(f):
            f.first(f.scope.from_)
            f.ordered()
            f.record(42, name="src/window.go")
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" from=900 count=1')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 ordered id=42 record={ name "src/window.go" }'
                         ' complete exhausted first=900')

    def test_a_position_of_nothing_still_crosses(self):
        # Nought is where a sequence begins and is a position like any other, so an
        # application saying it has said something -- which is what tells a naive
        # answer apart from one that honoured the request and landed at the top.
        def fill(f):
            f.first(0)
            f.ordered()
            f.record(42)
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" from=900 count=1')
        self.assertIn("first=0", "\n".join(c.since(1)))

    def test_under_extend_a_result_carries_only_what_its_place_did_not(self):
        # The display saying it will HOLD the places it is sent, so a result may
        # leave out what its place already carried -- and in the limit carry no
        # fields at all, just the counts, which is the claim only a result can
        # make. Nothing an author writes changes either way.
        def fill(f):
            f.place(17, name="src/parser.go")
            f.record(17, name="src/parser.go", size=1024)
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10 extend')
        self.assertEqual("\n".join(c.since(1)),
                         'place 1 id=17 fields={ name "src/parser.go" }\n'
                         'result 1 id=17 fields={ size 1024 } map=2'
                         ' complete exhausted')

    def test_without_extend_every_result_carries_the_lot(self):
        # Saying nothing is replace, and the places are then pure decoration a
        # reader may drop on the floor.
        def fill(f):
            f.place(17, name="src/parser.go")
            f.record(17, name="src/parser.go", size=1024)
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10')
        self.assertEqual("\n".join(c.since(1)),
                         'place 1 id=17 fields={ name "src/parser.go" }\n'
                         'result 1 id=17 record={ name "src/parser.go"; size 1024 }'
                         ' complete exhausted')

    def test_a_subset_counts_towards_what_was_sent(self):
        sent = []

        def fill(f):
            f.record(1, name="a")
            f.subset(2, named=3, name="b")
            sent.append(f.sent())
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=10')
        self.assertEqual(sent, [2])

    def test_a_refusal_is_an_answer(self):
        c, _ = serve_one(lambda f: f.fail('no records past "build.sh"'))
        send(c, 'q=new query source="files" count=10')
        self.assertEqual("\n".join(c.since(1)),
                         'result 1 complete error="no records past \\"build.sh\\""')

    def test_an_answered_scope_refuses_more(self):
        caught = []

        def fill(f):
            f.exhausted()
            for again in (lambda: f.record(1), lambda: f.filled(1)):
                try:
                    again()
                except RuntimeError as e:
                    caught.append(str(e))

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=1')
        self.assertEqual(len(caught), 2, "a finished scope took more")

    def test_a_long_answer_goes_out_in_batches(self):
        records = 400

        def fill(f):
            f.ordered()
            for i in range(records):
                f.record(i, name="file-%03d-%s" % (i, "x" * 80))
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=400')
        batches = c.since(1)
        self.assertGreater(len(batches), 1,
                           "the whole answer went in one message; it was meant to stream")
        lines = "\n".join(batches).split("\n")
        self.assertEqual(len(lines), records,
                         "every record once, carrying the declaration and the "
                         "terminator between them")
        self.assertTrue(lines[0].startswith("result 1 ordered id=0 "))
        for i in range(records):
            self.assertIn("id=%d " % i, lines[i])
        self.assertIn("complete exhausted", lines[records - 1])

    def test_a_different_sequence_is_a_different_query(self):
        """A query is stated when it is made and does not change, so the id IS
        the generation: results still in flight for the sort somebody just
        abandoned cannot be taken for results of the one they chose. The
        display opens the replacement before destroying what it replaces, which
        keeps the source in use while the reader moves across."""
        specs = []
        c, _ = serve_one(lambda f: (specs.append(f.descriptor), f.exhausted()))

        send(c, 'q=new query source="files" sort={ name natural } count=1')
        n = len(c.sent)
        send(c, 'r=new query source="files" sort={ size desc } count=1')
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
        send(c, 'q=new query source="files" sort={ name natural } count=1')

        n = len(c.sent)
        send(c, "set 1 sort={ size desc }")
        self.assertIn("error", "\n".join(c.sent[n:]),
                      "a restatement was accepted")
        self.assertEqual(c.query(1).descriptor().sort[0].field, "name",
                         "the refused restatement changed the query anyway")

    def test_the_display_can_drop_the_query(self):
        dropped, served = [], []
        c, s = serve_one(lambda f: (served.append(f), f.exhausted()))
        s.on_dropped(dropped.append)

        send(c, 'q=new query source="files" count=1')
        send(c, "destroy 1")
        self.assertEqual(len(dropped), 1, "the application was not told")
        self.assertIsNone(c.query(1))
        send(c, "query 1 count=1")
        self.assertEqual(len(served), 1, "a query that was let go was served again")

    def test_what_the_library_does_not_know_reaches_the_app(self):
        got = []
        c, s = serve_one(lambda f: f.exhausted())
        s.on_statement(lambda q, stmt: got.append(stmt))

        send(c, 'q=new query source="files" count=1')
        send(c, "do 1 cover handle=3 from={ key 1 } to={ key 200 }")
        self.assertEqual(len(got), 1)
        # Whole means whole: the target, the action word, and its three arguments.
        self.assertEqual(got[0].verb, "do")
        self.assertEqual(len(got[0].args), 5)

    def test_an_unknown_source_is_refused(self):
        c, _ = serve_one(lambda f: None)
        send(c, 'q=new query source="ledgers" count=1')
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
        send(c, 'q=new query source="files" count=2')
        self.assertNotIn("ordered", "\n".join(c.sent))

    def test_it_is_declared_once(self):
        def fill(f):
            f.ordered()
            f.ordered()
            f.exhausted()

        c, _ = serve_one(fill)
        send(c, 'q=new query source="files" count=2')
        self.assertEqual("\n".join(c.sent).count("ordered"), 1)
