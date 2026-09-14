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
// docs/sort-and-filter.md defines the comparison the sort and filter stand on.

import (
	"fmt"
	"strings"
)

// The verb a display opens and refills a query with, and the verb the
// application answers it with.
//
// Three pairs, and nothing carries two of them: `query` is answered by
// `result`, `ask` by `answer`, and `sub` -- or an object's mere existence --
// by `event`. So a record arriving for a list can never be mistaken for
// something a subscription raised.
const (
	QueryVerb  = "query"  // `new query source="files" sort={ name } count=30`
	ResultVerb = "result" // `result 9 id=42 record={ ... }`

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
)

// A Stop is why a scope ended, which the asker cannot work out for itself.
//
// A scope that filled and one that ran out of records look identical from the
// far end -- both are a run of records that stopped -- and they mean opposite
// things about whether there is any point asking again.
type Stop string

const (
	// StopFilled: the count was reached. There is more past the watermark.
	StopFilled Stop = "filled"

	// StopJoined: the walk reached `until`, so what the asker holds on this
	// side and what it holds on the other are now one run.
	StopJoined Stop = "joined"

	// StopExhausted: there are no more records this way. Nothing past the end
	// to be complete up to, so there is no watermark either.
	StopExhausted Stop = "exhausted"
)

// The operators a filter is built from.
const (
	OpAnd      = "and"
	OpOr       = "or"
	OpNot      = "not"
	OpEq       = "eq"
	OpNe       = "ne"
	OpLt       = "lt"
	OpLe       = "le"
	OpGt       = "gt"
	OpGe       = "ge"
	OpIn       = "in"
	OpContains = "contains"
	OpStarts   = "starts"
	OpEnds     = "ends"

	// OpHas and OpLacks ask whether a record carries a field at all, and take
	// no value. Every other operator compares one, and a field holding
	// something with no order of its own -- a nested list -- cannot be
	// compared, so presence needs an operator that does not try.
	OpHas   = "has"
	OpLacks = "lacks"

	// OpID matches a record's identity against a set of them, the way `in`
	// matches a field against a set of values. It names no field, because an
	// identity is not one: it travels beside a record's fields rather than
	// among them, and a field called `key` is a field like any other.
	//
	//	filter={ id (left/1) (left/note) }
	OpID = "id"
)

// Fields is a bag of named values, and one shape serves three jobs: the fields
// a record carries, the position a boundary stands at, and -- with the values
// left out -- the bare list of fields a query asks for.
//
// It is written as a block of one statement per field, the field name first
// and its value, if it has one, after: `{ name "src/parser.go"; size 1024 }`.
type Fields []*Arg

// Get is the value under a name, or nil where the bag does not name it. A
// field present with no value reads as nil too: a bare name is a name, not a
// value of its own.
func (f Fields) Get(name string) *Value {
	for _, a := range f {
		if a.Name == name {
			return a.Value
		}
	}
	return nil
}

// Has reports whether the bag names a field at all, valued or not.
func (f Fields) Has(name string) bool {
	for _, a := range f {
		if a.Name == name {
			return true
		}
	}
	return false
}

// Names lists the fields in the order they were written.
func (f Fields) Names() []string {
	out := make([]string, 0, len(f))
	for _, a := range f {
		out = append(out, a.Name)
	}
	return out
}

// Block renders the bag as a block value, for a statement to carry: one
// statement per field, the name first and its value, if it has one, after.
func (f Fields) Block() *Value {
	script := &Script{}
	for _, a := range f {
		st := &Statement{Verb: a.Name}
		if a.Value != nil {
			st.Args = []*Arg{{Value: a.Value}}
		}
		script.Statements = append(script.Statements, st)
	}
	return &Value{Kind: BlockValue, Block: script}
}

// Encode renders the bag as the text of that block.
func (f Fields) Encode() string { return EncodeValue(f.Block()) }

// ParseFields reads a field bag from a block value.
func ParseFields(v *Value) (Fields, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block of fields")
	}
	var out Fields
	for _, st := range v.Block.Statements {
		if st.Verb == "" {
			return nil, fmt.Errorf("a field is a name, and %q is not one", EncodeStatement(st))
		}
		switch len(st.Args) {
		case 0:
			out = append(out, &Arg{Name: st.Verb, Flag: FlagTrue})
		case 1:
			v, err := operandValue(st.Verb, st.Args[0])
			if err != nil {
				return nil, err
			}
			out = append(out, &Arg{Name: st.Verb, Value: v})
		default:
			return nil, fmt.Errorf("%s: a field carries one value, not %d", st.Verb, len(st.Args))
		}
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

// A Filter is one node of the tree a filter block parses to: a predicate over
// one field, or an and/or/not over other nodes.
//
// A block is an AND, so the top of a parsed filter is always an OpAnd -- one
// shape to walk, whether the filter held one predicate or twenty.
type Filter struct {
	Op       string
	Field    string    // predicates: the field being tested
	Values   []*Value  // what it is tested against; more than one only for `in`
	Collate  string    // text predicates: the collation, "" for the default
	Children []*Filter // and, or, not
}

// Value is the single operand of a comparison, and nil where there is none.
func (f *Filter) Value() *Value {
	if f == nil || len(f.Values) == 0 {
		return nil
	}
	return f.Values[0]
}

// A SortLevel is one level of a sort: which field, which way, and -- for
// strings -- under which collation.
type SortLevel struct {
	Field string
	Level
}

// Reverse turns every level over, which is what walking a sequence from its
// end amounts to: the same records, in the opposite order, with the level that
// settles ties turned over as well so that nothing is left facing the way it
// was.
func Reverse(levels []Level) []Level {
	out := make([]Level, len(levels))
	for i, l := range levels {
		out[i] = l
		out[i].Descending = !l.Descending
	}
	return out
}

// Levels drops the field names, leaving what CompareLevels compares tuples by.
func Levels(levels []SortLevel) []Level {
	out := make([]Level, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.Level)
	}
	return out
}

// A Spec says what sequence a query names: which records, in which order.
//
// It is stated once, when the query is made, and never again. A query is the
// sequence it was opened with and nothing restates it -- a different filter or
// a different sort is a different sequence, which is a different query, opened
// alongside this one and taking its place.
type Spec struct {
	Source  string
	Fields  Fields // the fields asked for; empty means whatever the record has
	Exclude Fields // the fields not wanted, valued the same way
	Filter  *Filter
	Sort    []SortLevel
}

// A Scope is the run of records a query asks for: where to start, which way to
// walk, how many, and where the asker's own knowledge picks up again.
//
// It is not a filter and it names no field. The sequence is already decided by
// the spec, and a scope only says which part of it to read -- so a source
// prepares one ordering and serves every scope of it cheaply, rather than
// preparing a new one because the reader scrolled.
//
// After and Until are identities, not positions. An identity means something
// only to the source that issued it, which is why a source made of several
// others never passes one down: it hands each of them that one's own.
type Scope struct {
	// After is the record to start past: the asker holds it already. Nil
	// starts at the first record in walk order.
	After *Value

	// Until is the record to stop before: the asker holds that one too, and
	// everything beyond it, so a walk that reaches it has joined two runs the
	// asker held separately. Nil walks until the count is reached or the
	// records run out.
	Until *Value

	// Count is how many records are wanted.
	Count int

	// Reversed walks the sequence from its end rather than its beginning.
	//
	// Every level turns over, the one the sort does not write included: an
	// identity settles what the named levels leave equal, and a sequence read
	// backwards settles it backwards too. That is what makes this the exact
	// mirror -- `sort={ size desc }` turns one level over and leaves ties
	// facing the way they were, which is a different sequence again.
	//
	// It belongs to the scope rather than the sequence because it costs
	// nothing: one prepared ordering is read either way, where a reversed
	// *sequence* would be a second ordering of the same records. And it names
	// no field, so it is the one way to turn over a sequence whose records are
	// read in a way that cannot name their identity at all.
	Reversed bool
}

// ParseSpec reads a query spec from the arguments of the statement carrying
// it: `new query source=... filter={...} sort={...}`, or the `set` that
// restates it.
func ParseSpec(args []*Arg) (*Spec, error) {
	s := &Spec{}
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
func (s *Spec) Encode() string {
	var parts []string
	if s.Source != "" {
		parts = append(parts, "source="+quoteString(s.Source))
	}
	if len(s.Fields) > 0 {
		parts = append(parts, "fields="+s.Fields.Encode())
	}
	if len(s.Exclude) > 0 {
		parts = append(parts, "exclude="+s.Exclude.Encode())
	}
	if s.Filter != nil {
		parts = append(parts, "filter="+s.Filter.Encode())
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
func ParseScope(args []*Arg) (*Scope, error) {
	s := &Scope{}
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
		case "after", "until":
			if a.Value == nil {
				return nil, fmt.Errorf("%s: expected an identity", a.Name)
			}
			if a.Value.Kind == BlockValue {
				return nil, fmt.Errorf("%s: an identity is a value, not a block", a.Name)
			}
			if a.Name == "after" {
				s.After = a.Value
			} else {
				s.Until = a.Value
			}
		case "reversed":
			if a.Value != nil {
				return nil, fmt.Errorf("reversed: it takes no value")
			}
			s.Reversed = a.Flag == FlagTrue
		}
	}
	return s, nil
}

// Encode renders the scope as the arguments that carry it.
func (s *Scope) Encode() string {
	var parts []string
	if s.After != nil {
		parts = append(parts, "after="+EncodeValue(s.After))
	}
	if s.Until != nil {
		parts = append(parts, "until="+EncodeValue(s.Until))
	}
	parts = append(parts, fmt.Sprintf("count=%d", s.Count))
	if s.Reversed {
		parts = append(parts, "reversed")
	}
	return strings.Join(parts, " ")
}

// A Complete ends a scope: which of the three ways it ended, and how far the
// answer is complete.
//
// Watermark says there is nothing between where the scope was asked from and
// that record that the asker does not now have. StopExhausted carries none,
// because there is no point past the end to be complete up to.
type Complete struct {
	Watermark *Value
	Stop      Stop

	// Error is a refusal, which is an answer: this scope cannot be produced,
	// the records are gone, the connection carrying the question broke.
	// Whoever asked carries on with what it has.
	Error string
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
	Ordered  bool
	ID       *Value // the record's identity; nil where no record rides here
	Fields   Fields
	Whole    bool // `record=` rather than `fields=`
	Complete *Complete
}

// ParseResult reads a result from the arguments after the query id.
func ParseResult(args []*Arg) (*Result, error) {
	r := &Result{}
	var done Complete
	ended := false
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
		case ResultComplete:
			ended = true
		case WatermarkArg:
			if a.Value == nil || a.Value.Kind == BlockValue {
				return nil, fmt.Errorf("watermark: expected an identity")
			}
			done.Watermark = a.Value
			ended = true
		case ErrorArg:
			if a.Value == nil || a.Value.Kind != StringValue {
				return nil, fmt.Errorf("error: expected a message")
			}
			done.Error = a.Value.Str
			ended = true
		case string(StopFilled), string(StopJoined), string(StopExhausted):
			done.Stop = Stop(a.Name)
			ended = true
		}
	}
	if r.Fields != nil && r.ID == nil {
		return nil, fmt.Errorf("a record carries an identity; this one has none")
	}
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
		out = append(out, &Arg{Name: what, Value: r.Fields.Block()})
	}
	if c := r.Complete; c != nil {
		out = append(out, &Arg{Name: ResultComplete, Flag: FlagTrue})
		if c.Watermark != nil {
			out = append(out, &Arg{Name: WatermarkArg, Value: c.Watermark})
		}
		if c.Stop != "" {
			out = append(out, &Arg{Name: string(c.Stop), Flag: FlagTrue})
		}
		if c.Error != "" {
			out = append(out, Named(ErrorArg, c.Error))
		}
	}
	return out
}

// ParseFilter reads a filter tree from a block value. A block is an AND.
func ParseFilter(v *Value) (*Filter, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block")
	}
	return parseFilterBlock(v.Block, OpAnd)
}

func parseFilterBlock(script *Script, op string) (*Filter, error) {
	node := &Filter{Op: op}
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
func parseID(st *Statement) (*Filter, error) {
	f := &Filter{Op: OpID}
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
		f.Values = append(f.Values, a.Value)
	}
	if len(f.Values) == 0 {
		return nil, fmt.Errorf("id: takes at least one identity")
	}
	return f, nil
}

func parsePredicate(st *Statement) (*Filter, error) {
	switch st.Verb {
	case OpAnd, OpOr, OpNot:
		if len(st.Args) != 1 || st.Args[0].Value == nil || st.Args[0].Value.Kind != BlockValue {
			return nil, fmt.Errorf("%s: takes one block", st.Verb)
		}
		inner, err := parseFilterBlock(st.Args[0].Value.Block, st.Verb)
		if err != nil {
			return nil, err
		}
		if st.Verb == OpNot && len(inner.Children) == 0 {
			return nil, fmt.Errorf("not: takes something to negate")
		}
		return inner, nil
	case OpID:
		return parseID(st)
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpIn, OpContains, OpStarts, OpEnds,
		OpHas, OpLacks:
	default:
		return nil, fmt.Errorf("no filter operator called %q", st.Verb)
	}

	f := &Filter{Op: st.Verb}
	for i, a := range st.Args {
		switch {
		case a.Name == "collate":
			if a.Value == nil || a.Value.Kind != WordValue {
				return nil, fmt.Errorf("%s: collate= expects a word", st.Verb)
			}
			f.Collate = a.Value.Word
		case i == 0:
			// The field, written bare, which is how a filter reads as a
			// filter rather than naming an argument for every operand.
			if a.Value != nil || a.Flag != FlagTrue {
				return nil, fmt.Errorf("%s: names no field", st.Verb)
			}
			f.Field = a.Name
		case a.Value != nil && a.Value.Kind == BlockValue && f.Op != OpIn:
			// A comparison takes a simple value. A block is a set, and a set is
			// only something `in` can be asked about.
			return nil, fmt.Errorf("%s %s: compares against a value, not a block",
				st.Verb, f.Field)
		case a.Value != nil && a.Value.Kind == BlockValue && f.Op == OpIn:
			// A set of words, which is what a block can hold: every statement
			// in it is one bare name.
			for _, item := range a.Value.Block.Statements {
				if item.Verb == "" || len(item.Args) != 0 {
					return nil, fmt.Errorf("in: a set holds bare names; write other values after the field")
				}
				f.Values = append(f.Values, NewWord(item.Verb))
			}
		default:
			v, err := operandValue(st.Verb, a)
			if err != nil {
				return nil, err
			}
			f.Values = append(f.Values, v)
		}
	}
	if f.Field == "" {
		return nil, fmt.Errorf("%s: names no field", st.Verb)
	}
	if f.Op == OpHas || f.Op == OpLacks {
		if len(f.Values) > 0 {
			return nil, fmt.Errorf("%s %s: asks whether the field is there, and takes no value",
				f.Op, f.Field)
		}
		return f, nil
	}
	if len(f.Values) == 0 {
		return nil, fmt.Errorf("%s %s: nothing to compare against", st.Verb, f.Field)
	}
	if f.Op != OpIn && len(f.Values) > 1 {
		return nil, fmt.Errorf("%s %s: compares against one value, not %d", st.Verb, f.Field, len(f.Values))
	}
	return f, nil
}

// Encode renders a filter node as wire text: a block for the top of a tree, a
// statement for anything inside one.
func (f *Filter) Encode() string {
	if f == nil {
		return "{}"
	}
	switch f.Op {
	case OpAnd, OpOr, OpNot:
		parts := make([]string, 0, len(f.Children))
		for _, c := range f.Children {
			parts = append(parts, c.encodeStatement())
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, "; ") + " }"
	}
	return "{ " + f.encodeStatement() + " }"
}

// encodeStatement renders one node as a statement inside a block.
func (f *Filter) encodeStatement() string {
	switch f.Op {
	case OpAnd, OpOr, OpNot:
		return f.Op + " " + f.encodeBlock()
	}
	var sb strings.Builder
	sb.WriteString(f.Op)
	if f.Op != OpID {
		// Every other operator names the field it tests. This one tests an
		// identity, which is not a field and has no name to write.
		sb.WriteByte(' ')
		sb.WriteString(f.Field)
	}
	for _, v := range f.Values {
		sb.WriteByte(' ')
		sb.WriteString(EncodeValue(v))
	}
	if f.Collate != "" {
		sb.WriteString(" collate=")
		sb.WriteString(f.Collate)
	}
	return sb.String()
}

func (f *Filter) encodeBlock() string {
	parts := make([]string, 0, len(f.Children))
	for _, c := range f.Children {
		parts = append(parts, c.encodeStatement())
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// ParseSort reads sort levels from a block value: one statement per level,
// naming a field and saying which way and under which collation.
func ParseSort(v *Value) ([]SortLevel, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block")
	}
	var out []SortLevel
	for _, st := range v.Block.Statements {
		if st.Verb == "" {
			return nil, fmt.Errorf("a level names a field")
		}
		level := SortLevel{Field: st.Verb}
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
			case a.Name == CollateExact || a.Name == CollateFold || a.Name == CollateNatural:
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
func EncodeSort(levels []SortLevel) string {
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
