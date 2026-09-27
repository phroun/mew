"""Goodbye smoke client: what a Python application hears when its display goes.

Run by python/00_interop_test.go against a REAL Go display host, which quits
once this says it is connected. What is proved is that the farewell crosses the
wire into a non-Go client: the display says `goodbye reason=quit` and hangs up,
and this reads the reason back off its own connection.

A closed socket cannot say why it closed -- a display that quit and a connection
that broke are the same silence -- so a client that cannot read the reason is a
client whose applications have to guess.

Protocol markers on stdout (the Go side reads them):
  READY            - connected; the host may now quit
  GOODBYE <word>   - the farewell it heard, or `-` for none
  DONE             - exiting 0
"""

import os
import sys

sys.path.insert(0, os.path.dirname(__file__))

import kittytk  # noqa: E402


def main(sock: str) -> int:
    conn = kittytk.dial(sock, "Py Goodbye App")

    # Work of its own, which has nothing to do with any display and outlives it.
    # Nothing about hearing a farewell ends an application; that is the
    # application's own decision, taken below by returning.
    own = [n * n for n in range(4)]

    print("READY", flush=True)

    if not conn.closed.wait(15):
        print("TIMEOUT", flush=True)
        return 2

    reason, said = conn.goodbye()
    print("GOODBYE %s" % (reason if said else "-"), flush=True)

    if own != [0, 1, 4, 9]:
        print("FAIL: its own work did not survive the display going", flush=True)
        return 1

    print("DONE", flush=True)
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: goodbye_smoke.py <socket>", file=sys.stderr)
        sys.exit(64)
    sys.exit(main(sys.argv[1]))
