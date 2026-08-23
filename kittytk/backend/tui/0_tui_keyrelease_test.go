package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A key coming back up becomes a KeyReleaseEvent.
//
// This backend produced none at all. direct-key-handler marks a release by
// trailing ":Release" on the name, and nothing here looked for it, so the whole
// decorated name fell through the press path as if a key called "a:Release" had
// been struck. A hosted child that wanted releases — a browser tracking a held
// key — could not be given any.
func TestReleaseNamesBecomeReleaseEvents(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	for _, c := range []struct {
		key  string
		want string
		what string
	}{
		{"a:Release", "a", "a plain letter"},
		{"Return:Release", "Return", "a named key"},
		{"^A:Release", "^A", "a Control chord keeps its caret"},
		{"M-Up:Release", "M-Up", "modifier prefixes stay on the name"},
	} {
		b.handleKey(c.key)
		select {
		case ev := <-b.eventQueue:
			kr, ok := ev.(core.KeyReleaseEvent)
			if !ok {
				t.Errorf("%s (%s): dispatched as %T, want KeyReleaseEvent",
					c.key, c.what, ev)
				continue
			}
			if kr.Key != c.want {
				t.Errorf("%s (%s): Key = %q, want %q — the marker comes off here, "+
					"matching what the SDL backend puts in this field",
					c.key, c.what, kr.Key, c.want)
			}
		default:
			t.Errorf("%s (%s): no event dispatched", c.key, c.what)
		}
	}
}

// The modifiers ride along with the release, so a consumer can tell Shift+Up
// coming up from Up coming up.
func TestReleaseCarriesItsModifiers(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	b.handleKey("M-S-Up:Release")
	select {
	case ev := <-b.eventQueue:
		kr, ok := ev.(core.KeyReleaseEvent)
		if !ok {
			t.Fatalf("dispatched as %T, want KeyReleaseEvent", ev)
		}
		if kr.Modifiers&core.MegaModifier == 0 || kr.Modifiers&core.ShiftModifier == 0 {
			t.Errorf("Modifiers = %v, want Mega and Shift both set", kr.Modifiers)
		}
	default:
		t.Fatal("no event dispatched")
	}
}

// A press is still a press, and a repeat is still a press.
//
// A repeat IS another press — every consumer here already treats it as one, and
// the distinction only matters to a child that negotiated event reporting for
// itself, which gets it from the name the trinket forwards rather than from
// this event type.
func TestPressesAreUnaffected(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	for _, c := range []struct{ key, want string }{
		{"a", "a"},
		{"a:Repeat", "a"},
		{"Return", "Return"},
	} {
		b.handleKey(c.key)
		select {
		case ev := <-b.eventQueue:
			kp, ok := ev.(core.KeyPressEvent)
			if !ok {
				t.Errorf("%s: dispatched as %T, want KeyPressEvent", c.key, ev)
				continue
			}
			if kp.Key != c.want {
				t.Errorf("%s: Key = %q, want %q", c.key, kp.Key, c.want)
			}
		default:
			t.Errorf("%s: no event dispatched", c.key)
		}
	}
}

// A held key becomes a press that SAYS it is a repeat.
//
// The marker came off the name and went nowhere: handleKey trimmed ":Repeat"
// and dispatched a press indistinguishable from a struck key. Trimming the name
// is right — every consumer reads Key as a plain key and one wearing a suffix
// would match nothing — but the fact was the outer terminal's to report and
// this backend's to pass on, and dropping it left a hosted browser unable to
// tell a held key from a drummed one.
func TestRepeatNamesBecomePressesThatSaySo(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	for _, c := range []struct {
		key    string
		want   string
		repeat bool
		what   string
	}{
		{"a:Repeat", "a", true, "a held letter"},
		{"M-Up:Repeat", "M-Up", true, "modifier prefixes stay on the name"},
		{"a", "a", false, "a struck key is not a repeat"},
	} {
		b.handleKey(c.key)
		select {
		case ev := <-b.eventQueue:
			kp, ok := ev.(core.KeyPressEvent)
			if !ok {
				t.Errorf("%s (%s): dispatched as %T, want KeyPressEvent", c.key, c.what, ev)
				continue
			}
			if kp.Key != c.want {
				t.Errorf("%s (%s): Key = %q, want %q — the marker belongs on the "+
					"event, not the name", c.key, c.what, kp.Key, c.want)
			}
			if kp.Repeat != c.repeat {
				t.Errorf("%s (%s): Repeat = %v, want %v", c.key, c.what, kp.Repeat, c.repeat)
			}
		default:
			t.Errorf("%s (%s): nothing dispatched", c.key, c.what)
		}
	}
}
