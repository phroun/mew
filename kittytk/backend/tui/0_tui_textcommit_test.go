package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Text the terminal received with NO KEY behind it is a commit, not a
// keystroke.
//
// The "kitty" protocol reports one as keycode 0, and an input method finishing
// a composition is what sends it. Spelled as a key it would be a keystroke that
// never happened — and the text a key carries is read as ONE rune from its
// name, so any commit longer than a single character would have arrived with
// its text silently empty. Which is most Japanese ones.
func TestATextEventBecomesACommitAndNotAKeystroke(t *testing.T) {
	for _, c := range []struct{ what, key, want string }{
		{"a multi-character commit", "Text:今日", "今日"},
		{"a single character", "Text:あ", "あ"},
		{"text holding a colon of its own", "Text:a:b", "a:b"},
		{"text holding a space", "Text:x y", "x y"},
	} {
		b := &TUIBackend{
			metrics:    core.DefaultCellMetrics(),
			eventQueue: make(chan core.Event, 8),
		}
		b.handleKey(c.key)
		select {
		case ev := <-b.eventQueue:
			commit, ok := ev.(core.TextCommitEvent)
			if !ok {
				t.Errorf("%s (%q): dispatched as %T, want TextCommitEvent",
					c.what, c.key, ev)
				continue
			}
			if commit.Text != c.want {
				t.Errorf("%s: committed %q, want %q", c.what, commit.Text, c.want)
			}
		default:
			t.Errorf("%s (%q): nothing dispatched", c.what, c.key)
		}
	}
}

// An empty one says nothing and is consumed, never passed on to be typed as a
// name.
func TestAnEmptyTextEventDispatchesNothing(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}
	b.handleKey("Text:")
	select {
	case ev := <-b.eventQueue:
		t.Errorf("an empty text event dispatched %#v, want nothing", ev)
	default:
	}
}

// And an ordinary key is untouched by the branch.
func TestAnOrdinaryKeyIsStillAKeystroke(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}
	b.handleKey("a")
	select {
	case ev := <-b.eventQueue:
		kp, ok := ev.(core.KeyPressEvent)
		if !ok || kp.Text != "a" {
			t.Errorf("a plain letter dispatched %#v, want a KeyPressEvent typing %q", ev, "a")
		}
	default:
		t.Error("a plain letter dispatched nothing")
	}
}
