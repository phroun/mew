package source

// A source made of several others.
//
// The fourth kind, and the second that wraps. It holds a sequence of named
// includes, each a source in its own right, and answers the scope out of all
// of them at once. Every record reaches the outer sequence under a key of its
// own -- the include's name, a slash, and the child's key -- so no two
// includes can collide however their own keys are spelled.
//
// Nothing shadows anything. Every record of every include is in the outer
// sequence under its own name, which is what separates this from the layering
// still ahead: that one replaces an inner record with an outer one of the same
// key, and here no two records can share a key at all.
//
// **The order is the include's name, then the child's key as the child itself
// orders it.** Within one include the name is constant, so the outer order and
// the child's own order are the same sequence -- which is what lets the merge
// below hand records on as they arrive instead of holding the answer to the
// end.
//
// There are no amendments here. One include may be an AmendedSource, or an
// AmendedSource may wrap the whole of this; either way the two stay separate
// and neither grows the other's job.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// An Include is one of the sources a ComposedSource is made of, under the name
// its records go out prefixed with.
type Include struct {
	Name   string
	Source Source
}

// A ComposedSource answers out of several sources at once.
type ComposedSource struct {
	includes []Include
}

// NewComposedSource composes sources under the names their records go out
// under.
//
// The includes are fixed for the life of the source. A sequence opened over it
// opens one over each of them, so a set that changed underneath would leave a
// sequence answering out of children it never opened.
func NewComposedSource(includes ...Include) (*ComposedSource, error) {
	seen := make(map[string]bool, len(includes))
	for _, in := range includes {
		switch {
		case in.Name == "":
			return nil, fmt.Errorf("an include needs a name")
		case strings.Contains(in.Name, separator):
			// The separator is what tells a name from a key, so a name holding
			// one would make the split ambiguous and two different records
			// could reach the same outer key.
			return nil, fmt.Errorf("include %q: a name cannot hold %q", in.Name, separator)
		case in.Source == nil:
			return nil, fmt.Errorf("include %q: no source to ask", in.Name)
		case seen[in.Name]:
			return nil, fmt.Errorf("include %q: named twice", in.Name)
		}
		seen[in.Name] = true
	}
	return &ComposedSource{includes: append([]Include(nil), includes...)}, nil
}

// separator stands between an include's name and the child's key.
const separator = "/"

// Includes are the sources this one is made of, in the order they were given.
func (c *ComposedSource) Includes() []Include {
	return append([]Include(nil), c.includes...)
}

// Open states a sequence, and opens the same one on every include.
func (c *ComposedSource) Open(spec *wire.Spec) (ResultSet, error) {
	if spec == nil {
		spec = &wire.Spec{}
	}
	if err := refuseKey(spec); err != nil {
		return nil, err
	}

	// Two levels past the query's own: the include's name, and then the child's
	// key. Together they are the composed key, and they are what makes a
	// position name exactly one record.
	set := &composedSet{
		src:    c,
		spec:   spec,
		levels: append(wire.Levels(spec.Sort), wire.Level{}, wire.Level{}),
	}
	for _, in := range c.includes {
		child, err := in.Source.Open(spec)
		if err != nil {
			set.Close()
			return nil, fmt.Errorf("include %q: %w", in.Name, err)
		}
		set.parts = append(set.parts, part{name: in.Name, set: child})
	}
	return set, nil
}

// refuseKey refuses a sequence that names the record key.
//
// The key this source hands out is the include's name and the child's key
// together, and no child has ever heard of it: a sort level or a filter
// predicate naming `key` would be answered by every include about its own key
// instead. Refusing says so. Answering nearly would put the sequence in an
// order that is almost right, which is the one thing worse than a refusal --
// it corrupts every answer after it and looks like data.
func refuseKey(spec *wire.Spec) error {
	for _, l := range spec.Sort {
		if l.Field == wire.KeyField {
			return fmt.Errorf("sort %s: a composed source orders by its includes, "+
				"and its key is not one any include holds", wire.KeyField)
		}
	}
	if names(spec.Filter, wire.KeyField) {
		return fmt.Errorf("filter: a composed source cannot be filtered on %s, "+
			"which is not a key any include holds", wire.KeyField)
	}
	return nil
}

// names reports whether a filter tests a field anywhere in it.
func names(f *wire.Filter, field string) bool {
	if f == nil {
		return false
	}
	if f.Field == field {
		return true
	}
	for _, c := range f.Children {
		if names(c, field) {
			return true
		}
	}
	return false
}

// --- the composed key ----------------------------------------------------

// composedKey is a child's record seen from outside: the include's name, a
// slash, and the child's key.
//
// It is a symbol rather than a string. A symbol is what this shape already is
// everywhere else it appears, and the wire writes one back bare or bracketed
// rather than turning it into something else.
func composedKey(name string, key *wire.Value) *wire.Value {
	return wire.NewWord(name + separator + segment(key))
}

// segment is a child's key as the text after the slash: a name as it stands,
// and a number as it is spelled.
func segment(v *wire.Value) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case wire.StringValue:
		return v.Str
	case wire.WordValue:
		return v.Word
	}
	return wire.EncodeValue(v)
}

// split takes a composed key apart: the include's name, and the text of the
// child's key.
//
// It splits at the FIRST slash, which is what lets a composed source hold
// another: the inner source's own composed keys arrive here as the child key,
// slashes and all, and go back down the same way.
func split(key *wire.Value) (string, string, bool) {
	if key == nil {
		return "", "", false
	}
	text := segment(key)
	i := strings.Index(text, separator)
	if i < 0 {
		return text, "", false
	}
	return text[:i], text[i+len(separator):], true
}

// childKey reads the text of a key back as a value, so a boundary that came in
// from outside can go back down to the include it names.
//
// A run of digits is the number it spells and anything else is a name, which is
// the wire's own rule for a bare token. It does not always land on the value
// the child holds: a child keying its records by string gets a symbol back, and
// a symbol ranks below every string. That is the safe direction -- the child
// answers from slightly before where it was asked, sends a record or two this
// scope already had, and those are dropped here. Landing after would cut
// records out of a range this source then claimed, which is the one thing that
// cannot be allowed.
func childKey(text string) *wire.Value {
	if text == "" {
		return nil
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return wire.NewInt(n)
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return wire.NewFloat(f)
	}
	return wire.NewWord(text)
}

// --- the sequence --------------------------------------------------------

type composedSet struct {
	src    *ComposedSource
	spec   *wire.Spec
	parts  []part
	levels []wire.Level
}

type part struct {
	name string
	set  ResultSet
}

// Close lets this sequence go, and every include's with it.
func (s *composedSet) Close() {
	for _, p := range s.parts {
		p.set.Close()
	}
}

// Fill answers one scope out of every include at once.
func (s *composedSet) Fill(f *wire.Fill, out Sink) error {
	if out == nil {
		return fmt.Errorf("a fill needs somewhere to put the answer")
	}
	g := &gathering{set: s, want: f, out: out}
	g.at = make([]*arrivals, len(s.parts))
	for i, p := range s.parts {
		g.at[i] = &arrivals{name: p.name}
	}
	g.from = s.boundary(f.From)
	g.to = s.boundary(f.To)

	// Asked for outside the lock: an include whose records are here answers
	// inside the call, and would reach for a lock this one was already holding.
	for i, p := range s.parts {
		if err := p.set.Fill(s.ask(f, p.name), g.lane(i)); err != nil {
			g.failed(i, err)
		}
	}
	g.settle()
	return nil
}

// ask is the scope as one include is asked for it.
//
// The boundaries lose the composed key and keep the child's, where the
// boundary names this include; where it names another, the key goes away
// entirely, which asks the include from the start of that sort value. Either
// way what comes back may be more than belongs in the scope, and the merge
// drops what the boundary already covered. Asking for less than belongs is
// what would be wrong.
//
// Every include is asked for the whole shortfall, because the whole of it may
// turn out to come from any one of them.
func (s *composedSet) ask(f *wire.Fill, name string) *wire.Fill {
	next := *f
	next.From = s.mapBoundary(f.From, name)
	next.To = s.mapBoundary(f.To, name)
	next.Have = 0
	next.Need = f.Need - f.Have
	if next.Need < 0 {
		next.Need = 0
	}
	return &next
}

// mapBoundary is a boundary as the named include reads it: the sort fields as
// they stand, and the key only where it is one of that include's own.
func (s *composedSet) mapBoundary(at wire.Fields, name string) wire.Fields {
	if len(at) == 0 {
		return nil
	}
	who, text, ok := split(at.Key())
	out := make(wire.Fields, 0, len(at))
	for _, a := range at {
		if a.Name == wire.KeyField {
			continue
		}
		out = append(out, a)
	}
	if ok && who == name {
		if k := childKey(text); k != nil {
			out = append(out, &wire.Arg{Name: wire.KeyField, Value: k})
		}
	}
	return out
}

// boundary is a boundary as a position in this sequence: the sort values, the
// include's name, and the child's key.
func (s *composedSet) boundary(at wire.Fields) []*wire.Value {
	if len(at) == 0 {
		return nil
	}
	who, text, _ := split(at.Key())
	out := make([]*wire.Value, 0, len(s.spec.Sort)+2)
	for _, l := range s.spec.Sort {
		out = append(out, at.Get(l.Field))
	}
	return append(out, wire.NewString(who), childKey(text))
}

// --- the merge -----------------------------------------------------------

// A gathering is one scope being answered out of every include at once.
//
// Each include delivers into a queue of its own, and a record leaves the queue
// when no include can still produce one before it -- which is when every
// include that has not finished is holding at least one. So what is buffered is
// how far the includes have drifted out of step with each other, and never the
// answer itself.
type gathering struct {
	set  *composedSet
	want *wire.Fill
	out  Sink

	mu   sync.Mutex
	at   []*arrivals
	from []*wire.Value
	to   []*wire.Value

	settled  bool          // every include has spoken, so the order is decided
	inOrder  bool          // and every one of them declared its records in order
	sent     int           // records handed on
	last     []*wire.Value // where the last of them sat
	lastMark wire.Fields
	ended    bool
}

// arrivals is what one include has delivered and not yet handed on.
type arrivals struct {
	name  string
	queue []waiting
	spoke bool // it has declared, delivered or finished
	said  bool // and it declared its records in order, before any of them
	done  bool
	c     Complete
}

// waiting is one record held until its place is settled.
type waiting struct {
	key    *wire.Value
	tuple  []*wire.Value
	fields wire.Fields
	whole  bool
}

// lane is the sink one include answers into.
type lane struct {
	g *gathering
	i int
}

func (g *gathering) lane(i int) Sink { return &lane{g: g, i: i} }

func (l *lane) Ordered() { l.g.declared(l.i) }

func (l *lane) Record(key *wire.Value, fields wire.Fields) error {
	return l.g.take(l.i, key, fields, true)
}

func (l *lane) Subset(key *wire.Value, fields wire.Fields) error {
	return l.g.take(l.i, key, fields, false)
}

func (l *lane) Done(c Complete) { l.g.finished(l.i, c) }

// declared is an include saying its records are in the sequence's order.
//
// It counts only before that include's first record, which is the only place it
// is worth anything, and it is what this source's own claim is built out of:
// ours are in order exactly when every one of theirs is.
func (g *gathering) declared(i int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.at[i]
	if !a.spoke {
		a.spoke, a.said = true, true
	}
}

// take is one record arriving from an include.
func (g *gathering) take(i int, key *wire.Value, fields wire.Fields, whole bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.at[i]
	a.spoke = true

	rec := waiting{
		key:    composedKey(a.name, key),
		tuple:  g.place(a.name, key, fields),
		fields: fields,
		whole:  whole,
	}
	// An include asked from before where the scope starts answers from there,
	// so what the boundary already covered is dropped here rather than sent
	// twice.
	if g.from == nil || wire.CompareLevels(rec.tuple, g.from, g.set.levels) > 0 {
		a.queue = append(a.queue, rec)
	}
	g.release()
	return nil
}

// finished is an include reaching the end of its own answer.
func (g *gathering) finished(i int, c Complete) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.at[i]
	if a.done {
		return
	}
	a.spoke, a.done, a.c = true, true, c
	g.release()
	g.close()
}

// failed is an include that could not be asked at all, which ends it here the
// same way a refusal would.
func (g *gathering) failed(i int, err error) {
	g.finished(i, Complete{Error: err.Error()})
}

// settle releases whatever is already settled, once every include has been
// asked. Called after the asking, for the includes that answered inside it.
func (g *gathering) settle() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.release()
	g.close()
}

// place is where a record sits in this sequence: its sort values, the
// include's name, and its own key.
//
// The child's key goes in as the child holds it, not as the composed key
// spells it, so an include's records keep exactly the order the include put
// them in.
func (g *gathering) place(name string, key *wire.Value, fields wire.Fields) []*wire.Value {
	out := make([]*wire.Value, 0, len(g.set.spec.Sort)+2)
	for _, l := range g.set.spec.Sort {
		out = append(out, fields.Get(l.Field))
	}
	return append(out, wire.NewString(name), key)
}

// release hands on every record whose place is settled.
func (g *gathering) release() {
	if !g.settled {
		// Nothing can be placed until every include has spoken, because one
		// that has said nothing could still produce a record before all of
		// them. One record from each is enough, which is the whole of the wait.
		for _, a := range g.at {
			if !a.spoke {
				return
			}
		}
		g.settled = true
		g.inOrder = g.allSaidOrdered()
		if g.inOrder {
			g.out.Ordered()
		}
	}

	if !g.inOrder {
		// An include that would not promise an order leaves the merge nothing
		// to merge on, so everything goes out as it arrives and this source
		// claims nothing about the order either. It cannot stop at the
		// shortfall while it is at it: cutting the answer at some arbitrary
		// record would drop ones that belong in the scope, and a superset is
		// always allowed where a gap is not.
		for _, a := range g.at {
			for _, rec := range a.queue {
				g.hand(rec)
			}
			a.queue = nil
		}
		return
	}

	for !g.full() {
		next := -1
		for i, a := range g.at {
			if len(a.queue) == 0 {
				if a.done {
					continue // it has no more to put before anything
				}
				return // and this one still might
			}
			if next < 0 || wire.CompareLevels(
				a.queue[0].tuple, g.at[next].queue[0].tuple, g.set.levels) < 0 {
				next = i
			}
		}
		if next < 0 {
			return // nothing anywhere is waiting
		}
		rec := g.at[next].queue[0]
		g.at[next].queue = g.at[next].queue[1:]
		g.hand(rec)
	}
}

// full reports whether the scope has as many records as it was asked for.
func (g *gathering) full() bool {
	return g.want.Have+g.sent >= g.want.Need
}

// hand passes one record on and remembers where it sat, which is as far as
// this answer is complete.
func (g *gathering) hand(rec waiting) {
	g.sent++
	g.last = rec.tuple
	g.lastMark = g.mark(rec)
	if rec.whole {
		_ = g.out.Record(rec.key, rec.fields)
		return
	}
	_ = g.out.Subset(rec.key, rec.fields)
}

// mark is a record's position, written the way a boundary is: the sort fields,
// and the composed key.
func (g *gathering) mark(rec waiting) wire.Fields {
	out := make(wire.Fields, 0, len(g.set.spec.Sort)+1)
	for _, l := range g.set.spec.Sort {
		out = append(out, &wire.Arg{Name: l.Field, Value: rec.fields.Get(l.Field)})
	}
	return append(out, &wire.Arg{Name: wire.KeyField, Value: rec.key})
}

// allSaidOrdered reports whether every include declared its records in order.
func (g *gathering) allSaidOrdered() bool {
	for _, a := range g.at {
		if !a.said {
			return false
		}
	}
	return true
}

// close ends the scope once every include has, and says what is true of the
// whole of it.
//
// **The watermark is the lowest, not the highest.** Complete up to a point
// means every include is complete up to it, so one that stopped early holds the
// claim back for all of them. And it can be no further than the last record
// that went out: records held back at the shortfall are ones the far end has
// not got, however complete the includes were.
func (g *gathering) close() {
	if g.ended {
		return
	}
	// Records left over are ones the scope filled before it reached them. They
	// are not gone -- they are the start of the next scope -- so the sequence
	// has not run out however far the includes themselves got.
	held := false
	for _, a := range g.at {
		if !a.done {
			return
		}
		if len(a.queue) > 0 {
			held = true
		}
	}
	g.ended = true

	out := Complete{Exhausted: !held}
	var lowest []*wire.Value
	for i, a := range g.at {
		if a.c.Error != "" && out.Error == "" {
			out.Error = a.c.Error
		}
		if a.c.Exhausted {
			continue // nothing past the end to hold anyone back
		}
		out.Exhausted = false
		tuple, bag := g.watermark(i)
		if bag == nil {
			lowest, out.Watermark = nil, nil
			break // an include that claimed nothing lets nobody claim anything
		}
		if lowest == nil || wire.CompareLevels(tuple, lowest, g.set.levels) < 0 {
			lowest, out.Watermark = tuple, bag
		}
	}
	if !out.Exhausted && g.last != nil &&
		(out.Watermark == nil || wire.CompareLevels(g.last, lowest, g.set.levels) < 0) {
		out.Watermark = g.lastMark
	}
	g.out.Done(out)
}

// watermark is one include's completeness claim as a position in this
// sequence, and the bag that says it.
func (g *gathering) watermark(i int) ([]*wire.Value, wire.Fields) {
	a := g.at[i]
	if len(a.c.Watermark) == 0 {
		return nil, nil
	}
	tuple := make([]*wire.Value, 0, len(g.set.spec.Sort)+2)
	bag := make(wire.Fields, 0, len(g.set.spec.Sort)+1)
	for _, l := range g.set.spec.Sort {
		v := a.c.Watermark.Get(l.Field)
		tuple = append(tuple, v)
		bag = append(bag, &wire.Arg{Name: l.Field, Value: v})
	}
	key := a.c.Watermark.Key()
	tuple = append(tuple, wire.NewString(a.name), key)
	bag = append(bag, &wire.Arg{Name: wire.KeyField, Value: composedKey(a.name, key)})
	return tuple, bag
}
