package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The accent palette replaces the letter it opened over.
//
// macOS commits a held letter the moment the key goes down, so by the time an
// accent is chosen the "o" is already in the field. The commit says how many
// runes it replaces because nothing else can: the input method tells a native
// client through a replacement range and tells us through nothing at all.
//
// Without it the field keeps both, which is the "ABCDoò" the palette produced.
func TestAPaletteCommitReplacesTheLetterItOpenedOver(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ABCDo")
	ti.SetCursorPosition(5)

	if !ti.HandleTextCommit(core.TextCommitEvent{Text: "ò", Replace: 1}) {
		t.Fatal("the field declined a commit it could take")
	}
	if got := ti.Text(); got != "ABCDò" {
		t.Errorf("text = %q, want the accent in place of the letter", got)
	}
	if got := ti.CursorPosition(); got != 5 {
		t.Errorf("cursor = %d, want 5 — one rune out, one rune in", got)
	}
}

// Replacing nothing is the ordinary case: a CJK composition appends its result,
// and every host that cannot know a replacement count reports zero.
func TestACommitThatReplacesNothingJustInserts(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")
	ti.SetCursorPosition(2)

	ti.HandleTextCommit(core.TextCommitEvent{Text: "きょう"})

	if got := ti.Text(); got != "abきょう" {
		t.Errorf("text = %q, want the composition appended", got)
	}
}

// The commit ENDS the composition it finishes, whichever order the platform
// sends the two events in. A preedit left standing would paint alongside the
// text it turned into.
func TestACommitEndsTheCompositionItFinishes(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ABCDo")
	ti.SetCursorPosition(5)
	ti.HandleTextEditing(core.TextEditingEvent{Text: "ò", Start: -1, Length: -1})

	ti.HandleTextCommit(core.TextCommitEvent{Text: "ò", Replace: 1})

	if ti.preedit.Active() {
		t.Errorf("the preedit survived the commit: %q", string(ti.preedit.Text))
	}
	if got := ti.Text(); got != "ABCDò" {
		t.Errorf("text = %q: the preedit must not be counted as text to replace", got)
	}
}

// A count larger than what is there erases what is there and stops. The number
// is inferred by the platform, so it is not something to trust into a panic.
func TestReplaceCannotReachPastTheCaret(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")
	ti.SetCursorPosition(1)

	ti.HandleTextCommit(core.TextCommitEvent{Text: "X", Replace: 9})

	if got := ti.Text(); got != "Xb" {
		t.Errorf("text = %q, want only the one rune before the caret gone", got)
	}
}

// A selection is deleted by the insert itself. Taking the replacement runes as
// well would erase text beyond it that the composition was never replacing.
func TestACommitOverASelectionTakesTheSelectionOnly(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("abcdef")
	ti.SelectAll()

	ti.HandleTextCommit(core.TextCommitEvent{Text: "X", Replace: 1})

	if got := ti.Text(); got != "X" {
		t.Errorf("text = %q, want the selection replaced and nothing more", got)
	}
}

// A read-only field declines, so the commit is dropped rather than landing
// somewhere it could never be typed — and the caller learns nobody took it.
func TestAReadOnlyFieldDeclinesACommit(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("locked")
	ti.SetReadOnly(true)

	if ti.HandleTextCommit(core.TextCommitEvent{Text: "ò", Replace: 1}) {
		t.Error("a read-only field accepted a commit")
	}
	if got := ti.Text(); got != "locked" {
		t.Errorf("text = %q, want it untouched", got)
	}
}
