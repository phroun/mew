package editor

import (
	"path/filepath"
	"testing"
)

// normalizeDocPath must leave ANY scheme:/ path untouched — not just mew: —
// otherwise a scheme path (no leading "/") is absolutized against the working
// directory, e.g. "help:/start.txt" -> "<cwd>/help:/start.txt", which then
// showed up as a bogus Save-as default. A genuinely relative name still
// absolutizes; an already-absolute path is unchanged.
func TestNormalizeDocPathSchemesPassThrough(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")

	unchanged := []string{
		"help:/start.txt",
		"file:///Users/x/y.txt",
		"http://example.com/a",
		"mew:///editor.conf",
		"s3://bucket/key",
		"/already/absolute.txt",
	}
	for _, p := range unchanged {
		if got := e.normalizeDocPath(p); got != p {
			t.Errorf("normalizeDocPath(%q) = %q, want it unchanged", p, got)
		}
	}

	// A truly relative name is absolutized against the working directory.
	rel := "notes.txt"
	got := e.normalizeDocPath(rel)
	if !filepath.IsAbs(got) || filepath.Base(got) != "notes.txt" {
		t.Errorf("normalizeDocPath(%q) = %q, want an absolute path ending in notes.txt", rel, got)
	}
}
