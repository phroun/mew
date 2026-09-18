package trinkets

// The current row, as both of the things it is.
//
// A list is driven by POSITION -- arrow down, page down, click row four -- and a
// source answers in IDENTITIES. Both are real and neither is the other, so the
// current row is a pair and the two are kept in step.
//
// Four states, from two fields:
//
//	index  id     |
//	-1     nil    | nothing is selected
//	i      nil    | selected by POSITION, identity not known yet
//	i      id     | both known, which is the settled case
//	-1     id     | selected by IDENTITY, position not known yet
//
// The two middle-of-the-road states are not loose ends. **Selecting a blank row
// has to work**, because moving the selection and scrolling are one motion here:
// arrow down past what is loaded picks the next row before anybody knows what
// record is there, and it becomes selected by identity when the record arrives.
// And **an identity has to be settable before it is resolved**, because an
// application restoring a selection knows the id it saved and nothing about
// where that row now stands.
//
// # Undefined resolves to nothing, but not before the order is settled
//
// An identity the sequence does not hold reports as nothing selected, at index
// -1. But "does not hold" is a claim, and it cannot be made while the answer is
// still arriving: a row that has not been mentioned yet is not a row that is
// absent. So an unresolved identity is PENDING -- reported back as asked for,
// with no position -- and becomes undefined only once the order is settled and
// it still is not there.

import "github.com/phroun/serval"

// SelectedID is the identity of the current row.
//
// Nil for nothing selected, and nil for a row selected by position whose record
// has not arrived. An identity asked for and not yet resolved reads back as
// itself, because it is still in play.
func (l *ListView) SelectedID() *serval.Value { return l.currentID }

// SetSelectedID moves the selection to a record by identity.
//
// Where the list knows where that record stands, this is the same as setting the
// index. Where it does not, the identity is held and the selection reports no
// position until the record turns up -- or is dropped once the order settles
// without it.
//
// **Unless the list already holds the whole sequence**, in which case there is
// nothing to wait for: an identity that is not in a sequence entirely in hand is
// absent, not unmentioned, and the answer is nothing selected straight away.
func (l *ListView) SetSelectedID(id *serval.Value) {
	if id == nil {
		l.setCurrent(-1, nil)
		return
	}
	if at, ok := l.bones.posOf(id); ok {
		l.setCurrent(at, id)
		return
	}
	if l.spineHolds(0, l.Count()) {
		// The list holds the WHOLE sequence and this identity is not in it, so
		// it is absent rather than unmentioned. Nothing to wait for, and the
		// answer is nothing selected.
		l.setCurrent(-1, nil)
		return
	}
	l.setCurrent(-1, id)
}

// IndexOf is where a record stands, and false for one the list cannot place.
//
// False is not "not in the sequence". It is "not in what this list holds", which
// for a record off the far end of a long sequence is the ordinary answer -- and
// is why this does not answer -1, that being the figure for nothing selected.
func (l *ListView) IndexOf(id *serval.Value) (int, bool) {
	return l.bones.posOf(id)
}

// IDAt is the identity of the row at a position, and false for a BLANK row --
// one the list knows is there and knows nothing else about yet.
func (l *ListView) IDAt(index int) (*serval.Value, bool) {
	return l.bones.idAt(index)
}

// setCurrent is the one place the pair moves, so there is one place the two can
// fall out of step and it is this one.
func (l *ListView) setCurrent(index int, id *serval.Value) {
	l.currentIndex = index
	l.currentID = id
}

// resolve fills in whichever half of the current row has become knowable.
//
// Called after an answer lands. A row selected by position learns its identity;
// an identity asked for learns its position. Neither is a change of selection --
// the same row was current before and after -- so neither tells anybody.
func (l *ListView) resolve() {
	switch {
	case l.currentIndex >= 0 && l.currentID == nil:
		if id, ok := l.bones.idAt(l.currentIndex); ok {
			l.currentID = id
		}
	case l.currentIndex < 0 && l.currentID != nil:
		if at, ok := l.bones.posOf(l.currentID); ok {
			l.currentIndex = at
			if l.selectionMode == SingleSelection {
				l.selectedItems = map[int]bool{at: true}
			}
		}
	}
}

// settled says the order is decided, which is the first moment an identity
// nobody mentioned can honestly be called absent.
//
// Before this, silence about a row means the answer has not reached it. After
// it, silence means it is not there -- and an identity that is not there is
// nothing selected, at index -1, which is what an application gets back when it
// restores a selection to a record that has since gone.
func (l *ListView) settled() {
	if l.currentIndex < 0 && l.currentID != nil {
		if _, ok := l.bones.posOf(l.currentID); !ok {
			l.currentID = nil
			if l.onCurrentChanged != nil {
				l.onCurrentChanged(-1)
			}
		}
	}
}
