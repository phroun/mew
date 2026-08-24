package editor

import "testing"

// Text the terminal received with no key behind it lands in the document.
//
// It arrives prefixed because a bare name in that stream means a key was
// pressed. Spelled as a key it also had to survive the rule that decides what
// an unbound key types — one rune carries itself — so a two-rune commit was
// known to nobody and silently dropped. Which is most Japanese ones.
func TestCommittedTextLandsInTheDocument(t *testing.T) {
	for _, c := range []struct{ what, key, want string }{
		{"a multi-character commit", "Text:今日", "今日"},
		{"a single character", "Text:あ", "あ"},
		{"text holding a colon of its own", "Text:a:b", "a:b"},
	} {
		e, w := newTestEditor(t, "")
		if !e.handleCommittedText(c.key) {
			t.Errorf("%s: %q was not taken", c.what, c.key)
			continue
		}
		if got := docContent(w); got != c.want {
			t.Errorf("%s: document = %q, want %q", c.what, got, c.want)
		}
	}
}

// An empty one is taken and types nothing.
func TestAnEmptyCommitTypesNothing(t *testing.T) {
	e, w := newTestEditor(t, "")
	if !e.handleCommittedText("Text:") {
		t.Error("an empty text event was not taken")
	}
	if got := docContent(w); got != "" {
		t.Errorf("document = %q, want it untouched", got)
	}
}

// Anything else is left for the keymap.
func TestAnOrdinaryKeyIsNotTakenAsText(t *testing.T) {
	e, _ := newTestEditor(t, "")
	for _, key := range []string{"a", "Return", "^A", "CPR:1;1"} {
		if e.handleCommittedText(key) {
			t.Errorf("%q was swallowed as text", key)
		}
	}
}
