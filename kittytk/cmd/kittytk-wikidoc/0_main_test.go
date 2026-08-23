package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// vocab is a small stand-in registry, so these tests do not move when a
// real trinket gains a property.
func vocab() *protocol.Vocabulary {
	return &protocol.Vocabulary{
		Common: []protocol.PropInfo{
			{Name: "enabled", Kind: "flag", Default: "true", Doc: "Whether it accepts input."},
		},
		Types: []protocol.TypeInfo{
			{Name: "widget",
				Props: []protocol.PropInfo{
					{Name: "caption", Kind: "string", Doc: "Display text."},
					{Name: "side", Kind: "enum", Enum: []string{"left", "right"}, Doc: "Which side | it sits on."},
				},
				Events: []protocol.EventInfo{
					{Name: "click", Doc: "It was activated.", Fields: []protocol.EventFieldDesc{
						{Name: "trinket", Kind: "uint", Doc: "The object ID."},
					}},
				}},
			{Name: "quiet"},
			{Name: "item", Virtual: true},
		},
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const page = `# Widget

Prose above the block, which must survive.

<!-- ktkdoc:props widget -->
whatever was here before
<!-- /ktkdoc -->

Prose between, with a | pipe and a <!-- comment --> in it.

<!-- ktkdoc:events widget -->
<!-- /ktkdoc -->

Prose below, the last line.
`

// The generated spans are replaced and everything else is left exactly
// as it was -- which is the whole promise of the marker scheme.
func TestPreservesProseAroundBlocks(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Widget.md", page)

	if _, err := apply(vocab(), []string{p}, false); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)

	for _, line := range []string{
		"# Widget",
		"Prose above the block, which must survive.",
		"Prose between, with a | pipe and a <!-- comment --> in it.",
		"Prose below, the last line.",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("prose lost: %q\n---\n%s", line, got)
		}
	}
	if strings.Contains(got, "whatever was here before") {
		t.Error("stale block body was not replaced")
	}
	if !strings.Contains(got, "| `caption` | string | — | Display text. |") {
		t.Errorf("property row missing:\n%s", got)
	}
	if !strings.Contains(got, "**`click`** — It was activated.") {
		t.Errorf("event heading missing:\n%s", got)
	}
	if !strings.HasSuffix(got, "Prose below, the last line.\n") {
		t.Error("trailing newline was not preserved")
	}
}

// Running twice changes nothing the second time: the render is a pure
// function of the vocabulary, so a no-change run must rewrite identical
// bytes rather than churn the wiki's history.
func TestSecondRunIsAByteForByteNoOp(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Widget.md", page)

	if _, err := apply(vocab(), []string{p}, false); err != nil {
		t.Fatal(err)
	}
	first := read(t, p)

	stale, err := apply(vocab(), []string{p}, false)
	if err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("second run reported %d stale page(s), want 0", stale)
	}
	if second := read(t, p); second != first {
		t.Errorf("second run changed the file:\n--- first\n%s\n--- second\n%s", first, second)
	}
}

// A pipe inside a doc string is escaped, or it would end the table cell
// and shift every column after it.
func TestPipeInDocIsEscaped(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Widget.md", "<!-- ktkdoc:props widget -->\n<!-- /ktkdoc -->\n")
	if _, err := apply(vocab(), []string{p}, false); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if !strings.Contains(got, `Which side \| it sits on.`) {
		t.Errorf("pipe not escaped:\n%s", got)
	}
	// Enum words render as the type, also pipe-separated and escaped.
	if !strings.Contains(got, "`left` \\| `right`") {
		t.Errorf("enum not rendered:\n%s", got)
	}
}

// A type with nothing to report says so, rather than emitting a table
// header with no rows under it.
func TestEmptySectionsSaySo(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Quiet.md",
		"<!-- ktkdoc:props quiet -->\n<!-- /ktkdoc -->\n<!-- ktkdoc:events quiet -->\n<!-- /ktkdoc -->\n")
	if _, err := apply(vocab(), []string{p}, false); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if !strings.Contains(got, "`quiet` takes no properties of its own.") {
		t.Errorf("empty props:\n%s", got)
	}
	if !strings.Contains(got, "`quiet` emits no events.") {
		t.Errorf("empty events:\n%s", got)
	}
	if strings.Contains(got, "|---|") {
		t.Errorf("an empty section should not emit a table:\n%s", got)
	}
}

// -check reports staleness and writes nothing, so it is safe to run
// against a working tree in CI.
func TestCheckReportsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Widget.md", page)

	stale, err := apply(vocab(), []string{p}, true)
	if err != nil {
		t.Fatal(err)
	}
	if stale != 1 {
		t.Errorf("stale: got %d, want 1", stale)
	}
	if read(t, p) != page {
		t.Error("-check wrote to the page")
	}
}

// A block naming a type the vocabulary does not have is an error. It
// must never render as an empty table, which is what would silently
// blank a page the day a type is renamed.
func TestUnknownTypeIsAnErrorNotAnEmptyTable(t *testing.T) {
	dir := t.TempDir()
	body := "keep me\n<!-- ktkdoc:props nosuch -->\nold rows\n<!-- /ktkdoc -->\n"
	p := write(t, dir, "Ghost.md", body)

	if _, err := apply(vocab(), []string{p}, false); err == nil {
		t.Fatal("want an error for an unregistered type")
	} else if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("error should name the type: %v", err)
	}
	if read(t, p) != body {
		t.Error("page was modified despite the error")
	}
}

// Malformed markers are reported with a line number rather than guessed
// at: guessing where a generated span ends is how a tool eats prose.
func TestMalformedMarkers(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"never closed", "a\n<!-- ktkdoc:props widget -->\nb\n", "never closed"},
		{"stray close", "a\n<!-- /ktkdoc -->\n", "nothing open"},
		{"nested", "<!-- ktkdoc:props widget -->\n<!-- ktkdoc:events widget -->\n<!-- /ktkdoc -->\n", "opened inside"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scan("P.md", strings.Split(tc.body, "\n"))
			if err == nil {
				t.Fatalf("want an error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The type index separates virtual types, which are part of another
// object rather than something placed on its own.
func TestTypeIndexSeparatesVirtual(t *testing.T) {
	out := strings.Join(typeIndex(vocab()), "\n")
	if !strings.Contains(out, "**Types** — `widget` `quiet`") {
		t.Errorf("real types: %s", out)
	}
	if !strings.Contains(out, "**Virtual types**") || !strings.Contains(out, "`item`") {
		t.Errorf("virtual types: %s", out)
	}
	if strings.Contains(strings.SplitN(out, "Virtual", 2)[0], "`item`") {
		t.Errorf("virtual type listed among the real ones: %s", out)
	}
}

// An indented marker keeps its indentation, so a block can sit inside a
// list item without the tool straightening it out.
func TestOpeningMarkerIndentationSurvives(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Widget.md", "  <!-- ktkdoc:props widget -->\n  <!-- /ktkdoc -->\n")
	if _, err := apply(vocab(), []string{p}, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); !strings.HasPrefix(got, "  <!-- ktkdoc:props widget -->\n") {
		t.Errorf("indentation lost:\n%q", got)
	}
}
