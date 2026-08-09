package editor

import (
	"strings"
	"testing"
)

// The tombstone names the closed buffer and offers a Re-open link back to a real
// file; an Untitled buffer (no filename) gets no link, and mew:/ surfaces are not
// reopenable.
func TestClosedPlaceholderContent(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")

	buf := e.newClosedPlaceholder("box:/notes.txt")
	got := buf.GetContent()
	if !strings.Contains(got, "The buffer box:/notes.txt has been closed.") {
		t.Errorf("placeholder should name the closed buffer; got %q", got)
	}
	if !strings.Contains(got, "[[box:/notes.txt|Re-open]]") {
		t.Errorf("a named buffer should offer a Re-open link; got %q", got)
	}
	if buf.GetFilename() != closedPlaceholderURL {
		t.Errorf("placeholder address = %q, want %q", buf.GetFilename(), closedPlaceholderURL)
	}

	u := e.newClosedPlaceholder("")
	if strings.Contains(u.GetContent(), "Re-open") {
		t.Errorf("an Untitled buffer must not offer Re-open; got %q", u.GetContent())
	}
	if !strings.Contains(u.GetContent(), "The buffer Untitled has been closed.") {
		t.Errorf("untitled placeholder text wrong; got %q", u.GetContent())
	}

	if reopenableName("mew:/buffers") {
		t.Error("generated surfaces are not reopenable")
	}
	if reopenableName("") {
		t.Error("an empty name is not reopenable")
	}
	if !reopenableName("/tmp/x.txt") {
		t.Error("a real path should be reopenable")
	}
}
