# Objects an application hosts

> **Status: settled in shape, not yet built.** The direction and the rules
> below are decided. No code implements them yet; the data source is the first
> thing that will — see `live-data-negotiation.md` for how it uses this, and
> `data-sources-and-bundles.md` for the layering underneath.

Everything so far runs one way. An application says `new`, `set`, `ask`, `do`,
`destroy`; the display owns the objects those verbs address and answers with
events. The display speaks statements too — `welcome`, `init` — but never about
an object, because there were no objects on the other end to speak about.

There are now. An application can hold something the display needs to read: the
records behind a list too large to send, the bytes behind a clipboard offer in
the format actually asked for, the contents of a directory in a filesystem the
display cannot see, the answer to "is this input acceptable", the state to save
before a restart. In every one of those the display is the one with the
question.

So the verbs go both ways, addressed to whichever end holds the object.

## What is the same

**The verb set.** `set`, `ask`, `do`, `destroy`, `sub`, `unsub` mean what they
mean, whichever direction they travel. There is no second vocabulary to learn
and no reverse spelling of anything.

**Events go from an object's host to its subscribers.** A trinket raising
`clicked` at an application and an application's source raising `filled` at the
display are one mechanism pointed two ways.

**Replies and errors.** A batch is answered by the end that received it, with
the ids it surfaced. A statement that will not parse refuses its batch and
leaves the connection standing, exactly as it does today.

## What is different, and it is one rule

**The display never says `new` to an application.**

An application creates every object it hosts and allocates every id for them,
because the application is the end that bound data to a trinket in the first
place. Every case that looked like it needed the display to create something
turns out not to: two trinkets on one set of data are two objects the
application made; a re-sort is a property change on one that exists; a tree
expanding a node asks the object it already has, and the application answers
with one it made.

So a client library needs no type registry and no factory to take part.
Receiving a statement is: find the object by id, apply a property, answer a
question, perform an action. That is a few hundred lines in C, not a mirror of
the toolkit.

## Ids are resolved by whoever receives them

An id means whatever it means **in the receiver's own space**, and the direction
of travel says whose space that is: a statement arriving at an application is
about the application's objects, one arriving at the display is about the
display's. Neither end ever has to ask whose number it is holding, and no range
has to be reserved anywhere.

The single place an id crosses is a property whose *value* refers to the other
side, and that is settled by the property's declared kind rather than by the
number.

## How an application's object reaches the display

Through a property, like any other value:

```
mine=new <type>                   (app -> display: the app makes it)
set <treeview> data=<mine>        (app -> display: the trinket is told where its data lives)
```

The trinket now holds an id belonging to the application, so everything it asks
goes back across the wire. Nothing else about a property changed: a value that
happens to be an object id is what `action`, `menu` and half the toolkit's
properties already carry.

## What this does not change

**The display is still the authority over what is on screen.** An
application-hosted object supplies data and answers; it is not a piece of
chrome. It draws nothing, it owns no pixels, and it cannot reach the desktop
except by answering what it is asked.

**Nor does it widen what an application can do.** The traffic added here is the
display asking; an application that answers nothing is an application whose
lists stay empty, which is between it and its own user.

**And it stays refusable.** An application may answer any question with an
error — it does not have the record, it will not honour that query, the thing
being asked about is gone. A refusal is an answer; the display carries on with
what it has.

## What it costs

A client library grows a small interpreter: receive a statement, find the
object, apply or answer. Today a client dispatches events and sends statements,
and needs none of that. The Go client has most of the pieces already; the C and
Python ones do not.

The way to keep that small is to keep the hosted types few. One — a data source
— is the whole of what is planned. Each one that follows should have to argue
for itself, because every one of them is a type three client libraries have to
know.
