package trinkets

// Where a list's rows come from.
//
// One mechanism, whether the rows are the list's own or somebody else's. A list
// given no source MAKES one out of the items it was handed, so there is no
// second code path for the plain case -- reading thirty rows out of a document
// in this process and reading thirty rows out of an application across a wire
// are the same call, and the difference is only how soon the answer arrives.
//
// **The plain case answers inside the call.** A ListSource holds its records, so
// stating the sequence and reading a scope of it both finish before Read
// returns. Nothing about a list built the way lists have always been built
// becomes asynchronous, which is what lets every existing example go on meaning
// exactly what it meant.
//
// # What a made source is
//
//	AmendedSource            replacements and deletions, at run time
//	  ListSource             the items this list was given
//
// The amendment layer is there in the plain case too, empty. It is where a
// change to one row will go without the sequence being restated, and having it
// in both shapes is what keeps there being ONE shape.
//
// # Keys in a made source are positions
//
// A list that made its own rows has no stable identity to offer, so a row's key
// IS its position: 0, 1, 2. Insert or remove and the keys after it are rewritten,
// exactly as the indices shift today -- which is what makes a plain list behave
// as it always has, and is why `IDAt(i)` and `i` are the same figure there.
//
// An application that needs identities to survive an insert declares a source
// whose records have keys of their own. That is the whole difference, and it is
// opt-in rather than something a plain list has to reason about.

import "github.com/phroun/serval"

// The fields a made row carries.
//
// display and value are two names for the same thing until somebody says
// otherwise: a plain list of strings has nothing to tell them apart, and one
// loaded from a delimited file can put the column it shows under one and the
// column it means under the other.
const (
	rowDisplay = "display"
	rowValue   = "value"
	rowEnabled = "enabled"
	rowIcon    = "icon"
)

// SetSource declares where this list's rows come from.
//
// It replaces whatever the list was reading, its own items included, and drops
// what it knew about where the rows were -- a different sequence puts them
// somewhere else, and nothing it held about the old one says anything about the
// new. Nil goes back to reading the items the list was given.
func (l *ListView) SetSource(src serval.Source) {
	l.close()
	l.source = src
	l.made = nil
	l.restate = true
	l.bones = spine{}
	l.setCurrent(-1, nil)
	l.scrollOffset = 0
	l.arrivals++
	if src != nil {
		hearArrivals(l, src, l.arrivals, func() int { return l.arrivals }, l.Reread)
	}
	l.Update()
}

// Reread says the source's answer has landed, so the list draws what it now has.
//
// **It does not forget what it holds, and that is the whole subtlety.** A list
// streams: the sink handed to `Read` puts records into the spine as they arrive,
// so by the time this is called the answer is already THERE. Forgetting would
// throw away the very records the notice is about -- which is what it did, and
// what left a list over an application source permanently empty.
//
// A tree is the other shape and wants the other thing: `flatten` reads a whole
// sequence into a fresh slice, so its Reread really does read again. Two shapes,
// two answers to one notice.
func (l *ListView) Reread() {
	if l.source == nil {
		return
	}
	l.Update()
}

// Source is what this list reads, and nil for one reading its own items.
func (l *ListView) Source() serval.Source { return l.source }

// close lets the stated sequence go. The next read states it again.
func (l *ListView) close() {
	if l.set != nil {
		l.set.Close()
		l.set = nil
	}
}

// touched says the items this list was given have changed, so a source made out
// of them is out of date. A list reading a DECLARED source does not care what
// its own items do, so nothing is restated there.
//
// It does not rebuild anything. A loop adding a thousand items would then state
// a thousand sequences and throw away nine hundred and ninety-nine of them, so
// what happens here is a note, and the rebuild happens once, when something
// asks to read.
func (l *ListView) touched() {
	if l.source == nil {
		l.restate = true
	}
	l.bones.forget()
}

// sequence is the stated sequence, made and opened if it is not already.
//
// Nil where there is nothing to read and nothing to make one out of, which is an
// empty list and is ordinary -- every reader here treats a nil sequence as no
// rows rather than as a failure.
func (l *ListView) sequence() serval.DataSet {
	read := l.source
	if read == nil {
		if l.restate || l.made == nil {
			l.close()
			l.made = l.makeSource()
			l.restate = false
		}
		read = l.made
	}
	if l.set == nil && read != nil {
		set, err := read.Open(&l.descriptor)
		if err != nil {
			// A sequence that cannot be stated is a list with no rows, and the
			// refusal is the source's to explain. There is nothing a list can
			// usefully do with it: it cannot ask a different question, having
			// been told which one to ask.
			return nil
		}
		l.set = set
	}
	return l.set
}

// makeSource builds a source out of the items this list was given.
func (l *ListView) makeSource() serval.Source {
	rows := make([]serval.Row, len(l.items))
	for i, item := range l.items {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)), serval.Record{
			serval.Named(rowDisplay, item.Text),
			serval.Named(rowValue, item.Text),
			serval.Named(rowEnabled, item.Enabled),
			serval.Named(rowIcon, item.Icon),
		})
	}
	return serval.NewAmendedSource(serval.NewListSource(rows))
}

// window makes sure the spine can name the rows from a position on, asking the
// source about the ones it cannot.
//
// It asks for the stretch WHOLE rather than for the gaps in it. A window is a
// screenful, the answer is one question, and asking three questions to fill
// three holes in it costs three times as much as asking once for all of it.
func (l *ListView) window(at, n int) {
	if n <= 0 {
		return
	}
	if at < 0 {
		at = 0
	}
	if l.spineHolds(at, n) {
		return
	}
	set := l.sequence()
	if set == nil {
		return
	}

	// From a position, because that is what a list has: it knows which rows are
	// on screen and need not know any identity to ask for them. Where the row
	// just before the stretch is one the spine names, `after` says the same
	// thing and says it better -- a record is exact where a position is best
	// effort -- so that is preferred.
	scope := &serval.Scope{Count: n}
	if before, ok := l.bones.idAt(at - 1); ok && at > 0 {
		scope.After = before
	} else {
		scope.From = at
	}

	// What the list EXPECTED, which it knows because it chose where to ask from.
	// An answer that says where it began is believed over this; one that says
	// nothing leaves the list its own arithmetic rather than nothing at all.
	// Nothing waits. Read hands the question over and returns; the answer
	// reaches the sink when it reaches it, which for records held here is inside
	// this call and for records somebody has to fetch is later. So what arrived
	// is written down by Done and not by this returning -- doing it here would
	// settle an empty answer every time and never settle a real one.
	if err := set.Read(scope, &rowSink{list: l, expected: at, asked: l.asks}); err != nil {
		return
	}
}

// spineHolds reports whether every row of a stretch is already named.
func (l *ListView) spineHolds(at, n int) bool {
	for i := 0; i < n; i++ {
		if _, ok := l.bones.idAt(at + i); !ok {
			return false
		}
	}
	return true
}

// A rowSink is one answer arriving.
//
// It takes PLACES as well as records, which is the whole reason a drag stays
// smooth: what a list needs first is where the rows are and not what they hold,
// so an identity is enough to lay a row out and the values fill it in behind.
type rowSink struct {
	list     *ListView
	ids      []*serval.Value
	begin    serval.RecordCount
	done     serval.Complete
	expected int    // where the list asked from, and so where it expects the answer
	asked    asking // which ask this answers, so a stale one can be dropped
}

func (s *rowSink) Ordered() {}

func (s *rowSink) Record(id *serval.Value, fields serval.Record) error {
	s.take(id, fields)
	return nil
}

func (s *rowSink) Subset(id *serval.Value, fields serval.Record, _ serval.Totals) error {
	s.take(id, fields)
	return nil
}

// Place is a row named and not yet filled in.
//
// One row arrives as exactly one of Place, Record or Subset -- a source sends a
// place INSTEAD of a result for a row whose values it has not got, not as well
// as one -- so this counts towards the run like the others and never doubles it.
// What is additional is the capability: a sink that cannot take places is handed
// every record it would have been handed anyway.
func (s *rowSink) Place(id *serval.Value, fields serval.Record) error {
	s.take(id, fields)
	return nil
}

// Placed says the order is settled, which is when a list can lay out rows it has
// no values for and be sure nothing will turn up between two it already holds.
func (s *rowSink) Placed(c serval.Complete) { s.begin = c.First }

// Done ends the answer, which is when what arrived is written down.
//
// For records held in this process that is inside the Read that asked; for
// records somebody had to fetch it is whenever they arrive, and the list repaints
// because rows that were blank are not any more.
func (s *rowSink) Done(c serval.Complete) {
	s.done = c
	s.settle()
	s.list.Update()
}

func (s *rowSink) take(id *serval.Value, fields serval.Record) {
	s.ids = append(s.ids, id)
	s.list.learnRow(id, fields)
}

// settle writes what arrived into the spine.
//
// Where the answer says it began, that is where the run goes -- it is the source
// speaking about its own sequence, and it outranks anything the list worked out.
//
// Where the answer says NOTHING, the list uses what it expected. That is not a
// guess: the list chose the place it asked from, and asked either past a record
// it holds or at a position, so it knows what it asked for. A source that will
// not say where it began has not contradicted that, and leaving the rows blank
// instead would mean a list could never fill them from such a source at all.
//
// What is never done is writing a run at a position NOBODY chose. A run put down
// somewhere nothing vouched for reads back as fact, and the rows on screen would
// then be somewhere other than where the list says they are.
func (s *rowSink) settle() {
	l := s.list

	// How long the sequence is holds good however old the answer: a count is
	// about the sequence and not about the place this one asked for.
	l.bones.learn(s.done)
	if !l.current(s.asked) {
		// An answer for somewhere the reader has since left. Writing it down
		// would leave the spine holding where the reader was passing through
		// rather than where it is.
		return
	}
	first := s.done.First
	if !first.Exact {
		first = s.begin
	}
	if !first.Exact {
		first = serval.Exactly(s.expected)
	}
	if first.Exact && len(s.ids) > 0 {
		l.bones.place(first.N, s.ids)
	}
	l.resolve()
	if s.done.Stop != "" || s.done.Error != "" {
		l.settled()
	}
}

// learnRow keeps what a record holds for the rows this list did not make.
//
// A row the list made itself is already a ListItem and needs nothing kept. One
// that came from a source has no ListItem at all until this, and what it can
// hold is what a serval value can be -- so the text and whether it is enabled
// cross, and a Go pointer could not have.
func (l *ListView) learnRow(id *serval.Value, fields serval.Record) {
	if id == nil {
		return
	}
	key := serval.Key(id)

	// What the row MEANS is kept whoever made it. A plain list means what it
	// shows, which is not nothing -- it is the same answer arrived at the same
	// way, and a caller asking what a row means should not have to know which
	// kind of list it is asking.
	if v := fields.Get(l.meaning()); v != nil {
		if l.values == nil {
			l.values = map[string]*serval.Value{}
		}
		l.values[key] = v
	}

	// A row the list made itself is already a ListItem, so there is nothing to
	// build. Only a source's rows need one.
	if l.source == nil {
		return
	}
	if l.fromSource == nil {
		l.fromSource = map[string]*ListItem{}
	}
	item := l.fromSource[key]
	if item == nil {
		item = &ListItem{Enabled: true}
		l.fromSource[key] = item
	}
	if v := fields.Get(l.showing()); v != nil {
		item.Text = v.Str
	} else if v := fields.Get(l.meaning()); v != nil {
		item.Text = v.Str
	}
	if v := fields.Get(rowEnabled); v != nil {
		item.Enabled = v.Bool
	}
	if v := fields.Get(rowIcon); v != nil {
		item.Icon = v.Str
	}
}

// rowAt is the item standing at a position, and nil for a row that is BLANK --
// one the list knows is there and knows nothing else about yet.
//
// Blank is ordinary. It is what every row is between the moment a thumb moves
// and the moment the answer arrives, and drawing one is drawing an empty row
// rather than drawing nothing.
func (l *ListView) rowAt(pos int) *ListItem {
	id, ok := l.bones.idAt(pos)
	if !ok {
		return nil
	}
	if l.fromSource != nil {
		if item := l.fromSource[serval.Key(id)]; item != nil {
			return item
		}
	}
	// A made source keys rows by position, so the item is the one at that index.
	if id.IsInt && int(id.Int) >= 0 && int(id.Int) < len(l.items) {
		return l.items[id.Int]
	}
	return nil
}

// blankRow is what a row the list cannot name yet is drawn as: a place with no
// words in it. Enabled, because a row nobody has described is not a row somebody
// has described as unavailable.
var blankRow = &ListItem{Enabled: true}
