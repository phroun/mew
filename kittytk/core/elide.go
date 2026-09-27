package core

// Text that does not fit the room it was given.
//
// A trinket asks for the width its whole text needs and is often handed less:
// a row squeezed by its neighbours, a column narrower than a fingerprint, a
// window the user made small. What it draws then has to say that something was
// taken out, rather than stopping mid-word as though the text ended there.
//
// What stands in for the missing text is the ellipsis character, on either
// surface: it is what the tree's columns and the dock already cut with, and
// one convention showing in one window is worth more than the cell it saves
// against three periods.

import "strings"

// Ellipsis stands in for the text that was taken out.
const Ellipsis = "\u2026"

// ElideMode says where the cut goes, and whether one is made at all.
//
// The zero value is ElideEnd, so a trinket that says nothing elides at the end
// -- which is what nearly every caption wants, and what a caption drawn past
// its own edge was failing to do.
type ElideMode int

const (
	ElideEnd    ElideMode = iota // keep the beginning: an ordinary caption
	ElideMiddle                  // keep both ends: a fingerprint, a long path
	ElideStart                   // keep the end: the file at the end of a path
	ElideOff                     // never cut; whatever does not fit is clipped
)

// ParseElideMode reads the word a wire or a config file uses for a mode.
func ParseElideMode(word string) (ElideMode, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "end", "":
		return ElideEnd, true
	case "middle", "mid":
		return ElideMiddle, true
	case "start", "begin":
		return ElideStart, true
	case "off", "none", "false":
		return ElideOff, true
	}
	return ElideEnd, false
}

// String is the word for a mode, which is the one the wire takes back.
func (m ElideMode) String() string {
	switch m {
	case ElideMiddle:
		return "middle"
	case ElideStart:
		return "start"
	case ElideOff:
		return "off"
	}
	return "end"
}

// ElideMode reports where this trinket cuts text that does not fit.
func (w *TrinketBase) ElideMode() ElideMode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.elide
}

// SetElideMode chooses where this trinket cuts text that does not fit. The
// text is unchanged -- only what is drawn of it -- so nothing that reads the
// trinket's own text sees the cut.
func (w *TrinketBase) SetElideMode(m ElideMode) {
	w.mu.Lock()
	if w.elide == m {
		w.mu.Unlock()
		return
	}
	w.elide = m
	w.mu.Unlock()
	w.Update()
}

// ElideText is what to draw of text in avail units of room, and whether the
// whole of it is being shown.
//
// The second answer is about the ROOM, not about the mode: text too long for
// its space is cut short whether this trinket elides it or the surface clips
// it, and either way there is more to read than is on the screen. It is what
// says a tooltip has something to offer.
func (w *TrinketBase) ElideText(text string, avail Unit) (shown string, whole bool) {
	return w.ElideTextWith(text, avail, func(s string) Unit { return w.MeasureText(w.CellRun(s)) })
}

// ElideTextWith is ElideText for text this trinket does not measure the way it
// measures its own: a caption in the three-quarter face, a run counted in
// screen width rather than local units. The mode is the trinket's either way --
// only the ruler changes.
func (w *TrinketBase) ElideTextWith(text string, avail Unit, measure func(string) Unit) (shown string, whole bool) {
	if measure(text) <= avail {
		w.noteCut(text, true)
		return text, true
	}
	// Cut or clipped, there is more to read than is on the screen, and that is
	// what a tooltip has to offer.
	w.noteCut(text, false)
	if w.ElideMode() == ElideOff {
		return text, false
	}
	return Elide(text, avail, w.ElideMode(), measure), false
}

// Elide cuts text to fit avail under the given mode, measured by measure.
//
// It is a binary search rather than a scan: text is never NARROWER for holding
// one more character -- the assumption eliding rests on to begin with -- so
// "fits" is downward closed, and every caption on the screen runs this on every
// paint.
func Elide(text string, avail Unit, mode ElideMode, measure func(string) Unit) string {
	if mode == ElideOff || measure(text) <= avail {
		return text
	}
	runes := []rune(text)
	fits := func(kept int) bool { return measure(elideKeeping(runes, kept, mode)) <= avail }

	// lo keeps nothing and fits or does not on the ellipsis alone; hi is the
	// first count known not to fit.
	lo, hi := 0, len(runes)
	for lo < hi-1 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		if measure(Ellipsis) <= avail {
			return Ellipsis
		}
		return ""
	}
	return elideKeeping(runes, lo, mode)
}

// elideKeeping is the text with kept runes of it left, arranged the way the
// mode asks: the beginning, the end, or both ends around the cut.
func elideKeeping(runes []rune, kept int, mode ElideMode) string {
	if kept >= len(runes) {
		return string(runes)
	}
	if kept <= 0 {
		return Ellipsis
	}
	switch mode {
	case ElideStart:
		return Ellipsis + string(runes[len(runes)-kept:])
	case ElideMiddle:
		// The head keeps the odd rune: reading starts there, and a name is
		// recognised from its beginning more often than from its end.
		head := (kept + 1) / 2
		tail := kept - head
		return string(runes[:head]) + Ellipsis + string(runes[len(runes)-tail:])
	}
	return string(runes[:kept]) + Ellipsis
}
