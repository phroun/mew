package wire

// A query, in structured form.
//
// An application hosts a query: one filter and one sort over its own records,
// which a display fills windows out of as somebody scrolls. The wire carries
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
	QueryVerb  = "query"  // `query 9 from={ ... } have=25 need=30`
	ResultVerb = "result" // `result 9 fields={ ... }`
	KeyField   = "key"    // the record key, inside a field bag

	// ResultComplete ends a window: everything for it has been sent. A result
	// without it carries a record.
	ResultComplete = "complete"
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

// Key is the record key: the field every record and every boundary carries,
// because it is the sort's implicit last level and what makes a position mean
// exactly one record.
func (f Fields) Key() *Value { return f.Get(KeyField) }

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

// Levels drops the field names, leaving what CompareLevels compares tuples by.
func Levels(levels []SortLevel) []Level {
	out := make([]Level, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.Level)
	}
	return out
}

// A Spec says what sequence a query names. It is stated when the query is
// announced and restated when the display changes it, which is a new
// generation of the same query rather than a new query.
type Spec struct {
	Source  string
	Fields  Fields // the fields asked for; empty means whatever the record has
	Exclude Fields // the fields not wanted, valued the same way
	Filter  *Filter
	Sort    []SortLevel
}

// A Fill is one window of the sequence, asked for.
//
// From and To are boundaries: where the display's own knowledge starts and how
// far it runs. Both are empty at the beginning of the sequence. Have is how
// much of the window the display can fill from what it already holds, and Need
// is how many rows the window is.
//
// Nothing stamps it. The application's results and its replies travel one
// ordered stream, so a window's results are the ones between the reply that
// accepted it and the result that completes it -- which is also what separates
// the generation before a re-sort from the one after it.
type Fill struct {
	From   Fields // empty: the start of the sequence
	To     Fields // empty: nothing is known past From
	Have   int
	Need   int
	Fields Fields // the fields wanted for this window; empty means the spec's
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

// ParseFill reads a fill request from the arguments after the question word.
func ParseFill(args []*Arg) (*Fill, error) {
	f := &Fill{}
	for _, a := range args {
		switch a.Name {
		case "have", "need":
			if a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt {
				return nil, fmt.Errorf("%s: expected a whole number", a.Name)
			}
			if a.Name == "have" {
				f.Have = int(a.Value.Int)
			} else {
				f.Need = int(a.Value.Int)
			}
		case "from", "to", "fields":
			bag, err := ParseFields(a.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", a.Name, err)
			}
			switch a.Name {
			case "from":
				f.From = bag
			case "to":
				f.To = bag
			case "fields":
				f.Fields = bag
			}
		}
	}
	return f, nil
}

// Encode renders the fill as the arguments after the question word.
func (f *Fill) Encode() string {
	var parts []string
	if len(f.From) > 0 {
		parts = append(parts, "from="+f.From.Encode())
	}
	if len(f.To) > 0 {
		parts = append(parts, "to="+f.To.Encode())
	}
	parts = append(parts, fmt.Sprintf("have=%d", f.Have), fmt.Sprintf("need=%d", f.Need))
	if len(f.Fields) > 0 {
		parts = append(parts, "fields="+f.Fields.Encode())
	}
	return strings.Join(parts, " ")
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
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpIn, OpContains, OpStarts, OpEnds:
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
	sb.WriteByte(' ')
	sb.WriteString(f.Field)
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
