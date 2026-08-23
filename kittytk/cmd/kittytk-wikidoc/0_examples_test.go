package main

import (
	"strings"
	"testing"
)

// A regular expression matching "```...```" pairs one example's
// CLOSING fence with the next one's OPENING fence, so the prose
// between two examples is reported as a third example. The state
// machine must not.
func TestFencesPairInOrder(t *testing.T) {
	body := "intro\n" +
		"```\nnew label caption=\"a\"\n```\n" +
		"prose between, with a `backtick` in it\n" +
		"```\nnew label caption=\"b\"\n```\n" +
		"trailing\n"

	got := fences(body)
	if len(got) != 2 {
		t.Fatalf("fences = %d, want 2: %+v", len(got), got)
	}
	if got[0].src != `new label caption="a"` {
		t.Errorf("first = %q", got[0].src)
	}
	if got[1].src != `new label caption="b"` {
		t.Errorf("second = %q", got[1].src)
	}
	if strings.Contains(got[0].src+got[1].src, "prose between") {
		t.Error("prose was captured as an example")
	}
}

// The reported line is the fence's first content line, so an error
// points at the statement rather than at the page.
func TestFenceLineNumbers(t *testing.T) {
	body := "a\nb\n```\nnew label\n```\n"
	got := fences(body)
	if len(got) != 1 || got[0].line != 4 {
		t.Fatalf("line = %+v, want 4", got)
	}
}

// The noexec marker exempts the NEXT block and only that one.
func TestNoexecAppliesToOneBlock(t *testing.T) {
	body := noexecMarker + "\n" +
		"```\nset x enum=738\n```\n" +
		"```\nnew label caption=\"b\"\n```\n"
	got := fences(body)
	if len(got) != 2 {
		t.Fatalf("fences = %d, want 2", len(got))
	}
	if !got[0].noexec {
		t.Error("marked block was not exempted")
	}
	if got[1].noexec {
		t.Error("exemption leaked to the following block")
	}
}

// A marker inside a fence is example text, not a directive.
func TestNoexecInsideAFenceIsNotADirective(t *testing.T) {
	body := "```\n" + noexecMarker + "\nnew label\n```\n" +
		"```\nnew label caption=\"b\"\n```\n"
	got := fences(body)
	if len(got) != 2 {
		t.Fatalf("fences = %d, want 2", len(got))
	}
	if got[0].noexec || got[1].noexec {
		t.Errorf("a marker inside a fence exempted something: %+v", got)
	}
}

// The noexec marker is a standalone directive, not a span. Read as a
// span it has no closing marker, and the whole page then fails with
// "never closed" -- which is a confusing way to say "that is not a
// span", and it stops every other page from being written too.
func TestNoexecIsNotAGeneratedSpan(t *testing.T) {
	lines := strings.Split(
		noexecMarker+"\n"+
			"```\nset x enum=738\n```\n"+
			"<!-- ktkdoc:props widget -->\n<!-- /ktkdoc -->\n", "\n")

	blocks, err := scan("P.md", lines)
	if err != nil {
		t.Fatalf("noexec read as a span: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1 (the props span alone)", len(blocks))
	}
	if blocks[0].kind != "props" {
		t.Errorf("kind = %q, want props", blocks[0].kind)
	}
}

// A standalone directive inside a span does not end it or count as a
// nested block.
func TestNoexecInsideASpanIsNotNesting(t *testing.T) {
	lines := strings.Split(
		"<!-- ktkdoc:props widget -->\n"+noexecMarker+"\n<!-- /ktkdoc -->\n", "\n")
	blocks, err := scan("P.md", lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
}

// Only wire scripts are executed. Client code, shell, elided
// fragments and result-annotated pairs are not wire scripts, and
// running them would report failures that are not failures.
func TestIsWireExample(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"new", `new label caption="a"`, true},
		{"set", `set tv selected=1`, true},
		{"destroy", `destroy tv.a`, true},
		{"correlation key", `tv=new treeview children={}`, true},
		{"reference statement", `aid=tv.a`, true},
		{"template", `template Danger=button fg=bright_red`, true},
		{"alias", `alias Caption="caption"`, true},
		{"go client", "l := ui.ListView(\"l\")\nl.Select(2)", false},
		{"shell", "git clone https://example.invalid/x.wiki.git", false},
		{"elided", "new panel children={ … }", false},
		{"result pair", "new listview selected=2\n  ->  selected = 0", false},
		{"empty", "   \n", false},
		{"prose", "Some words that are not a script.", false},
	} {
		if got := isWireExample(tc.src); got != tc.want {
			t.Errorf("%s: isWireExample = %v, want %v", tc.name, got, tc.want)
		}
	}
}
