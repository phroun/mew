# Embedding mew as a toolkit widget

A short guide for the kittyTK team (or any toolkit) that wants to host mew as a
"trinket" — a self-contained editor widget rendered on a terminal surface the
toolkit owns.

mew is a Go library: `import "github.com/phroun/mew"`. Everything below is on
that package. mew runs its full-screen TUI session against surfaces **you**
provide, and — wired as shown — never reads or writes the host machine on its
own. You feed it parsed keys, it emits a terminal escape stream you paint into
your emulator cell surface, and every file it touches goes through your hooks.

## The mental model

```
   your key handler  ──SendKey/SendPaste──▶  ┌──────────┐
                                             │   mew    │
   your emulator     ◀──escape byte stream── │ (goroutine)
   surface  (paint)      (Terminal.Output)   └──────────┘
                                                 │  │  │
                        document files ──────────┘  │  │
                        mew's own tree (config…) ───┘  │
                        state / identity / etc. ───────┘
```

A session is one blocking call (`mew.EditContent`, `EditFile`, `EditArgs`,
`EditArgv`). Run it on a goroutine, drive input through the key feed, and it
returns when you `Close()` the feed (or the buffer session otherwise ends).

## Wiring the surfaces

Compose a session from `Option` values. The ones a widget host cares about:

| Concern | Option | What you provide |
|---|---|---|
| **Render + size + resize** | `WithTerminal(Terminal)` | `Terminal.Output` (an `io.Writer`) receives mew's escape stream — pipe it into your emulator. `Terminal.Size` returns the surface's `(w,h)`. `Terminal.Resize` is a `<-chan struct{}`; send on it after the widget resizes and mew re-queries `Size` and repaints. Leave `Terminal.Input` **nil** — the key feed replaces it. |
| **Input (parsed keys)** | `WithKeyFeed(*KeyFeed)` | `f := mew.NewKeyFeed()`. Call `f.SendKey(name)` with one parsed key per call, `f.SendPaste(bytes, final)` for bracketed paste, and `f.Close()` to end the session. Send calls block only while mew is busy and the buffer is full; they return `false` once closed. |
| **Document files** | `WithFileSystem(FileSystem)` | `ReadFile`, `WriteFile`, `Glob` — mew opens/saves/completes documents only through these. Optionally implement `Statter` (one-path metadata, marks dirs in completion) and `DirGlobber` (glob + metadata in one call). `OSFileSystem()` is the real-OS default if you want to wrap it. |
| **mew's own tree** (editor.conf, profile.mew, syntax grammars, native locks, crash dumps) | `WithMewFileSystem(FileSystem)` | Virtualizes the `mew:/` URL scheme: every `mew:/x` path is handed to this FileSystem verbatim, confined so a `..` can never escape the tree root. The host owns the tree; nothing under the user's home is read. (Omit it to map `mew:/x` → `<home>/.mew/x` on the real OS instead — see `WithHomeDir`.) |
| **Home directory** | `WithHomeDir(dir)` | Overrides where the *local* `mew:/` tree and `~` completion resolve, when you are **not** virtualizing with `WithMewFileSystem`. |
| **Lock identity** | `WithIdentity(user, host, pid)` | The `user@host.pid` mew stamps into (and recognizes in) lock files. Set it when one process runs many editor widgets, or when "who" differs from the OS process. Empty fields fall back to OS values. |
| **Config without touching home** | `WithConfigText(text)`, `WithoutUserConfig()`, `WithProfileScript(script)`, `WithoutProfileScript()` | Supply `editor.conf` and the startup `profile.mew` as strings (or disable them). With these, mew never needs the user's real config files. Note: the `[storage]` section is ignored from `WithConfigText` — redirect scratch with `WithColdStoragePath`. |
| **State persistence** | `WithStateCallback(cb)`, `WithState(map)` | `cb` fires once at shutdown with a snapshot of runtime option state; serialize it with `EncodeState(state, FormatPSL\|FormatJSON)`. Restore it next launch with `WithState(DecodeState(saved))`. Together with the config/profile options, your toolkit is the full persistence go-between. |
| **Cold storage** | `WithColdStoragePath(dir)` | A **real local** directory Garland uses to page large files. Required even when documents are virtualized (Garland always needs a real spill path); defaults to the system temp dir. |
| **Crash dumps** | `WithDeadcat(name)` | Opts into DEADCAT: on a sudden shutdown, every modified buffer is dumped to `name` through your `WithFileSystem`, so unsaved work survives your process dying. |

## Key names

`SendKey` takes direct-key-handler naming — the same vocabulary mew binds in its
config:

- printable: `"a"`, `"A"`, `" "`
- control: `"^K"`, `"^["`  (mew's alias `"esc"` also works)
- named: `"Escape"`, `"Return"`, `"Tab"`, `"Backspace"`, `"Delete"`, `"Home"`, `"End"`, `"PageUp"`, `"PageDown"`
- arrows / meta / function: `"Left"`, `"M-Left"`, `"F1"` … `"F12"`

Send exactly the surfaces the widget is focused for — mew has no other input
channel. Paste content arrives via `SendPaste` (chunk on rune boundaries, flag
the final chunk); it collapses into one undo step and is never re-parsed as
keys.

## Minimal example

```go
package main

import "github.com/phroun/mew"

func runWidget(surface *MyEmulatorSurface, docFS mew.FileSystem, mewFS mew.FileSystem) (string, error) {
	keys := mew.NewKeyFeed()
	resize := make(chan struct{}, 1)

	term := mew.Terminal{
		Output: surface,                              // your emulator's byte sink
		Size:   func() (int, int, error) { return surface.Cols(), surface.Rows(), nil },
		Resize: resize,
		// Input stays nil — the key feed drives input.
	}

	// Drive input from the toolkit's own key handler.
	go func() {
		for ev := range surface.Events() {
			switch ev.Kind {
			case KeyEvent:
				keys.SendKey(ev.KeyName)     // "a", "^K", "M-Left", …
			case PasteEvent:
				keys.SendPaste(ev.Data, ev.Final)
			case ResizeEvent:
				select { case resize <- struct{}{}: default: }
			case CloseEvent:
				keys.Close()                 // ends the session
			}
		}
	}()

	// One blocking session; returns the final document when it ends.
	return mew.EditContent(initialText,
		mew.WithTerminal(term),
		mew.WithKeyFeed(keys),
		mew.WithFileSystem(docFS),           // document I/O
		mew.WithMewFileSystem(mewFS),         // mew:/ config, profile, syntax, locks, dumps
		mew.WithColdStoragePath("/var/tmp/mew-widget"),
		mew.WithoutUserConfig(),              // supply config yourself instead
		mew.WithConfigText(myEditorConf),
		mew.WithoutProfileScript(),
		mew.WithIdentity("kitty", "widget-host", instanceID),
		mew.WithStateCallback(func(s map[string]interface{}) {
			psl, _ := mew.EncodeState(s, mew.FormatPSL)
			persist(psl)
		}),
	)
}
```

`Terminal.Output` is any `io.Writer` — an `*os.File`, a `bytes.Buffer`, or your
emulator's write end all work.

## Lifecycle checklist

1. Build the `KeyFeed` and `Terminal`; start a goroutine translating toolkit
   events into `SendKey` / `SendPaste` / `Resize` sends.
2. Call one `Edit*` entry point on its own goroutine — it renders into
   `Terminal.Output` immediately and blocks until the feed closes.
3. On a size change, push to `Terminal.Resize`; mew re-queries `Size` and
   repaints the whole surface.
4. To dismiss the widget, `keys.Close()`. The session unwinds, the state
   callback fires, and the `Edit*` call returns the final content.

Wire all four of the surface hooks (`WithTerminal`, `WithKeyFeed`,
`WithFileSystem`, `WithMewFileSystem`) plus `WithColdStoragePath`, and mew is
fully sandboxed inside your toolkit: no real terminal, no real filesystem, no
signal handlers — just a widget you paint and drive.

## Licensing note

mew is source-available (see the repository `LICENSE`). You may statically link
the **unmodified** mew package into your program and ship the combined binary —
commercial builds included — so importing `github.com/phroun/mew` behind a build
flag is fine, and your own code stays under your own license. The one line you
can't cross is modifying or forking mew's own source; if a host needs a behavior
mew doesn't expose, treat it as a feature request rather than a local patch. The
embedded syntax grammars are separately MIT, and mew's dependencies are MIT/BSD
— carry `THIRD-PARTY-NOTICES.md` in whatever you distribute.
