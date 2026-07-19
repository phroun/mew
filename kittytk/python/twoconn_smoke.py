"""Two-connection TLS stress (mirrors the C twoconn smoke).

Dials one connection and builds, then dials a SECOND connection and
builds while its reader thread is already blocked reading - the demoapp
"New Window" shape. Prints OK on success. Endpoint is argv[1].
"""
import sys
import time

import kittytk


def main() -> int:
    ep = sys.argv[1] if len(sys.argv) > 1 else "tls://127.0.0.1:9797"

    c1 = kittytk.dial(ep, "Primary")
    c1.build('w=new window title="One" width=220 height=120 '
             'children={new label caption="hi"}')

    c2 = kittytk.dial(ep, "App 1")
    time.sleep(0.3)  # let conn2's reader settle into a blocking read
    c2.build(
        'w=new window title="Two" width=260 height=140 children={\n'
        '  p=new panel layout=vbox children={\n'
        '    new label caption="secondary application window content"\n'
        '    new button caption="a button"\n'
        '    new textinput\n'
        '  }\n'
        '}\n')
    for _ in range(20):
        c2.exec("tile")

    print("OK")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
