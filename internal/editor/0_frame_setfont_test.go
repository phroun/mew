package editor

import "testing"

// set_font parses "alias", "font"[, "fallback"...] and forwards to the host
// FontSink; with no sink it is a no-op warning.
func TestSetFontCommand(t *testing.T) {
	e, _ := newTestEditor(t, "hi\n")

	var gotAlias string
	var gotNames []string
	e.Config.FontSink = func(alias string, names []string) bool {
		gotAlias, gotNames = alias, names
		return true
	}
	e.PawScript.ExecuteAsync(`set_font "ui-term", "JetBrainsMono", "Monday"`)

	if gotAlias != "ui-term" {
		t.Fatalf("alias = %q, want ui-term", gotAlias)
	}
	if len(gotNames) != 2 || gotNames[0] != "JetBrainsMono" || gotNames[1] != "Monday" {
		t.Fatalf("names = %v, want [JetBrainsMono Monday]", gotNames)
	}

	// No sink: it must not panic (fonts are the terminal's on a plain host).
	e.Config.FontSink = nil
	e.PawScript.ExecuteAsync(`set_font "ui-term", "X"`)
}
