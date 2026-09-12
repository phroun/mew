# Objects an application hosts

> **Status: settled in shape, and half built.** The direction and the rules
> below are decided. The first hosted type — a query — is implemented in all
> three client libraries (`hosting-a-query.md`); the display side that speaks
> to it is not. See `live-data-negotiation.md` for what it is for, and
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

**Events go from an object's host to its subscribers**, and answers go to
whoever asked. `query` is answered by `result`, `ask` by `answer`, and `sub` --
or an object's mere existence -- by `event`; nothing carries two of them,
whichever direction it travels, so a request for data can never arrive looking
like something a subscription raised.

**Replies and errors.** A batch is answered by the end that received it, with
the ids it surfaced. A statement that will not parse refuses its batch and
leaves the connection standing, exactly as it does today.

## What is different, and it is one rule

**Whoever holds an object names it.**

An application's objects have the application's ids, the display's have the
display's, and each end mints its own. That works without partitioning any
range because **the direction a statement travelled says whose space it is
in**: a statement arriving at an application is about the application's
objects, one arriving at the display is about the display's. Neither end ever
has to ask whose number it is holding, and no range has to be reserved
anywhere.

So when the display creates something an application will hold — and it does:
only the display knows a list needs a query, and what sort and filter it wants
— it has no id to offer. It says `new`, and **the reply carries the
application's**, which is the same key-and-reply machinery a `new` in the other
direction has always used, pointed the other way.

The single place an id crosses is a property whose *value* refers to the other
side, and that is settled by the property's declared kind rather than by the
number.

A client library still needs no type registry and no factory to take part.
Receiving a statement is: find the object by id, apply a property, answer a
question, produce records. That is a few hundred lines in C, not a mirror of
the toolkit.

## Why the making is said out loud

An object with a lifetime needs both ends of that lifetime spoken. The display
opens a query when a list needs rows and destroys it when it is done with it
entirely, and **`destroy` is how the application learns it may let the records
go**. Without a `new` to pair with, there is nothing for that ending to end,
and an application holding a large result set would never find out it could
stop holding it.

## How the display learns there is anything to ask

Through a property, like any other value — and for a query, the value is a
**name** rather than an id:

```
set <treeview> data="files"       (app -> display: these rows are mine, under this name)
q=new query source="files" ...    (display -> app: then serve me this sequence)
reply q=9                         (app -> display: which I am calling 9)
```

So nothing is created until there is something to show, and the application
registers a source by writing one line of its own UI script. Where a hosted
type does need an object up front, the property carries its id instead, and the
property's declared kind is what says the number belongs to the other side.

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
object, apply or answer. Dispatching events and sending statements needs none
of that, so it is genuinely new ground in all three — and it came to a few
hundred lines each, most of it taking the query apart so that applications
never have to.

The way to keep it small is to keep the hosted types few. One — a query — is
the whole of what exists, and each one that follows should have to argue for
itself, because every one of them is a type three client libraries have to
know.
