"""KittyTK app-side client (Python port of the Go `client` package).

Typed handles with synchronous-looking reads served from an app-side
replica, writes as fire-and-forget protocol statements, and event
subscriptions folded into the replica before app handlers run - the same
veneer contract as the Go client, speaking the identical wire protocol to
the identical display service.
"""

from __future__ import annotations

import os
import queue
import socket
import threading
from typing import Callable, Dict, List, Optional

from . import endpoint as _endpoint
from . import protocol
from . import query as _query
from .protocol import Event, FlagState

DISPLAY_ENV = "KITTYTK_DISPLAY"
TOKEN_ENV = "KITTYTK_TOKEN"


def default_endpoint() -> str:
    """The conventional endpoint: $KITTYTK_DISPLAY, else a per-OS default,
    matching the Go host's DefaultEndpoint so client and host always agree.

    On Windows the default is loopback TCP (tcp://127.0.0.1:9797): AF_UNIX
    is unsupported under Wine and unreliable on older Windows. Elsewhere it
    is <runtime>/kittytk/display-0.sock, where <runtime> is $XDG_RUNTIME_DIR,
    else Go's os.TempDir() (which is $TMPDIR, else /tmp; on macOS $TMPDIR is
    /var/folders/.../T - NOT /tmp - so this must consult it)."""
    p = os.environ.get(DISPLAY_ENV)
    if p:
        return p
    if os.name == "nt":
        return "tcp://127.0.0.1:9797"
    runtime = (os.environ.get("XDG_RUNTIME_DIR")
               or os.environ.get("TMPDIR")
               or "/tmp")
    return os.path.join(runtime.rstrip("/") or "/", "kittytk", "display-0.sock")


# Historical name (kept so existing callers keep working).
def default_socket_path() -> str:
    return default_endpoint()


_CLOSED = object()  # reply-queue sentinel: the transport disconnected


class _ObjState:
    __slots__ = ("checked", "text", "selected", "result")

    def __init__(self):
        self.checked = FlagState.NONE
        self.text = ""
        self.selected = -1
        self.result = ""


class Conn:
    """One connection to one display service (never global: an app may
    hold any number of connections)."""

    def __init__(self, sock: socket.socket, dispatch: Optional[Callable[[str], None]]):
        self._sock = sock
        self._rfile = sock.makefile("rb")
        self._scanner = protocol.Scanner(self._rfile)
        self._dispatch = dispatch

        self._write_lock = threading.Lock()
        self._replies: "queue.Queue" = queue.Queue()
        self._events: "queue.Queue" = queue.Queue()
        # describe (D24): flat vocabulary statements buffered until the
        # reply that terminates the batch.
        self._pending_desc: List[str] = []

        self._lock = threading.Lock()
        self._state: Dict[int, _ObjState] = {}
        self._handlers: Dict[int, Dict[str, List[Callable[[Event], None]]]] = {}
        self._type_handlers: Dict[str, List[Callable[[Event], None]]] = {}
        self._subs = set()
        # What this application serves, and what the display is currently
        # reading of it. Statements arriving for one of these are the other
        # direction of the wire: the display asking, rather than being told
        # (the query section below).
        #
        # _last_hosted_id names them in this application's own space: each end
        # mints its own ids, and the direction a statement travelled says whose
        # space it is in, so the two never have to be told apart.
        self._sources: Dict[str, "Source"] = {}
        self._queries: Dict[int, "Query"] = {}
        self._last_hosted_id = 0
        self._inbound: "queue.Queue" = queue.Queue()
        self._pending_in: List = []

        # What this connection is waiting to be ANSWERED, by correlation key,
        # and the counter that mints the next one. The key is minted here and
        # never written by a caller: it exists so two answers cannot be
        # confused, which is a job for whoever is doing the confusing.
        #
        # On a queue and a thread of their own, for the reason events are: an
        # answer's handler may write, and the reader has to stay free.
        self._asked: Dict[str, Callable] = {}
        self._asked_seq = 0
        self._answers: "queue.Queue" = queue.Queue()

        self._closed_flag = False
        self.closed = threading.Event()  # set when the connection ends

        # Every object the display has handed this connection, by the name it
        # knows it by: its application, its store, its handle on the display,
        # and whatever else a display offers. Empty for a connection with no
        # handshake.
        #
        # One record rather than an attribute per object, so a display that
        # hands over something new reaches a client that was never taught its
        # name -- and so an init arriving later replaces what a name meant
        # rather than leaving two answers. Guarded by _given_lock: the read
        # thread writes it.
        self._given: Dict[str, int] = {}
        self._given_lock = threading.Lock()

    # --- lifecycle -------------------------------------------------------

    def _start(self):
        threading.Thread(target=self._read_loop, daemon=True).start()
        threading.Thread(target=self._event_loop, daemon=True).start()
        threading.Thread(target=self._answer_loop, daemon=True).start()
        threading.Thread(target=self._inbound_loop, daemon=True).start()

    def _read_loop(self):
        try:
            while True:
                text = self._scanner.next()
                try:
                    script = protocol.parse(text)
                except protocol.ParseError:
                    continue  # malformed inbound statement; skip
                for stmt in script.statements:
                    if stmt.verb == "reply":
                        desc = self._pending_desc
                        self._pending_desc = []
                        try:
                            ids = protocol.decode_reply(stmt)
                            self._replies.put(("reply", ids, desc))
                        except Exception as e:  # noqa: BLE001
                            self._replies.put(("error", str(e), None))
                    elif stmt.verb == "error":
                        self._pending_desc = []
                        msg = "display error"
                        for a in stmt.args:
                            if a.name == "text" and a.value is not None \
                                    and a.value.kind == protocol.ValueKind.STRING:
                                msg = a.value.str
                        self._replies.put(("error", msg, None))
                    elif stmt.verb in ("proptype", "prop", "propcommon",
                                       "ask", "askarg", "do", "doarg",
                                       "eventfield"):
                        # Two different lines start with `ask` and with `do`:
                        # the display putting a question to something this
                        # application hosts, and the describe stream's record
                        # of a question a type CAN answer. The first is
                        # addressed to an object and so opens with a bare id;
                        # the second opens with of=.
                        if _hosted_target(stmt, None) is not None:
                            self._pending_in.append(stmt)
                        else:
                            self._pending_desc.append(text.strip())
                    elif stmt.verb in ("new", _query.QUERY_VERB, "set",
                                       "destroy", "sub", "unsub"):
                        # The other direction: the display making, asking after,
                        # or letting go of something this application holds.
                        # Gathered until the batch ends, because a batch is what
                        # gets a reply.
                        self._pending_in.append(stmt)
                    elif stmt.verb == "end":
                        # The batch is closed. Handled off the reader, because
                        # answering writes and the reader has to stay free to
                        # route what comes back.
                        if self._pending_in:
                            self._inbound.put(self._pending_in)
                            self._pending_in = []
                    elif stmt.verb == "init":
                        # The display handing over something: a new object, or
                        # a new object under a name already in hand. Not only a
                        # handshake step -- the display says it whenever it has
                        # something to give.
                        self._hand_over(stmt)
                    elif stmt.verb == _query.ANSWER_VERB:
                        # What an `ask` is answered with, quoting the key the
                        # question carried. Not an event: nothing had to have
                        # subscribed, so there is no filter to pass and no
                        # emission suppression to wait for.
                        try:
                            self._answers.put(_query.parse_answer(stmt.args))
                        except Exception:  # noqa: BLE001
                            pass
                    elif stmt.verb == "event":
                        # Two different lines start with this word: an event
                        # the display raised, and a describe stream's record of
                        # an event a type CAN raise. The first parses as an
                        # event and the second does not, which is what tells
                        # them apart.
                        try:
                            self._events.put(protocol.parse_event(text))
                        except Exception:  # noqa: BLE001
                            self._pending_desc.append(text.strip())
        except (EOFError, OSError, ValueError):
            pass
        finally:
            self._mark_closed()

    def _event_loop(self):
        while True:
            ev = self._events.get()
            if ev is None:
                return
            self.deliver(ev)

    def _answer_loop(self):
        while True:
            ans = self._answers.get()
            if ans is None:
                return
            self._deliver_answer(ans)

    def _deliver_answer(self, ans):
        """Route one answer to whoever asked, and let go when it ends.

        An answer for a question nobody is waiting on is dropped. That is not a
        failure: an asker may have given up, and an unkeyed answer belongs to a
        question whose asker never wanted to be told."""
        if not ans.to:
            return
        with self._lock:
            fn = self._asked.get(ans.to)
            # Forgotten BEFORE the handler runs, so a handler that asks the
            # same question again cannot have its new registration dropped by
            # this one ending.
            if fn is not None and ans.complete:
                del self._asked[ans.to]
        if fn is not None:
            fn(ans)

    def _inbound_loop(self):
        """The batches the display sent, in the order they arrived. On a thread
        of its own rather than sharing the event one, because serving a scope
        can take as long as the records take and a list nobody is looking at
        must not hold up a click."""
        while True:
            batch = self._inbound.get()
            if batch is None:
                return
            self.inbound_batch(batch)

    def _mark_closed(self):
        with self._lock:
            if self._closed_flag:
                return
            self._closed_flag = True
        try:
            self._sock.close()
        except OSError:
            pass
        self._replies.put(_CLOSED)   # unblock a waiting exec
        self._events.put(None)       # stop the event loop
        self._answers.put(None)      # stop the answer loop
        self._inbound.put(None)      # stop the inbound loop
        self.closed.set()

    def close(self):
        self._mark_closed()

    # --- request / reply -------------------------------------------------

    def _exec_raw(self, src: str):
        """Execute one batch; returns (ids, extra_lines) where extra_lines
        are any verb-produced statements delivered ahead of the reply
        (the describe verb's flat vocabulary). Raises on error/disconnect."""
        with self._write_lock:
            with self._lock:
                if self._closed_flag:
                    raise ConnectionError("connection closed")
            self._sock.sendall((src + "\nend\n").encode("utf-8"))
            item = self._replies.get()
            if item is _CLOSED:
                raise ConnectionError("connection closed")
            kind, payload, extra = item
            if kind == "error":
                raise RuntimeError(payload)
            return payload, (extra or [])

    def send(self, src: str):
        """Write without waiting for anything back.

        The reverse direction needs it and the forward one does not: when the
        display sends a batch, the application answers it, and an answer that
        waited for an answer of its own would never be written at all.

        What goes out this way is never a batch: a request is terminated by
        `end` and answered, and this end is answering."""
        with self._write_lock:
            with self._lock:
                if self._closed_flag:
                    return
            self._sock.sendall((src + "\n").encode("utf-8"))

    def exec(self, src: str) -> Dict[str, int]:
        """Execute one batch of protocol text; returns the surfaced
        name->id map, or raises on a display error / disconnect."""
        ids, _ = self._exec_raw(src)
        return ids

    def _hand_over(self, stmt):
        """Record an init statement: every field of it is an object the display
        has handed this connection, under the name it knows it by. A name
        already in hand is replaced, because the display saying it again is the
        display saying what that name means NOW."""
        with self._given_lock:
            for a in stmt.args:
                if (a.value is None or a.value.kind != protocol.ValueKind.NUMBER
                        or not a.value.is_int):
                    continue
                self._given[a.name] = int(a.value.number)

    def init(self, name: str) -> int:
        """The ObjectID of a thing the display handed this connection, by the
        name it knows it by -- "app", "store", "host", and whatever a display
        offers beyond them. 0 for a name it has not handed over.

        The name is also a session key the display bound, so `set <name> ...`
        says the same thing as this id does; this is what reaches the object
        when a client has taken that name for something of its own."""
        with self._given_lock:
            return self._given.get(name, 0)

    def init_names(self):
        """Every name the display has handed over, sorted."""
        with self._given_lock:
            return sorted(self._given)

    def given(self, name: str) -> "Handle":
        """A handle on one of them, by name."""
        return Handle(self, self.init(name))

    @property
    def app_id(self) -> int:
        """This connection's Application ObjectID, as reported by the display
        service in the handshake. Use it to address application-wide
        properties, e.g. conn.exec("set %d multiwindow" % conn.app_id)."""
        return self.init("app")

    @property
    def store_id(self) -> int:
        """This connection's store ObjectID. The display knows it as "store"
        too."""
        return self.init("store")

    @property
    def host_id(self) -> int:
        """This connection's handle on the display. The display knows it as
        "host" too: the terminal's theme, the desktop's font and status bar,
        whether the desktop is showing."""
        return self.init("host")

    def store(self) -> "Store":
        """The connection's store as a handle."""
        return Store(self, self.init("store"))

    def blob(self, oid: int) -> "Blob":
        """One blob of the store, by the id it was named with. An app never
        invents one: it learns ids from an inventory's answers and from
        store_blob events."""
        return Blob(self, oid)

    def host(self) -> "Handle":
        """The display itself as a handle."""
        return Handle(self, self.init("host"))

    def on_store(self, event: str, fn: Callable[[Event], None]):
        """Register a handler for one of the store's events and open the flow
        for it. Subscribing does not ask what is in the store: Store.list does
        that, and is answered rather than heard, so an app after the inventory
        need subscribe to nothing at all."""
        self.store().on(event, fn)

    def on_host(self, event: str, fn: Callable[[Event], None]):
        """Register a handler for what the display says about itself."""
        self.host().on(event, fn)

    def relay(self, to: str, text: str):
        """Carry statements to another connected application, by name. What that
        application says back arrives as EVENT_RELAY -- so subscribe with
        on_host first, or the first statements over will have nowhere to
        land."""
        self.host().do("%s to=%s text=%s" % (
            DO_RELAY, protocol.quote(to), protocol.quote(text)))

    def set_app(self, props: str) -> Dict[str, int]:
        """Apply application-wide properties with the same syntax as any
        object: set_app("multiwindow contextonly") sends
        `set <app_id> multiwindow contextonly`."""
        if not self.init("app"):
            raise RuntimeError("set_app: no application id from the handshake")
        return self.exec("set app %s" % props)

    def describe(self) -> protocol.Vocabulary:
        """Query the host's wire vocabulary (D24): the supported trinket
        types and, for each, the properties it accepts with each
        property's kind, default, and a brief description. Common
        properties (accepted by every non-virtual type) are reported once."""
        _, extra = self._exec_raw("describe")
        return protocol.decode_vocabulary(extra)

    def build(self, src: str) -> "UI":
        return UI(self, self.exec(src))

    # --- events & replica ------------------------------------------------

    def deliver(self, ev: Event):
        tid = ev.trinket() or 0
        with self._lock:
            st = self._state.get(tid)
            if st is None:
                st = _ObjState()
                self._state[tid] = st
            dispatch_action = None
            if ev.type == "toggle":
                st.checked = ev.flag("checked")
            elif ev.type == "change":
                s = ev.text("text")
                if s is not None:
                    st.text = s
                n = ev.int_("selected")
                if n is not None:
                    st.selected = n
            elif ev.type == "finish":
                w = ev.word("result")
                if w is not None:
                    st.result = w
            elif ev.type == "command":
                a = ev.word("action")
                if a is not None:
                    dispatch_action = a
            fns = list(self._handlers.get(tid, {}).get(ev.type, ()))
            fns.extend(self._type_handlers.get(ev.type, ()))
            dispatch = self._dispatch
        if dispatch_action and dispatch:
            dispatch(dispatch_action)
        for fn in fns:
            fn(ev)

    def _ensure_sub(self, oid: int, event: str):
        with self._lock:
            key = (oid, event)
            if key in self._subs:
                return
            self._subs.add(key)
        try:
            self.exec("sub %d %s" % (oid, event))
        except Exception:  # noqa: BLE001
            pass  # connection without event support; replica never updates

    def on(self, oid: int, event: str, fn: Callable[[Event], None]):
        self._ensure_sub(oid, event)
        with self._lock:
            self._handlers.setdefault(oid, {}).setdefault(event, []).append(fn)

    def on_command(self, action: str, fn: Callable[[], None]):
        def handler(ev: Event):
            if ev.word("action") == action:
                fn()
        with self._lock:
            self._type_handlers.setdefault("command", []).append(handler)

    def state_of(self, oid: int) -> _ObjState:
        with self._lock:
            st = self._state.get(oid)
            if st is None:
                st = _ObjState()
                self._state[oid] = st
            return st

    def set(self, oid: int, args: str):
        self.exec("set %d %s" % (oid, args))

    def do(self, oid: int, action: str):
        self.exec("do %d %s" % (oid, action))

    def ask(self, oid: int, question: str):
        self.exec("ask %d %s" % (oid, question))

    def ask_for(self, target, question: str, fn: Callable):
        """Put a question and call fn for each piece of the answer.

        target is the id, or the name the display already knows the object by.
        fn is called for every piece, in the order they arrive, and once more
        for the one that completes -- which arrives whether or not anything came
        before it, so a question that answered with nothing is told apart from
        one still being worked on. A refusal arrives the same way, carrying
        `error`: the question was put, so it has an answer.

        It returns once the question has been SENT. Nothing here blocks for the
        answer: a caller that wants to wait waits on something of its own inside
        fn."""
        if fn is None:
            return self.ask(int(target), question)
        with self._lock:
            self._asked_seq += 1
            key = "q%d" % self._asked_seq
            self._asked[key] = fn
        try:
            self.exec("%s=ask %s %s" % (key, target, question))
        except Exception:
            # The question never went, so nothing will ever answer it. Letting
            # go here is what keeps a failed ask from leaving a handler waiting
            # for good.
            with self._lock:
                self._asked.pop(key, None)
            raise

    def provide_source(self, name: str, fill) -> "Source":
        """Register a body of records this application can serve, and what
        answers a scope of it.

        Nothing crosses the wire here: a source is a name, not an object, and
        the display learns of it when a trinket is told `data="<name>"`."""
        if not name:
            raise ValueError("provide_source: a source needs a name")
        if fill is None:
            raise ValueError("provide_source: a source needs something to fill it")
        s = Source(self, name, fill)
        with self._lock:
            self._sources[name] = s
        return s

    def query(self, oid: int):
        """The query this connection serves under an id, and None for an id it
        serves nothing under."""
        with self._lock:
            return self._queries.get(oid)

    def queries(self):
        """What this connection is currently serving."""
        with self._lock:
            return list(self._queries.values())

    def _mint_id(self) -> int:
        """Allocate an id in this application's own space.

        Each end names its own objects and the direction of travel says whose
        space a statement is in, so these never have to be told apart from the
        display's: a statement arriving here is about what this application
        holds."""
        with self._lock:
            self._last_hosted_id += 1
            return self._last_hosted_id

    def inbound_batch(self, stmts):
        """Run one batch the display sent, answer it, and then produce whatever
        records it asked for.

        The reply goes out before any result does, because the reply is what
        names a query the display has not heard of yet. That ordering is not
        policy: the application mints the id, so it writes it before anything
        that carries it.

        It must not run on the reader: serving a scope writes, and the reader
        has to stay free."""
        ids: Dict[str, int] = {}
        keys: Dict[str, int] = {}
        pending = []
        failed = None
        for stmt in stmts:
            try:
                self._inbound_one(stmt, keys, ids, pending)
            except (_query.QueryError, protocol.ParseError, ValueError) as e:
                if failed is None:
                    failed = str(e)
        if failed is not None:
            self.send("error text=" + protocol.quote(failed))
        else:
            line = "reply" + "".join(" %s=%d" % (k, ids[k]) for k in sorted(ids))
            self.send(line)
        for fn in pending:
            fn()

    def _inbound_one(self, stmt, keys, ids, pending):
        """Route one statement. Anything meant to answer with records is
        appended to pending rather than run, so the batch is replied to first."""
        if stmt.verb == "new":
            return self._open_query(stmt, keys, ids, pending)

        oid = _hosted_target(stmt, keys)
        if oid is None:
            return  # not addressed to anything this application holds
        q = self.query(oid)
        if q is None:
            raise ValueError("%s %d: no query of mine" % (stmt.verb, oid))
        rest = stmt.args[1:]

        if stmt.verb == _query.QUERY_VERB:
            raise ValueError(
                "query %d: a query is asked when it is made and answered "
                "once; ask a new one for the next scope" % oid)
        if stmt.verb == "set":
            raise ValueError(
                "set %d: a query is the sequence it was opened with and does "
                "not change; a different sequence is a different query" % oid)
        if stmt.verb == "destroy":
            with self._lock:
                self._queries.pop(oid, None)
            with q._source._lock:
                fn = q._source._dropped
            if fn is not None:
                pending.append(lambda: fn(q))
            return
        with q._source._lock:
            fn = q._source._other
        if fn is not None:
            pending.append(lambda: fn(q, stmt))

    def _open_query(self, stmt, keys, ids, pending):
        """Make a query and ask it for its first scope, which is one statement
        because the display never wants a sequence without wanting rows of it.

        The application names it. The display has no id to offer -- ids here are
        the application's own -- so the reply is what carries it back."""
        if not stmt.args or stmt.args[0].value is not None \
                or stmt.args[0].flag != FlagState.TRUE:
            raise ValueError("new: expected a type")
        if stmt.args[0].name != _query.QUERY_VERB:
            raise ValueError("new: I host nothing called %r" % stmt.args[0].name)
        args = stmt.args[1:]

        descriptor = _query.parse_descriptor(args)
        with self._lock:
            source = self._sources.get(descriptor.source)
        if source is None:
            raise ValueError("query: I serve nothing called %r" % descriptor.source)

        q = Query(self, source, self._mint_id(), descriptor)
        with self._lock:
            self._queries[q.id()] = q
        if stmt.key:
            ids[stmt.key] = q.id()
            keys[stmt.key] = q.id()
        scope = _query.parse_scope(args)
        with q._source._lock:
            fn = q._source._fill
        f = Fill(q, scope, descriptor, _query.parse_extend(args))
        pending.append(lambda: fn(f))


# --- Handles -------------------------------------------------------------


class Handle:
    def __init__(self, conn: Conn, oid: int):
        self._c = conn
        self._id = oid

    @property
    def id(self) -> int:
        return self._id

    def valid(self) -> bool:
        return self._c is not None and self._id != 0

    def set(self, args: str):
        self._c.set(self._id, args)

    def do(self, action: str):
        """Tell the object to do something: h.do("tile") sends `do <id> tile`.
        Nothing comes back from it -- that is what separates an action from a
        question -- though what it changes may raise the object's events."""
        self._c.do(self._id, action)

    def ask(self, question: str):
        """Put a question to the object: h.ask("bytes offset=2048") sends
        `ask <id> bytes offset=2048`.

        It carries NO correlation key, so the answer carries none either and
        nothing here routes it -- which suits an asker that is not waiting, and
        nothing else. ask_for is the one to use to be told."""
        self._c.ask(self._id, question)

    def ask_for(self, question: str, fn: Callable):
        """Put a question and call fn with each piece of the answer, ending with
        the one that completes. See Conn.ask_for."""
        self._c.ask_for(self._id, question, fn)

    def destroy(self):
        self._c.exec("destroy %d" % self._id)

    def on(self, event: str, fn: Callable[[Event], None]):
        self._c.on(self._id, event, fn)


class Button(Handle):
    def on_click(self, fn: Callable[[], None]):
        self.on("click", lambda ev: fn())

    def set_caption(self, s: str):
        self.set("caption=" + protocol.quote(s))


class Label(Handle):
    def set_caption(self, s: str):
        self.set("caption=" + protocol.quote(s))


class Checkbox(Handle):
    def state(self) -> FlagState:
        s = self._c.state_of(self._id).checked
        return s if s != FlagState.NONE else FlagState.FALSE

    def checked(self) -> bool:
        return self.state() == FlagState.TRUE

    def set_checked(self, v: bool):
        st = self._c.state_of(self._id)
        if v:
            st.checked = FlagState.TRUE
            self.set("checked")
        else:
            st.checked = FlagState.FALSE
            self.set("!checked")

    def on_toggle(self, fn: Callable[[FlagState], None]):
        self.on("toggle", lambda ev: fn(ev.flag("checked")))


class TextInput(Handle):
    def text(self) -> str:
        return self._c.state_of(self._id).text

    def set_text(self, s: str):
        self._c.state_of(self._id).text = s
        self.set("text=" + protocol.quote(s))

    def on_change(self, fn: Callable[[str], None]):
        def handler(ev: Event):
            s = ev.text("text")
            if s is not None:
                fn(s)
        self.on("change", handler)


class Selector(Handle):
    def selected(self) -> int:
        return self._c.state_of(self._id).selected

    def select(self, index: int):
        self._c.state_of(self._id).selected = index
        self.set("selected=%d" % index)

    def on_change(self, fn: Callable[[int], None]):
        def handler(ev: Event):
            n = ev.int_("selected")
            if n is not None:
                fn(n)
        self.on("change", handler)


class Window(Handle):
    def on_closed(self, fn: Callable[[], None]):
        self.on("window_closed", lambda ev: fn())

    def close(self):
        self.destroy()

    def set_title(self, s: str):
        self.set("title=" + protocol.quote(s))


# --- The store -----------------------------------------------------------

# A key beginning with this names something the desktop may throw away at any
# moment, the way `#` names a temporary table in SQL. That is the only
# difference between what is kept and what is cached: one namespace, and the
# name says how long the blob lives.
CACHE_MARK = "#"

# The events the store raises. All of them name the store as their source, so
# one subscription hears everything. None of them answers a question: each is the
# store saying what has happened to a blob, which nobody asked.
STORE_BLOB = "store_blob"    # one blob: what it is and how big
STORE_GONE = "store_gone"    # a blob is no longer there
STORE_ERROR = "store_error"  # what went wrong, and with which key

# The questions the display answers. Either is answered with one answer carrying
# both flags, so one round trip settles it.
ASK_DARK = "dark"
ASK_DESKTOP = "desktop"

# The display's debug relay: DO_RELAY carries statements to another connected
# application, and EVENT_RELAY is one statement it said back. Subscribed to
# rather than asked for, because it is that application's speech and there is no
# last one to wait for.
DO_RELAY = "relay"
EVENT_RELAY = "relay"


class Store(Handle):
    """The app's whole store on the desktop: a flat set of names, each holding
    one blob.

    Two flows, and which one a thing takes is settled by whether anybody asked.
    The inventory was ASKED FOR, so `list` takes a callback and is answered.
    Writing a blob is not a question, so what the blob now is arrives as a
    store_blob event, subscribed to with Conn.on_store."""

    def write(self, key: str, typ: str, data: bytes):
        """Put a blob in the store, replacing whatever the key held.

        A key is a NAME, not a path: no slashes, nothing that is only digits,
        nothing unprintable, and `#` only at the front. typ is one of txt, psl,
        bin, ini or conf. The store then reports a store_blob naming the id the
        blob can be addressed by, which is how something larger than one
        statement is continued -- see Blob.append."""
        self.set("blobs={ new blob key=%s type=%s data=%s }" % (
            protocol.quote(key), typ, protocol.quote_blob(data)))

    def list(self, fn: Callable):
        """Ask what the store holds, calling fn for each blob and once more for
        the completion, which carries count=.

        The completion arrives whether or not a blob came before it, so a store
        holding nothing is told apart from one still being listed -- and holding
        nothing is an answer rather than a refusal."""
        self.ask_for("inventory", fn)


class Blob(Handle):
    """One blob of the store, by the id it was named with."""

    def append(self, data: bytes):
        """Add to the end, as a terminal is fed. It is how something too large
        for one statement is written: write the first piece, append the rest."""
        self.do("append bytes=" + protocol.quote_blob(data))

    def replace(self, data: bytes):
        """Write the blob's whole contents again."""
        self.set("data=" + protocol.quote_blob(data))

    def read(self, offset: int, fn: Callable):
        """Ask for the chunk that starts at offset, calling fn with it.

        One chunk COMPLETES the question: it says where it starts and whether it
        is the last, and reading on is a fresh read from the end of what
        arrived."""
        self.ask_for("bytes offset=%d" % offset, fn)

    def drop(self):
        """Take the blob out of the store, which is the whole of what it was."""
        self.destroy()


class UI:
    """Handle access to one build's surfaced names."""

    def __init__(self, conn: Conn, ids: Dict[str, int]):
        self._conn = conn
        self._ids = ids

    def id(self, name: str) -> int:
        return self._ids.get(name, 0)

    def has(self, name: str) -> bool:
        return name in self._ids

    def _handle(self, name: str, *mirrors: str):
        oid = self._ids.get(name, 0)
        if oid != 0:
            for ev in mirrors:
                self._conn._ensure_sub(oid, ev)
        return oid

    def object(self, name: str) -> Handle:
        return Handle(self._conn, self._handle(name))

    def button(self, name: str) -> Button:
        return Button(self._conn, self._handle(name))

    def label(self, name: str) -> Label:
        return Label(self._conn, self._handle(name))

    def checkbox(self, name: str) -> Checkbox:
        return Checkbox(self._conn, self._handle(name, "toggle"))

    def text_input(self, name: str) -> TextInput:
        return TextInput(self._conn, self._handle(name, "change"))

    def selector(self, name: str) -> Selector:
        return Selector(self._conn, self._handle(name, "change"))

    def window(self, name: str) -> Window:
        return Window(self._conn, self._handle(name))


# --- Dial ----------------------------------------------------------------

def _dial(endpoint_str: str, app_name: str, dispatch, solo: bool,
          token=None, insecure=False, known_hosts=None,
          ssl_context=None) -> Conn:
    sock = _endpoint.connect(endpoint_str, insecure=insecure,
                             known_hosts=known_hosts, ssl_context=ssl_context)
    conn = Conn(sock, dispatch)

    if token is None:
        token = os.environ.get(TOKEN_ENV)

    hello = "hello version=1 app=" + protocol.quote(app_name)
    if solo:
        hello += " solo"
    if token:
        hello += " token=" + protocol.quote(token)
    sock.sendall((hello + "\nend\n").encode("utf-8"))

    welcome = conn._scanner.next()
    script = protocol.parse(welcome)
    if not script.statements or script.statements[0].verb != "welcome":
        sock.close()
        raise ConnectionError("handshake: unexpected response %r" % welcome)

    # Then an init statement: what this connection was handed, one field per
    # object. Every field of it is an object, so a client reads them all
    # without being taught the names (see Conn.init).
    #
    # This first one is read here so the ids are in hand before dial returns.
    # Later ones arrive on the read thread like anything else.
    init = conn._scanner.next()
    init_script = protocol.parse(init)
    if not init_script.statements or init_script.statements[0].verb != "init":
        sock.close()
        raise ConnectionError("handshake: unexpected init %r" % init)
    conn._hand_over(init_script.statements[0])

    conn._start()
    return conn


def dial(endpoint: str, app_name: str, dispatch=None, *, token=None,
         insecure=False, known_hosts=None, ssl_context=None) -> Conn:
    """Connect to a display service. endpoint is a unix socket path or a
    tcp://host:port / tls://host:port URL. dispatch (optional) receives
    action= command IDs; token (optional, else $KITTYTK_TOKEN) authorizes
    the client in the handshake."""
    return _dial(endpoint, app_name, dispatch, False, token=token,
                 insecure=insecure, known_hosts=known_hosts,
                 ssl_context=ssl_context)


def dial_solo(endpoint: str, app_name: str, dispatch=None, *, token=None,
              insecure=False, known_hosts=None, ssl_context=None) -> Conn:
    """dial() for an app that wants to be the whole display (its `main`
    window replaces the desktop)."""
    return _dial(endpoint, app_name, dispatch, True, token=token,
                 insecure=insecure, known_hosts=known_hosts,
                 ssl_context=ssl_context)


# --- serving a query ------------------------------------------------------
#
# Everything above points one way: the application says `new`, `set`, `ask`,
# `do`, and the display raises events at it. A query points the other way. Only
# the display knows a query is wanted and what it is -- the sort comes from the
# column header somebody clicked, the filter from the filter box, the scope
# from the scroll position -- so the display opens it, and the application,
# which is the end that holds the records, serves it.
#
# What an author writes is one function: given a scope of the sequence,
# produce the records in it. The statement is taken apart before it gets here,
# so nothing in that function parses anything; and the answer is written into a
# sink that goes out in batches as it fills, so a million records need not be
# one message, or one uninterruptible piece of work.
#
# It arrives nowhere near the event line. `query` is answered by `result`,
# `ask` by `answer`, and `sub` -- or an object's mere existence -- by `event`;
# nothing carries two of them, so a request for records can never be mistaken
# for something a subscription raised.
#
# docs/hosting-a-query.md is the wire spelling. kittytk/query.py is the
# structure.

# How much answer accumulates before it goes out on its own. It trades write
# syscalls against how long a record waits: big enough that a scope of a
# screenful is one message, small enough that a scope of a million records is
# not held in memory.
FLUSH_BYTES = 16 * 1024


def _hosted_target(stmt, keys):
    """The object a statement is addressed to: a bare id, or a key this same
    batch surfaced -- which is what lets a display open a query and address it
    again without waiting for the reply. None when it is addressed to nothing."""
    if not stmt.args:
        return None
    a = stmt.args[0]
    if a.value is not None and not a.name \
            and a.value.kind == protocol.ValueKind.NUMBER \
            and a.value.is_int and a.value.number >= 0:
        return int(a.value.number)
    if a.value is None and a.flag == FlagState.TRUE and keys:
        return keys.get(a.name)
    return None


class Source:
    """A body of records this application can serve, under the name a display
    asks for it by.

    It is not an object and has no id. The application says `data="files"` on
    whatever trinket is to show it, and the display opens queries against that
    name when somebody scrolls."""

    def __init__(self, conn: "Conn", name: str, fill):
        self._conn = conn
        self._name = name
        self._lock = threading.Lock()
        self._fill = fill
        self._dropped = None
        self._other = None

    def name(self) -> str:
        """What a display asks for this source by."""
        return self._name

    def on_dropped(self, fn):
        """A handler for the display letting a query go, which is one reader
        finishing.

        Records are held against the SOURCE rather than against any one query,
        so an application that materialised something may let it go when the
        last query against that source has gone -- which is why a display opens
        the query it is replacing something with before destroying the old
        one."""
        with self._lock:
            self._dropped = fn

    def stale(self, key=None, how=_query.CHANGE_REPLACED, *fields):
        """Say something about these records has stopped being true.

        **The one thing an application says first.** Everything else it writes
        answers a query the display put; only the application knows its own
        records moved, and nothing on the display's end can find out.
        Invalidation is told, never decided -- a display nobody tells goes on
        showing what it has.

        It causes forgetting rather than traffic: what is no longer true is let
        go of, and whether a replacement is ever asked for is the display's
        decision. A row scrolled out of view an hour ago may never be read
        again.

        Widening is free and narrowing is fatal. A key of None is every record
        of this source, and an alteration naming no field is every field -- so
        a source that cannot tell what moved says the wider thing and pays a
        refill. Saying less than happened is a missed invalidation: silent, and
        permanent."""
        # Refused here rather than written and refused at the far end, where the
        # only thing that comes back is silence: a notice is not answered, so a
        # source that wrote a contradiction would never learn it had.
        if how != _query.CHANGE_ALTERED and fields:
            raise ValueError(
                "stale: fields name what an alteration touched, and this is %s" % how)
        self._conn.send(_query.encode_stale(_query.Stale(
            source=self._name, id=key, how=how, fields=list(fields))))

    def on_statement(self, fn):
        """A handler for anything else the display addresses to one of this
        source's queries: a question this library does not know, an action, a
        property it does not read. The statement arrives as it parsed.

        This is the seam a fuller library is built on. Coverage travels this way
        and is not implemented here; invalidation has its own verb -- see
        Source.stale."""
        with self._lock:
            self._other = fn


class Query:
    """One sequence of a source's records that a display is reading: one
    filter, one sort, and a scope asked for at a time.

    The display opens it; the application names it, because the ids in every
    statement that follows are the application's own."""

    def __init__(self, conn: "Conn", source: Source, oid: int, descriptor):
        self._conn = conn
        self._source = source
        self._id = oid
        self._lock = threading.Lock()
        self._spec = descriptor

    def id(self) -> int:
        """What this application calls the query, and what the display
        addresses it by from the moment the reply carries it."""
        return self._id

    def source(self) -> Source:
        """Where its records come from."""
        return self._source

    def descriptor(self):
        """The sequence this query names, which is what it was opened with. A
        query does not change."""
        with self._lock:
            return self._spec


def _ident(v) -> str:
    """One identity as a dict key, for the places one answer is holding.

    How a value is WRITTEN, which is not how identity is decided anywhere that
    matters -- but both the place and the result come from the same author
    passing the same value, and this never leaves the answer it was made for."""
    return protocol.encode_value(v)


def _member_index(name: str):
    """A field name read as an ordered member's index, or None.

    Its position written out -- `0`, `1`, `.2` -- and only in that spelling: no
    sign, and no leading zeros but `0` itself. So `007` is a NAME that happens
    to be digits, which is the same distinction everything else here makes."""
    if name.startswith("."):
        name = name[1:]
    if not name or not name.isdigit() or (len(name) > 1 and name[0] == "0"):
        return None
    return int(name)


def _tally(fields):
    """How many members a record carries, by name and by position.

    An absence is not one: a name sent with nothing under it is knowledge ABOUT
    the record rather than a member of it, and counting it would say the record
    had a member it has not."""
    named = ordered = 0
    for f in fields:
        if f.value is None:
            continue
        if _member_index(f.name) is None:
            named += 1
        else:
            ordered += 1
    return named, ordered


def _lacking(fields, already):
    """A record without the members another already carried.

    By NAME, and not by name and value. Whoever asked merges what arrives into
    what it holds and the first answer about a field stands, so a source that
    contradicted itself inside one answer would be believed at its first word
    either way."""
    if not already:
        return fields
    seen = {f.name for f in already}
    out = _query.Fields()
    for f in fields:
        if f.name not in seen:
            out.append(f)
    return out


class Fill:
    """One scope of the sequence, asked for -- and where the records that
    answer it are written.

    The reading side is what was asked: start past after, walk the way
    reversed says, and send count records -- stopping early if you reach until,
    which is a record the display already holds.

    The writing side is record, as many times as there are records, and then
    one of filled, joined, exhausted or fail. Records go out in batches as they
    accumulate, so the answer may be produced over as long as it takes and
    interleaved with other work; nothing has to be held until the end."""

    def __init__(self, query: Query, scope, descriptor, extend=False):
        self.query = query
        self.descriptor = descriptor
        self.scope = scope
        self.after = scope.after
        self.until = scope.until
        self.count = scope.count
        self.reversed = scope.reversed

        self._lock = threading.Lock()
        self._buf: List[str] = []
        self._size = 0
        self._sent = 0       # statements written, which is what decides a flush
        self._records = 0    # records among them, which is what sent() reports
        self._ordered = False
        self._waiting = None  # the last record, held so the end can ride on it
        self._total = (0, False)  # how many the sequence has, and whether exact
        self._first = None        # where this answer began, where it says
        # The display said it will HOLD the places it is sent, so a result may
        # leave out what its place already carried -- and _placed is what each
        # of them did carry. Nothing an author writes changes: they place what
        # they have and send the record when they have it, and the leaving-out
        # happens here.
        self._extend = extend
        self._placed = {}
        self._closed = False

    def record(self, id, **fields):
        """One whole record: its identity, and every field it has.

            f.record(17, name="src/parser.go", size=1024)

        Whole matters beyond this answer. A record that arrived entire answers
        any question about that record, so whoever asked can keep it and use it
        for the next query as well; part of one answers only the question that
        asked for it. So say record when these are all the fields there are,
        and subset when they are the ones somebody asked for.

        The identity names the record and travels beside the fields rather
        than among them: a record is free to carry a field called `key` of its
        own, and that field is data like any other."""
        self._write(True, id, fields)

    def subset(self, id, named=0, ordered=0, **fields):
        """Some of a record: its identity, HOW MANY MEMBERS THE RECORD HAS, and
        the fields this query asked for. It crosses as
        `fields={ ... } map=5 len=3`.

            f.subset(17, named=5, name="parser.go")

        The totals are what keep a subset worth more than the one question it
        answered. Say a record has five named members and send two, and whoever
        asked knows three are missing; send the other three later and they know
        they now hold the lot. Say a record has three members standing by
        POSITION and they know `0`, `1` and `2` are all there is, so `3` is
        answered without anyone being asked -- an ordered member being named by
        where it stands.

        Count members only. A name sent as undefined is a guarantee that the
        record has NOT got it, which is worth sending rather than leaving out,
        and it is not counted: counting it would say the record had a member it
        has not.

        Either count may be zero, and a zero is not written."""
        self._write(False, id, fields, named, ordered)

    def place(self, id, **fields):
        """Add a PLACE: a record's position in the sequence, and whatever is
        known of it so far.

            f.place(17, name="src/parser.go")

        **Its fields are true and its silence is not.** What is sent can be
        believed; what is missing is not a claim that the record has not got it.
        Which is the whole difference between this and subset, and why a place
        carries no totals.

        Use it for a row whose position is known sooner than its contents, so
        that whoever asked can lay out its rows and stay reactive while the
        values arrive behind them. A record you can send outright is worth
        sending outright: a result with no place before it settles where the row
        stands and what it holds at once.

        **Places are additional, never substitutional.** Every record still
        arrives as a result before the answer ends, so a reader that does not
        know this verb skips these statements and is left with exactly the
        answer it would have got."""
        bag = _query.Fields()
        for name, v in fields.items():
            bag.append(protocol.named(name, v))
        self._placing(_query.Result(place=True, id=protocol.val(id), fields=bag))

    def placed(self, stop, watermark=None):
        """Say the ORDER is settled: every record of this scope has now been
        named, under place or as a result, and no further one will turn up
        between two already sent.

            f.placed(kittytk.STOP_FILLED, 42)
            f.placed(kittytk.STOP_EXHAUSTED)

        It carries the same claim the terminator will -- how the walk ended, and
        the watermark where there is one -- and is worth sending only where the
        order settles SOONER than the answer does. A reader cannot lay out a
        sequence, not even one of placeholders, until it knows it has all the
        rows; where the two moments are the same there is nothing to send,
        because the terminator settles the order too."""
        done = _query.Complete(stop=stop)
        if watermark is not None:
            done.watermark = protocol.val(watermark)
        self._placing(_query.Result(place=True, complete=done))

    def first(self, at: int):
        """Say where in the sequence this answer BEGAN: the position of its first
        record, counted in the sequence's own order however the scope walked it.

            f.first(900)   # it starts at the nine hundredth record

        **It is what answers `from`.** A scope carrying a position asks to begin
        NEAR somewhere, and a source honours that as well as it can -- which for
        an application walking its own body may be not at all, there being no
        index into a sequence the display named. Saying where the answer actually
        began is what turns "as well as it can" into something a reader can use.

        **Silence means the beginning**, for a scope that asked for a position:
        that is what a source which ignored it did, and it is the only reading
        that cannot misplace a record. So an application that HONOURS `from` is
        the one with something to say here, and a naive one has nothing to do --
        answering from the top and sending more records than were wanted is
        slower and is never wrong.

        There is nothing to say for a scope that named `after` instead: the
        record it starts past is the position, said better."""
        with self._lock:
            if not self._closed:
                self._first = at

    def total(self, n: int, exact: bool = False):
        """Say how many records the whole SEQUENCE has, which rides out on
        whatever ends this answer.

            f.total(20, exact=True)   # twenty, counted
            f.total(20)               # twenty so far, and there may be more

        Optional, and about the sequence rather than this scope of it: how many
        came back is something whoever asked can count. Say it where you know it
        cheaply and say nothing where you do not."""
        with self._lock:
            if not self._closed:
                self._total = (n, exact)

    def _placing(self, place):
        """Send one place statement at once, flushing whatever record was held
        back for a terminator to ride on.

        Not held itself: a place is a statement under another verb, so nothing
        can ride with it and there is nothing to wait for."""
        with self._lock:
            if self._closed:
                raise RuntimeError("this scope has already been answered")
            held = self._waiting
            self._waiting = None
            if place.id is not None:
                place.ordered = self._ordered and self._records == 0
                self._records += 1
                if self._extend:
                    self._placed[_ident(place.id)] = place.fields
        if held is not None:
            self._emit(self._result(*held.args()))
        self._emit(self._place(*place.args()))

    def _place(self, *extra) -> str:
        """One place statement, addressed to the query the same way a result is.
        The verb is the whole of what tells them apart."""
        args = [protocol.Arg(value=protocol.new_int(self.query.id()))]
        args.extend(extra)
        return protocol.encode_statement(
            protocol.Statement(verb=_query.PLACE_VERB, args=args))

    def _write(self, whole, id, fields, named=0, ordered=0):
        """Queue one record, holding it back until the next one or the end.

        Held back because the statement that carries the last record can carry
        the terminator too, and an answer of one record is then one line rather
        than three. Nothing waits long: the next record releases it, so does
        flush, and so does the end."""
        bag = _query.Fields()
        for name, v in fields.items():
            bag.append(protocol.named(name, v))
        key = protocol.val(id)
        rec = _query.Result(id=key, fields=bag, whole=whole,
                            named=named, ordered_members=ordered)
        with self._lock:
            if self._closed:
                raise RuntimeError("this scope has already been answered")
            if self._extend:
                was = self._placed.get(_ident(key))
                if was is not None:
                    # A result under extend states how much of the record there
                    # is, which is the one thing only a result can say, and
                    # leaves the rest to what the far end is already holding.
                    if rec.whole:
                        rec.named, rec.ordered_members = _tally(rec.fields)
                        rec.whole = False
                    rec.fields = _lacking(rec.fields, was)
            held = self._waiting
            rec.ordered = self._ordered and self._records == 0
            self._waiting = rec
            self._records += 1
        if held is not None:
            self._emit(self._result(*held.args()))

    def ordered(self):
        """Declare that the records are being sent in the query's own order,
        which goes out at once, before any of them.

        Up front because that is the only place it is worth anything. It
        changes what the far end does with what arrives -- ordered, it merges
        the records as they stand; unordered, it sorts them first -- and a far
        end that does not learn which until the records have all gone by cannot
        act on either. Saying nothing means unordered, which is always safe.

        So it is said before the first record or not at all: a declaration made
        after one has gone out is too late to be true of what has already
        crossed, and is dropped rather than sent."""
        with self._lock:
            # Late is measured in records produced, not statements sent: a
            # record held back for the terminator to ride on has still been
            # produced.
            if self._records > 0 or self._closed or self._ordered:
                return
            self._ordered = True

    def filled(self, watermark):
        """Finish with the count reached, and a watermark: there is nothing of
        mine between where you asked from and this record that you do not now
        have.

        The watermark is a completeness guarantee rather than a position, and
        it is what the next scope is asked from -- so it names a record this
        source sent, or one it is otherwise prepared to place."""
        self._finish(_query.Complete(stop=_query.STOP_FILLED,
                                     watermark=protocol.val(watermark)))

    def joined(self, watermark):
        """Finish at the record the display said it already held. What it holds
        on this side and what it holds on that are now one run."""
        self._finish(_query.Complete(stop=_query.STOP_JOINED,
                                     watermark=protocol.val(watermark)))

    def exhausted(self):
        """Finish with everything there is: no watermark, because there is
        nothing past the end to be complete up to.

        It is what the simplest possible implementation says -- ignore every
        hint, send all your records, say this -- and it is not a toy: the
        display then holds the whole layer and asks nothing again until
        something invalidates it."""
        self._finish(_query.Complete(stop=_query.STOP_EXHAUSTED))

    def fail(self, message: str):
        """Finish with a refusal: this query cannot be honoured, this scope
        cannot be produced, the records are gone. A refusal is an answer -- the
        display carries on with what it has."""
        self._finish(_query.Complete(error=message))

    def sent(self) -> int:
        """How many records have gone into the answer so far."""
        with self._lock:
            return self._records

    def flush(self):
        """Send what has accumulated without finishing the answer, the record
        being held for the terminator included."""
        with self._lock:
            held = self._waiting
            self._waiting = None
        if held is not None:
            self._emit(self._result(*held.args()))
        with self._lock:
            src = self._take()
        if src:
            self.query._conn.send(src)

    # --- the machinery under those ---

    def _result(self, *extra) -> str:
        """One result statement, addressed to the query it belongs to the way
        every other statement addresses an object."""
        args = [protocol.Arg(value=protocol.new_int(self.query.id()))]
        args.extend(extra)
        return protocol.encode_statement(
            protocol.Statement(verb=_query.RESULT_VERB, args=args))

    def _emit(self, stmt: str):
        with self._lock:
            if self._closed:
                raise RuntimeError("this scope has already been answered")
            self._buf.append(stmt)
            self._size += len(stmt) + 1
            self._sent += 1
            if self._size < FLUSH_BYTES:
                return
            src = self._take()
        self.query._conn.send(src)

    def _finish(self, done):
        """End the answer, on the last record's own statement where there is
        one, close it and send the rest."""
        with self._lock:
            if self._closed:
                raise RuntimeError("this scope has already been answered")
            end = self._waiting
            self._waiting = None
            if end is None:
                end = _query.Result(ordered=self._ordered and self._records == 0)
            done.total, done.exact = self._total
            done.first = self._first
            end.complete = done
            self._buf.append(self._result(*end.args()))
            self._closed = True
            src = self._take()
        self.query._conn.send(src)

    def _take(self) -> str:
        """Empty the buffer and return what was in it. Called under the lock."""
        src = "\n".join(self._buf)
        self._buf = []
        self._size = 0
        return src
