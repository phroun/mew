# Resource scheme architecture: `profile://` and `box://`

> **Status: design.** This describes the intended addressing and capability
> model for Application storage under the Desktop. It is not yet implemented;
> it is written down so the URL grammar and permission rules are settled before
> code depends on them.

KittyTK Applications need a uniform way to name stored resources — their own,
the ones they share with sibling Applications, and (in time) resources that
belong to another user — without hard-coding real filesystem paths and without
being able to reach outside what the user has allowed. Two schemes cover it:

- **`profile://`** — the user's profile: the shared space that spans all of
  that user's Applications.
- **`box://`** — a single Application's private sandbox (its *box*; plural
  *boxen*).

**The Desktop owns the profile.** It is the single authority for both schemes —
the resolver that maps a URL to storage and the capability broker that mediates
every access crossing a boundary. Applications hold no profile state of their
own; they address resources and the Desktop decides what that means and whether
it is allowed.

## The grammar

Every distinction rides on the **authority slot**, exactly as `file://` uses it
for the host. Nothing depends on counting slashes.

| URL | Authority | Resolves to | Default permission |
|---|---|---|---|
| `box:///path` | empty | **the calling Application's own** sandbox | auto-granted |
| `box://name/path` | `name` | another Application's sandbox | brokered |
| `profile:///path` | empty | **the current user's** profile root (shared space) | see below |
| `profile://user/path` | `user` | another user's profile *(future — see Multiuser)* | brokered |

The single rule that governs it:

> **Empty authority = "mine"; a named authority = "someone else's" → broker it.**

For `box://`, "someone else" is another Application. For `profile://`, it is
another user. One bit — is the authority empty? — drives the permission reflex
in both schemes.

Because meaning lives in the authority, these URLs parse correctly under any
RFC 3986 parser; there is no reliance on the non-standard distinction between
"no authority" (`scheme:/x`) and "empty authority" (`scheme:///x`). Do **not**
assign the single-slash form (`box:/x`) a separate meaning — it collapses to the
empty-authority form in stock parsers, so treat it as an alias of `box:///x`, or
reject it.

## Disjoint namespaces

An Application's sandbox is, by convention, stored *inside* the user's profile
(e.g. under a per-Application folder at the profile root). That is a **physical
storage** fact, not a second public address:

- `box://name/…` is the **canonical** way to name any sandbox — your own or a
  sibling's.
- `profile:///…` names the **shared** area of the profile and is **not** a back
  door into an Application's sandbox, even though the bytes live under the
  profile root on disk.

Keeping the two address spaces logically disjoint means every resource has one
canonical identity, which the identity/canonicalization layer relies on for
buffer de-duplication and caching. Co-locate on disk; keep the addresses apart.

## Resolution and the host boundary

The Desktop resolves `profile://` and `box://`; Applications never do their own
path math and never receive a real filesystem path back. The scheme is
**opaque** by design: an Application addresses `box:///config` and the Desktop
decides where that physically lives ("wherever the profile is"), so storage can
be relocated — or backed by something other than a local filesystem — without
the Application caring.

Confinement is intrinsic: a sandbox root clamps `..`, so no path can escape the
folder it is scoped to. The same clamp mechanism, parameterized by which root,
serves every sandbox.

## Permission model

Access control is **two independent axes**, each keyed by the empty-vs-named
test on its own scheme:

1. **Application capability** — governs whether an Application may leave its own
   sandbox: `box:///…` (its own) is auto-granted; `box://sibling/…` and
   `profile:///…` (the shared area, i.e. stepping outside the sandbox) are
   brokered as a per-Application grant.
2. **User access** — governs whether one may reach *another user's* profile:
   `profile://user/…` is brokered against the user-access layer.

A grant can be scoped **once**, **for the session**, or **always**, recorded by
the Desktop. The two axes are orthogonal: `profile:///shared.db` is *yours*
(no user-access prompt), yet reaching it *from inside an Application* is still an
Application-capability crossing (the app is stepping outside its sandbox). Same
URL; which gate fires depends on who is asking.

## Application-registered schemes (the overlay point)

`profile://` and `box://` address **stored** resources. An Application may also
register **its own schemes** with the Desktop, routed to a handler the
Application provides, for content that is not file storage — for example a
dynamically generated view assembled on demand rather than read from disk. Such
a scheme is the Application's own concern; it overlays the base model rather than
replacing it, and it is private to the Application that registered it. Other
Applications never address it — inter-Application access always goes through
`box://name/…`, never through another Application's private scheme.

This keeps a clean separation:

- **stored, file-backed, user-overridable** → `profile://` / `box://` (Desktop-owned);
- **generated, handler-backed** → an Application's own registered scheme.

## Canonical identity

Each resource has exactly one canonical URL. Where a physical location is
reachable by more than one route (e.g. the storage path underlying a sandbox),
the resolver folds them to the single canonical form so that two addresses for
the same bytes do not become two independent identities.

## Future: multiuser

Each scheme's authority is a **single axis** — `box://` names an Application,
`profile://` names a user. When KittyTK becomes multi-user, `profile://user/…`
reaches another user's profile, brokered by the user-access layer.

The **cross-product** — a *named Application's* sandbox belonging to a *named
user* — is deliberately left open rather than answered by pathing through
`profile://user/‹app-folder›/…` (which would re-break the disjoint-namespace
rule across the user boundary). Two provisions keep it uncornered:

- `box://` is **implicitly current-user** for now (it always means this user's
  Applications); and
- RFC 3986's authority is `userinfo@host:port` — richer than one token — so the
  cross-product has structural room (`box://name@user/…` or similar) if and when
  it is needed, with no grammar change.

Build the two single-axis schemes; treat the Application×user composition as a
named later.
