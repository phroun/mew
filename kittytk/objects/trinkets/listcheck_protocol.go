package trinkets

// What an application can do to a list's ticks, and ask of them.
//
//	do lv check_all
//	do lv check_none
//	do lv check_invert
//	do lv check id=42                 # tick the record 42
//	do lv check id=42 checked=false   # untick it
//
//	q=ask lv checked
//	→ answer to=q id=3
//	  answer to=q id=7
//	  answer to=q complete count=2
//
// A row is named by its record's identity, `id=`, spelled as a result spells
// one -- the same `id=` a `check` event carries, so a row heard about can be
// named straight back. Naming by identity rather than position means a row the
// list has not placed yet can be ticked all the same, which is what restoring a
// saved choice over a long list needs.
//
// **The answer is the selection's own shape.** A list told to tick everything
// cannot write down what everything is, so the answer names the rows it names
// and says, on its completion, which way to read them: `all` set means these
// are the rows SPARED out of every row, and unset means these are the rows
// ticked. Everything ticked and nothing spared is one statement however long
// the list.
//
// A list without checkboxes has nothing ticked, and says so -- `complete
// count=0` -- rather than refusing: the question was understood, and none is
// the answer. The do verbs, which ask it to CHANGE something it cannot hold,
// are refused.

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// The question a list answers, and what its completion carries.
const (
	AskChecked = "checked"

	// AllArg, on the completion, turns the answers over: they are the rows
	// spared out of every row rather than the rows ticked.
	AllArg = "all"
)

// Do performs one of the list's actions on its ticks (protocol.doer).
func (l *ListView) Do(action string, args []*protocol.Arg) error {
	switch action {
	case "check_all", "check_none", "check_invert", "check":
	default:
		return fmt.Errorf("a listview does nothing called %q", action)
	}
	if !l.SelectAllEnabled() {
		return fmt.Errorf("%s: this list has no checkboxes to tick", action)
	}
	switch action {
	case "check_all":
		l.SelectAll()
	case "check_none":
		l.ClearSelection()
	case "check_invert":
		l.InvertSelection()
	case "check":
		id, on, err := checkArgs(args)
		if err != nil {
			return err
		}
		l.SetCheckedID(id, on)
	}
	return nil
}

// checkArgs reads `id=` and `checked` off a check.
func checkArgs(args []*protocol.Arg) (*serval.Value, bool, error) {
	var id *serval.Value
	on := true
	for _, a := range args {
		switch a.Name {
		case wire.IDArg:
			if a.Value == nil {
				return nil, false, fmt.Errorf("check: id= needs a record's identity")
			}
			id = wire.AsData(a.Value)
		case "checked":
			b, err := protocol.AsBool("check: checked", a.Value, a.Flag)
			if err != nil {
				return nil, false, err
			}
			on = b
		}
	}
	if id == nil {
		return nil, false, fmt.Errorf("check: expected id=")
	}
	return id, on, nil
}

// SetCheckedID ticks the row of a record, or unticks it, whether or not the list
// has placed it yet. A list without checkboxes does nothing.
func (l *ListView) SetCheckedID(id *serval.Value, on bool) {
	if id == nil || !l.SelectAllEnabled() {
		return
	}
	l.chosen.set(id, on)
	row := -1
	if at, ok := l.bones.posOf(id); ok {
		row = at
	}
	l.Update()
	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
	l.tellCheck(CheckChange{What: CheckRow, Row: row, ID: id, Checked: on})
}

// Ask answers a question put to this list (protocol.asker).
func (l *ListView) Ask(question string, _ []*protocol.Arg, out *protocol.Answers) error {
	switch question {
	case AskChecked:
		l.answerChecked(out)
		return nil
	}
	// Unreachable through the wire, where a question the type does not declare
	// is turned away first. Here for an in-process caller.
	return fmt.Errorf("ask: a listview answers no question called %q", question)
}

// answerChecked sends what the ticks name, one answer each, and which way to
// read them.
func (l *ListView) answerChecked(out *protocol.Answers) {
	if !l.checkboxes {
		out.Done(wire.Named(CountArg, 0))
		return
	}
	named := l.chosen.ids()
	for _, id := range named {
		out.Send(&protocol.Arg{Name: wire.IDArg, Value: wire.AsWire(id)})
	}
	done := []*protocol.Arg{wire.Named(CountArg, len(named))}
	if l.chosen.all {
		done = append(done, &protocol.Arg{Name: AllArg, Flag: protocol.FlagTrue})
	}
	out.Done(done...)
}
