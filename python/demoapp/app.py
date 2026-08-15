"""The KittyTK demo as a display-protocol application, in Python.

A port of examples/demoapp (main.go + wire.go): it links only the kittytk
client, dials a running display service (kittytk-tui or kittytk-sdl), and
drives the whole UI - nine-tab gallery, menus, MDI, dialogs, secondary
apps - over the socket. Proof that the app side is language-neutral.
"""

from __future__ import annotations

import threading

import kittytk
from kittytk import FlagState
from kittytk.protocol import quote

from . import scripts

# Applications opened via Window > New Window.
_secondary_lock = threading.Lock()
_secondary_count = 0


class App:
    """One connection = one Application on the desktop."""

    def __init__(self, path: str, name: str, primary: bool, solo: bool = False):
        self.path = path
        self.primary = primary
        self.ui = None
        self.mdi_count = 0
        self.dock_seq = 0
        self.quit = threading.Event()
        if primary and solo:
            self.conn = kittytk.dial_solo(path, name)
        else:
            self.conn = kittytk.dial(path, name)

    # --- lifecycle -------------------------------------------------------

    def wait(self):
        """Block until the app quits or the display service disconnects."""
        while not self.quit.is_set() and not self.conn.closed.is_set():
            self.quit.wait(0.2)
        self.conn.close()

    def set_status(self, text: str):
        self.conn.exec("status text=" + quote(text))

    # --- primary application ---------------------------------------------

    def build_primary(self):
        self.ui = self.conn.build(scripts.main_build_script())
        self.wire_main_window()
        self.wire_menus()
        self.wire_mdi()
        self.open_protocol_window()
        # The demo ends when its main window closes (or the desktop exits).
        self.ui.window("w").on_closed(self.quit.set)

    def wire_main_window(self):
        ui = self.ui
        win = ui.object("w")
        tabs = ui.object("tabs")

        ui.text_input("binput").on_change(lambda s: self.set_status("Text: " + s))

        def wfont(state):
            win.set('font="tuesday12"' if state == FlagState.TRUE else 'font="default"')
        ui.checkbox("wfont").on_toggle(wfont)

        def dfont(state):
            self.conn.exec("desktopfont tuesday" if state == FlagState.TRUE else "desktopfont default")
        ui.checkbox("dfont").on_toggle(dfont)

        def grid(state):
            win.set("denomination=32" if state == FlagState.TRUE else "denomination=0")
        ui.checkbox("grid").on_toggle(grid)

        def set_bg(arg):
            def handler(ev):
                if ev.flag("checked") == FlagState.TRUE:
                    tabs.set("background=" + arg)
            return handler

        green, gray, default = "green", '"#333333"', "default"
        ui.object("bgdef").on("toggle", set_bg(default))
        ui.object("bggreen").on("toggle", set_bg(green))
        ui.object("bggray").on("toggle", set_bg(gray))
        ui.object("sbgdef").on("toggle", set_bg(default))
        ui.object("sbggreen").on("toggle", set_bg(green))
        ui.object("sbggray").on("toggle", set_bg(gray))

    def wire_menus(self):
        c = self.conn
        c.on_command("demo.file.new", self.open_terminal_window)

        c.on_command("demo.edit.cut", lambda: c.exec("cut"))
        c.on_command("demo.edit.copy", lambda: c.exec("copy"))
        c.on_command("demo.edit.paste", lambda: c.exec("paste"))
        c.on_command("demo.edit.selectall", lambda: c.exec("selectall"))
        c.on_command("demo.edit.rawkey", lambda: c.exec("rawkey"))

        c.on_command("demo.view.theme", lambda: c.exec("theme"))
        c.on_command("demo.view.announce", lambda: c.exec("announce_visual"))
        c.on_command("demo.view.speak", lambda: c.exec("announce_speak"))

        c.on_command("demo.window.new", lambda: open_secondary(self.path))
        c.on_command("demo.window.tile", lambda: c.exec("tile"))
        c.on_command("demo.window.cascade", lambda: c.exec("cascade"))

        c.on_command("demo.basic.ok", lambda: self.set_status("OK button clicked!"))
        c.on_command("demo.basic.cancel", lambda: self.set_status("Cancel button clicked!"))
        c.on_command("demo.basic.apply", lambda: self.set_status("Apply button clicked!"))

        c.on_command("demo.help.about", self.show_about)

    def wire_mdi(self):
        ui, c = self.ui, self.conn
        mdi = ui.object("mdi")
        status = ui.label("mdistatus")

        c.on_command("demo.mdi.spawn", self.spawn_mdi_child)
        c.on_command("demo.mdi.tile", lambda: mdi.set("tile"))
        c.on_command("demo.mdi.cascade", lambda: mdi.set("cascade"))
        c.on_command("demo.mdi.next", lambda: mdi.set("next"))
        c.on_command("demo.mdi.prev", lambda: mdi.set("prev"))

        entries = {}  # window id -> dock entry handle

        def drop_entry(win_id):
            h = entries.pop(win_id, None)
            if h is not None:
                h.destroy()

        def on_minimize(ev):
            win_id = ev.uint("window")
            title = ev.text("title") or ""
            drop_entry(win_id)  # never two entries for one window
            self.dock_seq += 1
            key = "e%d" % self.dock_seq
            try:
                entry_ui = c.build(
                    "set mdidock children={%s=new dockentry caption=%s window=%d}\nwentry=mdidock.%s"
                    % (key, quote(title), win_id, key))
            except Exception:
                return
            entry = entry_ui.object("wentry")

            def on_click(_ev):
                try:
                    mdi.set("restore=%d" % win_id)
                    drop_entry(win_id)
                except Exception:
                    pass
            entry.on("click", on_click)
            entries[win_id] = entry

        mdi.on("minimize", on_minimize)
        mdi.on("restore", lambda ev: drop_entry(ev.uint("window")))
        mdi.on("remove", lambda ev: drop_entry(ev.uint("window")))

        def on_active(ev):
            title = ev.text("title")
            status.set_caption("Active: " + title if title else "Active: none")
        mdi.on("active", on_active)

        self.spawn_mdi_child()  # the initial document

    def spawn_mdi_child(self):
        self.mdi_count += 1
        try:
            ui = self.conn.build(scripts.mdi_child_script(self.mdi_count))
        except Exception:
            return
        win_id = ui.id("wwin")
        ui.button("wnew").on_click(self.spawn_mdi_child)
        ui.button("wclose").on_click(lambda: self.ui.object("mdi").set("remove=%d" % win_id))

    def open_protocol_window(self):
        try:
            ui = self.conn.build(scripts.PROTOCOL_WINDOW_SCRIPT)
        except Exception:
            return
        status = ui.label("pstatus")

        def on_toggle(s):
            state = {FlagState.TRUE: "on", FlagState.INDETERMINATE: "mixed"}.get(s, "off")
            status.set_caption("event toggle checked=" + state)
        ui.checkbox("pcb").on_toggle(on_toggle)
        ui.text_input("pinp").on_change(lambda s: status.set_caption('event change text="' + s + '"'))
        ui.selector("pcombo").on_change(lambda i: status.set_caption("event change selected=%d" % i))

        def on_hello():
            status.set_caption("event command action=demo.hello")
            self.set_status("demo.hello dispatched from protocol-built button!")
        self.conn.on_command("demo.hello", on_hello)

    def open_terminal_window(self):
        self.mdi_count += 1
        try:
            ui = self.conn.build(scripts.demo_terminal_script(self.mdi_count))
        except Exception:
            return
        win = ui.window("dwin")
        ui.button("dcloser").on_click(win.close)

    def show_about(self):
        self.conn.exec(scripts.ABOUT_DIALOG_SCRIPT)

    # --- secondary application -------------------------------------------

    def build_secondary(self, n: int):
        self.ui = self.conn.build(scripts.secondary_build_script(n))
        self.wire_secondary(n)

    def wire_secondary(self, n: int):
        ui, c = self.ui, self.conn
        ui.button("closer").on_click(lambda: ui.window("w").close())

        c.on_command("demo.app.close", lambda: ui.window("w").close())
        c.on_command("demo.app.cut", lambda: c.exec("cut"))
        c.on_command("demo.app.copy", lambda: c.exec("copy"))
        c.on_command("demo.app.paste", lambda: c.exec("paste"))
        c.on_command("demo.app.selectall", lambda: c.exec("selectall"))
        c.on_command("demo.app.rawkey", lambda: c.exec("rawkey"))
        c.on_command("demo.app.info", lambda: c.exec(
            'dlg=new messagebox icon=information ok title="About App %d" '
            'text="This is Secondary Application #%d\\n\\nIt has its own menus and status bar."' % (n, n)))
        c.on_command("demo.app.about", lambda: c.exec(
            'dlg=new messagebox icon=information ok title="About" '
            'text="Secondary Application\\n\\nDemonstrates multi-application support."'))

        # Closing the window ends this secondary connection.
        ui.window("w").on_closed(self.conn.close)


def open_secondary(path: str):
    """Dial a new connection - a new Application with its own window, menu
    bar and status bar - the meaning of "New Window"."""
    global _secondary_count
    with _secondary_lock:
        _secondary_count += 1
        n = _secondary_count
    try:
        sec = App(path, "App %d" % n, primary=False)
        sec.build_secondary(n)
    except Exception:
        return
