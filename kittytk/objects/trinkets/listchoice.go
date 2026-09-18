package trinkets

// Which rows are chosen.
//
// A selection is kept by IDENTITY, not by position, for the same reason
// everything else here is: a position means nothing once the rows move, and a
// list reading a source of its own is told about positions a screenful at a
// time. What is chosen has to survive not being on screen.
//
// # Two shapes, because "all of them" is not a list
//
// A list told to select everything cannot write down what everything IS. It may
// never have been told: a million rows of which forty have been seen, and
// enumerating them would mean asking for a million records to throw away. So a
// selection is either the rows it NAMES, or everything EXCEPT the rows it names,
// and the second shape is what makes selecting all of something cost nothing.
//
// It also gives a blank row an honest answer. A row the list cannot name is not
// in any set of names -- so it is unselected where the selection is a list of
// what is in, and selected where it is a list of what is out, which is exactly
// right both times.

import (
	"sort"

	"github.com/phroun/serval"
)

type selection struct {
	// all turns the meaning of named over: with it, named is what is SPARED
	// rather than what is chosen.
	all   bool
	named map[string]*serval.Value
}

func (s *selection) init() {
	if s.named == nil {
		s.named = map[string]*serval.Value{}
	}
}

// clear chooses nothing.
func (s *selection) clear() { s.all, s.named = false, map[string]*serval.Value{} }

// everything chooses every row, which costs nothing and names nothing.
func (s *selection) everything() { s.all, s.named = true, map[string]*serval.Value{} }

// only chooses one row and nothing else, which is what a single-selection list
// does every time the current row moves.
func (s *selection) only(id *serval.Value) {
	s.clear()
	s.set(id, true)
}

// set chooses a row or unchooses it, whichever shape the selection is in.
func (s *selection) set(id *serval.Value, on bool) {
	if id == nil {
		return
	}
	s.init()
	if on == s.all {
		// Already what it is by default: the row has nothing to say of its own.
		delete(s.named, serval.Key(id))
		return
	}
	s.named[serval.Key(id)] = id
}

// has reports whether a row is chosen.
func (s *selection) has(id *serval.Value) bool {
	if id == nil {
		// A blank row, which is in no set of names. Chosen where the names are
		// what is left OUT, and not chosen where they are what is in.
		return s.all
	}
	_, named := s.named[serval.Key(id)]
	return named != s.all
}

// empty reports whether nothing at all is chosen.
func (s *selection) empty() bool { return !s.all && len(s.named) == 0 }

// ids is what the selection names, which is what is chosen where it is a list of
// what is in and what is spared where it is a list of what is out.
func (s *selection) ids() []*serval.Value {
	out := make([]*serval.Value, 0, len(s.named))
	for _, id := range s.named {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return serval.Key(out[i]) < serval.Key(out[j]) })
	return out
}

// shift renumbers integer identities from a position on, for a list whose rows
// are keyed BY position.
//
// Only a made source is keyed that way -- an item inserted at three rewrites the
// keys of everything after it, exactly as the indices shift -- so this is the
// one place a selection is touched by something that is not a selection. A
// declared source's keys are its own and are not positions, so nothing renumbers
// them however their values happen to be spelled.
func (s *selection) shift(at, by int) {
	if len(s.named) == 0 {
		return
	}
	moved := make(map[string]*serval.Value, len(s.named))
	for _, id := range s.named {
		if id.IsInt && int(id.Int) >= at {
			id = serval.NewInt(id.Int + int64(by))
		}
		moved[serval.Key(id)] = id
	}
	s.named = moved
}

// drop forgets one identity outright, which is what a row being removed means
// for a selection that named it.
func (s *selection) drop(id *serval.Value) {
	if id != nil && s.named != nil {
		delete(s.named, serval.Key(id))
	}
}

// --- what the list makes of it -------------------------------------------

// SelectsEverything reports whether the selection is "all of them" rather than a
// list of rows.
//
// It is what a caller asks before SelectedIndexes, which on a list of a million
// rows will hand back a million figures because that is what was chosen. A
// caller that only wants to know whether a row is in can ask IsSelected and
// never enumerate anything.
func (l *ListView) SelectsEverything() bool { return l.chosen.all }

// IsSelected reports whether the row at a position is chosen.
//
// A BLANK row answers too: it is in no list of names, so it is chosen exactly
// when the names are what is left out.
func (l *ListView) IsSelected(index int) bool {
	if index < 0 || index >= l.Count() {
		return false
	}
	id, _ := l.bones.idAt(index)
	return l.chosen.has(id)
}

// SetSelected chooses the row at a position, or unchooses it.
//
// It reads the row first, because choosing one means naming it and a blank row
// has no name. Where the row cannot be named even then -- a source that has not
// answered yet -- nothing is chosen rather than something else being chosen by
// mistake.
func (l *ListView) SetSelected(index int, selected bool) {
	if index < 0 || index >= l.Count() || l.selectionMode == NoSelection {
		return
	}
	l.window(index, 1)
	id, ok := l.bones.idAt(index)
	if !ok {
		return
	}
	if l.selectionMode == SingleSelection && selected {
		l.chosen.only(id)
	} else {
		l.chosen.set(id, selected)
	}
	l.Update()
	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// SelectedIndexes is where the chosen rows stand, in order.
//
// **In order**, which it was not before: it was read off a map and came back in
// whatever order the map felt like, so two runs of the same program disagreed.
//
// Where everything is chosen this is every position there is, less the ones
// spared -- which on a long list is a long slice, because that is what was
// chosen. SelectsEverything is the question to ask instead.
func (l *ListView) SelectedIndexes() []int {
	n := l.Count()
	var out []int
	if l.chosen.all {
		for i := 0; i < n; i++ {
			if l.IsSelected(i) {
				out = append(out, i)
			}
		}
		return out
	}
	for _, id := range l.chosen.ids() {
		if at, ok := l.bones.posOf(id); ok && at < n {
			out = append(out, at)
		}
	}
	sort.Ints(out)
	return out
}

// SelectedIDs is what the selection names: the chosen rows, or -- where
// everything is chosen -- the ones spared.
//
// Unlike SelectedIndexes this never enumerates a sequence and never needs the
// list to have been told where anything stands, so it is the one to keep hold of
// across a scroll.
func (l *ListView) SelectedIDs() []*serval.Value { return l.chosen.ids() }

// SelectAll chooses every row, at no cost and without naming any of them.
func (l *ListView) SelectAll() {
	if l.selectionMode == SingleSelection || l.selectionMode == NoSelection {
		return
	}
	l.chosen.everything()
	l.Update()
	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// ClearSelection chooses nothing.
func (l *ListView) ClearSelection() {
	l.chosen.clear()
	l.Update()
	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}
