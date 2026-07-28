# Notes for future: LSP

Background notes on the Language Server Protocol and how it would fit mew.
Written before any implementation exists — this is orientation and a plan,
not a specification. Nothing in mew implements LSP today.

Facts about the current tree that these notes lean on, verified at the time
of writing:

- `encoding/json` is already used (`internal/config/state.go`).
- mew core does **not** spawn subprocesses (only a deadcat signal test does).
  PawScript has `exec`, capability-tagged `system`.
- garland exposes decoration queries: `GetDecorationPosition`,
  `GetDecorationsInByteRange`, `GetDecorationsOnLine`.
- mew already has canonical document URLs (`file:///`, `mew:///`,
  "/"-normalised).

---

## 1. What LSP is, and why it exists

The problem is combinatorial. Before LSP, every editor needed its own
integration with every language: *N* editors × *M* languages. LSP makes it
*N* + *M* — each language ships **one server**, each editor writes **one
client**.

A language server is a **separate process**. The editor launches `gopls`,
`rust-analyzer`, `clangd`, `pyright` and talks to it over a pipe. The server
parses, type-checks and indexes; the editor displays and edits. Nothing about
the editor's buffer model has to change to satisfy it.

That is the whole idea. Everything below is protocol detail.

## 2. The wire

**JSON-RPC 2.0**, framed like HTTP, usually over the server's stdin/stdout:

```
Content-Length: 118\r\n
\r\n
{"jsonrpc":"2.0","id":1,"method":"textDocument/hover","params":{...}}
```

Three message kinds:

| kind | has `id` | expects reply |
|---|---|---|
| Request | yes | yes |
| Response | yes (matching) | — |
| Notification | no | no |

Everything is asynchronous and interleaves. Responses arrive out of order and
are matched by `id`. `$/cancelRequest` abandons one — worth having early,
because a user who keeps typing invalidates the completion request from three
keystrokes ago.

The channel is **bidirectional**. The server also sends requests to the
client; `workspace/applyEdit` is the server asking the editor to modify
buffers, which is how rename works. It is not client-polls-server.

## 3. Lifecycle

```
→ initialize          client capabilities, root URI
← ServerCapabilities  "here is what I actually support"
→ initialized
   ... work ...
→ shutdown
→ exit
```

The **capability exchange** is what makes this tractable. The client
advertises only what it has implemented, the server advertises what it
offers, and each side uses the intersection. **A client does not have to
implement all of LSP.** A client that only handles diagnostics is a valid
client.

## 4. Document synchronisation — the heart of it

The server keeps its own **mirror** of every open buffer. If the two mirrors
disagree by one character, every position the server returns is wrong. This
is the part to get exactly right; the rest is comparatively forgiving.

```
textDocument/didOpen    full text, version 1
textDocument/didChange  version N+1, incremental ranges (or full text)
textDocument/didSave
textDocument/didClose
```

Documents are identified by **URI** (`file:///home/user/mew/main.go`), and
every change bumps a monotonic **version number**.

Two fits with mew:

- Canonical document URLs already exist, so `textDocument.uri` is free.
- garland returns a `ChangeResult` (fork + revision) on every mutation, which
  is a natural driver for both the version counter and the change ranges.

## 5. The trap: position encoding

LSP positions are `{line, character}`, both zero-based. By default
`character` is an offset in **UTF-16 code units**.

Not bytes. Not runes.

In `héllo` the `l` is at character 3 either way, but a CJK glyph or emoji
outside the BMP counts as **two**, and combining marks count separately. This
is the most common source of off-by-N bugs in LSP clients, and it will hurt
mew more than most editors precisely because mew is rune-indexed and takes
bidi, CJK and combining marks seriously.

LSP 3.17 added `positionEncoding` negotiation: the client may request `utf-8`
or `utf-32`, and most modern servers accept `utf-8`.

**Negotiate explicitly and honour whatever comes back.** Put the conversion in
exactly one function, test it over Hebrew, CJK and astral-plane input, and
never let position arithmetic happen anywhere else in the client.

## 6. What LSP offers

Push — server to client, unprompted:

- **`textDocument/publishDiagnostics`** — errors and warnings with ranges.
  This is most of the felt value of LSP, and it arrives without being asked.

Pull — client to server:

| request | gives |
|---|---|
| `hover` | type and docs under the caret |
| `definition` / `typeDefinition` / `implementation` | jump targets |
| `references` | every use of a symbol |
| `documentSymbol` | the file's outline |
| `workspace/symbol` | project-wide symbol search |
| `completion` (+ `completionItem/resolve`) | completion, detail resolved lazily |
| `signatureHelp` | parameter hints mid-call |
| `rename` (+ `prepareRename`) | a `WorkspaceEdit` spanning files |
| `codeAction` | quick fixes and refactors |
| `formatting` / `rangeFormatting` | reformatting |
| `semanticTokens` | highlighting that knows a type from a variable |
| `inlayHint` | inferred types rendered inline |
| `documentHighlight` | occurrences of the symbol under the caret |
| `foldingRange` | fold regions |

`semanticTokens` is worth noting specifically: it delivers what jsf
structurally cannot, because it comes from a real type-checker rather than a
regex grammar.

## 7. How it maps onto mew

### Already present and directly reusable

| mew has | LSP needs it for |
|---|---|
| canonical `file:///` URLs | `textDocument.uri` |
| `PostAction` marshalling | delivering replies onto the editor main loop |
| PawScript `RequestToken` / `ResumeToken` | a request *is* a suspend/resume — same shape as the `insert_raw_byte` prompt |
| tagged transient warnings | diagnostics that replace rather than stack |
| modebar context slot | `documentSymbol` breadcrumb, replacing the jsf-derived one |
| display substitution engine | inlay hints — literally "render what is not in the source" |
| completion callback + shared-prefix autocomplete | `textDocument/completion` as another provider |
| option cascade per class/grammar/type | "which server for this file type" |
| **garland decorations** | **diagnostic ranges that survive edits** |

The last row is the strongest architectural fit and is worth building around.
Most editors store diagnostics as line/column pairs and re-map them by hand
after every keystroke, which is why diagnostics visibly drift while you type
in some editors. garland maintains decorations across edits already. Store a
diagnostic as a decoration and the drift problem does not exist.

### Has to be built

1. **Subprocess management.** mew core spawns nothing today.
2. **JSON-RPC framing and dispatch.** Modest; `encoding/json` is already in use.
3. **Position-encoding conversion.** Small code, high stakes (see §5).
4. **Document sync discipline**, driven off garland's change results.
5. **A server registry** — which binary for which grammar, launch args, root
   detection.
6. **Tool viewports for the results** — diagnostics list, references list,
   symbol list, hover, completion popup, signature help.

## 8. Sequencing — do the viewport architecture first

Item 6 above is the reason.

Almost every LSP surface is a ToolViewport: a transient hover near the caret,
a completion popup anchored to a position, a diagnostics list that wants to be
dockable, a references list whose selection drives a DocViewport. Designing
tiling and the Tool/Doc relationship **with those seven cases in hand** will
produce an architecture that fits them. Designing it abstractly and adding LSP
afterwards will surface the requirements late — the hover that must not steal
focus, the completion list that must track the caret through a scroll, the
references list that drives another viewport's contents.

LSP is a better test suite for the viewport design than anything invented for
the purpose.

### Then, in order of value per unit of work

1. **Diagnostics only** — transport, lifecycle, didOpen/didChange/didClose,
   `publishDiagnostics`. Roughly 15% of the protocol and 60% of what a
   programmer feels.
2. **Hover and go-to-definition** — small additions, immediately useful.
3. **`documentSymbol`** — replaces the jsf outline breadcrumb with a real one.
4. **Completion** — tightest latency budget, most UI. Wire into the existing
   completion callback rather than inventing a second path.
5. **References** — needs a results viewport.
6. **Rename** — last. It edits multiple files, so it needs a multi-buffer undo
   group, and that is where garland's forks earn their keep.
7. **Semantic tokens, inlay hints, code actions, formatting** — polish.

`gopls` is the obvious first target: mew is written in Go, so it can be
dogfooded from day one.

## 9. Cautions

**Keep it behind an interface.** LSP should be a *provider*, the way jsf is a
highlighting provider. "No server running" must be an ordinary state and not a
degraded one — mew's appeal includes working on a machine with nothing else
installed.

**Never block the main loop.** Servers stall; `gopls` on a cold cache can take
tens of seconds. Every call goes out asynchronously and comes back through
`PostAction`.

**Do not implement the whole protocol.** Capability negotiation exists exactly
so that a partial client is a correct client.

**Watch the capability boundary.** A language server is an arbitrary
executable reading the source tree. When the sandboxing work arrives (the
`LIBRARY allow/restrict` model, the `system`-tagged functions), launching a
language server is a capability decision, not a configuration detail.
