package wire

// What an `ask` is answered with.
//
// The third of the three pairs, and the one that was missing. `query` is answered
// by `result`, `sub` -- or an object's mere existence -- by `event`, and `ask` by
// this. The pair was named when `ask` was added and deliberately left unbuilt:
//
//	"Answers still travel as events, which they should not: an answer is
//	solicited and belongs to one question, so it passes the subscription filter
//	and the D20 suppression it has no business passing. Both cost something
//	here -- subscribing cannot push an inventory, because the run ends in an
//	event type the client has not subscribed to yet, and an answer raised
//	inside `set` has to wait for the suppression to lift. An `answer` statement
//	with a correlation key is the fix, and is not in this."
//
// Both costs are what this removes. An answer is not an event, so nothing has to
// have subscribed to receive one and nothing waits for a suppression to lift.
//
// # The correlation key was already in the language
//
// Every statement may carry one -- `w=new window` is the same mechanism, and
// `alias` and `template` refuse one in those words. An `ask` parsed it and threw
// it away. So the question side needs no new grammar at all:
//
//	a1=ask tree amendments
//	→ answer to=a1 id="i1;" how=altered fields={ kind "Archive" }
//	  answer to=a1 id="i9;" how=removed
//	  answer to=a1 complete count=2
//
// **`to=` and not a bare operand**, which is where `result` puts its query id. A
// query id is a NUMBER and so cannot be mistaken for anything else in the
// statement; a correlation key is a name, and `answer complete` would read the
// terminator as the key it is answering. So it is named, and the name is reserved.
//
// An unkeyed ask is answered without it. That keeps every `ask <store> inventory`
// written before this parsing, and suits a question whose asker is not waiting.
//
// # The envelope is the wire's and the payload is the question's
//
// `result` has a fixed shape because a query's answer is always records. An ask's
// answer is whatever that question answers -- an inventory, a chunk of bytes, the
// amendments a tree holds -- so what is fixed here is the correlation, the
// terminator and the refusal, and the rest is the question's own vocabulary,
// carried as named arguments and read by whoever asked.
//
// A record among them is spelled the way a result spells one, `id=` beside
// `record=` or `fields=`, so that "a record's identity" has one spelling in the
// language rather than one per verb.

import (
	"fmt"
	"strings"

	"github.com/phroun/serval"
)

const (
	// AnswerVerb carries one piece of an answer: `answer to=a1 ... [complete]`.
	AnswerVerb = "answer"

	// ToArg names the question being answered: the correlation key the asker put
	// on its `ask`. Absent where the ask carried none.
	//
	// Reserved, so a question's own vocabulary may not use it. Every other
	// argument on an answer belongs to whatever was asked.
	ToArg = "to"
)

// An Answer is one `answer` statement taken apart.
//
// Both the payload and the terminator can ride one statement, as they can on a
// result, so an answer of one piece crosses as a single line. Which form was
// written is not something either end has to care about.
type Answer struct {
	// To is the correlation key of the question, and empty for one that carried
	// none.
	To string

	// Carries is what the answer holds, in the question's own words, with the
	// envelope's own arguments taken out. Nil for a statement that carries only a
	// terminator.
	//
	// Named for what it is rather than `Args`, which is what this renders AS --
	// the encoder being called that to match the one on a result.
	Carries []*Arg

	// Complete says this is the last answer for that question. An asker holding
	// anything for it can let go when this arrives, whether or not anything came.
	Complete bool

	// Error is why the question was refused, and empty where it was not.
	//
	// **A refusal is still an answer**, and it rides the same verb rather than
	// arriving as an error somewhere else: the question was put, so it has an
	// answer, and an asker waiting for one must not be left waiting because the
	// answer happened to be no.
	Error string
}

// ParseAnswer reads an answer from a statement's arguments.
func ParseAnswer(args []*Arg) (*Answer, error) {
	a := &Answer{}
	for _, arg := range args {
		switch arg.Name {
		case ToArg:
			if arg.Value == nil || arg.Value.Kind != WordValue {
				return nil, fmt.Errorf("%s: expected the key the ask was given", ToArg)
			}
			a.To = arg.Value.Word
		case ResultComplete:
			if arg.Value != nil || arg.Flag != FlagTrue {
				return nil, fmt.Errorf("%s: takes no value", ResultComplete)
			}
			a.Complete = true
		case ErrorArg:
			if arg.Value == nil || arg.Value.Kind != StringValue {
				return nil, fmt.Errorf("%s: expected a reason", ErrorArg)
			}
			a.Error = arg.Value.Str
		default:
			// The question's own. Nothing here reads it: whoever asked knows what
			// the answer to that question looks like, and a middle that had to
			// know too would need teaching about every question ever added.
			a.Carries = append(a.Carries, arg)
		}
	}
	return a, nil
}

// Args renders the answer as the arguments after the verb.
//
// The envelope first, so a reader scanning the head of a statement finds the
// correlation before the payload it belongs to, and the terminator last, where a
// result's is.
func (a *Answer) Args() []*Arg {
	var out []*Arg
	if a.To != "" {
		out = append(out, &Arg{Name: ToArg, Value: &Value{Kind: WordValue, Word: a.To}})
	}
	out = append(out, a.Carries...)
	if a.Complete {
		out = append(out, &Arg{Name: ResultComplete, Flag: FlagTrue})
	}
	if a.Error != "" {
		out = append(out, Named(ErrorArg, a.Error))
	}
	return out
}

// Encode renders the answer as a statement, the way Event.Encode renders an event.
func (a *Answer) Encode() string {
	var sb strings.Builder
	sb.WriteString(AnswerVerb)
	for _, arg := range a.Args() {
		sb.WriteByte(' ')
		sb.WriteString(EncodeArg(arg))
	}
	return sb.String()
}

// Record is the record this answer carries, where it carries one, and how much of
// it there is.
//
// The same two spellings a result uses: `record=` is every member the record has,
// `fields=` is some of them. A question that answers with records gets this for
// nothing; one that answers with anything else has no record and says so.
func (a *Answer) Record() (id *Value, fields serval.Record, whole bool, ok bool) {
	for _, arg := range a.Carries {
		switch arg.Name {
		case IDArg:
			id = arg.Value
		case RecordArg:
			f, err := ParseFields(arg.Value)
			if err != nil {
				return nil, nil, false, false
			}
			fields, whole, ok = f, true, true
		case FieldsArg:
			f, err := ParseFields(arg.Value)
			if err != nil {
				return nil, nil, false, false
			}
			fields, ok = f, true
		}
	}
	return id, fields, whole, ok
}

// Arg is one of the answer's own arguments by name, and nil for one it has not
// got -- which is not the same as one carrying nothing, and a caller that needs to
// tell those apart reads Carries.
func (a *Answer) Arg(name string) *Arg {
	for _, arg := range a.Carries {
		if arg.Name == name {
			return arg
		}
	}
	return nil
}
