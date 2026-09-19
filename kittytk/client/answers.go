package client

// Asking a question and being answered.
//
// `Handle.Ask` puts a question and leaves the answer to whatever events it declared.
// This is the other way: the question carries a correlation key, and the answers
// quote it back, so an application with two questions outstanding can tell them
// apart -- which is what the `answer` verb is for, and what answers-as-events could
// not do.
//
// **The key is minted here and never written by a caller.** It exists so that two
// answers cannot be confused, which is a job for whoever is doing the confusing.

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/wire"
)

// asked is what this connection is waiting to be answered, by correlation key.
type asked struct {
	mu   sync.Mutex
	on   map[string]func(*wire.Answer)
	next atomic.Uint64
}

// key mints the next correlation key for this connection.
//
// A name and not a number, because a statement's key is a name -- the same
// mechanism `w=new window` uses -- and because a bare number after `answer` would
// be read as one of the question's own arguments.
func (a *asked) key() string {
	return "q" + strconv.FormatUint(a.next.Add(1), 36)
}

func (a *asked) want(key string, fn func(*wire.Answer)) {
	a.mu.Lock()
	if a.on == nil {
		a.on = map[string]func(*wire.Answer){}
	}
	a.on[key] = fn
	a.mu.Unlock()
}

func (a *asked) forget(key string) {
	a.mu.Lock()
	delete(a.on, key)
	a.mu.Unlock()
}

func (a *asked) handler(key string) func(*wire.Answer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.on[key]
}

// inboundAnswer routes one answer to whoever asked, and lets go when it ends.
//
// An answer for a question nobody is waiting on is dropped. That is not a failure:
// an asker may have given up, and an unkeyed answer belongs to a question whose
// asker never wanted to be told -- both of which are quieter than being noisy about
// something the other end was entitled to send.
func (c *Conn) inboundAnswer(a *wire.Answer) {
	if a == nil || a.To == "" {
		return
	}
	fn := c.asks.handler(a.To)
	if fn == nil {
		return
	}
	// Forgotten BEFORE the handler runs, so a handler that asks the same question
	// again cannot have its new registration dropped by this one ending.
	if a.Complete {
		c.asks.forget(a.To)
	}
	fn(a)
}

// AskFor puts a question and calls fn for each piece of the answer.
//
// fn is called for every piece, in the order they arrive, and once more for the one
// that completes -- which arrives whether or not anything came before it, so a
// question that answered with nothing is told apart from one still being worked on.
// A refusal arrives the same way, carrying `Error`: the question was put, so it has
// an answer, and an asker must not wait forever because the answer happened to be no.
//
// It returns once the question has been SENT. Nothing here blocks for the answer: an
// answer may take as long as whatever is answering takes, and a caller that wants to
// wait waits on something of its own inside fn.
func (h Handle) AskFor(question string, fn func(*wire.Answer)) error {
	if fn == nil {
		return h.Ask(question)
	}
	key := h.c.asks.key()
	h.c.asks.want(key, fn)
	if _, err := h.c.Exec(fmt.Sprintf("%s=ask %s %s", key, h.addr(), question)); err != nil {
		// The question never went, so nothing will ever answer it. Letting go here
		// is what keeps a failed ask from leaving a handler waiting for good.
		h.c.asks.forget(key)
		return err
	}
	return nil
}
