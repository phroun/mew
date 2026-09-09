# Data sources and bundles

> **Status: conversation only, not a specification yet.** This is a design
> discussion written down so it is not lost, not a contract. Nothing here is
> implemented, nothing here is settled enough to build against, and several
> points at the end are still open questions. Where it states a decision, that
> is a decision reached in conversation and nothing more.

A **data source** is a named collection an Application draws records from: an
object library, a message catalogue, a set of user-provided plug-in objects.
The question this explores is how a source can be assembled from parts that
come from different places, some of them fixed and some of them changing, in a
way that lets the fixed parts be checked, cached and shared without the
Application having to know where any of it physically lives.

## A bundle is a frozen data source

The central move is that there are not two kinds of object:

> **A bundle is a frozen data source. A source is a bundle that is still
> allowed to change.**

Freezing yields a hash, because a hash can only mean something about content
that has stopped moving. Hashing something mutable is theatre: the value
changes constantly and tells you nothing.

That gives the two identity rules different jobs rather than putting them in
tension. A **source** is identified by its name, because its content moves. A
**bundle** is identified by its hash, because its content cannot.

One object type, one naming scheme, one set of operations. "Bundles containing
bundles" and "a source referencing bundles" stop being two mechanisms and
become one, because they are the same kind of thing referring to the same kind
of thing.

## Layering

A source is a stack: an ordered list of immutable bundles, with one writable
layer above them. The union-mount shape — fixed lower layers, one changing
upper layer.

```
objectLibrary
  dynamic          records provided at run time; the only layer that changes
  userObjects      a bundle: objects the user dropped in
  appObjects       a bundle: the Application's own static objects
```

The records in the lower layers behave as though they are in the source,
because they are. Records added at run time coexist with them in the dynamic
layer.

Precedence is **ordered**, not a set: two bundles may hold a record under the
same key, and which one wins has to be a stated position in the stack rather
than an accident of enumeration.

One property falls out that is worth having on purpose: the resolved view of a
set of immutable bundles is *itself* immutable. The merged index can be
computed once and keyed on the combination of bundle hashes, so only the
dynamic layer ever invalidates it. Merging on every enumeration is the obvious
cost of an overlay, and this shape does not pay it.

## Bundles refer to bundles

A bundle names the bundles it includes rather than copying them in. That makes
the graph a Merkle DAG, and the integrity property composes for free: a
parent's hash covers its children's hashes, so verifying a root verifies
everything beneath it. Two sources naming the same bundle store it once.

## A key *and* a hash

Every bundle carries both a **key** (a name) and a **hash**. Not the hash
alone.

The name is not a convenience. An Application can decide it is only concerned
with things bearing its own names, so data arriving from elsewhere is labelled
rather than mysterious. And signing needs it: a signature over a bare hash
proves only that some bytes exist, because a hash is self-attesting and says
nothing about what the bytes *claim to be*. What gets signed is a statement —
this key, at this version, is this content, attested by this party — which is
why package systems sign name and version and hash together. Signing is not
part of this design, but the name is what would make it possible later.

Because a bundle has a name, a bundle can be loaded directly as a data source
and read from. That is the same statement as the collapse above, arrived at
from the other end.

## Keeping the filesystem off the wire

The Application is frequently **not on the same machine** as the data. That is
the premise of the scheme and filesystem work (see `scheme-architecture.md`),
not an edge case, so an Application cannot be asked to read a folder and push a
bundle up.

Named bundles resolve this without the wire ever learning what a folder is. The
Application says "`objectLibrary` includes the bundle named `userObjects`". The
Desktop knows that name is backed by a drop folder, enumerates it, hashes it,
and answers. The Application knows a name and receives a hash; it does not know
it is a folder, where it is, or which machine it is on.

This is `scheme-architecture.md`'s own principle — *the Desktop decides where
that physically lives* — extended from single resources to collections. It also
means the wire contract can be settled before the filesystem specification is,
because filling in the directory API behind it later does not change the
protocol.

The interior of the design is path-free for the same reason: the store for
immutable bundles is a **blob store**, not a filesystem. Put bytes and get a
hash, get bytes for a hash, ask whether a hash is held, evict. No paths,
directories, listing, rename or metadata.

## Caching and trust are the same boundary

A source persists beyond a connection, so the Desktop has to recognise an
Application as the same one it saw before. That identity already exists:
**`(host identity, app name)`**, which is computed, persisted and trusted
before a source is ever summoned. An Application cannot change its name without
the user's approval, because that would be a hole in the trust mechanism.

So the cache boundary and the trust boundary coincide, which is a property
worth keeping rather than an accident. A user's approval decision governs an
Application's cached sources by construction: revoke an app and its data goes
with it, with no separate lifetime to reason about, and eviction inherits a
policy hook that already has a user interface. A name collision cannot happen
either — two Applications colliding would need the same host identity *and* the
same approved name, which is one Application.

Names are scoped per Application. Applications are independent and do not
interact, so there is no namespace governance, no reserved prefixes and no
cross-application visibility to design.

## The shape on the wire

A bundle is a PSL list. The presence of a `_bundle` key is what marks it as
one.

```
(
  _bundle: (
    key: "figaro",
    author: "Jeffrey R. Day",
    date: "2026-09-08",
    includes: (
      subBundle: "d06f00d"
      another: ">= 1.0"
    )
  ),
  _hash: "deadbeef",
  _amendments: (
    subBundle/1: ("replacement_for_record_1"),
    subBundle/2: nil            # a deleted record
  ),
  ("ordered_record1", a: "one", b: 12),
  ("ordered_record2", a: "two", b: 14.5),
  key: ("keyed_record", a: "three", b: 2)
)
```

- `_bundle` carries the identity and the includes. An include is an alias
  bound either to a literal hash or to a version expression.
- `_hash` covers the bundle's contents, its includes, its amendments and its
  records.
- `_amendments` replaces or deletes records inherited from an include, which
  is how a source built on someone else's bundle can differ from it without
  copying it.
- The remaining members are the records themselves.

A record's id resolves *through* the bundle it is in.

## Two spaces, not one sequence

A PSL list holds two independent collections: an **ordered sequence** made of
its positional items, and a **keyed map** that sits outside that sequence
entirely.

```
(a: "apple", b: "banana", "cherry")
```

The sequence is `["cherry"]` and the map is `{a, b}`. Where a keyed member
appears in the text is cosmetic — the serializer writes named members first and
items after, and it is not reordering anything, because the two were never one
sequence.

PawScript keeps them apart with two operators, **space for position and dot for
key**, and its behaviour settles the semantics:

| | | |
|---|---|---|
| `~fruits 0` | `apple` | indices are **0-based** |
| `~mixed 0` | `cherry` | with `a:` and `b:` also present: keyed members **consume no indices** |
| `~mixed.a` | `apple` | dot is the key accessor |
| `~numkey 0` | `first-item` | with a key literally named `0` present: a numeric key does **not** shadow index 0 |

This matters for the bundle format because `_bundle`, `_hash` and
`_amendments` live in the keyed space. Adding or removing metadata therefore
**cannot shift a record's index**: a bundle's ordered records are numbered by
the records alone.

## Addressing a record

A record address is the bundle alias, then a separator, then either an index or
a key.

**The separator is a forward slash.** It parses bare, needs no quoting, and
composes:

```
objectLibrary/figaro/3          the fourth ordered record of figaro
objectLibrary/figaro/someKey    a keyed record
another/subBundle/2             through two levels of include
```

Slash was chosen over the two alternatives on evidence. Underscore is a legal
word character, so the split point is ambiguous — include `sub` with record key
`bundle_x` and include `sub_bundle` with record key `x` produce the same
string. Dot does not survive as a key at all: `(subBundle.1: "x")` comes back
as key `subBundle` with the `.1` swallowed. Slash survives quoted or bare, and
nests, which underscore could not.

Because the address form is one grammar all the way down, it reads the same way
as `box:///…` and `profile:///…` already do, so the system speaks one idiom for
addressing rather than two.

**Numeric keys are forbidden as bundle record keys.** With that ban, the last
segment parses with one rule: all ASCII digits means an index, anything else
means a key. Forbidding numeric keys outright also avoids `01` and `1` becoming
two spellings of one index, and it is a single validation at bundle load. Since
the separator is a slash, no restriction on underscores in aliases or keys is
needed.

**And a slash is forbidden inside a key**, which the argument above implies and
this states. Slash was chosen over underscore precisely because underscore's
split point is ambiguous; a slash sitting inside a key reintroduces exactly that
ambiguity, so reserving the separator is the whole of what made it the right
one.

That rule reaches further than bundles, because **a store key IS the bundle's
key** — the name an app writes an item under is the name that item carries when
it becomes a bundle. One namespace, not two kept in step. So the store enforces
it at the door: no slash, nothing that is only digits, and nothing unprintable
(its index is a line per item, and a key with a newline in it writes a line that
reads back as a different item).

A store is useful for more than bundles and will be used for more. But it is
**not a filesystem** — a real one is coming, separately — and it is nearer to
cookies or a browser's local storage: a flat set of names, each holding one
thing. Allowing paths in keys would invite an app to treat it as the filesystem
it is not, which is the second reason the slash is refused rather than merely
discouraged.

## Reaching a bundle from Go

`pawscript.PSLNode` presents both collections of a PSL list at once, which is
what reading a bundle needs:

```go
n, err := pawscript.ParsePSLNode(text)
n.Get("_bundle")    // the keyed metadata
n.Len(), n.Item(i)  // the ordered records
n.Child(i)          // an ordered record as a node in its own right
n.Map()             // the keyed members as a PSLMap
```

`SerializePSLNode` goes back the other way, so a bundle survives a round trip
with its records in place.

`ParsePSL` is the wrapper to avoid here: it returns a `PSLMap`, which is a Go
`map[string]interface{}` and so holds keyed members only. A bundle always has
`_bundle` and `_hash`, and read through that call every ordered record in it
would be dropped — silently, with no error.

One serializer behaviour to know: **keyed members are sorted on emit**, so
declaration order among keyed records is not something to rely on. Ordered
records keep their order.

## Open questions

- **Version expressions in includes.** The sketch binds one include to a
  literal hash and another to `">= 1.0"`. How a range resolves to a hash, who
  resolves it, and what happens when it cannot, are undiscussed.
- **Are records typed or opaque?** Whether a source knows anything about the
  shape of what it holds.
- **The missing-record event.** What happens when a consumer asks for
  something the source does not have, and how that reaches whoever could
  supply it.
- **Parts as the unit of versioning.** Whether a version is a property of a
  bundle, of a source, or of something smaller.
- **Whether sources carry bulk.** Whether this mechanism is meant for records
  measured in bytes or in megabytes, which decides much about the blob store.
