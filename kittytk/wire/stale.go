package wire

// What a source says has stopped being true.
//
// The other direction from everything else an application says. A `result` answers
// a question the display put; this is the application speaking first, because only
// it knows its records changed and nothing on this end can find out. Invalidation
// is TOLD, never decided -- there is no poll here, no expiry and no generation
// compared, and a reader nobody tells goes on believing what it holds.
//
//	stale source="papers"                              nothing said of it is trusted
//	stale source="papers" id=42 how=removed            one record, and it has left
//	stale source="papers" id=42 how=altered fields={ size }
//	stale source="papers" how=altered fields={ size }  any record's size may have moved
//
// # It is not an event, and it is not an answer
//
// Nobody subscribed and nobody asked, so it is neither of the two verbs that
// would otherwise carry it. An `event` belongs to a subscription or to an object
// reporting itself, and a source is neither; an `answer` belongs to one question,
// and there is no question here. It is a statement in its own right, the way
// `result` is -- and it shares that ordered stream, so a notice arriving after a
// result describes the world after that result.
//
// # The reason is what it costs
//
// A bare "forget this" throws away the one thing that settles whether the order
// moved or only the values did, and those are two prices. So `how=` carries one of
// the four words a change is classified by -- the same four an amendment is held
// under, because they are the same four facts:
//
//	added     nothing is known of it; a claim across that place is cut
//	removed   forgotten, and the completeness claim SURVIVES -- the record has
//	          left the sequence, so everything between a run's ends is still there
//	replaced  forgotten, and the run goes: it may have moved
//	altered   the named fields forgotten, and the run goes only where one of
//	          them decides the sequence
//
// A deletion is cheaper than a move, and telling the two apart is most of what
// this verb is for.
//
// # Coarsening is safe; narrowing never is
//
// Everything here may be widened freely. No `id=` is every record of the source;
// no `fields=` on an altered is every field. A source that cannot tell what moved
// says so by saying less, and the cost is a refill. Saying LESS than what actually
// changed is a missed invalidation: silent, and permanent.
//
// So the defaults lean wide. An absent `how=` reads as `replaced`, which is the
// widest thing that can be said about one named record, and that is what it
// encodes back as.
//
// # What it does NOT carry yet
//
// An extent -- `between these two records` -- is the other half, and it needs an
// address for the sequence it is a stretch OF. A stretch means nothing without an
// order, and the thing that holds one has no name here yet. Until it does, a
// notice is about records rather than about anyone's order, which is serval's
// `Stale(nil, notice)` and costs the order nothing.

import (
	"fmt"
	"strings"

	"github.com/phroun/serval"
)

const (
	// StaleVerb is a source saying what has stopped being true.
	StaleVerb = "stale"

	// SourceArg names which of the application's sources it is about. Required:
	// an application may serve several, and a notice that did not say which
	// would be a notice about all of them.
	SourceArg = "source"

	// HowArg is which of the four things happened. The same word an amendment is
	// held under, because they are the same four facts.
	HowArg = "how"
)

// A Stale is one `stale` statement taken apart.
type Stale struct {
	// Source is the name the application serves the records under.
	Source string

	// ID is the record it is about, and nil for a notice about the whole source.
	ID *Value

	// How is which of the four things happened. Replaced where the statement did
	// not say, that being the widest claim about one record.
	How serval.Change

	// Fields are the ones that may have moved, for Altered. Empty says the
	// source cannot tell, which is every field.
	Fields []string
}

// ParseStale reads a notice from a statement's arguments.
func ParseStale(args []*Arg) (*Stale, error) {
	s := &Stale{How: serval.Replaced}
	var said bool
	var fields *Arg
	for _, arg := range args {
		switch arg.Name {
		case SourceArg:
			if arg.Value == nil || arg.Value.Kind != StringValue {
				return nil, fmt.Errorf("%s: expected the name the application serves them under", SourceArg)
			}
			s.Source = arg.Value.Str
		case IDArg:
			if arg.Value == nil {
				return nil, fmt.Errorf("%s: expected a record's identity", IDArg)
			}
			s.ID = arg.Value
		case HowArg:
			if arg.Value == nil || arg.Value.Kind != WordValue {
				return nil, fmt.Errorf("%s: expected one of added, removed, replaced, altered", HowArg)
			}
			c, ok := changeNamed(arg.Value.Word)
			if !ok {
				return nil, fmt.Errorf("%s: %q is not one of added, removed, replaced, altered",
					HowArg, arg.Value.Word)
			}
			s.How, said = c, true
		case FieldsArg:
			fields = arg
		default:
			return nil, fmt.Errorf("%s: %s= is not one of its arguments", StaleVerb, arg.Name)
		}
	}
	if s.Source == "" {
		return nil, fmt.Errorf("%s: expected %s=", StaleVerb, SourceArg)
	}
	// **`fields=` only means anything on an alteration.** On any other reason the
	// values are gone entire, so a list of them would be a statement contradicting
	// the one beside it -- and quietly ignoring one half of a contradiction is how
	// a source comes to believe it said something it did not.
	if fields != nil {
		if !said || s.How != serval.Altered {
			return nil, fmt.Errorf("%s: %s= names what an alteration touched, and this is %s",
				StaleVerb, FieldsArg, s.How)
		}
		bag, err := ParseFields(fields.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FieldsArg, err)
		}
		for _, f := range bag {
			if f.Name == "" {
				return nil, fmt.Errorf("%s: a field is named, and %s carries one that is not",
					FieldsArg, FieldsArg)
			}
			if f.Value != nil {
				return nil, fmt.Errorf("%s: %s names fields and not their values; %s carries one",
					FieldsArg, StaleVerb, f.Name)
			}
			s.Fields = append(s.Fields, f.Name)
		}
	}
	return s, nil
}

// Args renders the notice as the arguments after the verb.
func (s *Stale) Args() []*Arg {
	out := []*Arg{Named(SourceArg, s.Source)}
	if s.ID != nil {
		out = append(out, &Arg{Name: IDArg, Value: s.ID})
	}
	out = append(out, &Arg{Name: HowArg, Value: &Value{Kind: WordValue, Word: s.How.String()}})
	if len(s.Fields) > 0 {
		named := make(serval.Record, 0, len(s.Fields))
		for _, f := range s.Fields {
			named = append(named, &serval.Field{Name: f})
		}
		out = append(out, &Arg{Name: FieldsArg, Value: &Value{
			Kind: BlockValue, Block: asScript(named),
		}})
	}
	return out
}

// Encode renders the notice as a statement.
func (s *Stale) Encode() string {
	var sb strings.Builder
	sb.WriteString(StaleVerb)
	for _, arg := range s.Args() {
		sb.WriteByte(' ')
		sb.WriteString(EncodeArg(arg))
	}
	return sb.String()
}

// Notice is this as serval states it, for handing to a source that holds
// something.
//
// No extent: a notice with neither end is every record it is answered against,
// which is the widest a source can say and the only thing sayable while the
// sequence has no address. See the file comment.
func (s *Stale) Notice() serval.Notice {
	n := serval.Notice{Change: s.How, Fields: s.Fields}
	if s.ID != nil {
		n.First = AsData(s.ID)
	}
	return n
}

// changeNamed reads one of the four words back.
//
// Written against String() rather than beside it, so the two cannot drift: a word
// this does not produce is one this will not take.
func changeNamed(word string) (serval.Change, bool) {
	for _, c := range []serval.Change{serval.Added, serval.Removed, serval.Replaced, serval.Altered} {
		if c.String() == word {
			return c, true
		}
	}
	return 0, false
}
