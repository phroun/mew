package wire

// A query, in structured form.
//
// An application hosts a query: one filter and one sort over its own records,
// which a display fills scopes out of as somebody scrolls. The wire carries
// that as text, and every client library would otherwise make its author walk
// a statement tree to find out what was being asked. So the walking happens
// once, here, and what reaches an application is a struct.
//
// docs/hosting-a-query.md is the spelling this implements, and
// testdata/query.wire is the corpus every implementation of it answers.
// docs/sort-and-filter.md is how a sort and a filter are written down; what
// they MEAN is serval's docs/ordering.md.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/phroun/serval"
)

// The verb a display opens and refills a query with, and the verb the
// application answers it with.
//
// Three pairs, and nothing carries two of them: `query` is answered by
// `result`, `ask` by `answer` (see answer.go), and `sub` -- or an object's mere
// existence -- by `event`. So a record arriving for a list can never be mistaken
// for something a subscription raised.
//
// The third pair was named here before it existed, and said so nowhere: `ask` was
// added with "an `answer` statement with a correlation key is the fix, and is not in
// this", and this comment was written three days later as though it were. It is
// there now, and the comment is true.
const (
	QueryVerb  = "query"  // `new query source="files" sort={ name } count=30`
	ResultVerb = "result" // `result 9 id=42 record={ ... }`

	// PlaceVerb carries a PLACE: a record's identity, in its position in the
	// sequence, and whatever is known of it so far with no claim about how much.
	//
	// A verb of its own, and that is the whole mechanism. Places are ADDITIONAL
	// -- every record still arrives as a result, and the scope's own `complete`
	// still ends the answer -- so a reader that does not know this verb skips
	// these statements and is left with exactly the answer it gets otherwise.
	// There is nothing to negotiate and nothing it can be misled about, because
	// it never saw them.
	//
	// It is the mirror of how a hint works, pointing the other way. A hint rides
	// the question and the answerer may drop it, because dropping it can only
	// produce more data than was asked for. A place rides the ANSWER and the
	// asker may drop it, because it was never part of what the answer claimed.
	PlaceVerb = "place" // `place 9 id=42 fields={ name "x" }`

	// IDArg carries a record's identity, beside its fields rather than among
	// them.
	//
	// An identity is not a field. A record can hold a field called `key` and
	// that field is data like any other -- it sorts, it filters, it is shown
	// in a column -- while what names the record travels here. Keeping the two
	// apart is what lets a reading expose a document's own keys as ordinary
	// data without those keys becoming the identities this end works by.
	IDArg = "id"

	// What a result carries, and how much of the record it is.
	//
	// `record` is every field the record has; `fields` is some of them. The
	// difference is worth a word because a whole record answers any question
	// about that record, and a subset answers only the one that asked for it --
	// which is what lets an answer be kept and reused rather than asked for
	// again.
	RecordArg = "record"
	FieldsArg = "fields"

	// MapArg and LenArg are how many members the record HAS -- by name and by
	// position -- whether or not they were all sent. They ride beside `fields=`
	// and never beside `record=`, a whole record being its own totals.
	//
	// `map` and `len` because those are already the two halves of a PSL node,
	// which is what a record is read out of: `Len()` is its items and `Map()`
	// its keyed members. A count of nothing is not written.
	MapArg = "map"
	LenArg = "len"

	// ResultComplete ends a scope: everything for it has been sent.
	//
	// It can ride on the statement carrying the last record, and `ordered` can
	// ride on the one carrying the first, so a scope of a single record crosses
	// as a single line. Both forms are read; which one was written is not
	// something either end has to care about.
	ResultComplete = "complete"
	WatermarkArg   = "watermark"
	OrderedArg     = "ordered"
	ErrorArg       = "error"

	// TotalArg is how many records the whole SEQUENCE has, and ExactArg says
	// that figure is the whole story rather than a floor.
	//
	// Weak by default and strengthened out loud, the same way round as `fields`
	// against `record`: `total=20` alone says there are at LEAST twenty, which
	// is what a reader that has seen part of a sequence can say and is most of
	// what a scrollbar wants; `total=20 exact` says twenty is all there are.
	//
	// It rides a completion, being a fact about the order rather than about any
	// record -- and about the sequence rather than the scope, how many came back
	// being something whoever asked can count. A figure of nothing is not
	// written, `total=0` alone saying only what is true of every sequence there
	// is; `total=0 exact` is a sequence counted and found empty, and is written.
	TotalArg = "total"
	ExactArg = "exact"

	// FirstArg is where in the sequence the answer BEGAN: the position of its
	// first record, counted in the sequence's own order however the scope
	// walked it.
	//
	// Beside `total` the two are a scroll thumb -- how long, and where -- and
	// together they are what lets a reader draw one over records it has never
	// seen. It is also what answers `from`: a scope that asked to begin at a
	// place is told where it actually began, and asks again from that.
	//
	// **It crosses only when it is known, and what crosses is exact.** A count
	// can honestly be a floor, part of a sequence seen being at least that many;
	// a position cannot be reckoned the same way, and "at least the six
	// hundredth" is not something a reader can put a thumb on. So there is no
	// weak form and `exact` does not apply to it: either the position is here or
	// nothing is, and silence means the reader stays where it was rather than
	// believing a figure nobody sent.
	FirstArg = "first"
	// FromArg is a position a scope asks to begin NEAR, for a reader that has a
	// place in mind and no identity there: a thumb dragged down a long sequence.
	// It is best effort, and `first` on the answer says where it really began.
	FromArg = "from"

	// ExtendArg is the display saying it will HOLD the places it is sent, so a
	// result may leave out what its place already carried.
	//
	// It rides the query because it is about how this asker reads rather than
	// about which records it wants, and it is the ASKER's to say for the one
	// reason that matters: a reader that dropped the places would then silently
	// lose fields. Only the end doing the dropping can promise not to.
	//
	// Which is the test for whether anything in this protocol needs opting into
	// -- does dropping the statement still leave the answer true? Places pass
	// it, and results that lean on them are the one thing that does not.
	//
	// Saying nothing is `replace`, where every result carries the lot. That is
	// the default, and it is what every answer here does today.
	ExtendArg = "extend"
)

// EncodeRecord renders a record as the block that carries it: one statement per
// field, the name first and its value, if it has one, after.
func EncodeRecord(r serval.Record) string {
	return EncodeValue(&Value{Kind: BlockValue, Block: asScript(r)})
}

// ParseFields reads a field bag, in either of the two forms it is written in.
//
// A block carries names and values together, which is what a record is:
// `{ name "src/parser.go"; size 1024 }`.
//
// A string carries names alone, separated by commas, which is what a query
// asking for a narrower record is: `fields=".name, .size"`. It is the shorter
// spelling of a list that never has values in it, and comma because that is
// what a list is separated by -- `;` is where a statement ends, one level up,
// and would be doing a second job here.
//
// A name holding a comma has no spelling in the string form. The block form
// carries it, which is why both are read.
func ParseFields(v *Value) (serval.Record, error) {
	if v != nil && v.Kind == StringValue {
		return parseFieldList(v.Str)
	}
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block of fields or a list of names")
	}
	var out serval.Record
	for _, st := range v.Block.Statements {
		if st.Verb == "" {
			return nil, fmt.Errorf("a field is a name, and %q is not one", EncodeStatement(st))
		}
		switch len(st.Args) {
		case 0:
			// A name with nothing under it: a field a query ASKED for, which
			// is the one place a bag carries names and no values.
			out = append(out, &serval.Field{Name: st.Verb})
		case 1:
			v, err := operandValue(st.Verb, st.Args[0])
			if err != nil {
				return nil, err
			}
			out = append(out, &serval.Field{Name: st.Verb, Value: asData(v)})
		default:
			return nil, fmt.Errorf("%s: a field carries one value, not %d", st.Verb, len(st.Args))
		}
	}
	return out, nil
}

// parseFieldList reads the string form: names separated by commas, each
// trimmed of the space around it.
//
// An empty list is a list of nothing, which is what `fields=""` says. An empty
// NAME is refused rather than skipped: a stray comma is a typo, and quietly
// dropping it would narrow a query by one field without saying so.
func parseFieldList(text string) (serval.Record, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var out serval.Record
	for _, piece := range strings.Split(text, ",") {
		name := strings.TrimSpace(piece)
		if name == "" {
			return nil, fmt.Errorf("%q: a field list holds names, and one of these is empty", text)
		}
		out = append(out, &serval.Field{Name: name})
	}
	return out, nil
}

// operandValue reads one operand as a value.
//
// A bare word in argument position is a flag as far as the grammar is
// concerned -- that is what `wrap` and `!enabled` are -- so a word written
// where a value belongs arrives as a flag carrying its own name, and this is
// where it becomes the word it was written as. `!` and `?` say something a
// value cannot, so they are refused rather than quietly read as words.
func operandValue(what string, a *Arg) (*Value, error) {
	if a.Value != nil {
		if a.Name != "" {
			return nil, fmt.Errorf("%s: no argument called %q", what, a.Name)
		}
		return a.Value, nil
	}
	if a.Flag != FlagTrue {
		return nil, fmt.Errorf("%s: %q is asserted, not valued", what, a.Name)
	}
	return NewWord(a.Name), nil
}

// operandField reads a predicate's first operand as the name of the field it
// tests.
//
// A field name is a NAME, whatever form it was written in. A bare word is the
// ordinary spelling and is what nearly every filter uses; a protected symbol
// carries a name the grammar has no bare spelling for, `(007)` among them; a
// number is a positional member's index, taken as the decimal it spells; and a
// quoted string is a name written as text, which is the only spelling left for
// a name holding a `)` or a newline.
//
// Reading the first operand rather than the first bare word is what lets an
// index reach a filter. A head may begin with a digit and so a sort level and
// a record field can already be called `0`, but a bare `0` in ARGUMENT
// position is the number zero -- so a filter has to say that a name is what it
// wanted, and it says it by position.
//
// A float and a block name nothing: a float has more than one spelling for one
// value, and a block is a set. Neither does a blob, which no parse produces --
// a caller building an Arg for itself can, and bytes are not a name.
func operandField(what string, a *Arg) (string, error) {
	if a.Value == nil {
		if a.Flag != FlagTrue {
			return "", fmt.Errorf("%s: %q is asserted, and a field is named", what, a.Name)
		}
		return a.Name, nil
	}
	if a.Name != "" {
		return "", fmt.Errorf("%s: names its field first, not %s=", what, a.Name)
	}
	switch v := a.Value; v.Kind {
	case WordValue:
		return v.Word, nil
	case NumberValue:
		if v.IsInt {
			return strconv.FormatInt(v.Int, 10), nil
		}
	case StringValue:
		if !v.Blob {
			return v.Str, nil
		}
	}
	return "", fmt.Errorf("%s: %s is not a field name", what, EncodeValue(a.Value))
}

// encodeFieldName writes a field's name so that the parser reads back the name
// that went out: bare where the grammar can read it as itself, a protected
// symbol where it cannot -- an index among them, since a bare `0` in argument
// position is a number -- and a quoted string for the names a symbol has no
// spelling for.
func encodeFieldName(name string) string {
	if name == "" || strings.ContainsAny(name, ")\n") {
		return quoteString(name)
	}
	return EncodeValue(NewWord(name))
}

// ParseSpec reads a query spec from the arguments of the statement carrying
// it: `new query source=... filter={...} sort={...}`, or the `set` that
// restates it.
func ParseSpec(args []*Arg) (*serval.Spec, error) {
	s := &serval.Spec{}
	for _, a := range args {
		switch a.Name {
		case "source":
			switch {
			case a.Value == nil:
				return nil, fmt.Errorf("source: expected a name")
			case a.Value.Kind == StringValue:
				s.Source = a.Value.Str
			case a.Value.Kind == WordValue:
				s.Source = a.Value.Word
			default:
				return nil, fmt.Errorf("source: expected a name")
			}
		case "fields", "exclude":
			f, err := ParseFields(a.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", a.Name, err)
			}
			if a.Name == "fields" {
				s.Fields = f
			} else {
				s.Exclude = f
			}
		case "filter":
			f, err := ParseFilter(a.Value)
			if err != nil {
				return nil, fmt.Errorf("filter: %w", err)
			}
			s.Filter = f
		case "sort":
			levels, err := ParseSort(a.Value)
			if err != nil {
				return nil, fmt.Errorf("sort: %w", err)
			}
			s.Sort = levels
		}
	}
	return s, nil
}

// Encode renders the spec as the arguments of the statement that carries it.
func EncodeSpec(s *serval.Spec) string {
	var parts []string
	if s.Source != "" {
		parts = append(parts, "source="+quoteString(s.Source))
	}
	if len(s.Fields) > 0 {
		parts = append(parts, "fields="+EncodeRecord(s.Fields))
	}
	if len(s.Exclude) > 0 {
		parts = append(parts, "exclude="+EncodeRecord(s.Exclude))
	}
	if s.Filter != nil {
		parts = append(parts, "filter="+EncodeFilter(s.Filter))
	}
	if len(s.Sort) > 0 {
		parts = append(parts, "sort="+EncodeSort(s.Sort))
	}
	return strings.Join(parts, " ")
}

// ParseScope reads the scope off the same arguments the spec was read from.
//
// The two travel together -- `new query` states the sequence and asks for a run
// of it in one statement, because nobody wants a sequence without wanting
// records of it -- and they are read apart because they are different things
// with different lifetimes: the spec is what the query is, and the scope is
// what this one question wanted.
func ParseScope(args []*Arg) (*serval.Scope, error) {
	s := &serval.Scope{}
	for _, a := range args {
		switch a.Name {
		case "count":
			if a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt {
				return nil, fmt.Errorf("count: expected a whole number")
			}
			if a.Value.Int < 0 {
				return nil, fmt.Errorf("count: %d records is not a number of records", a.Value.Int)
			}
			s.Count = int(a.Value.Int)
		case FromArg:
			if a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt {
				return nil, fmt.Errorf("from: expected a whole number")
			}
			if a.Value.Int < 0 {
				return nil, fmt.Errorf("from: %d is not a position", a.Value.Int)
			}
			s.From = int(a.Value.Int)
		case "after", "until":
			if a.Value == nil {
				return nil, fmt.Errorf("%s: expected an identity", a.Name)
			}
			if a.Value.Kind == BlockValue {
				return nil, fmt.Errorf("%s: an identity is a value, not a block", a.Name)
			}
			if a.Name == "after" {
				s.After = asData(a.Value)
			} else {
				s.Until = asData(a.Value)
			}
		case "reversed":
			if a.Value != nil {
				return nil, fmt.Errorf("reversed: it takes no value")
			}
			s.Reversed = a.Flag == FlagTrue
		}
	}
	if s.After != nil && s.From != 0 {
		// A record is not a position. One names a thing and the other names a
		// place in a sequence, and a scope carrying both has a bug that only
		// ever shows here.
		return nil, fmt.Errorf("from: a scope says where to start with after or" +
			" with from, not both")
	}
	return s, nil
}

// ParseExtend reads the display's declaration off the same arguments.
//
// It is not part of the scope and not part of the spec: the sequence is the
// same sequence and the records wanted are the same records, and this says only
// how the answer may be spelled.
func ParseExtend(args []*Arg) (bool, error) {
	for _, a := range args {
		if a.Name == ExtendArg {
			if a.Value != nil {
				return false, fmt.Errorf("extend: it takes no value")
			}
			return a.Flag == FlagTrue, nil
		}
	}
	return false, nil
}

// Encode renders the scope as the arguments that carry it.
func EncodeScope(s *serval.Scope) string {
	var parts []string
	if s.After != nil {
		parts = append(parts, "after="+EncodeValue(asWire(s.After)))
	}
	if s.Until != nil {
		parts = append(parts, "until="+EncodeValue(asWire(s.Until)))
	}
	if s.From != 0 {
		parts = append(parts, fmt.Sprintf("from=%d", s.From))
	}
	parts = append(parts, fmt.Sprintf("count=%d", s.Count))
	if s.Reversed {
		parts = append(parts, "reversed")
	}
	return strings.Join(parts, " ")
}

// A Result is one `result` statement taken apart: the order declaration, a
// record, and the terminator, any of which may be absent.
//
// All three can ride on one statement. `ordered` is worth saying only before
// the first record, and the terminator only after the last, so an answer of one
// record carries all three and crosses as a single line. Which form was written
// is not something either end has to care about: a reader takes them in that
// order -- order, then record, then end -- whichever statement they arrived on.
type Result struct {
	Ordered bool
	ID      *Value // the record's identity; nil where no record rides here
	Fields  serval.Record
	Whole   bool // `record=` rather than `fields=`

	// Has is how many members the record has altogether, which a subset states
	// and a whole record does not need to: what a whole record carries IS all
	// of them.
	Has serval.Totals

	// Place says this came under `place` rather than `result`: a position, and
	// whatever is known, with no claim about how much. Its fields are true and
	// its silence is not, which is the whole difference -- so it carries no
	// totals, and `record=` on one is refused.
	//
	// A completion riding a place ends the ORDER rather than the scope: every
	// record has now been named, under either verb, and no further one will turn
	// up between two already sent. Which is what a reader needs before it can
	// lay a sequence out, even a sequence of placeholders.
	Place bool

	Complete *serval.Complete
}

// countArg reads one of the two totals: a whole number of members, and never a
// negative one -- there is no such thing as fewer than none.
func countArg(a *Arg) (int, error) {
	if a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt || a.Value.Int < 0 {
		return 0, fmt.Errorf("%s: expected a count of members", a.Name)
	}
	return int(a.Value.Int), nil
}

// ParseResult reads a result from the arguments after the query id.
//
// ParsePlace is the same statement under the other verb, which changes what may
// be in it: a place makes no claim about how much of the record there is, so it
// carries no totals and never `record=`.
func ParseResult(args []*Arg) (*Result, error) { return parseResult(args, false) }
func ParsePlace(args []*Arg) (*Result, error)  { return parseResult(args, true) }

func parseResult(args []*Arg, place bool) (*Result, error) {
	r := &Result{Place: place}
	var done serval.Complete
	ended, counted, exact, totalled := false, false, false, false
	for _, a := range args {
		switch a.Name {
		case OrderedArg:
			r.Ordered = true
		case IDArg:
			if a.Value == nil || a.Value.Kind == BlockValue {
				return nil, fmt.Errorf("id: expected an identity")
			}
			r.ID = a.Value
		case RecordArg, FieldsArg:
			bag, err := ParseFields(a.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", a.Name, err)
			}
			r.Fields = bag
			r.Whole = a.Name == RecordArg
		case MapArg, LenArg:
			n, err := countArg(a)
			if err != nil {
				return nil, err
			}
			if a.Name == MapArg {
				r.Has.Named = n
			} else {
				r.Has.Ordered = n
			}
			counted = true
		case ResultComplete:
			ended = true
		case WatermarkArg:
			if a.Value == nil || a.Value.Kind == BlockValue {
				return nil, fmt.Errorf("watermark: expected an identity")
			}
			done.Watermark = asData(a.Value)
			ended = true
		case TotalArg:
			n, err := countArg(a)
			if err != nil {
				return nil, err
			}
			done.Total.N = n
			ended, totalled = true, true
		case FirstArg:
			n, err := countArg(a)
			if err != nil {
				return nil, err
			}
			done.First = serval.Exactly(n)
			ended = true
		case ExactArg:
			// It strengthens a figure rather than stating one, so it ends
			// nothing on its own.
			exact = true
		case ErrorArg:
			if a.Value == nil || a.Value.Kind != StringValue {
				return nil, fmt.Errorf("error: expected a message")
			}
			done.Error = a.Value.Str
			ended = true
		case string(serval.StopFilled), string(serval.StopJoined), string(serval.StopExhausted):
			done.Stop = serval.Stop(a.Name)
			ended = true
		}
	}
	if r.Fields != nil && r.ID == nil {
		return nil, fmt.Errorf("a record carries an identity; this one has none")
	}
	if counted && r.Whole {
		// A whole record is everything the record has, so a count beside it is
		// either saying that again or contradicting it, and there is no reading
		// where it adds anything.
		return nil, fmt.Errorf("record=: a whole record is its own totals")
	}
	if r.Place && (counted || r.Whole) {
		// Not making that claim is the entire difference between a place and a
		// result. A place that said how much of the record there is would be a
		// subset under the wrong verb, and one that said `record=` would be a
		// whole record under it.
		return nil, fmt.Errorf("place: a place says nothing about how much of the record there is")
	}
	if exact && !totalled {
		return nil, fmt.Errorf("exact: nothing here states a total")
	}
	done.Total.Exact = exact
	if ended {
		r.Complete = &done
	}
	return r, nil
}

// Args renders the result as the arguments after the query id.
func (r *Result) Args() []*Arg {
	var out []*Arg
	if r.Ordered {
		out = append(out, &Arg{Name: OrderedArg, Flag: FlagTrue})
	}
	if r.ID != nil {
		out = append(out, &Arg{Name: IDArg, Value: r.ID})
		what := FieldsArg
		if r.Whole {
			what = RecordArg
		}
		out = append(out, &Arg{Name: what, Value: &Value{Kind: BlockValue, Block: asScript(r.Fields)}})
		if !r.Whole {
			// A count of nothing is not written: most records have no members
			// standing by position, and saying so every time would be noise.
			if r.Has.Named != 0 {
				out = append(out, &Arg{Name: MapArg, Value: NewInt(int64(r.Has.Named))})
			}
			if r.Has.Ordered != 0 {
				out = append(out, &Arg{Name: LenArg, Value: NewInt(int64(r.Has.Ordered))})
			}
		}
	}
	if c := r.Complete; c != nil {
		out = append(out, &Arg{Name: ResultComplete, Flag: FlagTrue})
		if c.Watermark != nil {
			out = append(out, &Arg{Name: WatermarkArg, Value: asWire(c.Watermark)})
		}
		if c.Stop != "" {
			out = append(out, &Arg{Name: string(c.Stop), Flag: FlagTrue})
		}
		if c.First.Exact {
			// Only when it is known, and what is written is exact. Unknown does
			// not cross: a reader told nothing stays where it was, and there is
			// no weaker thing to say about a position.
			out = append(out, &Arg{Name: FirstArg, Value: NewInt(int64(c.First.N))})
		}
		if !c.Total.Nothing() {
			// A figure of nothing is not written: `total=0` alone says only
			// what is true of every sequence there is. A sequence counted and
			// found empty is `total=0 exact`, and does cross.
			out = append(out, &Arg{Name: TotalArg, Value: NewInt(int64(c.Total.N))})
			if c.Total.Exact {
				out = append(out, &Arg{Name: ExactArg, Flag: FlagTrue})
			}
		}
		if c.Error != "" {
			out = append(out, Named(ErrorArg, c.Error))
		}
	}
	return out
}

// ParseFilter reads a filter tree from a block value. A block is an AND.
func ParseFilter(v *Value) (*serval.Filter, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block")
	}
	return parseFilterBlock(v.Block, serval.OpAnd)
}

func parseFilterBlock(script *Script, op string) (*serval.Filter, error) {
	node := &serval.Filter{Op: op}
	for _, st := range script.Statements {
		child, err := parsePredicate(st)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, child)
	}
	return node, nil
}

// parseID reads `id <identity> <identity> ...`.
//
// Every argument is a value, with no field in front of them: an identity is
// not a field, so there is nothing to name. That is also what keeps it apart
// from `in key ...`, which is a question about a field that happens to be
// called key.
func parseID(st *Statement) (*serval.Filter, error) {
	f := &serval.Filter{Op: serval.OpID}
	for _, a := range st.Args {
		if a.Value == nil {
			return nil, fmt.Errorf("id: %q names no identity; write the identities as values", a.Name)
		}
		if a.Name != "" {
			return nil, fmt.Errorf("id: takes identities, not %s=", a.Name)
		}
		if a.Value.Kind == BlockValue {
			return nil, fmt.Errorf("id: an identity is a value, not a block")
		}
		f.Values = append(f.Values, asData(a.Value))
	}
	if len(f.Values) == 0 {
		return nil, fmt.Errorf("id: takes at least one identity")
	}
	return f, nil
}

func parsePredicate(st *Statement) (*serval.Filter, error) {
	switch st.Verb {
	case serval.OpAnd, serval.OpOr, serval.OpNot:
		if len(st.Args) != 1 || st.Args[0].Value == nil || st.Args[0].Value.Kind != BlockValue {
			return nil, fmt.Errorf("%s: takes one block", st.Verb)
		}
		inner, err := parseFilterBlock(st.Args[0].Value.Block, st.Verb)
		if err != nil {
			return nil, err
		}
		if st.Verb == serval.OpNot && len(inner.Children) == 0 {
			return nil, fmt.Errorf("not: takes something to negate")
		}
		return inner, nil
	case serval.OpID:
		return parseID(st)
	case serval.OpEq, serval.OpNe, serval.OpLt, serval.OpLe, serval.OpGt, serval.OpGe, serval.OpIn, serval.OpContains, serval.OpStarts, serval.OpEnds,
		serval.OpHas, serval.OpLacks:
	default:
		return nil, fmt.Errorf("no filter operator called %q", st.Verb)
	}

	f := &serval.Filter{Op: st.Verb}
	named := false
	for _, a := range st.Args {
		switch {
		case a.Name == "collate" && a.Value != nil:
			// `collate=` is the one name a predicate reserves, and it reserves
			// it WITH ITS VALUE. A bare `collate` is a word like any other
			// bare word here, so a field can be called that and a dangling
			// one is caught by the operand count rather than by its spelling.
			if a.Value.Kind != WordValue {
				return nil, fmt.Errorf("%s: collate= expects a word", st.Verb)
			}
			f.Collate = a.Value.Word
		case !named:
			// The first operand is the field, in whatever form it was
			// written. Naming it by position is what lets a filter read as a
			// filter rather than naming an argument for every operand -- and
			// what lets a name that is not a bare word be one.
			name, err := operandField(st.Verb, a)
			if err != nil {
				return nil, err
			}
			f.Field, named = name, true
		case a.Value != nil && a.Value.Kind == BlockValue && f.Op != serval.OpIn:
			// A comparison takes a simple value. A block is a set, and a set is
			// only something `in` can be asked about.
			return nil, fmt.Errorf("%s %s: compares against a value, not a block",
				st.Verb, f.Field)
		case a.Value != nil && a.Value.Kind == BlockValue && f.Op == serval.OpIn:
			// A set of words, which is what a block can hold: every statement
			// in it is one bare name.
			for _, item := range a.Value.Block.Statements {
				if item.Verb == "" || len(item.Args) != 0 {
					return nil, fmt.Errorf("in: a set holds bare names; write other values after the field")
				}
				f.Values = append(f.Values, serval.NewSymbol(item.Verb))
			}
		default:
			v, err := operandValue(st.Verb, a)
			if err != nil {
				return nil, err
			}
			f.Values = append(f.Values, asData(v))
		}
	}
	if f.Field == "" {
		return nil, fmt.Errorf("%s: names no field", st.Verb)
	}
	if f.Op == serval.OpHas || f.Op == serval.OpLacks {
		if len(f.Values) > 0 {
			return nil, fmt.Errorf("%s %s: asks whether the field is there, and takes no value",
				f.Op, f.Field)
		}
		return f, nil
	}
	if len(f.Values) == 0 {
		return nil, fmt.Errorf("%s %s: nothing to compare against", st.Verb, f.Field)
	}
	if f.Op != serval.OpIn && len(f.Values) > 1 {
		return nil, fmt.Errorf("%s %s: compares against one value, not %d", st.Verb, f.Field, len(f.Values))
	}
	return f, nil
}

// Encode renders a filter node as wire text: a block for the top of a tree, a
// statement for anything inside one.
//
// **Only an AND is flattened into the outer block**, because a block IS an and
// wherever one appears -- so writing an `or`'s children straight into it would
// turn the disjunction into a conjunction, and a top-level `not` into its
// opposite. They are written as the statement they are, inside the block the top
// of a filter always is.
//
// The reading back is an and of one or, which means what the or meant: an
// operator of one operand is that operand.
func EncodeFilter(f *serval.Filter) string {
	if f == nil {
		return "{}"
	}
	if f.Op == serval.OpAnd {
		parts := make([]string, 0, len(f.Children))
		for _, c := range f.Children {
			parts = append(parts, encodeFilterStatement(c))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, "; ") + " }"
	}
	if (f.Op == serval.OpOr || f.Op == serval.OpNot) && len(f.Children) == 0 {
		// Nothing to join and nothing to negate, which is no filter rather than an
		// operator with an empty block after it.
		return "{}"
	}
	return "{ " + encodeFilterStatement(f) + " }"
}

// encodeStatement renders one node as a statement inside a block.
func encodeFilterStatement(f *serval.Filter) string {
	switch f.Op {
	case serval.OpAnd, serval.OpOr, serval.OpNot:
		return f.Op + " " + encodeFilterBlock(f)
	}
	var sb strings.Builder
	sb.WriteString(f.Op)
	if f.Op != serval.OpID {
		// Every other operator names the field it tests. This one tests an
		// identity, which is not a field and has no name to write.
		sb.WriteByte(' ')
		sb.WriteString(encodeFieldName(f.Field))
	}
	for _, v := range f.Values {
		sb.WriteByte(' ')
		sb.WriteString(EncodeValue(asWire(v)))
	}
	if f.Collate != "" {
		sb.WriteString(" collate=")
		sb.WriteString(f.Collate)
	}
	return sb.String()
}

func encodeFilterBlock(f *serval.Filter) string {
	parts := make([]string, 0, len(f.Children))
	for _, c := range f.Children {
		parts = append(parts, encodeFilterStatement(c))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// ParseSort reads sort levels from a block value: one statement per level,
// naming a field and saying which way and under which collation.
func ParseSort(v *Value) ([]serval.SortLevel, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block")
	}
	var out []serval.SortLevel
	for _, st := range v.Block.Statements {
		if st.Verb == "" {
			return nil, fmt.Errorf("a level names a field")
		}
		level := serval.SortLevel{Field: st.Verb}
		for _, a := range st.Args {
			switch {
			case a.Name == "collate" && a.Value != nil && a.Value.Kind == WordValue:
				level.Collation = a.Value.Word
			case a.Value != nil:
				return nil, fmt.Errorf("%s: no argument called %q", st.Verb, a.Name)
			case a.Name == "desc":
				level.Descending = a.Flag == FlagTrue
			case a.Name == "asc":
				level.Descending = a.Flag != FlagTrue
			case a.Name == serval.CollateExact || a.Name == serval.CollateFold || a.Name == serval.CollateNatural:
				level.Collation = a.Name
			default:
				return nil, fmt.Errorf("%s: %q says nothing about a sort level", st.Verb, a.Name)
			}
		}
		out = append(out, level)
	}
	return out, nil
}

// EncodeSort renders sort levels as a block.
func EncodeSort(levels []serval.SortLevel) string {
	parts := make([]string, 0, len(levels))
	for _, l := range levels {
		s := l.Field
		if l.Collation != "" {
			s += " " + l.Collation
		}
		if l.Descending {
			s += " desc"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}
