# KittyTK — Python client

A pure-Python client for the KittyTK display protocol, plus the demo app,
proving the protocol is **language-neutral**: a Python program drives the
exact same display host (`kittytk-tui` / `kittytk-sdl`) as the Go client,
over the identical wire.

```
kittytk/          the client library
  protocol.py     wire format: scanner, parser, string quoting, events
  client.py       Conn, UI handles (Button/Label/Checkbox/TextInput/…), dial
  endpoint.py     transport: unix/tcp/tls, TOFU pinning, client identity
demoapp/          the demo, ported from examples/demoapp
  scripts.py      the protocol-text build scripts (mostly a verbatim copy)
  app.py          the wiring (menus, MDI, secondary apps, dialogs)
tests/            wire-format fidelity tests (no server needed)
interop_test.go   Go harness: a real headless host driven by the clients
```

## Run the demo

```sh
# terminal 1 — a desktop host (either renderer):
go run ./cmd/kittytk-tui                 # terminal
go run -tags sdl ./cmd/kittytk-sdl       # graphical

# terminal 2 — the Python app (from this directory):
python3 -m demoapp                            # attaches to the host
python3 -m demoapp --solo                     # becomes the whole display
```

The app attaches over whatever `$KITTYTK_DISPLAY` names — a unix socket
(default), `tcp://host:port`, or `tls://host:port`. `tls://` needs the
`cryptography` package or the `openssl` CLI to mint the client identity.
See [../docs/transports-and-security.md](../docs/transports-and-security.md).

## Verify

```sh
# Wire-format fidelity (pure, offline):
python3 -m unittest discover -s python/tests

# Interop against a REAL Go host (bidirectional events + full demo build):
go test ./python/
```

The interop test stands up a headless Go display service and drives input
into the Python client, confirming events flow both ways over a live
socket. No Python dependencies beyond the standard library.
