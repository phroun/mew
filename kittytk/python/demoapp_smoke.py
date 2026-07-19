"""Full-demo build smoke: runs the Python demoapp's build path against a
REAL Go display host (driven by interop_test.go), the Python mirror of Go's
TestDemoBuildsOverService.

It builds the whole nine-tab main window, the protocol companion window,
the About dialog, an MDI document + dock entry, exercises the tab/window
properties, and fires every desktop-action app verb - proving the display
session accepts the full demo vocabulary from a Python client. Shell-bearing
scripts (terminal windows, secondary apps) are omitted, as in the Go test.

Prints OK on success.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(__file__))

import kittytk  # noqa: E402
from demoapp import scripts  # noqa: E402


def main(sock: str) -> int:
    conn = kittytk.dial(sock, "KittyTK Demo")
    # The demo is a multi-window app: declare it so the host allows the
    # additional (normal) windows the smoke test builds below.
    conn.set_app("multiwindow")

    ui = conn.build(scripts.main_build_script())
    for key in ["w", "tabs", "binput", "wfont", "dfont", "grid",
                "bgdef", "bggreen", "bggray", "sbgdef", "sbggreen", "sbggray",
                "mdi", "mdistatus", "mdidock", "mb", "sb"]:
        if ui.id(key) == 0:
            print("FAIL surfaced id %s missing" % key, flush=True)
            return 1

    conn.build(scripts.PROTOCOL_WINDOW_SCRIPT)
    conn.exec(scripts.ABOUT_DIALOG_SCRIPT)

    child = conn.build(scripts.mdi_child_script(1))
    win_id = child.id("wwin")
    if win_id == 0:
        print("FAIL mdi child window id missing", flush=True)
        return 1
    conn.exec('set mdidock children={e1=new dockentry caption="Document 1" window=%d}' % win_id)

    tabs = ui.object("tabs")
    tabs.set("background=green")
    tabs.set('background="#333333"')
    tabs.set("background=default")

    win = ui.object("w")
    win.set('font="tuesday12"')
    win.set("denomination=32")

    for verb in ['status text="hi there"', "cut", "copy", "paste", "selectall",
                 "tile", "cascade", "theme", "desktopfont tuesday",
                 "desktopfont default", "announce_visual", "announce_speak", "rawkey"]:
        conn.exec(verb)

    print("OK", flush=True)
    conn.close()
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: demoapp_smoke.py <socket>", file=sys.stderr)
        sys.exit(64)
    sys.exit(main(sys.argv[1]))
