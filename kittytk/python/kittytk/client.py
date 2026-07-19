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

        self._closed_flag = False
        self.closed = threading.Event()  # set when the connection ends

        # This connection's Application ObjectID, from the handshake (0 until
        # the welcome is parsed). Address app-wide properties by it: set_app.
        self._app_id = 0

    # --- lifecycle -------------------------------------------------------

    def _start(self):
        threading.Thread(target=self._read_loop, daemon=True).start()
        threading.Thread(target=self._event_loop, daemon=True).start()

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
                    elif stmt.verb in ("proptype", "prop", "propcommon"):
                        # describe verb output: buffer until the reply.
                        self._pending_desc.append(text.strip())
                    elif stmt.verb == "event":
                        try:
                            self._events.put(protocol.parse_event(text))
                        except Exception:  # noqa: BLE001
                            pass
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

    @property
    def app_id(self) -> int:
        """This connection's Application ObjectID, as reported by the display
        service in the handshake. Use it to address application-wide
        properties, e.g. conn.exec("set %d multiwindow" % conn.app_id)."""
        return self._app_id

    def set_app(self, props: str) -> Dict[str, int]:
        """Apply application-wide properties with the same syntax as any
        object: set_app("multiwindow contextonly") sends
        `set <app_id> multiwindow contextonly`."""
        if not self._app_id:
            raise RuntimeError("set_app: no application id from the handshake")
        return self.exec("set %d %s" % (self._app_id, props))

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

    # The handshake carries this connection's Application ObjectID, so the app
    # can address application-wide properties (see Conn.app_id / Conn.set_app).
    for a in script.statements[0].args:
        if (a.name == "app" and a.value is not None
                and a.value.kind == protocol.ValueKind.NUMBER and a.value.is_int):
            conn._app_id = int(a.value.number)

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
