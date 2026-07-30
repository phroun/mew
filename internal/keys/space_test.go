package keys

import "testing"

// A chord whose final key is the spacebar must resolve once the space is
// delivered as the "space" token (as normalizeKey now produces). The sequence
// join is activeSequence + " " + key, so "^B" then "space" rebuilds "^B space".
func TestSpaceChordResolves(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{"^B space": "prompt_peek_down"})

	if r := sp.ProcessKey("^B"); r.Command != "" {
		t.Fatalf("^B should open a sequence, not fire a command; got %q", r.Command)
	}
	if r := sp.ProcessKey("space"); r.Command != "prompt_peek_down" {
		t.Fatalf(`"^B space" should resolve to prompt_peek_down; got %q`, r.Command)
	}

	// And the raw character must NOT resolve — normalization is upstream's job,
	// so the processor only ever sees the "space" token for a spacebar.
	sp.ClearActiveSequence()
	sp.ProcessKey("^B")
	if r := sp.ProcessKey(" "); r.Command == "prompt_peek_down" {
		t.Fatal(`a raw " " must not match the "space" binding (it is normalized upstream)`)
	}
}
