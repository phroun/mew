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

The same rewrite applies to a **value** that names another UI alias, so the
underscored spelling works on both sides of the equals sign:

```ini
[window]
ui_term_hebrew = ui_term_hebrew_sans   ; same as ui-term-hebrew-sans
```

Only entries naming the `ui` tree are rewritten — a real font family containing
underscores (`My_Custom_Mono`) survives verbatim.

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

## Per-face metric corrections

Baseline placement is automatic: every terminal face is shifted so its baseline
lands where the primary face puts its own (see `baselineShiftPx`). A face whose
own metrics misplace it can be corrected in `[fonts]`, keyed by the FAMILY on
the left of the equals sign:

```ini
[fonts]
Noto Kufi Arabic  = fonts/NotoKufiArabic.ttf (baseline: -6)  ; path + correction
Noto Serif Hebrew = (baseline: +1, size: 1.1)                ; correction alone
```

Two keys are recognised, alone or together:

| key | means |
|---|---|
| `baseline: -6` | move the face's baseline, in cell units (1/16 row), positive **down** |
| `size: 1.1` | render the face at 110% of its natural size (`110%` spells the same thing) |

`size` is for balancing OPTICAL sizes: a face that reads small or large beside
the base `ui-term` / `ui-text` faces can be brought into line without touching
the base face. On the terminal grid the glyph fills more or less of its fixed
cell; on the proportional path the whole face renders larger or smaller, so its
advances change with it. It reaches a face chosen by per-glyph fallback, which
is the case it exists for — the shaping size is computed once from the
requested font, so scaling only that would leave every script face untouched.

The path is optional, so an embedded face that needs no registration can still
be nudged. Three things to know:

- Keyed by **face, not alias** — one entry corrects the family through every
  route that reaches it: any alias in the `ui` tree, per-glyph script fallback,
  or a direct name. An alias-keyed override would only catch one route.
- **Cell units** (1/16 of a row), positive **down**. Not device pixels, so a
  correction survives the live font zoom.
- A **delta** on top of the automatic alignment, not a replacement — if the
  automatic pass already moved a face +4, `baseline: -6` nets to -2.

The family is matched canonically, so `Noto Kufi Arabic`, `noto-kufi-arabic`
and `NotoKufiArabic` are the same face.

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
