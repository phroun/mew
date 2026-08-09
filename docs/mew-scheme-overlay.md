# mew's scheme overlay

> **Status: mew-private. The storage/generated split below is LANDED**; the
> cross-Application and multiuser parts of the base model remain future KittyTK
> work. How mew's URL schemes sit on top of KittyTK's `profile://` / `box://`
> model — the base is specified in
> [`kittytk/docs/scheme-architecture.md`](../kittytk/docs/scheme-architecture.md)
> and is deliberately mew-free; this document is the mew side and stays here.
>
> **Landed today:** mew's storage moved `mew:/// → box:///` (mew's own box,
> resolved through mew's existing injected `FileIO` — the seam the Desktop
> resolver plugs into later), generated content moved to `mew:/` (Quick Help),
> and the help wiki root moved to `box:///help`. **Still future:** named
> `box://other/` (cross-Application), `profile://`, the Desktop-owned resolver,
> the capability broker, and multiuser — today mew resolves only `box:///`
> (its own box). See [`mew-scheme.md`](mew-scheme.md) / [`help-scheme.md`](help-scheme.md)
> for the full as-shipped inventory.

mew is one KittyTK Application. It gets storage from the Desktop-owned profile,
and it registers its own schemes for mew-specific features. Three roles, three
homes:

| Content | Scheme | Backed by |
|---|---|---|
| mew's stored, user-overridable files (config, grammars, help pages) | `box:///…` | the Desktop profile (mew's own sandbox) |
| mew-generated dynamic surfaces (Quick Help, and future ones) | `mew:/…` | a mew-registered handler, no file |
| the help wiki front-end | `help:/…` | storage under `box:///help/…` |

## Storage → `box:///` (was `mew:///`)

Everything mew reads from or writes to its own tree becomes the calling
Application's sandbox — `box:///…`, empty authority = "me". The Desktop resolves
it; mew never sees a real path. This replaces today's `mew:///` storage role
(see `mew-scheme.md`). The three-layer overlay mew relies on today
(shipped-embedded → system → user override) is exactly the general `box://`
sandbox behavior — mew ships defaults and the user shadows them in mew's
sandbox — so it stops being a mew special case.

Migration of the identities documented in `mew-scheme.md`:

| Today | Target |
|---|---|
| `mew:///editor.conf` | `box:///editor.conf` |
| `mew:///profile.mew` | `box:///profile.mew` |
| `mew:///syntax/<name>.jsf` | `box:///syntax/<name>.jsf` |
| `mew:///help/<page>.txt` | `box:///help/<page>.txt` |
| `mew:///<timestamp>.ans` (screen dumps) | `box:///<timestamp>.ans` |

## Generated surfaces → `mew:/` (a mew-registered scheme)

`mew:/` is reserved for content mew **generates**, not content it stores —
Quick Help today, and the family of internal surfaces to follow. It is a
handler-backed scheme mew registers with the Desktop (the "Application-registered
scheme" overlay point in the KittyTK doc), private to mew: other Applications
never address `mew:/`; they would reach mew's stored files, if ever granted,
through `box://mew/…`.

This finally retires the wart in `mew-scheme.md`/`help-scheme.md` where
`mew:///quickhelp` had to be placed *deliberately outside* `mew:///help` so the
wiki resolver would not treat it as a file. Under the split, generated content
simply lives in a different scheme (`mew:/quickhelp`) from stored content
(`box:///…`), and the "sits outside" hack disappears.

### Surface display traits (keyed by address)

A generated surface's chrome is derived from the buffer's `mew:` address, never
stored on the viewport — so it applies while the surface is shown and reverts
the moment the viewport navigates back to a document, with no per-viewport state
to reset (`Viewport.EffectiveClass` / `Viewport.LineNumbersVisible`):

- **No line-number gutter.** These are listings, not editable documents; the
  gutter is suppressed regardless of the `showLineNumbers` option.
- **A `surface` styling class.** Colors and focused chrome resolve under the
  class `surface`, so a user can theme the generated listings apart from their
  documents — e.g. `[surface.color.background]`, `[surface.color.link]` — without
  those rules leaking onto ordinary buffers. With no `[surface.*]` rules present,
  resolution falls through to the buffer type / global defaults exactly as
  before, so the default look is unchanged. (Behavioral options — read-only,
  link-browsing — stay fixed by address and are not part of this class overlay.)

## The help wiki → `help:/` over `box:///help/`

`help:/` stays exactly as `help-scheme.md` describes it — the registered wiki
front-end, `help:/start` and friends — but its backing root moves from
`mew:///help` to **`box:///help/`** (mew's own sandbox, still file-backed and
user-overridable). The `help:/…` spelling users type is unchanged; only the
storage root beneath the wiki registry moves. Any future mew wiki is the same
shape: a `…:/` front-end over `box:///…` storage.

## Where the code changed

- **`internal/editor/mewfs.go`** — `mewVFS` resolves the `box:` storage tree
  (`isBoxPath`/`confine` recognize `box:`; identities emitted as `box:///rel`).
  The generated `mew:` scheme is gated by `isGenPath` and handled in canon.go,
  not here.
- **`internal/editor/canon.go`** — the identity layer: `box:` folds to the real
  `~/.mew` file identity (OS mode) or `box:///` (virtual); a new `mew:` branch
  returns a stable synthetic identity for generated content and never touches
  the filesystem. One canonical URL per resource is preserved.
- **`internal/config/config.go`** — the config tree root is `box:///`
  (`box:///editor.conf`, `box:///profile.mew`); `boxToLocal` maps `box:///rel →
  <home>/.mew/rel`; `@include` confinement stays inside `box://`.
- **`internal/editor/wikiref.go`** — the help wiki `Root` is `box:///help`;
  `box` and `mew` are both registered link schemes; `box`/`file` are the
  followable document schemes. The `help:` front-end logic is untouched.
- **`internal/editor/editor.go`** — Quick Help's identity is `mew:/quickhelp`;
  the screen-capture dump path is `box:///<timestamp>.ans`.

The on-disk location is unchanged: `box:///` still maps to `~/.mew` — only the
URL scheme moved. Renaming the actual directory is a separate, user-visible
change and was deliberately left out.

## Boundary note

Nothing in this overlay goes upstream. `profile://` / `box://` are KittyTK's and
belong in the KittyTK tree, mew-free. `mew:` and `help:` are mew features and
stay in mew — the same fork boundary the `//go:build mew` files already hold
(see `kittytk/docs/fork-sync-policy.md`).
