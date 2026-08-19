package platform

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// An insertion point is taken back only on evidence.
//
// It used to be taken back on silence: whatever a frame did not report, it was
// held to have denied. A frame reports by PAINTING, so the silence of one
// clipped to a damaged region — or one that caught a cursor in the dark half
// of its blink — was read as "text is not going anywhere", and every input
// method anchored on it was told the text it was composing for had gone.

// areaCall is one SetTextInputArea the surface was given.
type areaCall struct {
	x, y    core.Unit
	visible bool
}

// caretSurface records what a frame's outcome did to a surface. Only the
// members ApplyTextCaret touches are real.
type caretSurface struct {
	areas   []areaCall
	visible []bool
	style   int
	moved   []areaCall
}

func (s *caretSurface) Size() core.UnitSize       { return core.UnitSize{} }
func (s *caretSurface) Metrics() core.CellMetrics { return core.CellMetrics{} }
func (s *caretSurface) SetHandler(SurfaceHandler) {}
func (s *caretSurface) Invalidate(core.UnitRect)  {}
func (s *caretSurface) SetCursorVisible(v bool)   { s.visible = append(s.visible, v) }
func (s *caretSurface) SetCursorStyle(style int)  { s.style = style }
func (s *caretSurface) SetCursorPosition(x, y core.Unit) {
	s.moved = append(s.moved, areaCall{x: x, y: y})
}
func (s *caretSurface) SetTextInputArea(x, y core.Unit, visible bool) {
	s.areas = append(s.areas, areaCall{x, y, visible})
}

// lastArea is what the surface was last told, and whether it was told anything.
func (s *caretSurface) lastArea() (areaCall, bool) {
	if len(s.areas) == 0 {
		return areaCall{}, false
	}
	return s.areas[len(s.areas)-1], true
}

// A frame that reported a position sets it, which is the ordinary case and the
// one every other rule is measured against.
func TestReportedPositionIsApplied(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{
		Caret:    core.TextCaret{InputArea: true, X: 12, Y: 4},
		Sink:     core.TextSinkPresent,
		Complete: true,
	})
	got, told := s.lastArea()
	if !told || got != (areaCall{12, 4, true}) {
		t.Errorf("area = %+v told=%v, want (12,4) visible", got, told)
	}
}

// A PARTIAL frame that reported nothing has said nothing. This is the blink,
// and the damage-clipped repaint, and anything else that keeps the trinket
// that reports from painting.
func TestPartialFrameSilenceChangesNothing(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{
		Sink:     core.TextSinkPresent,
		Complete: false,
	})
	if got, told := s.lastArea(); told {
		t.Errorf("a partial frame withdrew the insertion point: %+v", got)
	}
	if len(s.visible) != 0 {
		t.Errorf("a partial frame hid the caret: %v", s.visible)
	}
}

// A COMPLETE frame that reported nothing has said there is none: the caret is
// scrolled out of view or hidden, and an input method should fall back to the
// platform's own placement rather than anchor where it used to be.
func TestCompleteFrameSilenceWithdraws(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{
		Sink:     core.TextSinkPresent,
		Complete: true,
	})
	got, told := s.lastArea()
	if !told || got.visible {
		t.Errorf("area = %+v told=%v, want a withdrawal", got, told)
	}
}

// Focus on something that does not type withdraws whatever the frame said.
// This is the one answer that does not need a complete frame behind it: where
// text is going is a question about focus, and focus has answered it.
func TestFocusOnANonSinkWithdraws(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{
		Caret:    core.TextCaret{InputArea: true, X: 9, Y: 9},
		Sink:     core.TextSinkAbsent,
		Complete: false,
	})
	got, told := s.lastArea()
	if !told || got.visible {
		t.Errorf("area = %+v told=%v, want a withdrawal", got, told)
	}
}

// Unknown is not "no". A caller with no focus manager to ask has learned
// nothing, and a partial frame adds nothing to it, so nothing is said.
func TestUnknownSinkOnAPartialFrameSaysNothing(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{Sink: core.TextSinkUnknown, Complete: false})
	if got, told := s.lastArea(); told {
		t.Errorf("an unknown sink withdrew the insertion point: %+v", got)
	}
}

// The DRAWN caret follows the same evidence rule — a partial frame that drew
// none is not a surface being told to stop.
func TestDrawnCaretIsNotHiddenByAPartialFrame(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{Sink: core.TextSinkPresent, Complete: false})
	if len(s.visible) != 0 {
		t.Errorf("SetCursorVisible%v on a partial frame", s.visible)
	}

	full := &caretSurface{}
	ApplyTextCaret(full, TextInputFrame{Sink: core.TextSinkPresent, Complete: true})
	if len(full.visible) != 1 || full.visible[0] {
		t.Errorf("SetCursorVisible%v, want one false on a complete frame", full.visible)
	}
}

// A frame asking for a drawn caret gets one, wearing its shape, wherever it
// said — the path that was never in question and must not have moved.
func TestDrawnCaretStillPlaced(t *testing.T) {
	s := &caretSurface{}
	ApplyTextCaret(s, TextInputFrame{
		Caret:    core.TextCaret{Visible: true, InputArea: true, X: 3, Y: 7, Style: 5},
		Sink:     core.TextSinkPresent,
		Complete: true,
	})
	if s.style != 5 {
		t.Errorf("style = %d, want 5", s.style)
	}
	if len(s.moved) != 1 || s.moved[0] != (areaCall{x: 3, y: 7}) {
		t.Errorf("moved to %+v, want (3,7)", s.moved)
	}
	if len(s.visible) != 1 || !s.visible[0] {
		t.Errorf("SetCursorVisible%v, want one true", s.visible)
	}
}
