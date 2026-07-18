package editor

import (
	"strings"
	"testing"

	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/window"
)

// insert_bidi_control with a name argument inserts the mapped code point at the
// caret, just like insert.
func TestInsertBidiControlArg(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	res := e.PawScript.ExecuteAsync("insert_bidi_control 'rlm'")
	if res != pawscript.BoolStatus(true) {
		t.Fatalf("insert_bidi_control rlm returned %v", res)
	}
	got := []rune(docContent(w))
	if len(got) != 3 || got[0] != 'a' || got[1] != '‏' || got[2] != 'b' {
		t.Fatalf("expected RLM inserted between a and b, got %q", string(got))
	}

	// Case-insensitive, and each remaining name maps.
	for name, cp := range map[string]rune{"LRM": 0x200E, "alm": 0x061C, "fsi": 0x2068, "lri": 0x2066, "rli": 0x2067, "pdi": 0x2069} {
		e2, w2 := newTestEditor(t, "\n")
		w2.SetCursorPos(window.Position{Line: 0, Rune: 0})
		e2.PawScript.ExecuteAsync("insert_bidi_control '" + name + "'")
		r := []rune(docContent(w2))
		if len(r) != 1 || r[0] != cp {
			t.Fatalf("%s inserted %q, want U+%04X", name, string(r), cp)
		}
	}
}

// An unknown name inserts nothing and reports false with a warning.
func TestInsertBidiControlUnknown(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	res := e.PawScript.ExecuteAsync("insert_bidi_control 'zzz'")
	if res != pawscript.BoolStatus(false) {
		t.Fatalf("unknown name returned %v, want false", res)
	}
	if docContent(w) != "x" {
		t.Fatalf("buffer changed on unknown name: %q", docContent(w))
	}
	if !hasWarning(e, "Unknown control mark") {
		t.Fatal("expected an unknown-mark warning")
	}
}

// With no argument it prompts; answering with a name inserts it.
func TestInsertBidiControlPrompt(t *testing.T) {
	e, w := newTestEditor(t, "\n")
	e.PawScript.ExecuteAsync("insert_bidi_control")
	p := focusedPrompt(e)
	if p == nil {
		t.Fatal("insert_bidi_control with no arg should prompt")
	}
	label := ""
	if len(p.RowMessages) > 0 {
		label = p.RowMessages[0]
	}
	if !strings.Contains(label, "[lrm/rlm/alm, fsi/lri/rli, pdi, ?]") {
		t.Fatalf("prompt label: %q", label)
	}
	answerPrompt(t, e, "lri")
	if r := []rune(docContent(w)); len(r) != 1 || r[0] != '⁦' {
		t.Fatalf("prompt answer 'lri' inserted %q", docContent(w))
	}
}

// "?" shows the two legend notifications and re-prompts with the same prompt,
// without inserting anything.
func TestInsertBidiControlHelpReprompts(t *testing.T) {
	e, w := newTestEditor(t, "\n")
	e.PawScript.ExecuteAsync("insert_bidi_control")
	answerPrompt(t, e, "?")

	if !hasNotification(e, "lrm=left-to-right, rlm=right-to-left, alm=arabic letter mark") {
		t.Fatal("expected the first legend notification")
	}
	if !hasNotification(e, "fsi=first strong isolate, lri=left-to-right isolate, rli=right-to-left-isolate, pdi=pop directional isolate") {
		t.Fatal("expected the second legend notification")
	}
	// Re-prompted: a prompt is focused again and nothing was inserted.
	if focusedPrompt(e) == nil {
		t.Fatal("'?' should re-prompt")
	}
	if docContent(w) != "" {
		t.Fatalf("'?' should insert nothing, got %q", docContent(w))
	}
	// The re-prompt is live and usable.
	answerPrompt(t, e, "pdi")
	if r := []rune(docContent(w)); len(r) != 1 || r[0] != '⁩' {
		t.Fatalf("after help, answering 'pdi' inserted %q", docContent(w))
	}
}

// Cancelling the prompt inserts nothing.
func TestInsertBidiControlCancel(t *testing.T) {
	e, w := newTestEditor(t, "\n")
	e.PawScript.ExecuteAsync("insert_bidi_control")
	cancelPrompt(t, e)
	if docContent(w) != "" {
		t.Fatalf("cancel should insert nothing, got %q", docContent(w))
	}
}
