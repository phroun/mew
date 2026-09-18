package trinkets

// What a list knows about where its rows are.
//
// A list draws positions and a source answers in identities, and the spine is
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
// **A row the spine cannot name is not an error.** It is a BLANK: the list
// knows a row stands there, because it knows how long the sequence is, and
// knows nothing else about it yet. Blank is a state a row is drawn in, not a
// failure to draw one, and it is what lets a thumb drag stay smooth while the
// records catch up.

import "github.com/phroun/serval"

// spineKept is how many identities one list remembers. A reader looks at a
// screenful and scrolls through a few more, so several screenfuls is already
// generosity -- and a list that held more would be keeping rows it will have
// re-asked for by the time it wants them.
const spineKept = 512

// A run is a stretch of the sequence the list has been told about: where it
// starts, and the identities from there on with no gaps in them.
type run struct {
	at  int // the position of ids[0] in the sequence
	ids []*serval.Value
}

func (r run) end() int { return r.at + len(r.ids) } // one past its last row

// A spine is the positions a list knows identities for, and how long the
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
// for one that would not say. A list draws a true thumb, a shrinking one, or
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

// place writes down a run of identities beginning at a position.
//
// Where it touches or overlaps a run already held, the two become one: a reader
// scrolling asks for what comes after what it has, and its knowledge should
// grow rather than fragment. Overlap is resolved in favour of what just
// arrived, that being the more recent word on where those rows are.
func (s *spine) place(at int, ids []*serval.Value) {
	if at < 0 || len(ids) == 0 {
		return
	}
	fresh := run{at: at, ids: append([]*serval.Value(nil), ids...)}

	kept := make([]run, 0, len(s.runs)+1)
	for _, r := range s.runs {
		switch {
		case r.end() < fresh.at || r.at > fresh.end():
			kept = append(kept, r) // apart, and stays its own run
		default:
			fresh = join(r, fresh)
		}
	}
	s.runs = insert(kept, fresh)
	s.anchor = at
	s.trim(at, at+len(ids))
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
	ids := make([]*serval.Value, end-at)
	copy(ids[held.at-at:], held.ids)
	copy(ids[fresh.at-at:], fresh.ids)
	return run{at: at, ids: ids}
}

// insert puts a run among the others, in position order.
func insert(runs []run, r run) []run {
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
// list that scrolled a long way would be holding the whole sequence under the
// name of holding a window of it.
//
// The run holding the reader is never dropped, only shortened.
func (s *spine) trim(from, to int) {
	for i, r := range s.runs {
		if len(r.ids) > spineKept {
			s.runs[i] = cut(r, from, to)
		}
	}

	held := 0
	for _, r := range s.runs {
		held += len(r.ids)
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
		held -= len(s.runs[worst].ids)
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
	if len(r.ids) <= spineKept {
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
	ids := make([]*serval.Value, spineKept)
	copy(ids, r.ids[beg-r.at:])
	return run{at: beg, ids: ids}
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

// idAt is the identity of the row at a position, and false for a row the list
// knows is there and nothing else about -- which is a blank, and is ordinary.
func (s *spine) idAt(at int) (*serval.Value, bool) {
	for _, r := range s.runs {
		if at >= r.at && at < r.end() {
			return r.ids[at-r.at], true
		}
	}
	return nil, false
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
		for i, held := range r.ids {
			if held != nil && serval.Key(held) == want {
				return r.at + i, true
			}
		}
	}
	return 0, false
}

// forget drops every identity and keeps the length.
//
// What a notice says is that the rows have MOVED, so every position the list
// holds is suspect -- and which ones is exactly what is no longer known. How
// many there are is a different fact and survives until something says
// otherwise, which is what keeps the thumb from jumping to nothing and back
// every time the data changes underneath it.
func (s *spine) forget() {
	s.runs = nil
	s.anchor = 0
}

// held is how many identities the spine is carrying, for a test to look at.
func (s *spine) held() int {
	n := 0
	for _, r := range s.runs {
		n += len(r.ids)
	}
	return n
}
