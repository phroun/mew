# The bundle format

> **Status: built, and verified from this file.** Everything below is read by
> the loader today. The example is not a sketch: `display`'s tests read this
> document, load the bundle in it, and fail if it stops meaning what it says.
>
> `bundles.md` is the design conversation behind it and serval's
> `docs/sources.md` is what the sources it becomes then do.

A bundle is an authored **document**, held in an app's store, that a loader
turns into a data source. It is not a source itself and has no operations of its
own: it says what to assemble, and the loader assembles it.

## An example

```psl
(
  # -- what this bundle IS -------------------------------------------------
  _bundle: (
    key:     "objectLibrary",
    version: "2.1.0",
    author:  "Jeffrey R. Day",
    date:    "2026-09-18",

    includes: (
      # The TERSE form: the alias is the bundle's key, so one word says both
      # what to include and what to call it here.
      figaro:  ">= 0.1.0",          # the newest there is, and not below 0.1.0
      palette: "1.4.2, optional",   # that one exactly, and it may be absent
      grammar: ">= 2.0, < 3.0",     # the newest under 3.0
      icons:   "0000000000000000000000000000000000000000000000000000000000000000",

      # The LIST form says what the terse form cannot: a key that differs from
      # the alias, and which namespace is meant.
      drops: ( bundle: "drop-folder", want: ">= 0.1.0, optional" ),
      inbox: ( source: "mail.incoming" )
    )
  ),

  # -- what it changes about what it included ------------------------------
  _amendments: (
    figaro/1:      ("stands in for figaro's second record"),
    figaro/legacy: nil
  ),

  # -- its own records -----------------------------------------------------
  ("the first",  kind: widget),
  ("the second", kind: widget),
  notes: "a keyed record, addressed objectLibrary/notes"
)
```

## What is about the document, and what is in it

`_bundle` and `_amendments` are **about** the document. Everything else is a
record of it: the positional items keyed by their index, the keyed members by
their name. Adding or removing metadata cannot shift a record's index, because
PSL's two spaces were never one sequence.

There is **no `_hash`**. A bundle's hash is sha256 over its bytes, computed by
whoever holds them and kept in the store's index. A document asserting a hash
about itself would be a claim rather than a fact, and would have to cover
itself.

## Where it is stored

An app chooses the store key. The only rule is that it must **begin with the
bundle's key**, after the cache mark if there is one:

| filed as | |
|---|---|
| `objectLibrary` | the bare name |
| `objectLibrary-2.1.0` | a version after it |
| `objectLibrary-draft` | any suffix at all |
| `#objectLibrary` | and the same, cached |
| `scratch` | **not indexed**, whatever its contents say |

Being a bundle is two statements agreeing: the document says what it is, and the
app says it meant to file it as that. Without the rule, any document claiming a
high version joins the shelf its contents name, and a scratch copy can win a
resolution nothing about the app's own filing predicted.

Nothing parses a store key. This is a prefix test between two independently
stated names, not a derivation of one from the other.

## What an include names

Two namespaces, and an include says which it means.

| | names | found through |
|---|---|---|
| `bundle:` | a bundle's key | the store's bundle index |
| `source:` | a source registered by name | the live registry |

A **bundle** is a document, selected by version. A **source** is a live thing
somebody registered — an application's records, a CSV read at run time, anything
at all. It has no version to select between, because it is one object rather
than a shelf of them, so a `want` on one is refused rather than ignored. That is
what lets a bundle wrap and amend a source of *any* kind rather than only other
bundles.

The terse form is a bundle by its alias. Anything else takes the list form.

## What a version expression says

Comma-separated terms, in any order:

| | |
|---|---|
| `1.4.2` | a pin: exactly that version, for good |
| *64 hex characters* | a pin: exactly that content, for good |
| `>= 0.1.0` | the newest there is, and not below 0.1.0 |
| `< 3.0` | the newest under 3.0 |
| `optional` | its absence is survivable |

A bare version is a **pin** rather than a floor: an author who means "or newer"
has `>=` to say it with. Pinning and bounding at once is refused, as is naming
two versions.

**Selection is newest-wins, and the floor is not part of the question.** An
include takes the highest version under its key and fails if that is below its
floor. So `>= 0.1.0` and `>= 0.1.2` select the same thing and differ only in
what they will accept.

That is what decides **sharing**: a selector is the key and the *upper* bound,
so two includes with different floors get the same source object, and every
cache and invalidation over it is shared. Resolution happens once per selector
per load, so two includes cannot disagree about what "newest" was — only one of
them ever asked. Divergence is opt-in: it takes an upper bound or a pin to end
up with two of something.

## Optional, and what stops a load

An include is **mandatory** unless it says otherwise. One that cannot be met
stops the load; an optional one is absent and reported. `optional` is a term of
the expression, so it reads the same wherever an expression is written, and the
list form keeps a member of its own for a `source:`, which has no expression to
put it in:

```psl
palette: "1.4.2, optional"
drops:   ( bundle: "drop-folder", want: ">= 0.1.0, optional" )
inbox:   ( source: "mail.incoming", optional: true )
```

Everything short of an unmet mandatory include is a **report** rather than a
refusal: the load finishes and hands back what went wrong. A ring is refused and
named, one hop or several.

## What it becomes

| declares | becomes |
|---|---|
| `includes` | a `ComposedSource`, every record under its include's name |
| `_amendments` | an `AmendedSource` over the whole of that |
| its own records | records of that amended layer, under keys of their own |

So a layer stack is a composition with an amendment over it. **Nothing shadows
by position** — every included record keeps its include's name in front of its
own key, so `objectLibrary/figaro/0` and `objectLibrary/drops/0` are two records
and neither hides the other. What shadows is an amendment, naming one record.

A bundle that includes nothing is its records and no more: no composition to
merge, no amendment layer to carry.

## Addressing a record

The include's name, a slash, and the record's key — all the way down:

```
objectLibrary/figaro/3        the fourth ordered record of figaro
objectLibrary/notes           a keyed record of the bundle itself
objectLibrary/drops/tools/7   through two levels of include
```

The split takes the **first** slash, so each layer sheds one name and hands the
rest down untouched. Within a last segment, all digits means an index and
anything else means a key — which is why a bundle record key of nothing but
digits is forbidden.
