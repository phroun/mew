package source

// Reading a PSL list as records.
//
// A PSL list holds two independent collections: an ordered sequence of items,
// and a keyed map that sits outside that sequence entirely. Both are records
// here. An item's key is its index and a keyed member's key is its name, and
// because an integer ranks below a string in the comparison core, the two
// spaces fall into one total order with no rule of their own: the items first,
// in their order, then the names.
//
// This is a flat reading. A `_bundle` member is a record like any other, and
// nothing here follows an include, applies an amendment or resolves a hash --
// that is a layer above this one, and it is built on this rather than into it
// (docs/data-sources-and-bundles.md).
//
// What it is for is access: a window of a sorted, filtered sequence, found
// without walking the records that precede it. The sequence is ordered once per
// spec, and a window is a binary search for the boundary and a walk forward as
// far as the window is long -- so scrolling to the end of a large list costs
// what scrolling to the start of it costs.

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/phroun/kittytk/wire"
	"github.com/phroun/pawscript"
)

// orderingsKept is how many orderings of one source are held at once. Each is a
// column header somebody clicked, and clicking back is the next thing they do,
// so the previous few are worth keeping and an unbounded pile of them is not.
const orderingsKept = 4

// ValueField is the record itself, as a field name. It is the companion of
// wire.KeyField: a record that is a bare string has a key and a value and
// nothing else, and one that is a list has its members as well.
const ValueField = "value"

// A Reading is how a record's contents are named. Both are useful and neither
// is a subset of the other's behaviour, so it is said once, when the source is
// made, rather than guessed per record.
type Reading int

const (
	// Whole exposes the record entire. `key` is its key and `value` is its
	// value, and every member wears a dot: `.size` is the member called size,
	// `.0` is the item at position 0.
	//
	// Nothing can shadow anything, so a member called `key` -- which the bundle
	// format's own `_bundle: (key: "figaro")` has -- is `.key` and is reachable
	// like any other. It is the reading for data whose records are not all the
	// same shape, and for anything that has to survive a round trip.
	Whole Reading = iota

	// Members exposes the members alone, under their own names: `size`, not
	// `.size`. It is the shorter reading, and it suits a source whose records
	// are all lists of named fields -- which is most of them.
	//
	// It is deliberately not complete. The record's key and its value cannot be
	// named; positions cannot be named at all, the wire grammar reading a bare
	// `0` as a number rather than as a name; and a member called `key` is
	// neither reachable nor sent, because the field bag already carries one and
	// two of them would make the record's identity ambiguous.
	Members
)

// A PSL is a data source backed by one parsed PSL list.
//
// It is read-only and its records do not move, so everything computed from them
// stays true: an ordering is built once per spec and reused for every window
// drawn from it.
type PSL struct {
	recs    []pslRecord
	reading Reading

	mu     sync.Mutex
	cache  map[string]*ordering
	recent []string // cache keys, oldest first
}

// ParsePSL reads PSL text and presents it as a data source.
func ParsePSL(text string, reading Reading) (*PSL, error) {
	n, err := pawscript.ParsePSL(text)
	if err != nil {
		return nil, err
	}
	return NewPSL(n, reading), nil
}

// NewPSL presents an already-parsed PSL list as a data source: its items as
// records keyed by index, its keyed members as records keyed by name.
func NewPSL(n *pawscript.PSLNode, reading Reading) *PSL {
	p := &PSL{reading: reading, cache: map[string]*ordering{}}
	p.recs = make([]pslRecord, 0, n.Len()+len(n.Map()))
	for i := 0; i < n.Len(); i++ {
		v, _ := n.Item(i)
		p.recs = append(p.recs, pslRecord{reading: reading, key: wire.NewInt(int64(i)), value: v})
	}
	for _, k := range sortedKeys(n) {
		v, _ := n.Get(k)
		p.recs = append(p.recs, pslRecord{reading: reading, key: wire.NewString(k), value: v})
	}
	return p
}

// Len is how many records the source holds, before any filter.
func (p *PSL) Len() int { return len(p.recs) }

// Open states a sequence over the records: one filter, one sort.
func (p *PSL) Open(spec *wire.Spec) (View, error) {
	if spec == nil {
		spec = &wire.Spec{}
	}
	if err := supported(spec.Sort); err != nil {
		return nil, err
	}
	return &pslView{src: p, spec: spec, ord: p.order(spec)}, nil
}

// supported refuses a sort this source cannot produce exactly. A collation it
// does not carry would yield an order that is nearly right, which is worse than
// a refusal: a refusal is recoverable and says what is wrong.
func supported(levels []wire.SortLevel) error {
	for _, l := range levels {
		switch l.Collation {
		case "", wire.CollateExact, wire.CollateFold, wire.CollateNatural:
		default:
			return fmt.Errorf("sort %s: no collation called %q", l.Field, l.Collation)
		}
	}
	return nil
}

// A pslRecord is one record: its key, and the PSL value it stands for.
type pslRecord struct {
	reading Reading
	key     *wire.Value
	value   any // *pawscript.PSLNode for a list, the value itself otherwise
}

// Field is one of the record's values by name, as this source's reading names
// them: `key`, `value` and `.member` under Whole, and `member` alone under
// Members.
func (r pslRecord) Field(name string) *wire.Value {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList || name == wire.KeyField {
			return nil
		}
		if v, ok := node.Get(name); ok {
			return pslValue(v)
		}
		return nil
	}

	switch {
	case name == wire.KeyField:
		return r.key
	case name == ValueField:
		return pslValue(r.value)
	case len(name) < 2 || name[0] != '.':
		return nil
	}
	if !isList {
		return nil
	}
	if i, digits := itemIndex(name); digits {
		if v, ok := node.Item(i); ok {
			return pslValue(v)
		}
		return nil
	}
	if v, ok := node.Get(name[1:]); ok {
		return pslValue(v)
	}
	return nil
}

// fields is everything the record carries, once. The key is not among them
// because it travels beside them.
//
// Under Whole that is a list's members under their own names, or a bare value
// under `value`. Under Members it is the keyed members alone: a record's
// positions have no name to go out under, and a member called `key` would
// collide with the key the bag already carries.
func (r pslRecord) fields() wire.Fields {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList {
			return nil
		}
		out := make(wire.Fields, 0, len(node.Map()))
		for _, k := range sortedKeys(node) {
			if k == wire.KeyField {
				continue
			}
			v, _ := node.Get(k)
			out = append(out, &wire.Arg{Name: k, Value: pslValue(v)})
		}
		return out
	}

	if !isList {
		return wire.Fields{{Name: ValueField, Value: pslValue(r.value)}}
	}
	out := make(wire.Fields, 0, node.Len()+len(node.Map()))
	for i := 0; i < node.Len(); i++ {
		v, _ := node.Item(i)
		out = append(out, &wire.Arg{Name: itemName(i), Value: pslValue(v)})
	}
	for _, k := range sortedKeys(node) {
		v, _ := node.Get(k)
		out = append(out, &wire.Arg{Name: memberName(k), Value: pslValue(v)})
	}
	return out
}

// pslValue is a PSL value as a wire value.
//
// A nested list has no order of its own, so it takes the unordered rank, and it
// crosses as a block of its own members -- the same shape a record's fields
// take, which is what it is. Its contents are written the Whole way whatever
// the source's reading, because a position inside it has no other spelling.
func pslValue(v any) *wire.Value {
	switch x := v.(type) {
	case nil:
		return wire.NewWord(wire.WordNil)
	case bool:
		if x {
			return wire.NewWord(wire.WordTrue)
		}
		return wire.NewWord(wire.WordFalse)
	case int64:
		return wire.NewInt(x)
	case int:
		return wire.NewInt(int64(x))
	case float64:
		return wire.NewFloat(x)
	case string:
		return wire.NewString(x)
	case *pawscript.PSLNode:
		return pslRecord{reading: Whole, value: x}.fields().Block()
	}
	return wire.NewString(fmt.Sprintf("%v", v))
}

// itemName and memberName are how a record's own contents are written: a dot,
// and then the position or the name.
func itemName(i int) string      { return "." + strconv.Itoa(i) }
func memberName(k string) string { return "." + k }

// itemIndex reads a member name that addresses a position rather than a key:
// after the dot, ASCII digits and nothing else. A run of digits too long to be
// an index is still one, and is simply past the end of every list there could
// be.
func itemIndex(name string) (int, bool) {
	digits := name[1:]
	if digits == "" {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	i, err := strconv.Atoi(digits)
	if err != nil {
		return -1, true
	}
	return i, true
}

// sortedKeys is a node's keyed members in a settled order. Go's map iteration
// is deliberately unordered, and a record's fields have to come out the same
// way twice; PSL's own serializer sorts them for the same reason.
func sortedKeys(n *pawscript.PSLNode) []string {
	m := n.Map()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- the ordering -------------------------------------------------------

// An ordering is the sequence one spec names: which records are in it, and in
// what order.
//
// The sort tuples are kept beside the rows because they are what the sort and
// every later binary search compare. Extracting a field is a map lookup and a
// conversion; doing it once per record rather than once per comparison is the
// difference between a sort that reads the data n log n times and one that
// reads it once.
type ordering struct {
	rows   []int
	tuples [][]*wire.Value
	levels []wire.Level
}

func (o *ordering) Len() int { return len(o.rows) }
func (o *ordering) Swap(i, j int) {
	o.rows[i], o.rows[j] = o.rows[j], o.rows[i]
	o.tuples[i], o.tuples[j] = o.tuples[j], o.tuples[i]
}
func (o *ordering) Less(i, j int) bool {
	return wire.CompareLevels(o.tuples[i], o.tuples[j], o.levels) < 0
}

// order is the sequence a spec names, built if it has not been built already.
//
// Two views of the same sequence share one, and so does a view opened again on
// an order somebody had before -- which is the same click that produced it the
// first time.
func (p *PSL) order(spec *wire.Spec) *ordering {
	key := orderKey(spec)
	p.mu.Lock()
	defer p.mu.Unlock()
	if o := p.cache[key]; o != nil {
		return o
	}

	// The filter runs first, so a record that is not in the sequence is never
	// sorted and never has its sort fields read.
	o := &ordering{levels: append(wire.Levels(spec.Sort), wire.Level{})}
	for i := range p.recs {
		if !wire.Match(p.recs[i], spec.Filter) {
			continue
		}
		o.rows = append(o.rows, i)
		o.tuples = append(o.tuples, tupleOf(p.recs[i], spec.Sort))
	}
	// The record key is the last level and no two records share one, so no two
	// tuples are equal and there is nothing for stability to settle.
	sort.Sort(o)

	p.cache[key] = o
	p.recent = append(p.recent, key)
	for len(p.recent) > orderingsKept {
		delete(p.cache, p.recent[0])
		p.recent = p.recent[1:]
	}
	return o
}

// orderKey names a sequence by what decides it. The fields a query asks for do
// not: they change what a window carries, not which records are in it or where.
func orderKey(spec *wire.Spec) string {
	return wire.EncodeSort(spec.Sort) + "\x00" + spec.Filter.Encode()
}

// tupleOf is a record's position: the value at each sort level, and then its
// key. Without that last one two records could tie, and "the record after this
// point" would name more than one place.
func tupleOf(rec pslRecord, levels []wire.SortLevel) []*wire.Value {
	out := make([]*wire.Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, rec.Field(l.Field))
	}
	return append(out, rec.key)
}

// boundaryTuple reads a boundary the same way, out of the bag it arrived in. A
// boundary is named rather than positional, so neither end has to agree on the
// order the levels were written in; a field the boundary does not carry is
// undefined, which is a position of its own at the bottom of the order.
func boundaryTuple(at wire.Fields, levels []wire.SortLevel) []*wire.Value {
	out := make([]*wire.Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, at.Get(l.Field))
	}
	return append(out, at.Key())
}

// --- the view -----------------------------------------------------------

type pslView struct {
	src  *PSL
	spec *wire.Spec
	ord  *ordering
}

// Close lets the view go. The ordering stays in the source's cache until
// something newer pushes it out, because the records it orders have not moved.
func (v *pslView) Close() { v.ord = nil }

// Fill produces one window.
//
// Where it starts is a binary search: the ordering is total, so the first
// record past a boundary is found in the log of the sequence's length rather
// than by walking to it. Then it emits every record in (From..To], and carries
// on past To only while the window is still short of Need.
func (v *pslView) Fill(f *wire.Fill, out Sink) (Complete, error) {
	o := v.ord
	if o == nil {
		return Complete{}, fmt.Errorf("this view has been closed")
	}

	start := 0
	if len(f.From) > 0 {
		at := boundaryTuple(f.From, v.spec.Sort)
		start = sort.Search(len(o.rows), func(i int) bool {
			return wire.CompareLevels(o.tuples[i], at, o.levels) > 0
		})
	}
	var to []*wire.Value
	if len(f.To) > 0 {
		to = boundaryTuple(f.To, v.spec.Sort)
	}

	sent := 0
	for i := start; i < len(o.rows); i++ {
		past := to == nil || wire.CompareLevels(o.tuples[i], to, o.levels) > 0
		if past && f.Have+sent >= f.Need {
			break
		}
		rec := v.src.recs[o.rows[i]]
		if err := out.Record(rec.key, v.fields(rec, f)); err != nil {
			return Complete{}, err
		}
		sent++
	}

	done := Complete{Ordered: true}
	switch {
	case start+sent >= len(o.rows):
		// Nothing past here, so there is no point past which to be complete.
		done.Exhausted = true
	case sent > 0:
		done.Watermark = v.boundary(v.src.recs[o.rows[start+sent-1]])
	default:
		// The window was already full. Nothing new crossed, and everything
		// between where it was asked from and that same point is held: which is
		// true, and is what the far end is told.
		done.Watermark = f.From
	}
	return done, nil
}

// boundary is a record's position: its sort fields, and its key.
func (v *pslView) boundary(rec pslRecord) wire.Fields {
	out := make(wire.Fields, 0, len(v.spec.Sort)+1)
	for _, l := range v.spec.Sort {
		out = append(out, &wire.Arg{Name: l.Field, Value: rec.Field(l.Field)})
	}
	return append(out, &wire.Arg{Name: wire.KeyField, Value: rec.key})
}

// fields is what one record carries in this window: the fields the window asked
// for where it named fewer than the query did, the query's own where it did
// not, and everything the record has where neither named any.
//
// A field the record has not got is left out rather than sent as `undefined`,
// which is the same answer in fewer bytes: an absent field reads as undefined
// at the far end.
func (v *pslView) fields(rec pslRecord, f *wire.Fill) wire.Fields {
	want := f.Fields
	if len(want) == 0 {
		want = v.spec.Fields
	}
	if len(want) == 0 {
		return v.without(rec.fields())
	}
	out := make(wire.Fields, 0, len(want))
	for _, a := range want {
		if a.Name == wire.KeyField || v.spec.Exclude.Has(a.Name) {
			continue
		}
		if val := rec.Field(a.Name); val != nil {
			out = append(out, &wire.Arg{Name: a.Name, Value: val})
		}
	}
	return out
}

// without drops the fields the query said it did not want.
func (v *pslView) without(bag wire.Fields) wire.Fields {
	if len(v.spec.Exclude) == 0 {
		return bag
	}
	out := make(wire.Fields, 0, len(bag))
	for _, a := range bag {
		if !v.spec.Exclude.Has(a.Name) {
			out = append(out, a)
		}
	}
	return out
}
