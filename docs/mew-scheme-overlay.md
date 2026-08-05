# mew's scheme overlay (plan)

> **Status: plan, mew-private.** How mew's URL schemes sit on top of KittyTK's
> `profile://` / `app://` model. The KittyTK base is specified in
> [`kittytk/docs/scheme-architecture.md`](../kittytk/docs/scheme-architecture.md)
> and is deliberately mew-free — this document is the mew side and stays here.
> Current, as-shipped behavior is in [`mew-scheme.md`](mew-scheme.md) and
> [`help-scheme.md`](help-scheme.md); this is the target we migrate them toward.

mew is one KittyTK Application. It gets storage from the Desktop-owned profile,
and it registers its own schemes for mew-specific features. Three roles, three
homes:

| Content | Scheme | Backed by |
|---|---|---|
| mew's stored, user-overridable files (config, grammars, help pages) | `app:///…` | the Desktop profile (mew's own sandbox) |
| mew-generated dynamic surfaces (Quick Help, and future ones) | `mew:/…` | a mew-registered handler, no file |
| the help wiki front-end | `help:/…` | storage under `app:///help/…` |

## Storage → `app:///` (was `mew:///`)

Everything mew reads from or writes to its own tree becomes the calling
Application's sandbox — `app:///…`, empty authority = "me". The Desktop resolves
it; mew never sees a real path. This replaces today's `mew:///` storage role
(see `mew-scheme.md`). The three-layer overlay mew relies on today
(shipped-embedded → system → user override) is exactly the general `app://`
sandbox behavior — mew ships defaults and the user shadows them in mew's
sandbox — so it stops being a mew special case.

Migration of the identities documented in `mew-scheme.md`:

| Today | Target |
|---|---|
| `mew:///editor.conf` | `app:///editor.conf` |
| `mew:///profile.mew` | `app:///profile.mew` |
| `mew:///syntax/<name>.jsf` | `app:///syntax/<name>.jsf` |
| `mew:///help/<page>.txt` | `app:///help/<page>.txt` |
| `mew:///<timestamp>.ans` (screen dumps) | `app:///<timestamp>.ans` |

## Generated surfaces → `mew:/` (a mew-registered scheme)

`mew:/` is reserved for content mew **generates**, not content it stores —
Quick Help today, and the family of internal surfaces to follow. It is a
handler-backed scheme mew registers with the Desktop (the "Application-registered
scheme" overlay point in the KittyTK doc), private to mew: other Applications
never address `mew:/`; they would reach mew's stored files, if ever granted,
through `app://mew/…`.

This finally retires the wart in `mew-scheme.md`/`help-scheme.md` where
`mew:///quickhelp` had to be placed *deliberately outside* `mew:///help` so the
wiki resolver would not treat it as a file. Under the split, generated content
simply lives in a different scheme (`mew:/quickhelp`) from stored content
(`app:///…`), and the "sits outside" hack disappears.

## The help wiki → `help:/` over `app:///help/`

`help:/` stays exactly as `help-scheme.md` describes it — the registered wiki
front-end, `help:/start` and friends — but its backing root moves from
`mew:///help` to **`app:///help/`** (mew's own sandbox, still file-backed and
user-overridable). The `help:/…` spelling users type is unchanged; only the
storage root beneath the wiki registry moves. Any future mew wiki is the same
shape: a `…:/` front-end over `app:///…` storage.

## Where the code changes

The two chokepoints that own the scheme surface today (`mew-scheme.md` names
them) are where the migration lands:

- **`internal/editor/mewfs.go`** — today's `mewVFS` resolver for `mew:`. Becomes
  the client of the Desktop's `app://` resolver for stored content, plus the
  registrar/handler for the generated `mew:/` scheme.
- **`internal/editor/canon.go`** — the identity layer. Its normalization flips
  from "fold every `mew:` spelling to one identity" to the authority-driven
  `app://` / `mew:` identities, keeping one canonical URL per resource (the
  KittyTK doc's canonical-identity rule).

The wiki registry (`internal/editor/wikiref.go`) changes only the help root
string (`mew:///help` → `app:///help`); the `help:` front-end logic is untouched.

## Boundary note

Nothing in this overlay goes upstream. `profile://` / `app://` are KittyTK's and
belong in the KittyTK tree, mew-free. `mew:` and `help:` are mew features and
stay in mew — the same fork boundary the `//go:build mew` files already hold
(see `kittytk/docs/fork-sync-policy.md`).
