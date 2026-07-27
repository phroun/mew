# UI font selection — the `ui-text` / `ui-term` alias tree

Status: implemented. The tree is built in `kittytk/text/fontdb.go`
(`installUIAliases`, `scriptContext`, `fallbackMap.scriptTarget`); the config
side is `[window] ui_*` in `internal/config/config.go`, mirrored for the
standalone KittyTK host in `kittytk/hostcfg/hostcfg.go`.

This document is the answer to "how do I switch the terminal face between serif
and sans", and to the larger question the same machinery answers: *which* face
paints a given glyph, and where to intervene.

## The shape of it

Every UI face is reached through a **redefinable alias**, never a font name in
the code. The aliases form a three-level tree:

```
ui-{text,term}-{western,hebrew,arabic,cjk}-{sans,serif}
    root            script class              style
```

with a cascading default at each level, so you override at exactly the level you
care about and everything below follows:

| Name | Resolves to | Override key |
|---|---|---|
| `ui-term` | `ui-term-sans` | `ui_term` |
| `ui-term-serif` | `ui-term-western-serif` | `ui_term_serif` |
| `ui-term-hebrew` | `ui-term-hebrew-serif` | `ui_term_hebrew` |
| `ui-term-hebrew-sans` | Noto Sans Hebrew | `ui_term_hebrew_sans` |

`ui-text` is KittyTK's proportional face (menus, dialogs, chrome). `ui-term` is
the terminal grid — purfecterm's font slot 0, which is what mew's own text is
painted with.

## Two different questions

Switching serif/sans means one of two things, and they use different mechanisms.

### 1. Which face gets used — a NAME, not a config key

A renderer or trinket selects a face by asking for `ui-term`, `ui-term-sans`, or
`ui-term-serif`. That is a deliberate serif/sans/default **tristate that needs no
font knowledge**: code names the intent, the tree resolves the family.

To flip the default for the whole terminal root, re-point the root at the style
you want:

```ini
[window]
ui_term = ui-term-serif
```

An alias may target another alias; resolution follows the chain
(`ui-term` → `ui-term-serif` → `ui-term-western-serif` → `Noto Serif`).

### 2. What serif/sans MEAN — the per-leaf override

To keep the tristate but change the families behind it:

```ini
[window]
ui_term_western_sans  = JetBrains Mono
ui_term_western_serif = Iosevka Etoile
```

## The `[window] ui_*` keys

Any key of the form `ui_<...>` re-points the alias `ui-<...>` — underscores
become hyphens, and that is the whole rule. There is no fixed list of keys; the
tree defines what is meaningful.

The value is a **comma-separated fallback list**, tried in order:

```ini
[window]
ui_term = "Berkeley Mono, JetBrains Mono, Noto Sans Mono"
```

The value trichotomy applies as everywhere else in the config: `default` (or
its aliases `system` / `system default`), `inherit`, or a blank value clears the
override and restores the built-in.

Related keys in the same section:

- `fonts_path` — extra font search directories (comma-separated; relative paths
  resolve against the config file's own directory).
- `font_size` — the UI point size; see also the live zoom (Command/Meta with
  `+` / `-` / `0`) in the graphical host.

## Changing a face at runtime

```
set_font "<alias>", "<font>" [, "<fallback>"...]
```

Same alias names, same fallback-list semantics — `set_font "ui-term-hebrew",
"SBL Hebrew", "Noto Serif Hebrew"`. Bumping a face this way invalidates the
glyph caches (the engine's epoch changes), so it takes effect on the next frame.

## Three things that surprise people

**Per-script defaults are not all sans.** Each script class carries its own
default style:

| Script | Default style |
|---|---|
| western | sans |
| cjk | sans |
| hebrew | **serif** |
| arabic | **serif** |

So `ui-term-hebrew` is already Noto Serif Hebrew without asking. This is
deliberate: for Hebrew and Arabic the serif/traditional face is the neutral
reading choice, not the decorative one.

**`ui-term-western-serif` is not monospaced.** No libre "Noto Serif Mono"
exists, so that leaf maps to the *proportional* Noto Serif. The terminal grid
still places every glyph in a fixed cell, so it reads as a serif terminal face —
but it is not a true monospace design, and glyphs will sit in their cells more
loosely than `ui-term-western-sans` (Noto Sans Mono), which is a real mono.

**Arabic follows its own style analogy.** `sans` maps to Noto **Kufi** (geometric)
and `serif` to Noto **Naskh** (traditional). The Latin sans/serif distinction has
no direct Arabic equivalent; this is the closest honest mapping, and it means
switching a document to sans changes Arabic to a markedly different construction,
not merely a different finish.

## Per-glyph fallback keeps your choice

When the primary face has no glyph for a character, the fallback does **not** jump
to some global default. It follows the same root and style as the request: a
`ui-text-serif` primary pulls `ui-text-hebrew-serif` for an uncovered Hebrew
glyph; a `ui-term` primary pulls the `ui-term-*` variants. A request naming a
concrete family (not a `ui-*` alias) has no context to follow and defaults to the
`ui-term` root.

The practical consequence: mixed-script text stays visually coherent — you do not
get a serif Latin line with a sans Hebrew word dropped into it — and overriding
one leaf does not silently leak into the others.
