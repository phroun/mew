# KittyTK

> **KittyTK** — *image/tty Trinket Kit*

A cross-surface UI toolkit in Go. Its components — **trinkets** — render
either inside a terminal or break out onto their own graphical windows via
SDL, and applications build and drive their interface over a client/server
**display protocol** (in-process or across a socket).

The name is a recursive acronym in the GNU tradition: the first word refers
back to the whole, and the rest name what it is — **image** (graphical) and
**tty** (terminal — short for *teletype*, whose *tele-* even hints at driving
a display over a network, which the protocol reaches toward), a kit of
**T**rinkets (`TK` = Trinket Kit). It's also a cat pun — there's a `tty`
hiding in `Kitty` — a sibling to the rest of the line: **PurfecTerm**
(terminal emulator), **Mew** (text editor), and **PawScript** (language).

## Two desktops, one protocol

A **desktop host** renders the display and serves the protocol socket;
applications dial in and attach without knowing which renderer is on the
other end. Two interchangeable hosts ship under `cmd/`:

```
go run ./cmd/kittytk-tui             # terminal desktop
go run -tags sdl ./cmd/kittytk-sdl   # graphical (SDL) desktop
go run ./examples/demoapp            # an app that attaches to either
```

`make` builds both hosts into `bin/`; `make increment` bumps the build
counter in `core/version.go`.

The socket can be a unix socket (default), `tcp://host:port`, or
`tls://host:port` — the same protocol over any transport, from Go, Python,
or C, on Windows, Linux, or macOS. TLS is PKI-free (SSH-style fingerprint
pinning) and the host approves each remote client interactively. See
[docs/transports-and-security.md](docs/transports-and-security.md).

## Module

```
github.com/phroun/kittytk
```

The stylized wordmark is `KittyTK`; the code identifier — module path,
package imports, binary, and the `KITTYTK_DISPLAY` environment variable — is
lowercase `kittytk`.
