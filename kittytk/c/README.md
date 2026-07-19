# KittyTK — C client

A pure-C client for the KittyTK display protocol, plus the demo app. Like
the Python port, it proves the protocol is **language-neutral**: a C program
drives the exact same Go display host (`kittytk-tui` / `kittytk-sdl`) over
the identical wire. Depends only on libc + pthreads.

```
kittytk.h / kittytk.c   the client library: wire format (scanner, quoting,
                        flat inbound parser, events), socket transport,
                        reader + event pthreads, subscriptions, UI ids
scripts.h / scripts.c   the demo's protocol-text build scripts
counter.c               the smallest useful app: a label + a button that
                        increments the number on the label
demoapp.c               the interactive demo (menus, MDI, dialogs, secondary)
interop_smoke.c         bidirectional interop client (for the Go harness)
demoapp_smoke.c         full-demo build client (for the Go harness)
interop/                Go harness: a real headless host driving the C clients
```

## Build & run

```sh
make                                          # builds ./demoapp + ./counter
make KT_TLS=1                                 # same, with tls:// support (OpenSSL)
# macOS TLS: brew install openssl@3  (the Makefile finds Homebrew's copy)

# terminal 1 — a desktop host (either renderer):
go run ./cmd/kittytk-tui                 # terminal
go run -tags sdl ./cmd/kittytk-sdl       # graphical

# terminal 2 — the C app:
./demoapp                                     # attaches to the host
./demoapp --solo                              # becomes the whole display
./counter                                     # the minimal example
```

The app attaches over whatever `$KITTYTK_DISPLAY` names — a unix socket
(default), `tcp://host:port`, or `tls://host:port`. The default build
(unix + `tcp://`) needs only libc + pthreads; `tls://` is opt-in via
`make KT_TLS=1` and links OpenSSL. `kittytk.c` also compiles natively on
Windows (Winsock2 + Win32 threads). See
[../docs/transports-and-security.md](../docs/transports-and-security.md).

The minimal example, `counter.c`, is the whole KittyTK pattern in ~40 lines
of C: dial, build a window with a label and a button, subscribe to the
button's click, and rewrite the label on each click.

## Verify

```sh
# Interop against a REAL Go host: the harness compiles the C smokes with cc,
# stands up a headless display service, and drives input into the C client,
# confirming events flow both ways over a live socket, plus the full demo
# build is accepted from C.
go test ./c/interop/
```
