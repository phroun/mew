package trinkets

// echo=password_on_edit masks a field the way echo=password does, except that
// the character just typed shows as itself for a moment -- long enough to
// check the key, and gone at once if anything else happens to the field.

import (
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// onEditField is a masked field on a desktop, so it can reach the timer that
// takes a revealed character away again.
func onEditField(t *testing.T) (*Desktop, *TextInput) {
	t.Helper()
	d := NewDesktop()
	ti := NewTextInput()
	ti.SetParent(d)
	ti.SetEchoMode(EchoPasswordOnEdit)
	ti.SetBounds(core.UnitRect{Width: 320, Height: 16})
	return d, ti
}

func shownAs(ti *TextInput) string { return string(ti.getDisplayText()) }

// expire runs the reveal timer as though its second had passed.
func expire(t *testing.T, ti *TextInput) {
	t.Helper()
	if ti.revealTimer == nil {
		t.Fatal("no reveal timer is running")
	}
	ti.revealTimer.Callback()
}

func TestTheKeyJustTypedShowsForAMoment(t *testing.T) {
	_, ti := onEditField(t)
	for _, k := range []string{"a", "b", "c"} {
		ti.HandleKeyPress(core.KeyPressEvent{Key: k})
	}
	if got := shownAs(ti); got != "••c" {
		t.Errorf("after typing abc the field shows %q, want %q", got, "••c")
	}
	if got := ti.revealTimer.Interval; got != time.Second {
		t.Errorf("the reveal lasts %v, want a second", got)
	}
	expire(t, ti)
	if got := shownAs(ti); got != "•••" {
		t.Errorf("once the moment has passed the field shows %q, want all masked", got)
	}
	if ti.Text() != "abc" {
		t.Errorf("the field holds %q, want abc", ti.Text())
	}
}

// The reveal follows the caret: a key typed mid-word is the one shown.
func TestTheRevealIsWhereTheKeyLanded(t *testing.T) {
	_, ti := onEditField(t)
	ti.SetText("abc")
	ti.SetCursorPosition(1)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "x"})
	if got := shownAs(ti); got != "•x••" {
		t.Errorf("typing x after the first character shows %q, want %q", got, "•x••")
	}
}

// Anything else that happens to the field puts the character back under the
// mask straight away.
func TestAnythingElseEndsTheReveal(t *testing.T) {
	cases := map[string]func(*TextInput){
		"backspace": func(ti *TextInput) { ti.HandleKeyPress(core.KeyPressEvent{Key: "Backspace"}) },
		"focus out": func(ti *TextInput) { ti.HandleFocusOut() },
		"set text":  func(ti *TextInput) { ti.SetText("zz") },
		"re-masked": func(ti *TextInput) { ti.SetEchoMode(EchoPassword) },
		"paste":     func(ti *TextInput) { ti.pasteText("q") },
	}
	for name, do := range cases {
		_, ti := onEditField(t)
		ti.HandleKeyPress(core.KeyPressEvent{Key: "a"})
		ti.HandleKeyPress(core.KeyPressEvent{Key: "b"})
		do(ti)
		if ti.revealed {
			t.Errorf("%s: a character is still revealed", name)
		}
		if ti.revealTimer != nil {
			t.Errorf("%s: the reveal timer is still running", name)
		}
		if got := shownAs(ti); strings.ContainsAny(got, "abzq") {
			t.Errorf("%s: the field shows %q", name, got)
		}
	}
}

// Only a single key typed is ever shown: a paste, or an input method
// committing a whole word, is masked from the start.
func TestOnlyASingleTypedKeyIsShown(t *testing.T) {
	_, ti := onEditField(t)
	ti.pasteText("p")
	if got := shownAs(ti); got != "•" {
		t.Errorf("a pasted character shows as %q, want masked", got)
	}

	_, ti = onEditField(t)
	ti.HandleTextCommit(core.TextCommitEvent{Text: "word"})
	if got := shownAs(ti); got != "••••" {
		t.Errorf("a committed word shows as %q, want masked", got)
	}

	// One character arriving as a commit is what an SDL keystroke is.
	_, ti = onEditField(t)
	ti.HandleTextCommit(core.TextCommitEvent{Text: "k"})
	if got := shownAs(ti); got != "k" {
		t.Errorf("one committed character shows as %q, want it shown", got)
	}

	_, ti = onEditField(t)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	if got := shownAs(ti); got != " " {
		t.Errorf("a typed space shows as %q, want it shown", got)
	}
}

// A key whose name cannot carry what it types -- macOS Option-b is "∫" -- is
// typed from what the host watched it produce, and is shown like any other.
func TestAWatchedChordIsShown(t *testing.T) {
	d := NewDesktop()
	host := &watchingHost{observed: map[string]string{"M-b": "∫"}}
	host.Init(host)
	host.SetParent(d)
	ti := NewTextInput()
	ti.SetParent(host)
	ti.SetEchoMode(EchoPasswordOnEdit)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "M-b"})
	if got := shownAs(ti); got != "∫" {
		t.Errorf("M-b shows as %q, want the character it typed", got)
	}
}

// A composition in progress is masked whole.
func TestACompositionIsMasked(t *testing.T) {
	_, ti := onEditField(t)
	ti.preedit = core.Preedit{Text: []rune("ka")}
	runes, _, _, _ := ti.composedText()
	if got := string(runes); strings.ContainsAny(got, "ka") {
		t.Errorf("a composition paints as %q, want masked", got)
	}
}

// With no timer to take a character away again, none is ever shown.
func TestNoRevealWithoutATimer(t *testing.T) {
	ti := NewTextInput()
	ti.SetEchoMode(EchoPasswordOnEdit)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "a"})
	if got := shownAs(ti); got != "•" {
		t.Errorf("a field with no desktop shows %q, want masked", got)
	}
}

// Other modes never reveal.
func TestOnlyPasswordOnEditReveals(t *testing.T) {
	for _, mode := range []EchoMode{EchoPassword, EchoNormal, EchoNoEcho} {
		d := NewDesktop()
		ti := NewTextInput()
		ti.SetParent(d)
		ti.SetEchoMode(mode)
		ti.HandleKeyPress(core.KeyPressEvent{Key: "a"})
		if ti.revealed || ti.revealTimer != nil {
			t.Errorf("mode %v revealed a typed character", mode)
		}
	}
}

// It is a password field to a screen reader, as echo=password is.
func TestPasswordOnEditIsAPasswordField(t *testing.T) {
	_, ti := onEditField(t)
	if got := ti.AccessibleInfo().Role; got != core.RolePasswordInput {
		t.Errorf("role = %v, want a password input", got)
	}
}

func TestPasswordOnEditArrivesOverTheWire(t *testing.T) {
	f, _ := buildWithEvents(t, nil, `new textinput text="s" echo=password_on_edit`)
	if got := f.targets[0].(*TextInput).EchoMode(); got != EchoPasswordOnEdit {
		t.Errorf("echo=password_on_edit gave mode %v", got)
	}
}
