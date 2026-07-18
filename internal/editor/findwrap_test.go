package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Content used by the wrap tests: matches on lines 0, 2 and 4, with the
// search origin parked mid-file (line 2) so wrap and loop are distinct.
const wrapDoc = "foo\nxx\nfoo\nyy\nfoo\n"

// A forward search that starts over from the bottom announces "Search
// continued from top"; when it then crosses back over its origin, it
// announces "Search has looped"; and the cycle repeats on the next full
// revolution.
func TestFindWrapAndLoopForward(t *testing.T) {
	e, w := newTestEditor(t, wrapDoc)
	w.SetCursorPos(window.Position{Line: 2, Rune: 0}) // origin mid-file
	e.startFind("foo", "", "", true, true, false)
	if w.CursorPos().Line != 4 {
		t.Fatalf("first match should be line 4, got %d", w.CursorPos().Line)
	}
	if hasNotification(e, "Search continued") || hasNotification(e, "Search has looped") {
		t.Fatal("no wrap notification before wrapping")
	}

	e.PawScript.ExecuteAsync("find_next") // wraps to line 0
	if w.CursorPos().Line != 0 {
		t.Fatalf("wrap should land on line 0, got %d", w.CursorPos().Line)
	}
	if !hasNotification(e, "Search continued from top") {
		t.Fatal("expected 'Search continued from top'")
	}

	clearNotifications(e)
	e.PawScript.ExecuteAsync("find_next") // line 2 — crosses the origin
	if w.CursorPos().Line != 2 {
		t.Fatalf("should reach line 2, got %d", w.CursorPos().Line)
	}
	if !hasNotification(e, "Search has looped") {
		t.Fatal("expected 'Search has looped' on crossing the origin")
	}

	clearNotifications(e)
	e.PawScript.ExecuteAsync("find_next") // line 4 — plain step, no messages
	if hasNotification(e, "Search continued") || hasNotification(e, "Search has looped") {
		t.Fatal("no notification expected on a plain forward step")
	}

	e.PawScript.ExecuteAsync("find_next") // wraps again: cycle restarts
	if !hasNotification(e, "Search continued from top") {
		t.Fatal("expected the wrap message again on the second revolution")
	}
}

// A backwards search announces "Search continued from bottom" on wrap, then
// "Search has looped" when it crosses the origin from above.
func TestFindWrapAndLoopBackward(t *testing.T) {
	e, w := newTestEditor(t, wrapDoc)
	w.SetCursorPos(window.Position{Line: 2, Rune: 0})
	e.startFind("foo", "b", "", true, true, false)
	if w.CursorPos().Line != 0 {
		t.Fatalf("first backwards match should be line 0, got %d", w.CursorPos().Line)
	}

	e.PawScript.ExecuteAsync("find_next") // wraps to the bottom match (line 4)
	if w.CursorPos().Line != 4 {
		t.Fatalf("backwards wrap should land on line 4, got %d", w.CursorPos().Line)
	}
	if !hasNotification(e, "Search continued from bottom") {
		t.Fatal("expected 'Search continued from bottom'")
	}

	clearNotifications(e)
	e.PawScript.ExecuteAsync("find_next") // line 2 — at the origin: looped
	if w.CursorPos().Line != 2 {
		t.Fatalf("should reach line 2, got %d", w.CursorPos().Line)
	}
	if !hasNotification(e, "Search has looped") {
		t.Fatal("expected 'Search has looped'")
	}
}

// When the search originates at the very start of the buffer, the first
// wrapped match is already at/past the origin — that IS a full loop, and is
// announced as such.
func TestFindWrapAtOriginTop(t *testing.T) {
	e, w := newTestEditor(t, wrapDoc)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.startFind("foo", "", "", true, true, false) // matches line 2
	e.PawScript.ExecuteAsync("find_next")         // line 4
	e.PawScript.ExecuteAsync("find_next")         // wraps to line 0 == origin
	if w.CursorPos().Line != 0 {
		t.Fatalf("wrap should land on line 0, got %d", w.CursorPos().Line)
	}
	if !hasNotification(e, "Search has looped") {
		t.Fatal("wrapping straight onto the origin is a full loop")
	}
}

// The origin cursor is a live garland cursor: edits above it slide it, so
// loop detection stays anchored to the logical position, not a stale offset.
func TestFindOriginSlidesWithEdits(t *testing.T) {
	e, w := newTestEditor(t, wrapDoc)
	w.SetCursorPos(window.Position{Line: 2, Rune: 0})
	e.startFind("foo", "", "", true, true, false) // origin at old line 2; caret at line 4

	// Insert two lines at the very top: the origin's logical line is now 4.
	w.Buffer.InsertLine(0, "pad")
	w.Buffer.InsertLine(0, "pad")

	e.PawScript.ExecuteAsync("find_next") // wraps to (old line 0, now line 2)
	if !hasNotification(e, "Search continued from top") {
		t.Fatal("expected wrap message")
	}
	clearNotifications(e)
	e.PawScript.ExecuteAsync("find_next") // old line 2 (now 4) — the slid origin
	if !hasNotification(e, "Search has looped") {
		t.Fatal("loop detection should follow the slid origin")
	}
}
