# Generating the wiki's property and event tables

The wiki's property and event tables are generated from the running
vocabulary. The prose around them is written by hand and is never touched.

`cmd/kittytk-wikidoc` does the generating. Anyone with a checkout can run it.

---

## Cloning the wiki

A GitHub wiki is a **separate git repository** from the code. It is not a
branch, and it is not in the main clone — which is why you will not find it
by looking. Its URL is the repository's, with `.wiki` before `.git`:

```sh
git clone https://github.com/phroun/kittytk.wiki.git
```

That gives you a `kittytk.wiki/` directory holding one Markdown file per
page. `Home.md` is the landing page, `_Sidebar.md` and `_Footer.md` are
rendered on every page, and a page named `Foo Bar` lives in `Foo-Bar.md`.

Edit, commit and push as with any repository:

```sh
cd kittytk.wiki
git add -A && git commit -m "..." && git push
```

There is one branch and no pull requests — a push publishes immediately.
The wiki has no CI, so nothing checks it but you.

Two things worth knowing:

- **The wiki must have at least one page created through the web UI before
  it can be cloned.** A wiki with no pages has no repository behind it yet,
  and the clone fails with a repository-not-found error that looks like a
  permissions problem and is not one.
- **Anonymous clone works for a public wiki; pushing needs your credentials**
  — the same ones you push the code with. If you use SSH for the code, use
  `git@github.com:phroun/kittytk.wiki.git`.

## Where to keep the clone

Anywhere — the tool takes a path, so this is only your convenience. Two
arrangements are common:

```sh
git clone https://github.com/phroun/kittytk.wiki.git   # beside the code
git clone https://github.com/phroun/kittytk.wiki.git wiki   # inside it
```

A clone inside the checkout needs ignoring. Prefer `.git/info/exclude` over
`.gitignore` — where you keep it is your layout, not the project's:

```sh
echo "/wiki/" >> .git/info/exclude
```

It is safe there. `git clean -xdf` skips an untracked directory that is
itself a repository, so the sync procedure in
[fork-sync-policy.md](fork-sync-policy.md) will not touch it; only
`git clean -xdff`, forcing twice, removes it, and that would take unpushed
wiki commits with it. The Go tool never looks inside either, since the
directory holds no `.go` files. The sync procedure's recursive diff excludes
`wiki` for the same reason this section exists.

## Running it

```sh
go run ./cmd/kittytk-wikidoc -wiki ../kittytk.wiki   # or -wiki ./wiki
```

It rewrites the generated spans, prints which pages changed, and stops.
Review the diff in the wiki checkout and commit it yourself — the tool
never commits and never pushes.

| Flag | Does |
|---|---|
| `-wiki PATH` | The wiki checkout. Required. |
| `-check` | Report stale pages and exit non-zero. Writes nothing. |
| `-list` | Coverage both ways: types with no page, pages with no blocks. |
| `-examples` | Execute the wiki's wire examples. Writes nothing. |
| `-endpoint ADDR` | Describe a **running** display service instead of this binary's own registry. |

`-endpoint` documents whichever build is actually serving, which is what you
want when the wiki should match a deployed host rather than your working
tree. Everything else is identical: a described stream decodes back into the
same structure the local registry produces, so the two paths cannot drift.

## Marking a page

A generated span is bounded by HTML comments, which render as nothing:

```markdown
## Properties

<!-- ktkdoc:props textinput -->
<!-- /ktkdoc -->

Prose here is yours and survives every run.
```

Leave the body empty when you add a block; the first run fills it in.

| Block | Renders |
|---|---|
| `ktkdoc:props <type>` | The type's own properties. |
| `ktkdoc:events <type>` | The events the type emits, and their fields. |
| `ktkdoc:common` | The properties every non-virtual type accepts. |
| `ktkdoc:types` | The index of registered types. |

Everything outside a block is left byte for byte as it was. Running twice
in a row changes nothing the second time, so re-generating costs the wiki's
history nothing when the vocabulary has not moved.

## Where the words come from

The **Meaning** column is the `Tip(...)` on the property's registration, and
an event's description and field docs are its `EventDesc`. There is no
second place to write them.

So improving the wiki's wording means improving the registration:

```go
"placeholder": stringProp("placeholder", (*TextInput).SetPlaceholder).
        Tip("Placeholder text shown when empty."),
```

which also improves what `conn.Describe()` tells every client, in every
language, and anything built on it later. One source, several readers. Keep
tips to a sentence — they are read in a table cell and in a tooltip.

## Failure modes, and why they are failures

- **A block naming an unregistered type is an error**, not an empty table.
  An empty table is what a renamed type would silently leave behind.
- **An unbalanced or nested marker is an error with a line number.** Guessing
  where a generated span ends is how a tool eats the prose around it.
- **Nothing is written when any page fails**, so a bad run leaves the wiki
  exactly as it was.

## Keeping it honest

`-check` exits non-zero when the wiki disagrees with the vocabulary, so it
can run wherever you want the reminder:

```sh
go run ./cmd/kittytk-wikidoc -wiki ../kittytk.wiki -check
```

`-list` answers the other question — what is not documented at all. A type
with no page is the gap worth closing next.

## Checking the examples

`-examples` runs every wire script in the wiki against the registry:

```sh
go run ./cmd/kittytk-wikidoc -wiki ../kittytk.wiki -examples
```

**Parsing an example proves nothing.** The protocol accepts any
syntactically valid property name and rejects only one the registry has
never heard of, so an invented property parses cleanly and fails when it
runs. A wrong child under a parent behaves the same way. Examples have to
be *executed*, and this is the difference between a page that looks right
and one that is.

Each page gets a session of its own, in page order, so an example that
continues the one above it — `set tv.b children={…}` after the build that
made `tv` — runs against what that build produced.

Blocks that are not wire scripts are left alone: client code, shell
commands, a fragment with an `…` in it, and an example shown beside the
result it produces. A wire script that genuinely cannot run as written —
one quoting an object ID an application would have read out of a reply —
is exempted where it sits:

```markdown
<!-- ktkdoc:noexec -->
```

on the line before the fence. The marker renders as nothing, applies to
that one block, and keeps the exemption visible to whoever edits the page
next. Use it sparingly: every block it covers is a block nothing checks.
