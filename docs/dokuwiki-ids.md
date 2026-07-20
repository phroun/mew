# DokuWiki-Compatible Reference Resolution — clean-room spec

Status: the page resolver is implemented in `internal/editor/wikiref.go`
(Layers 1–3 plus resolution-time file matching; media references, anchors, and
per-wiki config discovery remain open). This document specifies how mew parses,
resolves, and canonicalizes DokuWiki-style page and media references, so that mew
can *navigate real DokuWiki repositories* faithfully while layering its own
scheme/interwiki system on top.

## Provenance and licensing (read first)

DokuWiki is licensed **GPL-2.0-only**. mew is source-available/proprietary and
intends to keep the option of a future permissive (MIT/FSF-style) relicense open.
GPL code — or GPL data tables — must therefore **never enter this tree**.

This spec is a **clean-room, behavioral** description. It records *what* DokuWiki
does (functional facts — algorithms, resolution rules, ordering — which are not
copyrightable), in our own words. It deliberately contains **no** DokuWiki source
code, no verbatim regular expressions, and **not** DokuWiki's curated
special-character table. Where DokuWiki's implementation leans on a hand-curated
data table, mew instead derives equivalent behavior from the public Unicode
standard (Go's `unicode` package). Any implementation built from this document
must likewise be written from the described behavior, not transcribed from
DokuWiki's code.

This mirrors the discipline already used for the syntax grammars (see
`internal/editor/syntax/LICENSE`: original works, no JOE grammar content).

## The core idea: three layers, resolved outermost-first

A reference a user follows (or an author types in `[[...]]`) is resolved in three
distinct passes. Keeping them separate is what prevents mew's scheme layer from
corrupting DokuWiki fidelity, and prevents DokuWiki's context-dependent rules from
leaking into pure canonicalization.

1. **mew scheme / interwiki layer** (mew's own; runs first, on the raw ref).
   Decides whether the reference leaves the current wiki system at all.
2. **DokuWiki reference resolution** (context-dependent). Turns a within-wiki
   reference plus the current page's namespace into an *absolute* id.
3. **DokuWiki id canonicalization** (`cleanID`; context-free). Normalizes an
   absolute id's character set and separators into its canonical form.

Implement these as three functions. Do not merge them.

---

## Layer 1 — mew scheme / interwiki gate (mew's own layer)

This layer is mew's, not DokuWiki's. It runs on the raw reference before any
DokuWiki parsing.

- A reference invokes a **scheme** only when it begins with a **registered**
  scheme name immediately followed by a slash form:
  - `scheme:/…` — one slash — rooted **within** that scheme, no authority.
  - `scheme://authority/…` — two slashes — an **authority** is present, selecting
    a specific instance under the scheme. "Authority" is scheme-defined: it may be
    a hostname, but it may equally be an application id, a device, a session, or
    any other instance selector the scheme defines.
- A reference invokes an **interwiki** target only when it matches a **registered**
  interwiki shortcut in the form `shortcut>rest` (DokuWiki's interwiki syntax).
- **Everything else is a DokuWiki reference**, handed to Layer 2 untouched. In
  particular, a bare single-colon reference such as `wiki:syntax` is a DokuWiki
  namespace path — never a scheme — because `wiki` is not gated by a slash form.
  This is the rule that lets schemes and DokuWiki namespaces coexist without ever
  mis-eating real wiki content.

Both the scheme set and the interwiki set are **explicit registries**. Nothing is
recognized by grammar guesswork; if it is not registered, it is DokuWiki content.

Schemes and interwiki targets each resolve within their own system; a **relative**
DokuWiki reference (Layer 2) never crosses a scheme or interwiki boundary. Cross-
system links must be absolute.

The scheme registry connects to the link-target dispatcher described in
`docs/hyperlink-ideas.md` (the `help:` / `goto:` / `url:` / `cmd:` vocabulary and
its security rule: content-derived links may carry only navigation schemes).

---

## Layer 2 — DokuWiki reference resolution (context-dependent)

Input: a within-wiki reference string, plus the **context namespace** (the
namespace of the page currently being viewed). Output: an *absolute* id, ready for
Layer 3.

Per-wiki configuration consulted (see "Per-wiki config" below): `useslash`.

Steps, in order:

1. **Split off the fragment.** If the reference contains `#`, everything from the
   first `#` onward is an in-page anchor (a heading id); set it aside and resolve
   only the id portion. The anchor rides along to the final result.

2. **Optional slash-as-separator.** *Only if the wiki has `useslash` enabled*,
   treat `/` as equivalent to the namespace separator `:` for the rest of this
   pass. When `useslash` is disabled (the DokuWiki default), a `/` is an ordinary
   character in a page name, not a separator — do not split on it.

3. **Normalize a leading dot-run.** A reference may begin with a run of `.` /
   `..` relative markers, and the run may be written **glued** directly to the
   following name (`..example`) or **separated** by a colon (`..:example`). Treat a
   glued leading dot-run as though a separator sat between the dot-run and the name
   that follows — i.e. `..example` is the markers `..` then the name `example`,
   never a namespace literally named `..example`. The dot-run may chain
   (`.:..:example` = current, then up one).

4. **Decide relative vs. absolute** (the rule most implementations get wrong):
   - A reference that begins with a **leading dot** (`.` or `..`, after step 3) is
     **relative** to the context namespace: prepend the context namespace, then
     apply the walk in step 5.
   - A reference that contains **no separator at all** (a bare name) is **relative**
     to the context namespace: prepend the context namespace.
   - A reference that **contains a separator** but does **not** begin with a dot is
     **absolute from the wiki root** — it is left as-is (not prefixed). This
     includes a leading-colon reference like `:foo:bar`, which is absolute (the
     leading colon is dropped during canonicalization).

   In short: **bare name ⇒ relative; anything with a colon ⇒ absolute; leading dot
   ⇒ explicitly relative.** A namespaced link is *not* relative to the current
   namespace unless it starts with a dot.

5. **Walk `.` and `..`.** Split the (now possibly namespace-prefixed) id on the
   separator and process segments left to right into a result stack:
   - `..` pops the last segment off the result stack (climb to parent namespace);
     popping at the root is a no-op.
   - `.` is skipped (stay in the current namespace).
   - any other segment is pushed.

   Re-join the stack with `:`.

6. **Namespace-target start page.** A reference that designates a *namespace*
   rather than a page (conventionally one ending at a namespace boundary, e.g. a
   trailing separator) resolves to that namespace's **start page** (DokuWiki's
   default start-page name is `start`, itself configurable). Record this as a
   resolution-time rule, applied after the walk.

7. **Auto-plural (optional DokuWiki behavior).** DokuWiki can, when configured,
   resolve a missing singular/plural page to its counterpart. Treat this as an
   optional resolution-time fallback, not part of canonical id formation.

8. **Canonicalize.** Pass the resulting absolute id to Layer 3.

### Worked examples

Context namespace = `a:b` (viewing page `a:b:c`):

| reference        | resolves to   | why                                   |
|------------------|---------------|---------------------------------------|
| `foo`            | `a:b:foo`     | bare name ⇒ relative                  |
| `foo:bar`        | `foo:bar`     | contains a colon ⇒ absolute from root |
| `:foo:bar`       | `foo:bar`     | leading colon ⇒ absolute (colon dropped) |
| `.foo`           | `a:b:foo`     | leading dot ⇒ current namespace       |
| `.foo:bar`       | `a:b:foo:bar` | leading dot ⇒ relative                |
| `..foo`          | `a:foo`       | `..` climbs out of `b`                |
| `..:foo`         | `a:foo`       | same, separated form                  |
| `.:..:example`   | `a:example`   | current, then up one, then page       |
| `..:..:x`        | `x`           | climb twice to root                   |

---

## Layer 3 — id canonicalization (`cleanID`; context-free)

Input: any id. Output: its canonical form. No namespace context, no relative
logic — those belong to Layer 2. This layer only normalizes characters and
separators.

Per-wiki configuration consulted: `useslash`, `deaccent`, `sepchar` (see below).

Steps, in order:

1. **Trim** surrounding whitespace.
2. **Lowercase.** DokuWiki page ids are case-insensitive; canonical ids are
   lowercase. (See "Case sensitivity" for the filesystem tension this creates.)
3. **Normalize alternate separators.** A semicolon `;` is *always* treated as a
   namespace separator (mapped to `:`). A slash `/` is mapped to `:` when
   `useslash` is enabled, otherwise mapped to the **sepchar** (i.e. it becomes part
   of a page *name*, not a separator).
4. **Optional transliteration.** When the wiki is configured to fold accents
   (`deaccent`), replace accented letters with their unaccented ASCII equivalents;
   a stronger setting additionally romanizes non-Latin scripts. mew derives these
   from Unicode decomposition / a transliteration table of our own, **not** from
   DokuWiki's tables.
5. **Reduce to the legal character set.** Replace every character that is not
   *legal in an id* with the sepchar (see "Character set" below for mew's
   Unicode-category rule). Control characters are removed/replaced likewise.
6. **Collapse runs.** Collapse repeated sepchars to one, and repeated colons to
   one.
7. **Trim edge punctuation.** Strip leading/trailing separator and boundary
   punctuation (`:`, `.`, `-`, and the sepchar) from the ends of the id. This is
   what drops the leading colon of an absolute `:foo:bar`.
8. **Tidy punctuation around separators.** Collapse a separator that is adjacent
   to boundary punctuation down to a single clean separator, so a namespace break
   is never padded by stray dots/hyphens/underscores.

The anchor set aside in Layer 2 is re-attached to the canonical id (anchors follow
the page's heading-id rules, which are similar but resolved against headings, not
the page tree).

### Character set (mew's Unicode-category rule)

DokuWiki defines "special character" via a large, hand-curated codepoint table.
That table is GPL and is **not** reproduced here. mew instead classifies
characters from Unicode general categories, which reproduces DokuWiki's behavior
for every common case:

- **Keep** Unicode letters (`L*`) and decimal digits (`Nd`).
- **Keep** the id punctuation `_` (the default sepchar), `.`, and `-`.
- **Keep** `:` as the namespace separator.
- **Everything else** — spaces, symbols, other punctuation, marks, controls — is
  replaced by the sepchar. (So `hello world` → `hello_world`, matching DokuWiki.)

Where this classification diverges from DokuWiki's curated table on exotic
codepoints, the divergence is immaterial in practice because of the resolution-
time fallback below. If byte-exact parity ever becomes necessary, it must be
achieved by an independently-authored table (e.g. generated from Unicode data),
never by importing DokuWiki's.

---

## Resolution-time fidelity (why byte-exact `cleanID` is unnecessary)

mew's use case is *browsing real files*, so the resolver does not need to
reproduce DokuWiki's id→filename mapping perfectly. After computing a canonical
id, the resolver matches it against the **actual files present** in the wiki tree:

- case-fold the comparison (a canonical lowercase id matches `MyPage.txt` on a
  case-sensitive filesystem);
- tolerate the sepchar/space and minor punctuation ambiguity by comparing cleaned
  forms of the on-disk names;
- when a namespace is targeted, look for its start page file.

This "does a file by that name exist here" step absorbs any small divergence
between mew's Unicode-category cleaning and DokuWiki's curated table, so exotic
edge cases degrade to a normal not-found rather than a silent mis-link.

---

## Per-wiki configuration

DokuWiki's cleaning/resolution is parameterized. When mew opens a DokuWiki tree it
should honor that wiki's settings where discoverable, else fall back to DokuWiki's
documented defaults:

| setting    | meaning                                             | default |
|------------|-----------------------------------------------------|---------|
| `useslash` | treat `/` as a namespace separator (≡ `:`)          | off     |
| `deaccent` | fold accents (and, stronger, romanize)              | on      |
| `sepchar`  | the character non-id characters collapse to         | `_`     |
| start page | namespace default page name                         | `start` |

Discovery of a wiki's config from its tree is a resolution concern to be specified
when the resolver is built; the safe default is the table above.

## Case sensitivity

Canonical ids are lowercase, but the files backing a wiki may live on a
case-sensitive filesystem with mixed-case names. The resolver therefore compares
case-insensitively and treats the on-disk name as authoritative for display,
while the canonical (lowercase) id is authoritative for identity/linking. This is
handled entirely in the resolution-time matching step; it does not change Layer 3.

## Open items to pin when implementing

- A leading `~` sigil appears in DokuWiki's current resolver alongside `.`; its
  exact meaning (current-page-name-as-namespace for subpages, most likely) should
  be confirmed from documentation before relying on it.
- Media references vs. page references differ slightly (media have no start-page /
  auto-plural behavior); specify the media resolver as a sibling of the page
  resolver.
- Heading-anchor id formation (the part after `#`) follows heading-specific rules
  worth their own short spec when in-page anchors are implemented.

## Relationship to mew's link system

This spec supplies Layer 2/3 for the **syntax-derived** link provider in
`docs/hyperlink-ideas.md`: for a DokuWiki (or markdown) buffer, the grammar marks
link spans, and the reference text inside each span is resolved by the three
layers above into a concrete target the dispatcher can follow. The scheme registry
in Layer 1 is the same registry the target dispatcher consults.
