package render

import (
	"strings"
	"testing"
)

// What a cell wears divides into ink, which travels with the glyph, and what is
// painted at the cell, which a miscounting host puts somewhere else. Past the
// point that starts, the second kind is dropped.
func TestOnlyWhatIsPaintedAtTheCellIsDropped(t *testing.T) {
	for _, c := range []struct {
		name, style, want string
	}{
		{"a background goes", "\x1b[0;30;47m", "\x1b[0;30m"},
		{"a bright background goes", "\x1b[0;97;104m", "\x1b[0;97m"},
		{"reverse goes -- it is a background by another name", "\x1b[0;7;32m", "\x1b[0;32m"},
		{"an underline goes: it is drawn at the cell", "\x1b[0;4;32m", "\x1b[0;32m"},
		{"a strike goes for the same reason", "\x1b[0;9;32m", "\x1b[0;32m"},
		{"weight and slant stay: they are the glyph", "\x1b[0;1;3;32m", "\x1b[0;1;3;32m"},
		{"a 256-colour background goes", "\x1b[0;38;5;7;48;5;27m", "\x1b[0;38;5;7m"},
		{"a direct-colour background goes", "\x1b[0;48;2;1;2;3;32m", "\x1b[0;32m"},
		{"several sequences at once", "\x1b[0;92;44m\x1b[1;96;44m", "\x1b[0;92m\x1b[1;96m"},
		{"nothing to drop is left alone", "\x1b[0;32m", "\x1b[0;32m"},
		// Asking for the terminal's own background is still asking, and past the
		// drift it arrives at a cell it was not meant for and clears it.
		{"so does the default background", "\x1b[0;32;49m", "\x1b[0;32m"},
	} {
		if got := dropGround(c.style); got != c.want {
			t.Errorf("%s: dropGround(%q) = %q, want %q", c.name, c.style, got, c.want)
		}
	}
}

// A blank cell loses nothing by the drop: with no glyph of its own it can draw
// its background instead of being given one, so a filled region keeps its shape.
func TestABlankDrawsItsOwnBackground(t *testing.T) {
	for _, c := range []struct {
		name, style, want string
		found             bool
	}{
		{"the ground becomes the ink", "\x1b[0;30;47m", "\x1b[0;37m", true},
		{"and a bright one likewise", "\x1b[0;30;104m", "\x1b[0;94m", true},
		{"a 256-colour ground keeps its index", "\x1b[0;48;5;27m", "\x1b[0;38;5;27m", true},
		{"a direct colour keeps its channels", "\x1b[0;48;2;1;2;3m", "\x1b[0;38;2;1;2;3m", true},
		{"no ground, nothing to draw", "\x1b[0;32m", "\x1b[0m", false},
		// Black is the ground a terminal already shows, so there is nothing to
		// draw in its place -- but it still goes, since a black fill landing on
		// a coloured cell would black that cell out.
		{"black is the ground itself", "\x1b[0;32;40m", "", false},
		{"and so is a black said the long way", "\x1b[0;48;5;0m", "", false},
		{"but bright black is a colour", "\x1b[0;100m", "\x1b[0;90m", true},
		// Weight and slant described a glyph, and a cell drawing its own ground
		// has none. Carried across they would not say the ground's colour but a
		// different one: bold blue is bright blue, which the gutter never was.
		{"the colour comes across and nothing else", "\x1b[1;96;44m", "\x1b[34m", true},
		{"nor does slant", "\x1b[0;3;30;47m", "\x1b[0;37m", true},
	} {
		got, found := groundAsInk(c.style)
		if found != c.found {
			t.Errorf("%s: groundAsInk(%q) found=%v, want %v", c.name, c.style, found, c.found)
		}
		if found && got != c.want {
			t.Errorf("%s: groundAsInk(%q) = %q, want %q", c.name, c.style, got, c.want)
		}
	}
}

// The rule reaches cells no other rule was written for. A row that turns over
// and carries a surviving mark drifts from that run onwards, so the fill on
// everything after it is dropped -- and a blank draws its own instead.
func TestTheFallbackCatchesWhatNothingElseHandled(t *testing.T) {
	row := func(rideSafe bool, cells ...bbCell) string {
		b := newBackBuffer(len(cells)+2, 1)
		b.flipBidi = true
		b.flipRideSafe = rideSafe
		b.begin()
		for i, c := range cells {
			c.width = 1
			b.cur[0][i] = c
		}
		var sb strings.Builder
		b.emitRow(&sb, 0)
		return sb.String()
	}
	// A pointed Hebrew letter, then a blank and a letter wearing a background
	// nothing has given up -- the gutter's own colours, say, on an rtl row.
	pointed := bbCell{runes: []rune("ש" + "ְ")}
	blank := bbCell{style: "\x1b[0;30;44m"}
	glyph := bbCell{runes: []rune("4"), style: "\x1b[0;96;44m"}

	got := row(true, pointed, blank, glyph)
	if !strings.Contains(got, "\x1b[0;34m"+fallbackBlank) {
		t.Errorf("the blank after the run did not draw its own background: %q", got)
	}
	if strings.Contains(got, "44m") {
		t.Errorf("a background survived past the drift: %q", got)
	}
	if !strings.Contains(got, "\x1b[0;96m4") {
		t.Errorf("the glyph did not keep its own colour: %q", got)
	}

	// On a host that places what it is sent, nothing is touched.
	got = row(false, pointed, blank, glyph)
	if !strings.Contains(got, "44m") {
		t.Errorf("a host that can place a fill lost one anyway: %q", got)
	}
	if strings.Contains(got, fallbackBlank) {
		t.Errorf("a host that can place a fill got a shade instead: %q", got)
	}
}

// And it reaches nothing BEFORE the drift starts: those cells are placed where
// they were written.
func TestTheFallbackLeavesWhatComesFirstAlone(t *testing.T) {
	b := newBackBuffer(6, 1)
	b.flipBidi = true
	b.flipRideSafe = true
	b.begin()
	b.cur[0][0] = bbCell{runes: []rune("a"), style: "\x1b[0;30;44m", width: 1}
	b.cur[0][1] = bbCell{runes: []rune("ש" + "ְ"), width: 1}
	var sb strings.Builder
	b.emitRow(&sb, 0)
	if got := sb.String(); !strings.Contains(got, "\x1b[0;30;44ma") {
		t.Errorf("the cell before the run lost a fill it can carry: %q", got)
	}
}

// The mirrored gutter is emitted after all the content, so on a row this host
// cannot count it sits past the drift wherever the drift began -- and its blue
// reaches the screen somewhere inside the text. It draws its ground instead of
// asking for one, in its own shade so an affected gutter still reads as the
// gutter.
func TestTheMirroredGutterDrawsItsOwnGround(t *testing.T) {
	sr, w := testRenderer()
	t.Cleanup(func() { w.ViewState.Direction = "" })

	// Nothing to drift: the gutter asks for its background as it always has.
	if sr.lineDriftsFill(w, "abc שָם") {
		t.Error("a host that places what it is sent was said to drift")
	}

	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true
	w.ViewState.SuppressRTLCombining = false

	if !sr.lineDriftsFill(w, "abc שָם") {
		t.Error("a line carrying a surviving mark was not said to drift")
	}
	if sr.lineDriftsFill(w, "abc שם") {
		t.Error("a line whose runs carry no marks was said to drift")
	}
	// Nothing right-to-left opens a run, so nothing is reordered and nothing
	// drifts, whatever marks the line carries.
	if sr.lineDriftsFill(w, "é") {
		t.Error("a mark outside any run was said to drift")
	}

	// With the marks suppressed there is nothing to miscount.
	w.ViewState.SuppressRTLCombining = true
	if sr.lineDriftsFill(w, "abc שָם") {
		t.Error("a line whose marks are not emitted was said to drift")
	}
}

// Black is the ground a terminal already shows, so a blank wearing it has
// nothing to draw in its place -- and mew's own styles say it constantly. Left
// to the shade it would stand texture over every blank in the content area on
// a row past the drift.
func TestABlackGroundNeedsNoShade(t *testing.T) {
	row := func(cells ...bbCell) string {
		b := newBackBuffer(len(cells)+2, 1)
		b.flipBidi = true
		b.flipRideSafe = true
		b.begin()
		for i, c := range cells {
			c.width = 1
			b.cur[0][i] = c
		}
		var sb strings.Builder
		b.emitRow(&sb, 0)
		return sb.String()
	}
	pointed := bbCell{runes: []rune("ש" + "ְ")}

	// A blank on black, past the drift: nothing to draw.
	got := row(pointed, bbCell{style: "\x1b[0;32;40m"})
	if strings.Contains(got, fallbackBlank) {
		t.Errorf("a blank on black was given a shade: %q", got)
	}
	if strings.Contains(got, "40m") {
		t.Errorf("a black ground survived past the drift, where it would black "+
			"out whatever cell it lands on: %q", got)
	}

	// A blank on a colour still draws it.
	got = row(pointed, bbCell{style: "\x1b[0;32;44m"})
	if !strings.Contains(got, "\x1b[0;34m"+fallbackBlank) {
		t.Errorf("a blank on a colour did not draw its own ground: %q", got)
	}
}
