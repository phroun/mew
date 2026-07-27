package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// Syntax colors cascade by the PAINTING viewport's class/type: a [syntax.dokuwiki]
// mapping resolves its color name through the viewport's class, so the same buffer
// paints the "Table" separators one color in a plain doc viewport and another in a
// "quickhelp"-class viewport.
func TestSyntaxColorPerViewportClass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	txt := strings.Join([]string{
		"[colors]",
		`syntaxTable="\e[0;31m"`, // red globally
		"[quickhelp::colors]",
		`syntaxTable="\e[0;34m"`, // blue for the quickhelp class
		"[syntax.dokuwiki]",
		"table=syntaxTable",
		"[options]",
		"syntax=dokuwiki",
	}, "\n")
	cfg.ConfigText = &txt
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	buf := buffer.NewFromString("| a | b |\n") // starts with a Table separator
	docWin := e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, Type: viewport.DocViewport, Dock: viewport.DockNone, Buffer: buf, SetFocus: true,
	}))
	qhWin := e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, Type: viewport.ToolViewport, Class: "quickhelp", Dock: viewport.DockTop, Buffer: buf,
	}))

	docColors := e.syntaxLineColors(docWin, 0)
	qhColors := e.syntaxLineColors(qhWin, 0)
	if len(docColors) == 0 || len(qhColors) == 0 {
		t.Fatal("expected per-rune colors for the table line")
	}
	// Rune 0 is the leading "|" — the Table separator.
	if docColors[0] != "\x1b[0;31m" {
		t.Errorf("doc viewport separator = %q, want red \\e[0;31m", docColors[0])
	}
	if qhColors[0] != "\x1b[0;34m" {
		t.Errorf("quickhelp viewport separator = %q, want blue \\e[0;34m", qhColors[0])
	}
}
