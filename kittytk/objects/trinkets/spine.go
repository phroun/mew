package trinkets

// What a view knows about where its rows are.
//
// A view draws positions and a source answers in identities, and the spine is
// what stands between them. It is NOT a copy of the sequence: a reader that
// scrolled past nine hundred thousand records would leave nine hundred thousand
// identities behind it, which is the thing this is built to avoid.
//
// What it holds is a few RUNS -- stretches somebody was actually told about,
// each anchored by the position of its first row -- plus how long the sequence
// is. Normally one run, the rows on screen and a little either side; briefly
// two while a scrub settles. Anything further from the reader is dropped,
// because a row nobody is looking at is a row that can be asked for again.
//
// **A row the spine cannot name is not an error.** It is a BLANK: the view
// knows a row stands there, because it knows how long the sequence is, and
// knows nothing else about it yet. Blank is a state a row is drawn in, not a
// failure to draw one, and it is what lets a thumb drag stay smooth while the
// records catch up.
//
// # Why a held row carries a depth
//
// A list's rows all stand at the top, so its depth is always nought and the
// field is dead weight there -- one int per held row, of which there are at most
// `spineKept`. It is carried anyway because a TREE's flattening is a sequence
// like any other, and the depth is what makes two of its questions answerable
// from what is held: how far a row is indented, and where a subtree ENDS. The
// second is what tells a collapse how many rows are going, and it cannot be
// worked out from identities alone.
//
// # The sequence moving is not the same as the records changing
//
// `grew` and `shrank` are the answer to a tree opening and closing, and they
// exist because that is not an invalidation. Every level of a tree is a data set
// of its own, opened with its own descriptor and cached on its own, so opening a
// node makes no record anywhere untrue: it changes only which levels the walk
// visits, and therefore what POSITION each row below the mark stands at. Nothing
// above the mark moves at all.
//
// So a view that knows the delta shifts by it and keeps what it holds, and one
// that does not forgets from the mark DOWN -- which costs a re-ask and not a
// re-fetch, the levels' own caches still holding every record either way.

import "github.com/phroun/serval"

// spineKept is how many identities one view remembers. A reader looks at a
// screenful and scrolls through a few more, so several screenfuls is already
// generosity -- and a view that held more would be keeping rows it will have
// re-asked for by the time it wants them.
const spineKept = 512

// A named row is one the spine can name: its identity, and how deep it stands
// where the sequence is a tree's flattening.
type named struct {
	id   *serval.Value
	deep int
}

// A run is a stretch of the sequence the view has been told about: where it
// starts, and the rows from there on with no gaps in them.
type run struct {
	at   int // the position of rows[0] in the sequence
	rows []named
}

func (r run) end() int { return r.at + len(r.rows) } // one past its last row

// A spine is the positions a view knows identities for, and how long the
// sequence is.
//
// Not guarded. A trinket is painted and driven from one goroutine, and what
// arrives from a source reaches it the same way everything else does.
type spine struct {
	runs  []run
	total serval.RecordCount

	// anchor is where the reader is, which decides what is worth keeping when
	// there is more than there is room for.
	anchor int
}

// length is how long the sequence is, as far as anyone has said: Exactly for a
// source that counted, AtLeast for one that could only floor it, and Unknown
// for one that would not say. A view draws a true thumb, a shrinking one, or
// none, and those are three different answers rather than three guesses.
func (s *spine) length() serval.RecordCount { return s.total }

// rows is how many rows to draw: the length where one is known, and otherwise
// as far as anything has been placed. A sequence nobody has counted still has
// the rows somebody was sent.
func (s *spine) rows() int {
	n := s.total.N
	for _, r := range s.runs {
		if r.end() > n {
			n = r.end()
		}
	}
	return n
}

// learn takes what an answer said about the sequence itself.
//
// A figure only ever replaces one that says less. An answer that could not
// count says Unknown, and Unknown is not news -- taking it would throw away a
// count somebody else managed, and leave the thumb flickering between a scale
// and none as answers of different kinds came back.
func (s *spine) learn(c serval.Complete) {
	switch {
	case c.Total.Nothing():
	case !s.total.Exact || c.Total.Exact:
		s.total = c.Total
	}
}

// place writes down a run of rows beginning at a position.
//
// Where it touches or overlaps a run already held, the two become one: a reader
// scrolling asks for what comes after what it has, and its knowledge should
// grow rather than fragment. Overlap is resolved in favour of what just
// arrived, that being the more recent word on where those rows are.
func (s *spine) place(at int, rows []named) {
	if at < 0 || len(rows) == 0 {
		return
	}
	fresh := run{at: at, rows: append([]named(nil), rows...)}

	kept := make([]run, 0, len(s.runs)+1)
	for _, r := range s.runs {
		switch {
		case r.end() < fresh.at || r.at > fresh.end():
			kept = append(kept, r) // apart, and stays its own run
		default:
			fresh = join(r, fresh)
		}
	}
	s.runs = inOrder(kept, fresh)
	s.anchor = at
	s.trim(at, at+len(rows))
}

// join makes one run of two that touch or overlap, the fresh one winning
// wherever they disagree.
func join(held, fresh run) run {
	at := held.at
	if fresh.at < at {
		at = fresh.at
	}
	end := held.end()
	if fresh.end() > end {
		end = fresh.end()
	}
	rows := make([]named, end-at)
	copy(rows[held.at-at:], held.rows)
	copy(rows[fresh.at-at:], fresh.rows)
	return run{at: at, rows: rows}
}

// inOrder puts a run among the others, in position order.
func inOrder(runs []run, r run) []run {
	at := len(runs)
	for i, held := range runs {
		if held.at > r.at {
			at = i
			break
		}
	}
	runs = append(runs, run{})
	copy(runs[at+1:], runs[at:])
	runs[at] = r
	return runs
}

// trim drops what is furthest from the reader until what is left fits.
//
// TWO things go, and the second is the one that is easy to miss. Whole runs
// that are somewhere else go first, because a run is what a question came back
// as and half of one is worth no less than all of it while the reader is near
// it. But a reader scrolling steadily never makes a second run at all -- every
// answer joins the one before it -- so the run it is IN has to be cut too, or a
// view that scrolled a long way would be holding the whole sequence under the
// name of holding an Extent of it.
//
// The run holding the reader is never dropped, only shortened.
func (s *spine) trim(from, to int) {
	for i, r := range s.runs {
		if len(r.rows) > spineKept {
			s.runs[i] = cut(r, from, to)
		}
	}

	held := 0
	for _, r := range s.runs {
		held += len(r.rows)
	}
	for held > spineKept && len(s.runs) > 1 {
		worst, far := -1, -1
		for i, r := range s.runs {
			if s.anchor >= r.at && s.anchor < r.end() {
				// The one being read. No test kills this and none can: distance
				// is nought for the run holding the reader and at least one for
				// every other, so it is never the furthest while there is
				// anything else to drop. It says here what is meant rather than
				// resting on that arithmetic.
				continue
			}
			if d := distance(r, s.anchor); d > far {
				worst, far = i, d
			}
		}
		if worst < 0 {
			return // everything left is where the reader is
		}
		held -= len(s.runs[worst].rows)
		s.runs = append(s.runs[:worst], s.runs[worst+1:]...)
	}
}

// cut shortens one run to the cap, keeping the rows around the stretch that was
// just placed -- which is where the reader is, and what it asked for.
//
// It COPIES rather than reslicing, and no test kills that. A slice keeps the
// whole of its backing array alive, so reslicing would hold rows it reports
// having dropped -- but every run reaching here was just built by join or by
// place, both of which allocate, so the excess is one answer's worth and does
// not accumulate. The copy is what makes that a fact about this function rather
// than a fact about its callers.
func cut(r run, from, to int) run {
	if len(r.rows) <= spineKept {
		return r
	}
	beg := (from+to)/2 - spineKept/2
	if beg < r.at {
		beg = r.at
	}
	if beg+spineKept > r.end() {
		beg = r.end() - spineKept
	}
	if beg < r.at {
		beg = r.at
	}
	rows := make([]named, spineKept)
	copy(rows, r.rows[beg-r.at:])
	return run{at: beg, rows: rows}
}

// distance is how far a run is from a position, and nought for one holding it.
func distance(r run, at int) int {
	switch {
	case at < r.at:
		return r.at - at
	case at >= r.end():
		return at - r.end() + 1
	}
	return 0
}

// idAt is the identity of the row at a position, and false for a row the view
// knows is there and nothing else about -- which is a blank, and is ordinary.
func (s *spine) idAt(at int) (*serval.Value, bool) {
	for _, r := range s.runs {
		if at >= r.at && at < r.end() {
			return r.rows[at-r.at].id, true
		}
	}
	return nil, false
}

// deepAt is how deep the row at a position stands, and false for a blank.
//
// Nought for every row of a list, which has one level. It is a tree that has
// anything to say here, and what it says is what makes a subtree's end findable
// from the positions in hand.
func (s *spine) deepAt(at int) (int, bool) {
	for _, r := range s.runs {
		if at >= r.at && at < r.end() {
			return r.rows[at-r.at].deep, true
		}
	}
	return 0, false
}

// lastBefore is the nearest row the spine can name BELOW a position: where it
// stands and what it is, and false where nothing below it is held.
//
// **It is how a reader gets to a place a source will not jump to.** A position is
// best effort and a record is exact, so `after` is always the better question -- and
// it used to be asked only where the row IMMEDIATELY above the stretch was held,
// which is the scrolling case. A reader that asked for a position and was answered
// from the beginning holds rows nowhere near the one it wants, and had nothing to
// carry on from: it asked for the same position again, was answered from the
// beginning again, and never moved.
//
// Anything held below is something to carry on from, however far below it is. That is
// the convergence `Scope.From` describes -- ask, read where the answer began, and ask
// again from what you learned -- and it is why the walk terminates.
func (s *spine) lastBefore(at int) (int, *serval.Value, bool) {
	best, found := -1, (*serval.Value)(nil)
	for _, r := range s.runs {
		if r.at >= at {
			continue
		}
		end := r.end()
		if end > at {
			end = at
		}
		if end-1 > best {
			best, found = end-1, r.rows[end-1-r.at].id
		}
	}
	if best < 0 {
		return 0, nil, false
	}
	return best, found, true
}

// posOf is where a record stands, and false for one outside what is held.
//
// A scan, because the runs are few and short and this is asked when something
// is selected rather than on every row of every frame.
func (s *spine) posOf(id *serval.Value) (int, bool) {
	if id == nil {
		return 0, false
	}
	want := serval.Key(id)
	for _, r := range s.runs {
		for i, held := range r.rows {
			if held.id != nil && serval.Key(held.id) == want {
				return r.at + i, true
			}
		}
	}
	return 0, false
}

// forget drops every identity and keeps the length.
//
// What a notice says is that the rows have MOVED, so every position the view
// holds is suspect -- and which ones is exactly what is no longer known. How
// many there are is a different fact and survives until something says
// otherwise, which is what keeps the thumb from jumping to nothing and back
// every time the data changes underneath it.
func (s *spine) forget() {
	s.runs = nil
	s.anchor = 0
}

// forgetFrom drops what is held from a position on, and keeps what is above it.
//
// **What is above a mark did not move**, whatever happened at it. A tree opening
// a node shifts every row below by the size of what appeared and leaves
// everything above exactly where it stood, so forgetting the lot would throw
// away the half that is still true -- including, very often, the whole of what is
// on screen.
//
// The length goes to a floor at the position, because what is below it is the
// part nobody can now count. A count that survived would be a claim about rows
// the view just admitted it knows nothing about.
func (s *spine) forgetFrom(at int) {
	if at <= 0 {
		s.forget()
		s.total = serval.Unknown()
		return
	}
	kept := make([]run, 0, len(s.runs))
	for _, r := range s.runs {
		switch {
		case r.at >= at:
			// wholly below the mark, and gone
		case r.end() <= at:
			kept = append(kept, r)
		default:
			kept = append(kept, run{at: r.at, rows: append([]named(nil), r.rows[:at-r.at]...)})
		}
	}
	s.runs = kept
	if s.anchor >= at {
		s.anchor = at - 1
	}
	s.total = serval.AtLeast(at)
}

// grew says n rows appeared at a position: everything from there on stands n
// further down, and the new rows are blanks.
//
// **This is a tree opening a node, and it is not an invalidation.** Every level
// is its own data set with its own cache, so no record became untrue -- what
// changed is which levels the walk visits, and so where each row below the mark
// stands. The view knows how far by, so it can say so rather than forgetting and
// asking again: the rows on screen stay where they are, the twisty flips at once,
// and the blanks fill in when the answer lands.
func (s *spine) grew(at, n int) {
	if n <= 0 || at < 0 {
		return
	}
	s.split(at)
	for i := range s.runs {
		if s.runs[i].at >= at {
			s.runs[i].at += n
		}
	}
	if s.anchor >= at {
		s.anchor += n
	}
	if !s.total.Nothing() {
		s.total.N += n
	}
}

// shrank says the n rows at a position went: they are dropped, and everything
// after them stands n further up.
//
// A tree closing a node, which is the same move the other way about and is exact
// whenever the view holds the end of the subtree -- the rows going are the ones
// in hand.
func (s *spine) shrank(at, n int) {
	if n <= 0 || at < 0 {
		return
	}
	s.split(at)
	s.split(at + n)
	kept := make([]run, 0, len(s.runs))
	for _, r := range s.runs {
		switch {
		case r.at >= at && r.end() <= at+n:
			// inside the stretch that went
		case r.at >= at+n:
			r.at -= n
			kept = append(kept, r)
		default:
			kept = append(kept, r)
		}
	}
	s.runs = kept
	if s.anchor >= at+n {
		s.anchor -= n
	} else if s.anchor > at {
		s.anchor = at
	}
	if !s.total.Nothing() {
		if s.total.N -= n; s.total.N < 0 {
			s.total.N = 0
		}
	}
}

// split breaks any run straddling a position into two, so that every run lies
// wholly on one side of it. It is what lets grew and shrank move runs whole.
func (s *spine) split(at int) {
	for i, r := range s.runs {
		if r.at >= at || r.end() <= at {
			continue
		}
		above := run{at: r.at, rows: append([]named(nil), r.rows[:at-r.at]...)}
		below := run{at: at, rows: append([]named(nil), r.rows[at-r.at:]...)}
		s.runs = append(s.runs[:i], append([]run{above, below}, s.runs[i+1:]...)...)
		return // one run can straddle one position
	}
}

// held is how many identities the spine is carrying, for a test to look at.
func (s *spine) held() int {
	n := 0
	for _, r := range s.runs {
		n += len(r.rows)
	}
	return n
}
