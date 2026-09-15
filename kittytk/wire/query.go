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
	"strings"

	"github.com/phroun/serval"
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

// EncodeRecord renders a record as the block that carries it: one statement per
// field, the name first and its value, if it has one, after.
func EncodeRecord(r serval.Record) string {
	return EncodeValue(&Value{Kind: BlockValue, Block: asScript(r)})
}

// ParseFields reads a field bag from a block value.
func ParseFields(v *Value) (serval.Record, error) {
	if v == nil || v.Kind != BlockValue {
		return nil, fmt.Errorf("expected a block of fields")
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
	return s, nil
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

	Complete *serval.Complete
}

// ParseResult reads a result from the arguments after the query id.
func ParseResult(args []*Arg) (*Result, error) {
	r := &Result{}
	var done serval.Complete
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
			done.Watermark = asData(a.Value)
			ended = true
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
	}
	if c := r.Complete; c != nil {
		out = append(out, &Arg{Name: ResultComplete, Flag: FlagTrue})
		if c.Watermark != nil {
			out = append(out, &Arg{Name: WatermarkArg, Value: asWire(c.Watermark)})
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
func EncodeFilter(f *serval.Filter) string {
	if f == nil {
		return "{}"
	}
	switch f.Op {
	case serval.OpAnd, serval.OpOr, serval.OpNot:
		parts := make([]string, 0, len(f.Children))
		for _, c := range f.Children {
			parts = append(parts, encodeFilterStatement(c))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, "; ") + " }"
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
		sb.WriteString(f.Field)
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
