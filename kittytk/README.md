# KittyTK

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **KittyTK** — *image/tty Trinket Kit*

*If you use this, please support me on ko-fi:  [https://ko-fi.com/jeffday](https://ko-fi.com/F2F61JR2B4)*

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/F2F61JR2B4)

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

## What it is

Most UI toolkits pick a surface. A terminal library gives you cells and
keystrokes; a desktop library gives you pixels and a compositor; and an
application written against either is written against that choice for good.

KittyTK does not ask an application to make it. An application describes its
interface — windows, menus, a tree view, an embedded terminal — and connects to
a **display service** that renders it. One service draws to a terminal, in
cells, over whatever escape sequences that terminal turns out to support.
Another opens real OS windows through SDL, with proportional text, fractional
scaling and a GPU renderer. The application does not know which one answered,
and does not contain a line of rendering code either way.

That connection is a **protocol**, so the service need not be in the
application's process, on its machine, or written in its language. The same
program runs as a linked-in library, as a client on a unix socket, and as a
client over TLS to a display service on another continent. In the terminal
case, that means a full windowed interface — overlapping windows, menu bars,
dialogs, drag-resize — arriving over an SSH connection with nothing installed
at the far end but a terminal emulator.

## A complete application

`examples/remoteapp` is a display-protocol application in its entirety: it
imports `client` and `protocol`, and nothing that can draw.

```go
conn, err := client.Dial(client.DefaultSocketPath(), "Remote App", dispatch)

ui, err := conn.Build(`
w=new window title="Remote App" x=96 y=96 width=400 height=224 children={
	p=new panel layout=vbox spacing=0 children={
		new label caption="This window's process is NOT the desktop's process." wrap
		status=new label caption="Interact - events cross the socket."
		new separator
		cb=new checkbox caption="Remote tri-state checkbox" tristate
		inp=new textinput placeholder="Typed text travels as events..."
		btn=new button caption="Quit Remote App" action=remote.quit
	}
}
watch=w.p.status
wcb=w.p.cb
`)

status := ui.Label("watch")
ui.Checkbox("wcb").OnToggle(func(s protocol.FlagState) {
	status.SetCaption("toggled — that round-tripped the socket")
})
```

The block passed to `Build` is the wire format, not a template that compiles
to one. Property assignments travel the same way, so `SetCaption` on a label
handle is one message, and a toggle coming back is another. Run
`examples/demo` in one terminal and `examples/remoteapp` in a second, and the
window belongs to the second process.

## Two surfaces

|  | Terminal | Graphical |
|---|---|---|
| Host | `cmd/kittytk-tui` | `cmd/kittytk-sdl` (build tag `sdl`) |
| Unit | character cells | pixels, at fractional scale |
| Text | the terminal's glyphs | shaped and rasterized here, proportional |
| Renderer | escape sequences | software raster, or WebGPU |
| Windows | drawn inside the one terminal | real OS windows, one per torn-off window |

The terminal host negotiates what it is talking to rather than assuming: it
probes for an inline-image protocol and falls back through the alternatives to
sixel, reads pixel geometry so images and mouse coordinates land on exact
cells, and adapts to known terminal quirks in RTL and combining-mark placement.
Images placed by an embedded terminal trinket are re-emitted through whichever
protocol survived the probe.

The graphical host draws through a raster backend that is pixel-exact at
fractional font sizes — chrome, carets and cell grids all derive from a
fractional pixels-per-unit value rather than integer points. SDL3 is bound
through purego, so there is nothing to link and no dev headers are needed;
`make standalone` embeds SDL3 in the binary for machines that have none.

## The protocol

A display service listens on any of:

```
/path/to/socket   or   unix:/path        tcp://host:port        tls://host:port
```

The same protocol over each, from **Go** (`client/`), **Python** (`python/`)
and **C** (`c/`), on Windows, Linux and macOS. TLS is PKI-free: SSH-style
fingerprint pinning on the client, interactive per-client approval on the host
with persistent allow/deny stores. See
[docs/transports-and-security.md](docs/transports-and-security.md).

The vocabulary is **introspectable over the wire** — `conn.Describe()` returns
the live type and property registry, with tips and defaults, from the service
that is actually running. A client does not need a matching build to know what
it can ask for.

## What you get

**Trinkets** — `button` `checkbox` `radiobutton` `label` `separator` `spacer`
`progress` `textinput` `panel` `fixedbox` `splitter` `scrollarea` `listview`
`treeview` (multicolumn, editable, sortable) `combobox` `tabs` `mdipane`
`terminal` `editor`, inside `window`s, with `menu` / `menubar`, `messagebox`
and `statusbar`.

`terminal` embeds a full terminal emulator ([PurfecTerm][pt]) as a trinket, on
either surface. `editor` is a documented contract with a deliberately minimal
placeholder in stock builds ([docs/editor-trinket.md](docs/editor-trinket.md));
[Mew][mew] supplies a real one.

**Windows** — a window manager with overlapping windows, keyboard and pointer
resize, modal stacks, z-order for owned overlays, cascade and tile layouts, and
a minimize dock. `mdipane` gives the same inside a trinket.

**Tear-off** — on the graphical surface any window can detach to its own OS
window, carrying its own menu bar and status bar, and dock back by drag.
**Solo mode** takes it further: the main window tears off and the desktop
surface closes, so a KittyTK application presents as an ordinary native
application with no desktop behind it.

**Layout** — box, flex and grid, sized in *denomination cells* so a layout
resolves identically against a character grid and against pixels.

## Text and input

The parts that are usually an afterthought are most of the work here.

**Text** is shaped through [go-text/typesetting][gt]: proportional fonts,
system font discovery, real measurement rather than cell arithmetic for carets
and menus. Arabic is shaped contextually — joined across cells, with kashida
and harakat surviving the join — and Hebrew combining points are folded into
presentation forms for terminals that misplace them. RTL runs carry a rendering
hint so the host can compensate for a specific terminal's drift.

**Keyboard** input is handled to the level the available host allows:
disambiguated keys, press/repeat/release, associated text, and key names that
mean the same thing on both surfaces. Composition is real — dead keys, IME
preedit with an anchored composition range, and press-and-hold character
palettes are each carried through to a commit, on terminals that report them
three different ways.

**Mouse** reporting is pixel-precise where the terminal offers it (`?1016`),
so a click at a fractional font size lands on the cell that was painted.

**Accessibility** is in the core model, not bolted on: semantic roles,
announcements, focus chains that report inactive when their window is
backgrounded, and throttled navigation announcements.

## Building and running

```sh
make              # both hosts into bin/
make tui          # terminal host
make sdl          # graphical host, software renderer
make webgpu       # graphical host with the WebGPU renderer compiled in
make standalone   # graphical host with SDL3 embedded — runs with nothing installed
```

Or without make:

```sh
go run ./cmd/kittytk-tui             # terminal desktop
go run -tags sdl ./cmd/kittytk-sdl   # graphical desktop
go run ./examples/demoapp            # an app that attaches to either
```

The render engine is chosen at run time — `renderer =` in `kittytk.ini`, or
`--webgpu` / `--software`. `make increment` bumps the build counter in
`core/version.go`.

## Repository map

| Path | |
|---|---|
| `protocol/` | wire format, parser, type and property registry, introspection |
| `client/` | Go client: dial, build, handles, events |
| `python/`, `c/` | Python and C client libraries |
| `objects/trinkets/` | the trinkets |
| `objects/window/`, `objects/app/` | window manager, tear-off, title bars, app model |
| `display/` | display service: sessions, endpoints, TLS, authorization |
| `core/` | trinket model, focus, keymaps, key names, accessibility, repaint |
| `layout/` | box, flex, grid |
| `backend/tui/`, `backend/raster/` | the two render backends |
| `sdl/`, `platform/` | SDL3 platform, renderers, shaders |
| `text/`, `hebrew/` | shaping, font database, measurement, Hebrew folding |
| `style/` | palettes, schemes, icons |
| `hostterm/`, `hostcfg/` | terminal identification; host auth stores |
| `ptydriver/` | client-side pty driver for terminal child processes |
| `cmd/`, `examples/` | the two hosts; demo and example applications |
| `docs/` | design notes, protocol vocabulary, security model |

## Module

```
github.com/phroun/kittytk
```

The stylized wordmark is `KittyTK`; the code identifier — module path,
package imports, binary, and the `KITTYTK_DISPLAY` environment variable — is
lowercase `kittytk`.

## Tests

```sh
go build ./... && go build -tags sdl ./...
go test ./...  && go test -tags sdl ./objects/...
```

Test files are named for the source file they test — `0_keymap_test.go`
tests `keymap.go`, `00_` marks one that spans several files. **Read
[TEST-NAMING.md](TEST-NAMING.md) before adding a test file**; it is short,
and it covers a trap that removes tests from the build without failing
anything.

## Status

Alpha, at `0.1.x`, and moving. The protocol vocabulary is still growing and
property names are not yet frozen. The WebGPU renderer links on macOS and
Windows; the Linux link is blocked by an upstream `goffi` relocation issue,
so `make sdl` is the Linux path for now. Everything else described above is
implemented and under test.

[pt]: https://github.com/phroun/purfecterm
[mew]: https://github.com/phroun/mew
[gt]: https://github.com/go-text/typesetting
