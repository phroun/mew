"""Entry point for the Python KittyTK demo application.

    # terminal 1: a desktop host
    go run ./examples/kittytk-tui             (or -tags sdl ./examples/kittytk-sdl)
    # terminal 2: this app
    python3 -m demoapp                         (from the python/ directory)
    python3 -m demoapp --solo                  (become the whole display)
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import kittytk  # noqa: E402
from demoapp.app import App  # noqa: E402


def main() -> int:
    parser = argparse.ArgumentParser(prog="demoapp")
    parser.add_argument("--solo", action="store_true",
                        help="run as the whole display (solo mode)")
    args = parser.parse_args()

    path = kittytk.default_socket_path()
    try:
        app = App(path, "KittyTK Demo", primary=True, solo=args.solo)
    except OSError as e:
        print("cannot reach display service at %s: %s" % (path, e), file=sys.stderr)
        print("start a desktop first: go run ./examples/kittytk-tui "
              "(or -tags sdl ./examples/kittytk-sdl)", file=sys.stderr)
        return 1

    try:
        app.build_primary()
    except Exception as e:  # noqa: BLE001
        print("build main window: %s" % e, file=sys.stderr)
        app.conn.close()
        return 1

    app.wait()  # blocks until the main window closes or the desktop exits
    return 0


if __name__ == "__main__":
    sys.exit(main())
