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
        # What this connection holds on the application's behalf, by the id the
        # display addresses it by. Statements arriving for one of these are the
        # other direction of the wire: the display asking, rather than being
        # told (the query section below).
        self._hosted: Dict[int, "Query"] = {}
        self._inbound: "queue.Queue" = queue.Queue()

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
                        if _hosted_target(stmt) is not None:
                            self._inbound.put(stmt)
                        else:
                            self._pending_desc.append(text.strip())
                    elif stmt.verb in ("set", "destroy", "sub", "unsub"):
                        # The other direction: the display addressing something
                        # this application hosts. Queued rather than handled
                        # here, because answering executes statements and the
                        # reader has to stay free to route their replies.
                        self._inbound.put(stmt)
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
        """Statements the display addressed to what this application hosts, in
        the order they arrived. On a thread of its own rather than sharing the
        event one: answering a fill can take as long as the records take, and a
        list nobody is looking at must not hold up a click."""
        while True:
            stmt = self._inbound.get()
            if stmt is None:
                return
            self.inbound(stmt)

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

    def host_query(self, spec, fill) -> "Query":
        """Announce a query this application hosts and register what answers its
        fills. The display addresses it from here on, and every question it puts
        reaches fill with the statement already taken apart."""
        if spec is None:
            raise ValueError("host_query: a query needs a spec")
        if fill is None:
            raise ValueError("host_query: a query needs something to fill it")
        src = "q=new " + _query.QUERY_TYPE
        args = spec.encode()
        if args:
            src += " " + args
        ids = self.exec(src)
        oid = ids.get("q", 0)
        if not oid:
            raise RuntimeError("host_query: the display surfaced no id for the query")
        q = Query(self, oid, spec, fill)
        with self._lock:
            self._hosted[oid] = q
        return q

    def hosted(self, oid: int) -> Optional["Query"]:
        """The query this connection hosts under an id, and None for an id it hosts
        nothing under."""
        with self._lock:
            return self._hosted.get(oid)

    def inbound(self, stmt):
        """Route one statement the display addressed to something this connection
        hosts. The transport calls it for every such statement.

        It must not run on the reader: answering a fill executes statements, and
        those need the reader free to route their replies."""
        oid = _hosted_target(stmt)
        if oid is None:
            return
        q = self.hosted(oid)
        if q is None:
            return
        rest = stmt.args[1:]
        if stmt.verb == "ask":
            if rest and rest[0].value is None and rest[0].flag == FlagState.TRUE \
                    and rest[0].name == _query.ASK_FILL:
                q._dispatch_fill(rest[1:])
                return
        elif stmt.verb == "set":
            try:
                spec = _query.parse_spec(rest)
            except (_query.QueryError, protocol.ParseError):
                spec = None
            if spec is not None:
                q._dispatch_respec(spec)
                return
        elif stmt.verb == "destroy":
            with self._lock:
                self._hosted.pop(oid, None)
            q._dispatch_dropped()
            return
        q._dispatch_other(stmt)


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


# --- hosting a query ------------------------------------------------------
#
# Everything above points one way: the application says `new`, `set`, `ask`,
# `do`, and the display answers with events. A query points the other way. The
# application holds records the display cannot see, so the display asks -- and
# this is where those questions arrive.
#
# What an author has to write is one function: given a window of the sequence,
# produce the records in it. The statement is taken apart before it gets here,
# so nothing in that function parses anything; and the answer is written into a
# sink that goes out in batches as it fills, so a million records need not be
# one message, or one uninterruptible stretch of work.
#
# docs/hosting-a-query.md is the wire spelling. kittytk/query.py is the
# structure.

# How much answer accumulates before it goes out on its own. It trades round
# trips against how long a record waits: big enough that a fill of a screenful
# is one message, small enough that a fill of a million records is not held in
# memory.
FLUSH_BYTES = 16 * 1024


def _hosted_target(stmt) -> Optional[int]:
    """The object a statement is addressed to: the leading operand, which is a
    bare id. None when the statement is addressed to nothing."""
    if not stmt.args:
        return None
    a = stmt.args[0]
    if a.name or a.value is None or a.value.kind != protocol.ValueKind.NUMBER \
            or not a.value.is_int or a.value.number < 0:
        return None
    return int(a.value.number)


class Query:
    """A sequence of an application's own records that a display is reading:
    one filter, one sort, and a window asked for at a time.

    The application makes it -- the display never says `new` to an application
    -- and holds it for as long as anything is looking."""

    def __init__(self, conn: "Conn", oid: int, spec, fill):
        self._conn = conn
        self._id = oid
        self._lock = threading.Lock()
        self._spec = spec
        self._fill = fill
        self._respec = None
        self._other = None
        self._dropped = None

    def id(self) -> int:
        """What the display addresses this query by."""
        return self._id

    def spec(self):
        """The sequence as it currently stands. It changes when the display
        restates it, which is a new generation of the same query."""
        with self._lock:
            return self._spec

    def on_respec(self, fn):
        """A handler for the display restating the sequence: a re-sort, a new
        filter, a different set of fields. Everything cached against the old
        spec that was keyed by position is stale; what was keyed by record
        identity is not.

        A query that ignores this is still correct -- the next fill carries the
        new spec -- so it is for applications with something to tear down."""
        with self._lock:
            self._respec = fn

    def on_dropped(self, fn):
        """A handler for the display letting the query go."""
        with self._lock:
            self._dropped = fn

    def on_statement(self, fn):
        """A handler for anything else the display addresses to this query: a
        question this library does not know, an action, a property it does not
        read. The statement arrives as it parsed.

        This is the seam a fuller library is built on. Coverage and
        invalidation both travel this way and neither is implemented here, so a
        library that wants them adds them without this module having to grow."""
        with self._lock:
            self._other = fn

    def destroy(self):
        """Tell the display the query is gone and stop answering for it."""
        with self._conn._lock:
            self._conn._hosted.pop(self._id, None)
        self._conn.exec("destroy %d" % self._id)

    # --- what the connection calls ---

    def _dispatch_respec(self, spec):
        with self._lock:
            self._spec = spec
            fn = self._respec
        if fn is not None:
            fn(spec)

    def _dispatch_fill(self, args):
        with self._lock:
            spec, fn = self._spec, self._fill
        try:
            request = _query.parse_fill(args)
        except (_query.QueryError, protocol.ParseError) as e:
            # The tag is in the request that would not parse, so there is
            # nothing to stamp a refusal with. Say so where it can be seen
            # rather than dropping the question on the floor.
            Fill(self, _query.Fill(), spec).fail(str(e))
            return
        fn(Fill(self, request, spec))

    def _dispatch_dropped(self):
        with self._lock:
            fn = self._dropped
        if fn is not None:
            fn()

    def _dispatch_other(self, stmt):
        with self._lock:
            fn = self._other
        if fn is not None:
            fn(stmt)


class Fill:
    """One window of the sequence, asked for -- and where the answer to it is
    written.

    The reading side is what was asked: from_ and to are where the display's
    own knowledge starts and how far it runs, have is how much of the window it
    can fill from that, and need is how many rows the window is. Emit every
    record of your own in (from_..to], and if that does not make up the
    shortfall, keep going past to until it does.

    The writing side is record, as many times as there are records, and then
    one of done, exhausted or fail. Records go out in batches as they
    accumulate, so the answer may be produced over as long as it takes and
    interleaved with other work; nothing has to be held until the end."""

    def __init__(self, q: Query, request, spec):
        self.query = q
        self.spec = spec
        self.request = request
        self.tag = request.tag
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
        the fields are whatever this fill asked for, which may be fewer than
        the query's own list when the display wants the skeleton of a wide
        stretch."""
        bag = _query.Fields([protocol.named(_query.KEY_FIELD, key)])
        for name, v in fields.items():
            bag.append(protocol.named(name, v))
        ev = Event(_query.EVENT_QUERY_RECORD, [
            protocol.named("query", self.query.id()),
            protocol.named("tag", self.tag),
            protocol.Arg(name="fields", value=bag.block()),
        ])
        self._emit(ev.encode())

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
        ev = self._terminator()
        if watermark:
            ev.fields.append(protocol.Arg(name="watermark",
                                          value=_query.Fields(watermark).block()))
        self._finish(ev)

    def exhausted(self):
        """Finish with everything there is: no watermark, because there is
        nothing past the end to be complete up to.

        It is what the simplest possible implementation says -- ignore every
        hint, send all your records, say this -- and it is not a toy: the
        display then holds the whole layer and asks nothing again until
        something invalidates it."""
        ev = self._terminator()
        ev.fields.append(protocol.Arg(name="exhausted", flag=FlagState.TRUE))
        self._finish(ev)

    def fail(self, message: str):
        """Finish with a refusal: this query cannot be honoured, this window
        cannot be produced, the records are gone. A refusal is an answer -- the
        display carries on with what it has."""
        ev = self._terminator()
        ev.fields.append(protocol.named("error", message))
        self._finish(ev)

    def sent(self) -> int:
        """How many records have gone into the answer so far."""
        with self._lock:
            return self._sent

    def flush(self):
        """Send what has accumulated without finishing the answer."""
        with self._lock:
            src = self._take()
        if src:
            self.query._conn.exec(src)

    # --- the machinery under those ---

    def _terminator(self) -> Event:
        with self._lock:
            ordered = self._ordered
        ev = Event(_query.EVENT_QUERY_FILLED, [
            protocol.named("query", self.query.id()),
            protocol.named("tag", self.tag),
        ])
        if ordered:
            ev.fields.append(protocol.Arg(name="ordered", flag=FlagState.TRUE))
        return ev

    def _emit(self, stmt: str):
        with self._lock:
            if self._closed:
                raise RuntimeError("this fill has already been answered")
            self._buf.append(stmt)
            self._size += len(stmt) + 1
            self._sent += 1
            if self._size < FLUSH_BYTES:
                return
            src = self._take()
        self.query._conn.exec(src)

    def _finish(self, ev: Event):
        with self._lock:
            if self._closed:
                raise RuntimeError("this fill has already been answered")
            self._buf.append(ev.encode())
            self._closed = True
            src = self._take()
        self.query._conn.exec(src)

    def _take(self) -> str:
        """Empty the buffer and return what was in it. Called under the lock."""
        src = "\n".join(self._buf)
        self._buf = []
        self._size = 0
        return src
