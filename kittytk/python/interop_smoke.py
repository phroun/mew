"""Interop smoke client: a Python app driving a REAL Go display host.

Run by python/interop_test.go, which stands up a headless KittyTK display
service and then drives input into the window this builds. It proves, over
an actual socket:

  * handshake + build (Python -> host): the surfaced ids come back,
  * write-through (Python -> host): SetText lands in the real trinket,
  * events (host -> Python): a server-side toggle and button click arrive
    as toggle/command events on this Python connection.

Protocol markers on stdout (the Go side reads them):
  READY        - built + subscribed; safe for the host to drive input
  TOGGLE ok    - received the toggle event
  COMMAND ok   - received the command event
  DONE         - both received; exiting 0
"""

import os
import sys
import threading

sys.path.insert(0, os.path.dirname(__file__))

import kittytk  # noqa: E402


def main(sock: str) -> int:
    conn = kittytk.dial(sock, "Py Interop App")

    ui = conn.build(
        'w=new window title="Py Interop" width=320 height=160 children={\n'
        '  p=new panel layout=vbox children={\n'
        '    cb=new checkbox caption="remote checkbox"\n'
        '    inp=new textinput\n'
        '    btn=new button caption="Go" action=remote.act\n'
        '  }\n'
        '}\n'
        'wcb=w.p.cb\n'
        'winp=w.p.inp\n'
        'wbtn=w.p.btn\n'
    )
    for key in ("w", "wcb", "winp", "wbtn"):
        if ui.id(key) == 0:
            print("FAIL missing id " + key, flush=True)
            return 1

    # App -> host write-through: the Go side reads this back from the real
    # trinket to prove the direction.
    ui.text_input("winp").set_text("over the wire")

    got_toggle = threading.Event()
    got_command = threading.Event()

    def on_toggle(state):
        # state is a FlagState; TRUE after the host toggles it on.
        print("TOGGLE ok state=%d" % int(state), flush=True)
        got_toggle.set()

    ui.checkbox("wcb").on_toggle(on_toggle)
    conn.on_command("remote.act", lambda: (print("COMMAND ok", flush=True), got_command.set()))

    # Introspection (D24): the host describes its wire vocabulary.
    vocab = conn.describe()
    common = {p.name for p in vocab.common}
    if "enabled" not in common:
        print("FAIL describe: no common 'enabled'", flush=True)
        return 1
    button = next((t for t in vocab.types if t.name == "button"), None)
    caption = button and next((p for p in button.props if p.name == "caption"), None)
    if caption is None or caption.kind != "string" or not caption.doc:
        print("FAIL describe: button.caption missing/undescribed", flush=True)
        return 1
    print("DESCRIBE ok types=%d" % len(vocab.types), flush=True)

    # The other two objects the handshake handed over.
    if conn.store_id == 0 or conn.host_id == 0:
        print("FAIL handshake: store=%d host=%d" % (conn.store_id, conn.host_id),
              flush=True)
        return 1

    # The display answers what it is asked.
    said = threading.Event()
    dark = []

    def on_host(ev):
        dark.append(ev.flag("dark") == kittytk.FlagState.TRUE)
        said.set()

    conn.on_host(kittytk.HOST_STATE, on_host)
    conn.host().ask(kittytk.ASK_DARK)
    if not said.wait(5):
        print("FAIL ask host dark: no answer", flush=True)
        return 1
    print("ASK ok dark=%d" % int(dark[0]), flush=True)

    # The store: write a blob of every byte there is, in two pieces, and read
    # it back. Anything the wire mangled shows up as a mismatch.
    ramp = bytes(i % 256 for i in range(512))
    blob_id = []
    read_back = bytearray()
    got_blob = threading.Event()
    read_done = threading.Event()
    listed = threading.Event()

    def on_blob(ev):
        blob_id.append(ev.uint("blob"))
        got_blob.set()

    def on_data(ev):
        read_back.extend(ev.blob("data") or b"")
        if ev.flag("last") == kittytk.FlagState.TRUE:
            read_done.set()

    conn.on_store(kittytk.STORE_BLOB, on_blob)
    conn.on_store(kittytk.STORE_DATA, on_data)
    conn.on_store(kittytk.STORE_DONE, lambda ev: listed.set())

    conn.store().write("py-interop", "bin", ramp[:256])
    if not got_blob.wait(5):
        print("FAIL store write: no blob id", flush=True)
        return 1
    blob = conn.blob(blob_id[0])
    blob.append(ramp[256:])
    blob.read(0)
    if not read_done.wait(5):
        print("FAIL store read: never finished", flush=True)
        return 1
    if bytes(read_back) != ramp:
        print("FAIL store read: %d bytes back, not the %d written"
              % (len(read_back), len(ramp)), flush=True)
        return 1
    conn.store().list()
    if not listed.wait(5):
        print("FAIL store inventory: no answer", flush=True)
        return 1
    print("STORE ok bytes=%d" % len(read_back), flush=True)

    # The vocabulary says what a type does and answers, not just what it holds.
    vocab = conn.describe()
    host = next((t for t in vocab.types if t.name == "host"), None)
    blob_t = next((t for t in vocab.types if t.name == "blob"), None)
    if host is None or not host.hosted:
        print("FAIL describe: the host type is not reported as hosted", flush=True)
        return 1
    if not any(d.name == "tile" for d in host.does):
        print("FAIL describe: the host does no tile", flush=True)
        return 1
    if not any(a.name == "dark" for a in host.asks):
        print("FAIL describe: the host answers no dark", flush=True)
        return 1
    append = blob_t and next((d for d in blob_t.does if d.name == "append"), None)
    if append is None or len(append.args) != 1 or append.args[0].name != "bytes":
        print("FAIL describe: a blob's append takes %r"
              % (append and [a.name for a in append.args]), flush=True)
        return 1
    if not any(e.name == "store_blob" and e.fields for e in
               next(t for t in vocab.types if t.name == "store").events):
        print("FAIL describe: the store's events carry no fields", flush=True)
        return 1
    print("VOCAB ok", flush=True)

    print("READY", flush=True)  # host may now drive input

    if not got_toggle.wait(10) or not got_command.wait(10):
        print("TIMEOUT", flush=True)
        return 2

    print("DONE", flush=True)
    conn.close()
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: interop_smoke.py <socket>", file=sys.stderr)
        sys.exit(64)
    sys.exit(main(sys.argv[1]))
