# Data sources and bundles

> **Status: the design conversation, with the reading half built.** This is
> where the reasoning lives, not the contract. Reading records out of a
> document, composing several sources under names of their own, amending one
> with replacements and deletions, caching the result and invalidating it on
> notice are all built, and `psl-as-a-data-source.md` is what they do. So is the
> loader: a store notices a `_bundle` on the way past and hashes it, includes
> resolve by version or by hash, and what a bundle declares becomes a source.
> `bundle-format.md` is what that looks like and what it becomes, and its
> example is a fixture rather than a sketch.
>
> What is still conversation is the blob store, name resolution beyond the app's
> own store, and the live binding that would tell a source its newest had moved.
> The sections below say which of them they are. Where this states a decision,
> that is a decision reached in conversation and nothing more.

A **data source** is a named collection an Application draws records from: an
object library, a message catalogue, a set of user-provided plug-in objects.
The question this explores is how a source can be assembled from parts that
come from different places, some of them fixed and some of them changing, in a
way that lets the fixed parts be checked, cached and shared without the
Application having to know where any of it physically lives.

## What a bundle is

An authored **document**, held in an app's store, that a loader turns into a
data source. It is not a source itself and has no operations of its own: it
states what to assemble — includes, amendments, records of its own — and the
loader assembles it. `bundle-format.md` is what one looks like.

What makes a document a bundle is a `_bundle` member, and what `_bundle` carries
first is a **key** and a **version**. That pair is the name others reach it by:
an include says a key and what versions it will take, and the loader finds the
item. Two versions of one key are two bundles and can be held at once, which is
why the key alone cannot be what an item is stored under. The store key is the
app's to choose and need only **begin** with the bundle's key, so being a bundle
is two statements agreeing — the document says what it is, and the app says it
meant to file it as that.

Having a name is what lets a bundle be a thing an include can name at all. A
hash alone would be reachable but not askable for: you cannot request the newest
of something you can only name by its content.

## Layering

A bundle declares includes and amendments, and the loader assembles them: a
composition of the includes, with one amendment layer over the whole of it.

```
objectLibrary
  amendments       what this bundle states it changes, and what run time adds
  userObjects      an include: objects the user dropped in
  appObjects       an include: the Application's own static objects
```

Fixed below, changing above, which is what a stack is for. But it is not a
union mount, because **nothing shadows by position**. Every included record
keeps a name of its own — the include's alias, then its own key — so
`objectLibrary/userObjects/x` and `objectLibrary/appObjects/x` are two records
and neither hides the other. There is no precedence to state because there is
nothing for it to decide.

What shadows is an amendment, and it shadows one named record rather than a
whole layer: `userObjects/x` replaced, `userObjects/y` deleted. A record the
source holds of its own is simply a record under a key of its choosing —
including a key written as an address into an include's namespace, which is how
a bundle adds to what it included.

## Bundles refer to bundles

A bundle names the bundles it includes rather than copying them in, so two
bundles including the same one store it once. Where an include names a literal
hash the graph is a Merkle DAG and the integrity property composes: a parent's
bytes hold its children's hashes, so verifying a root verifies everything
beneath it. See "Where the hash lives" for what happens where an include names
a range instead.

## A key *and* a hash

Every bundle is named by both a **key** (a name) and a **hash**. Not the hash
alone.

The name is not a convenience. An Application can decide it is only concerned
with things bearing its own names, so data arriving from elsewhere is labelled
rather than mysterious. And signing needs it: a signature over a bare hash
proves only that some bytes exist, because a hash is self-attesting and says
nothing about what the bytes *claim to be*. What gets signed is a statement —
this key, at this version, is this content, attested by this party — which is
why package systems sign name and version and hash together. Signing is not
part of this design, but the name is what would make it possible later.

## Where the hash lives

**Not in the bundle.** A bundle does not carry its own hash: the hash of a
bundle is what you get when you hash its bytes, and a document asserting one
about itself is a claim rather than a fact. It is computed by whoever holds the
bytes and recorded in the store's index, beside the key and the version it was
found under.

That settles the obvious problem — a hash inside the document would have to
cover itself — without a canonical form to write down and get wrong. There is
nothing to exclude and nothing to normalise, because what is hashed is the file.

It covers the file ENTIRE, `_bundle` and all. So an author who corrects a date
or a name has a different bundle, and anything pinning the old hash still names
the old one. That is what a version is for.

The store hashes every item, not only bundles, because the hash answers a
question an Application asks about all of them: whether the copy the Desktop
holds is still the copy it meant to put there, and so whether it needs
re-uploading. An item is uploaded in chunks and its size is the cursor through
that; the hash is what says the upload finished, so it is taken once on
completion and cleared by anything that appends.

Two things follow from what the hash covers. An include may name a literal
hash, and where every include in a graph does, a parent's bytes hold its
children's hashes and hashing the parent covers the lot — the Merkle property,
earned rather than asserted. Where an include names a version RANGE instead, it
does not: what the range resolves to is not in the parent's bytes and may
resolve differently later. So the graph is verifiable exactly as far as it is
pinned, which is a property to state rather than a gap to apologise for.

## Keeping the filesystem off the wire

*Conversation. Nothing below this line is built.*

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
immutable bundles would be a **blob store**, not a filesystem. Put bytes and get
a hash, get bytes for a hash, ask whether a hash is held, evict. No paths,
directories, listing, rename or metadata.

## Caching and trust are the same boundary

*Conversation. The identity it rests on exists; the cache lifetime built on it
does not.*

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

This is what lets `_bundle` and `_amendments` be about the document rather than
records of it: they live in the keyed space, so adding or removing metadata
**cannot shift a record's index**. A bundle's ordered records are numbered by
the records alone.

## Why the separator is a slash

An address is the include's name, a slash, and the record's key, all the way
down; `bundle-format.md` shows the form. The slash was chosen over the two
alternatives on evidence.

Underscore is a legal word character, so the split point is ambiguous — include
`sub` with record key `bundle_x` and include `sub_bundle` with record key `x`
produce the same string. Dot does not survive as a key at all: `(subBundle.1:
"x")` comes back as key `subBundle` with the `.1` swallowed. Slash survives
quoted or bare, and nests, which underscore could not — and because it does, no
restriction on underscores in aliases or keys is needed anywhere.

Because the address form is one grammar all the way down, it reads the same way
as `box:///…` and `profile:///…` already do, so the system speaks one idiom for
addressing rather than two.

**A slash inside a key is not forbidden. It is what makes the key an address.**
A key holding one reads as one: the text before the first slash names an
include, and the rest is the key within it. That is how a composed source
builds its identities in the first place — the include's name, a slash, and
the child's key — and taking the FIRST slash is what lets the form nest, since
each layer sheds one name and hands the rest down untouched.

So a bundle adds a record *into an include's namespace* by authoring it under
such a key. `subBundle/9` as a record of the bundle's own is the same spelling
`_amendments` already uses for `subBundle/1`, and it needs no new mechanism: a
source holds its own records and merges them into its child's answer rather
than routing them by slash, so nothing structural collides. Two records wanting
one id is the author's to avoid, and that is the only collision there is.

The last segment is the only place a rule is needed. **Numeric keys are
forbidden as bundle record keys**, and with that ban the segment parses with one
rule: all ASCII digits means an index, anything else means a key. Forbidding
them outright also avoids `01` and `1` becoming two spellings of one index, and
it is a single validation at load.

**The store's key rules are its own, and are not this grammar.** A store key
never appears in an address: an include names a key and a version, and the
bundle index is what turns that into an item. The store refuses a slash because
a store is **not a filesystem** — a real one is coming, separately — and is
nearer to cookies or a browser's local storage: a flat set of names, each
holding one thing. Allowing paths in keys would invite an app to treat it as the
filesystem it is not, which is why the slash is refused there rather than merely
discouraged. Its other rule travels with it: nothing unprintable, its index
being a line per item, so that a key with a newline in it writes a line that
reads back as a different item. Neither is load-bearing for addressing, and
relaxing one would say nothing about the other.

## Reaching a bundle from Go

`pawscript.ParsePSL` returns a `PSLNode`, which presents both collections of a
PSL list at once — which is what reading a bundle needs:

```go
n, err := pawscript.ParsePSL(text)
n.Get("_bundle")    // the keyed metadata
n.Len(), n.Item(i)  // the ordered records
n.Child(i)          // an ordered record as a node in its own right
n.Map()             // the keyed members as a PSLMap
```

`SerializePSLNode` goes back the other way, so a bundle survives a round trip
with its records in place, and a node nests inside a document being built by
hand as well.

One serializer behaviour to know: **keyed members are sorted on emit**, so
declaration order among keyed records is not something to rely on. Ordered
records keep their order.

Reading a bundle by hand is rarely what is wanted, though: `LoadBundle` on an
app's store takes a key and a version, resolves the includes and hands back the
assembled source together with whatever it has to report.

## Open questions

- **Who tells a selector its newest has moved.** Selection is newest-wins and
  sharing is by selector, so every include asking for "the newest" of a key
  holds one object between them. Nothing tells that object when a newer version
  arrives in the store. Invalidation is told rather than decided, so the
  question is what notice the store would send and what it costs — `Replaced`
  over the whole source is the blunt answer and probably the wrong one.
- **Name resolution beyond the app's own store.** An include resolves against
  the store the loading app can see. Where a bundle is somewhere else, nothing
  says who is asked or what the asking looks like.
- **Are records typed or opaque?** Whether a source knows anything about the
  shape of what it holds.
- **The missing-record event.** What happens when a consumer asks for
  something the source does not have, and how that reaches whoever could
  supply it.
- **Whether sources carry bulk.** Whether this mechanism is meant for records
  measured in bytes or in megabytes, which decides much about the blob store.
