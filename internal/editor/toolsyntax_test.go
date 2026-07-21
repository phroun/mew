package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// [options/tool] syntax=dokuwiki gives every tool window the dokuwiki grammar
// (via a grammar-agnostic overlay), while documents keep the global syntax
// and prompts are unaffected.
func TestToolWindowSyntaxOverlay(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t,
		"return 1\n", "[options]\nsyntax=go\nsyntaxDetect=yes\n\n[options/tool]\nsyntax=dokuwiki\n")

	// A tool readout (no filename) — like toggle_help — resolves to dokuwiki,
	// not the global go.
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "help", Type: window.ToolWindow, WindowSet: "help",
		Dock: window.DockTop, Buffer: bufFromString("== Help ==\n[[a:b|X]]\n"),
	})
	tw := e.WindowManager.GetWindow("help")
	e.reconcileGrammarOptions(tw)
	if tw.ViewState.Syntax != "dokuwiki" {
		t.Fatalf("tool window syntax overlay = %q, want dokuwiki", tw.ViewState.Syntax)
	}
	if g, _ := e.bufferGrammar(tw.Buffer); g == nil || g.Name != "dokuwiki" {
		t.Fatalf("tool buffer grammar = %v, want dokuwiki", g)
	}
	// The document keeps the global go grammar.
	if g, _ := e.bufferGrammar(doc.Buffer); g == nil || g.Name != "go" {
		t.Fatalf("doc buffer grammar = %v, want go", g)
	}
}

// Without any [options/tool] syntax, a filename-less tool readout stays plain
// (does NOT inherit the global document grammar).
func TestToolWindowNoSyntaxStaysPlain(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t,
		"return 1\n", "[options]\nsyntax=go\nsyntaxDetect=yes\n")
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "help", Type: window.ToolWindow, WindowSet: "help",
		Dock: window.DockTop, Buffer: bufFromString("plain readout\n"),
	})
	tw := e.WindowManager.GetWindow("help")
	e.reconcileGrammarOptions(tw)
	if g, _ := e.bufferGrammar(tw.Buffer); g != nil {
		t.Fatalf("plain tool readout should have no grammar; got %v", g.Name)
	}
}

func bufFromString(s string) *buffer.Buffer { return buffer.NewFromString(s) }
