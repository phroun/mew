# mew-backed Editor trinket (concept baseline)

A `trinkets.Editor` embeds the [mew](https://github.com/phroun/mew)
editor library into KittyTK as a normal trinket, rendered through an
internal PurfecTerm running as an **editor display surface**. This is
the concept-proving baseline; the file/path story (server-side host FS
vs. transported over the KittyTK protocol to the client) is left open on
purpose, behind the `mew.FileSystem` the caller supplies.

## How it fits together

```
   user keys/mouse ─▶ PurfecTerm (encodes to raw terminal bytes)
                          │  input sink
                          ▼
                     io.Pipe ─▶ mew.Terminal.Input (io.Reader)
                                     │
                                mew editor session
                                     │
   PurfecTerm.Feed ◀── mew.Terminal.Output (io.Writer, escape stream)
        │
        ▼
   cell grid painted by the trinket
```

The PurfecTerm is a **pure display + input surface** — exactly its role
when driving a remote PTY. mew does its own input parsing and screen
rendering, so there is **no key-name translation**: keystrokes go out as
the same raw terminal bytes a real terminal would send, and mew's escape
output is fed straight back into the emulator. Grid size flows from the
trinket's layout into `mew.Terminal.Size`, and each relayout pokes the
`Resize` channel so mew re-queries and repaints.

### Editor mode on PurfecTerm

`PurfecTerm.SetEditorMode(true)` (untagged, in the default build)
configures the emulator as an editor surface rather than a scrolling
terminal:

- the **scrollback buffer is disabled** — a full-screen editor repaints
  and owns its viewport; lines that scroll off the top are discarded, not
  accumulated;
- **no scrollbar lane** is reserved or drawn — text uses the full width;
- the local **Shift+navigation scroll keys are not intercepted** — they
  reach the child (the editor) like any other key.

This part is fully tested in the standard gate
(`purfecterm_editormode_test.go`).

## Building

The Editor trinket is behind the `mew` build tag so the default build
and the standard test gate never pull in the mew module (an alpha, with
its own transitive dependencies). To build or run it:

```sh
go get github.com/phroun/mew@0.3.1-alpha
go build -tags mew ./...
```

The main module's `go.mod` is deliberately left free of mew so the
default toolkit build and its dependency versions are unaffected;
`go get` above adds it (and bumps shared transitive deps) only in your
working tree.

## Minimal use

```go
ed := trinkets.NewEditor(trinkets.EditorOptions{
    Content:      "hello from mew\n",
    // Filename:  "/path/to/file",   // opens via mew.EditFile instead
    // FileSystem: myVFS,             // nil = real local disk (host OS)
    IdentityUser: "kitty",
    IdentityHost: "widget-host",
    IdentityPID:  os.Getpid(),
    OnExit:       func(err error) { /* close the window, etc. */ },
})
win.SetContent(ed)
// ...
ed.Close() // ends the mew session (EOF on input) and releases the surface
```

## Open questions (deliberately deferred)

- **Files & paths.** `EditorOptions.FileSystem` is the seam. `nil` uses
  `mew.OSFileSystem()` (the KittyTK host OS, server-side). A custom
  `mew.FileSystem` is where document access can instead be transported
  over the KittyTK protocol to the client. `MewFileSystem` virtualizes
  mew's own config/profile/lock storage (the `mew:/` scheme) the same
  way.
- **Lifecycle/self-dispatch.** `Editor` embeds `*PurfecTerm`, so focus
  and layout resolve to the PurfecTerm (correct — it owns all display
  and input semantics). The host must call `Editor.Close` (not
  `PurfecTerm.Close`) so the mew session is stopped too.
- **Save integration.** mew (via garland) owns save/consistency/locks;
  how KittyTK surfaces those to the user is future work.
