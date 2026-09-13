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

    def _inbound_loop(self):
        """The batches the display sent, in the order they arrived. On a thread
        of its own rather than sharing the event one, because serving a window
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
        """One blob of the store, by the id an answer named it with. An app
        never invents one: it learns ids from store_blob and store_data."""
        return Blob(self, oid)

    def host(self) -> "Handle":
        """The display itself as a handle."""
        return Handle(self, self.init("host"))

    def on_store(self, event: str, fn: Callable[[Event], None]):
        """Register a handler for one of the store's answers and open the flow
        for it. Subscribing does not ask what is in the store -- Store.list
        does that."""
        self.store().on(event, fn)

    def on_host(self, event: str, fn: Callable[[Event], None]):
        """Register a handler for what the display says about itself."""
        self.host().on(event, fn)

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

    def host_source(self, name: str, fill) -> "Source":
        """Register a body of records this application can serve, and what
        answers a window of it.

        Nothing crosses the wire here: a source is a name, not an object, and
        the display learns of it when a trinket is told `data="<name>"`."""
        if not name:
            raise ValueError("host_source: a source needs a name")
        if fill is None:
            raise ValueError("host_source: a source needs something to fill it")
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

        It must not run on the reader: serving a window writes, and the reader
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
        if stmt.verb == _query.QUERY_VERB:
            if oid is None:
                raise ValueError("query: expected the query to ask")
            q = self.query(oid)
            if q is None:
                raise ValueError("query %d: no query of mine" % oid)
            return self._window(q, stmt.args[1:], pending)
        if oid is None:
            return  # not addressed to anything this application holds
        q = self.query(oid)
        if q is None:
            raise ValueError("%s %d: no query of mine" % (stmt.verb, oid))
        rest = stmt.args[1:]

        if stmt.verb == "set":
            spec = _query.parse_spec(rest)
            with q._lock:
                q._spec = spec
            with q._source._lock:
                fn = q._source._respec
            if fn is not None:
                pending.append(lambda: fn(q, spec))
            return
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
        """Make a query and ask it for its first window, which is one statement
        because the display never wants a sequence without wanting rows of it.

        The application names it. The display has no id to offer -- ids here are
        the application's own -- so the reply is what carries it back."""
        if not stmt.args or stmt.args[0].value is not None \
                or stmt.args[0].flag != FlagState.TRUE:
            raise ValueError("new: expected a type")
        if stmt.args[0].name != _query.QUERY_VERB:
            raise ValueError("new: I host nothing called %r" % stmt.args[0].name)
        args = stmt.args[1:]

        spec = _query.parse_spec(args)
        with self._lock:
            source = self._sources.get(spec.source)
        if source is None:
            raise ValueError("query: I serve nothing called %r" % spec.source)

        q = Query(self, source, self._mint_id(), spec)
        with self._lock:
            self._queries[q.id()] = q
        if stmt.key:
            ids[stmt.key] = q.id()
            keys[stmt.key] = q.id()
        return self._window(q, args, pending)

    def _window(self, q, args, pending):
        """Take a request for one window apart and queue serving it."""
        request = _query.parse_fill(args)
        with q._lock:
            spec = q._spec
        with q._source._lock:
            fn = q._source._fill
        f = Fill(q, request, spec)
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
        `ask <id> bytes offset=2048`. The answer arrives as the events the
        question declares it answers with, so register for those first."""
        self._c.ask(self._id, question)

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

# The events the store answers with. All of them name the store as their
# source, so one subscription hears everything.
STORE_BLOB = "store_blob"    # one blob: what it is and how big
STORE_DONE = "store_done"    # the end of an inventory
STORE_DATA = "store_data"    # one chunk of a blob being read back
STORE_GONE = "store_gone"    # a blob is no longer there
STORE_ERROR = "store_error"  # what went wrong, and with which key

# What the display says about itself, and the questions it answers.
HOST_STATE = "host_state"
ASK_DARK = "dark"
ASK_DESKTOP = "desktop"


class Store(Handle):
    """The app's whole store on the desktop: a flat set of names, each holding
    one blob. Every one of these SENDS; the answers arrive as events on the
    store, because a blob comes back in pieces."""

    def write(self, key: str, typ: str, data: bytes):
        """Put a blob in the store, replacing whatever the key held.

        A key is a NAME, not a path: no slashes, nothing that is only digits,
        nothing unprintable, and `#` only at the front. typ is one of txt, psl,
        bin, ini or conf. The answer is a store_blob naming the id the blob can
        be addressed by, which is how something larger than one statement is
        continued -- see Blob.append."""
        self.set("blobs={ new blob key=%s type=%s data=%s }" % (
            protocol.quote(key), typ, protocol.quote_blob(data)))

    def list(self):
        """Ask what the store holds: a store_blob per blob, then a store_done
        saying how many there were."""
        self.ask("inventory")


class Blob(Handle):
    """One blob of the store, by the id an answer named it with."""

    def append(self, data: bytes):
        """Add to the end, as a terminal is fed. It is how something too large
        for one statement is written: write the first piece, append the rest."""
        self.do("append bytes=" + protocol.quote_blob(data))

    def replace(self, data: bytes):
        """Write the blob's whole contents again."""
        self.set("data=" + protocol.quote_blob(data))

    def read(self, offset: int):
        """Ask for the chunk that starts at offset. The answer says where it
        starts and whether it is the last; ask again from the end of what
        arrived until it is."""
        self.ask("bytes offset=%d" % offset)

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
# column header somebody clicked, the filter from the filter box, the window
# from the scroll position -- so the display opens it, and the application,
# which is the end that holds the records, serves it.
#
# What an author writes is one function: given a window of the sequence,
# produce the records in it. The statement is taken apart before it gets here,
# so nothing in that function parses anything; and the answer is written into a
# sink that goes out in batches as it fills, so a million records need not be
# one message, or one uninterruptible stretch of work.
#
# It arrives nowhere near the event line. `query` is answered by `result`,
# `ask` by `answer`, and `sub` -- or an object's mere existence -- by `event`;
# nothing carries two of them, so a request for records can never be mistaken
# for something a subscription raised.
#
# docs/hosting-a-query.md is the wire spelling. kittytk/query.py is the
# structure.

# How much answer accumulates before it goes out on its own. It trades write
# syscalls against how long a record waits: big enough that a window of a
# screenful is one message, small enough that a window of a million records is
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
        self._respec = None
        self._dropped = None
        self._other = None

    def name(self) -> str:
        """What a display asks for this source by."""
        return self._name

    def on_respec(self, fn):
        """A handler for the display restating a query's sequence: a re-sort, a
        new filter, a different set of fields. Everything cached against the old
        spec that was keyed by position is stale; what was keyed by record
        identity is not.

        A source that ignores this is still correct -- the next window carries
        the new spec -- so it is for applications with something to tear down."""
        with self._lock:
            self._respec = fn

    def on_dropped(self, fn):
        """A handler for the display letting a query go, which is how the
        application learns it may drop the records it was holding."""
        with self._lock:
            self._dropped = fn

    def on_statement(self, fn):
        """A handler for anything else the display addresses to one of this
        source's queries: a question this library does not know, an action, a
        property it does not read. The statement arrives as it parsed.

        This is the seam a fuller library is built on. Coverage and
        invalidation both travel this way and neither is implemented here."""
        with self._lock:
            self._other = fn


class Query:
    """One sequence of a source's records that a display is reading: one
    filter, one sort, and a window asked for at a time.

    The display opens it; the application names it, because the ids in every
    statement that follows are the application's own."""

    def __init__(self, conn: "Conn", source: Source, oid: int, spec):
        self._conn = conn
        self._source = source
        self._id = oid
        self._lock = threading.Lock()
        self._spec = spec

    def id(self) -> int:
        """What this application calls the query, and what the display
        addresses it by from the moment the reply carries it."""
        return self._id

    def source(self) -> Source:
        """Where its records come from."""
        return self._source

    def spec(self):
        """The sequence as it currently stands. It changes when the display
        restates it, which is a new generation of the same query."""
        with self._lock:
            return self._spec


class Fill:
    """One window of the sequence, asked for -- and where the records that
    answer it are written.

    The reading side is what was asked: from_ and to are where the display's
    own knowledge starts and how far it runs, have is how much of the window it
    can fill from that, and need is how many rows the window is. Emit every
    record of your own in (from_..to], and if that does not make up the
    shortfall, keep going past to until it does.

    The writing side is record, as many times as there are records, and then
    one of done, exhausted or fail. Records go out in batches as they
    accumulate, so the answer may be produced over as long as it takes and
    interleaved with other work; nothing has to be held until the end."""

    def __init__(self, query: Query, request, spec):
        self.query = query
        self.spec = spec
        self.request = request
        self.from_ = request.from_
        self.to = request.to
        self.have = request.have
        self.need = request.need
        self.fields = request.fields

        self._lock = threading.Lock()
        self._buf: List[str] = []
        self._size = 0
        self._sent = 0
        self._ordered = False
        self._closed = False

    def record(self, key, **fields):
        """One record: its key, and the fields asked for.

            f.record(17, name="src/parser.go", size=1024)

        The key is what identifies the record, view-independent and permanent;
        the fields are whatever this window asked for, which may be fewer than
        the query's own list when the display wants the skeleton of a wide
        stretch."""
        bag = _query.Fields([protocol.named(_query.KEY_FIELD, key)])
        for name, v in fields.items():
            bag.append(protocol.named(name, v))
        self._emit(self._result(protocol.Arg(name="fields", value=bag.block())))

    def ordered(self):
        """Declare that the records are being sent in the query's own order.

        It is the one hint that cannot be left unsaid and assumed, because it
        changes what the display does with what arrives: ordered, it merges the
        records as they stand; unordered, it sorts them first. Saying nothing
        means unordered, which is always safe."""
        with self._lock:
            self._ordered = True

    def done(self, watermark=None):
        """Finish with a watermark: there is nothing of mine between where you
        asked from and this point that you do not now have.

        It is a completeness guarantee rather than a position, and it is what
        lets the display shrink the window, grow it back and scroll inside it
        without asking anything."""
        extra = []
        if watermark:
            extra.append(protocol.Arg(name="watermark",
                                      value=_query.Fields(watermark).block()))
        self._finish(extra)

    def exhausted(self):
        """Finish with everything there is: no watermark, because there is
        nothing past the end to be complete up to.

        It is what the simplest possible implementation says -- ignore every
        hint, send all your records, say this -- and it is not a toy: the
        display then holds the whole layer and asks nothing again until
        something invalidates it."""
        self._finish([protocol.Arg(name="exhausted", flag=FlagState.TRUE)])

    def fail(self, message: str):
        """Finish with a refusal: this query cannot be honoured, this window
        cannot be produced, the records are gone. A refusal is an answer -- the
        display carries on with what it has."""
        self._finish([protocol.named("error", message)])

    def sent(self) -> int:
        """How many records have gone into the answer so far."""
        with self._lock:
            return self._sent

    def flush(self):
        """Send what has accumulated without finishing the answer."""
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
                raise RuntimeError("this window has already been answered")
            self._buf.append(stmt)
            self._size += len(stmt) + 1
            self._sent += 1
            if self._size < FLUSH_BYTES:
                return
            src = self._take()
        self.query._conn.send(src)

    def _finish(self, extra):
        with self._lock:
            if self._closed:
                raise RuntimeError("this window has already been answered")
            # `ordered` rides on the terminator, so it can be decided after the
            # records have been produced rather than promised before.
            args = [protocol.Arg(name=_query.RESULT_COMPLETE, flag=FlagState.TRUE)]
            if self._ordered:
                args.append(protocol.Arg(name="ordered", flag=FlagState.TRUE))
            args.extend(extra)
            self._buf.append(self._result(*args))
            self._closed = True
            src = self._take()
        self.query._conn.send(src)

    def _take(self) -> str:
        """Empty the buffer and return what was in it. Called under the lock."""
        src = "\n".join(self._buf)
        self._buf = []
        self._size = 0
        return src
