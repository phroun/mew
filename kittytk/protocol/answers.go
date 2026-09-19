package protocol

// Where an object sends what a question is answered with.
//
// One of these is handed to `Ask`, carrying the correlation key the asker put on its
// `ask`. Everything sent through it goes out as an `answer` statement quoting that
// key back, so an asker with two questions outstanding can tell the answers apart --
// which is what `wire/answer.go` is about, and what answers-as-events could not do.
//
// **An object never sees the key.** It sends pieces and says which is the last one;
// the correlation is the envelope's, and an object that had to thread a key through
// its own code would be one more place for the wrong key to be written.
//
// A question asked without a key is answered without one. Nothing here changes: the
// pieces go out, they simply do not say which question they are for, which suits an
// asker that is not waiting.

import "fmt"

// Answers is one question's answer, being written.
type Answers struct {
	ctx  *BindContext
	to   string
	done bool
}

// AnswerControl is an optional Factory capability: where an answer goes.
//
// The same shape as EventControl, and reached the same way. A session holds no
// connection of its own -- it is handed a Factory, and the factory is what knows
// which connection this is -- so anything that has to reach the client is asked of
// the factory. A factory that cannot carry answers leaves a question with nowhere to
// send one, and `Answers` being nil-safe is what makes that harmless rather than a
// crash in whatever was asked.
type AnswerControl interface {
	Answers(key string) *Answers
}

// Answers is the handle for one ask, for a caller holding a BindContext -- which is
// what a trinket reaches its connection through.
func (c *BindContext) Answers(key string) *Answers { return &Answers{ctx: c, to: key} }

// Send is one piece of the answer, and there may be more.
//
// The arguments are the QUESTION's own: whoever asked knows what the answer to that
// question looks like, and nothing in the middle has to be taught about a question
// for its answer to cross.
func (a *Answers) Send(carries ...*Arg) {
	a.write(&Answer{Carries: carries})
}

// Done is the last piece, with whatever rides on it.
//
// **It is said even when nothing came.** An asker holding something for a question
// lets go when this arrives, and a question that answered with no records at all has
// still been answered -- which is a different fact from one still being worked on,
// and the only thing that tells them apart.
func (a *Answers) Done(carries ...*Arg) {
	a.write(&Answer{Carries: carries, Complete: true})
}

// Fail ends the question with a reason.
//
// For a question that was UNDERSTOOD and could not be answered. One nothing answers,
// or one the type does not declare, is a statement that was not understood, and the
// batch's reply is where that belongs -- see askObject.
//
// It completes, because a refusal is an answer: an asker waiting for one must not
// wait forever because the answer happened to be no.
func (a *Answers) Fail(format string, v ...any) {
	a.write(&Answer{Error: fmt.Sprintf(format, v...), Complete: true})
}

// Complete reports whether this question has been answered, for an object that
// streams and wants to know whether it already stopped.
func (a *Answers) Complete() bool { return a == nil || a.done }

// write sends one statement and remembers an ending.
//
// **Nothing goes out after the end.** A question is answered once, and a second
// completion -- or a piece arriving after one -- would have an asker that has
// already let go trying to place a record in a question it has forgotten.
func (a *Answers) write(out *Answer) {
	if a == nil || a.done {
		return
	}
	out.To = a.to
	a.done = out.Complete
	a.ctx.EmitAnswer(out)
}
