package trinkets

// What a tree can be asked.
//
// The first trinket to answer a question, and the first user of the `answer` verb.
// Nothing Go-side is reachable from an application unless the wire language says it,
// and what a tree HOLDS against its source is the case that made that plain: a reader
// edits a cell, the edit is held as an amendment so it survives the next read, and
// somebody eventually has to write it to a file or a database. Without a way to ask,
// the edit is real, durable for the session, and invisible.
//
// # One answer per amendment
//
//	q1=ask tree amendments
//	→ answer to=q1 id=1 how=altered fields={ kind "Archive" }
//	  answer to=q1 id=9 how=removed
//	  answer to=q1 complete count=2
//
// `id=` is the record's identity, spelled as a result spells one. `how=` is which of
// the four things has been said about it, in the same words a source ANNOUNCES a
// change with -- added, removed, replaced, altered -- because they are the same four
// facts and giving the held version four words of its own would mean two
// vocabularies for one idea.
//
// **`record=` or `fields=`, and the difference is the point.** A replacement and an
// addition are records ENTIRE, which is what `record=` means everywhere else; an
// alteration names some members and the rest stand, which is `fields=`. A reader
// writing these out has to know which it is holding, and the two spellings say so
// without a flag to interpret.
//
// A removal carries neither. The record is gone, so there is nothing of it to state,
// and members left on one would be written to a file as though they were in force.
//
// # A tree that holds nothing answers none, rather than refusing
//
// A tree reading its own items, or a declared source with no amendment layer to
// reach, holds no amendments. That is an answer -- `complete count=0` -- and not a
// refusal: the question was understood, and none is what there is. A refusal would
// have an application unable to tell "nothing to save" from "I cannot tell you".

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// The questions a tree answers, and the arguments its answers carry.
const (
	AskAmendments = "amendments"

	// HowArg is which of the four things is held against the record.
	HowArg = "how"

	// ClashedArg marks an addition whose key turned out to be the child's after
	// all. The child's record stands and this one no longer goes out, so it is
	// reported to be dealt with rather than written out as though it were in force.
	ClashedArg = "clashed"

	// CountArg rides the completion: how many AMENDMENTS were sent, not how many
	// members among them.
	CountArg = "count"
)

// Ask answers a question put to this tree.
//
// It takes no arguments today. The signature is the protocol's, and a question that
// wanted narrowing -- one key, or only the alterations -- would read them here.
func (t *TreeView) Ask(question string, _ []*protocol.Arg, out *protocol.Answers) error {
	switch question {
	case AskAmendments:
		t.answerAmendments(out)
		return nil
	}
	// Unreachable through the wire, where checkAskName turns away a question the
	// type does not declare before this is called. It is here for an in-process
	// caller, which has no such gate.
	return fmt.Errorf("ask: a treeview answers no question called %q", question)
}

// answerAmendments sends what this tree's source holds, one answer each.
func (t *TreeView) answerAmendments(out *protocol.Answers) {
	over := t.amendable()
	if over == nil {
		out.Done(wire.Named(CountArg, 0))
		return
	}
	held := over.Amendments()
	for _, am := range held {
		out.Send(amendmentArgs(am)...)
	}
	out.Done(wire.Named(CountArg, len(held)))
}

// amendmentArgs is one amendment as an answer's arguments.
func amendmentArgs(am serval.Amendment) []*protocol.Arg {
	args := []*protocol.Arg{
		{Name: wire.IDArg, Value: wire.AsWire(am.Key)},
		{Name: HowArg, Value: &wire.Value{Kind: wire.WordValue, Word: am.How.String()}},
	}
	if am.Clashed {
		args = append(args, &protocol.Arg{Name: ClashedArg, Flag: protocol.FlagTrue})
	}
	if am.Fields == nil {
		return args // a removal states nothing of the record: it is gone
	}
	// Entire or partial, said by which argument carries it. See the file comment.
	what := wire.RecordArg
	if am.How == serval.Altered {
		what = wire.FieldsArg
	}
	return append(args, &protocol.Arg{
		Name:  what,
		Value: &wire.Value{Kind: wire.BlockValue, Block: wire.RecordBlock(am.Fields)},
	})
}
