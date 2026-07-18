package editor

import (
	"strings"
	"testing"
)

// Go: functions and types at column zero give a single-entry breadcrumb.
func TestOutlineGo(t *testing.T) {
	e, w := newTestEditor(t,
		"package x\n\nfunc Alpha() {\n\ty := 1\n\t_ = y\n}\n\nfunc (m *T) Beta() {\n\treturn\n}\n",
		"syntax=go")
	atPos(t, w, 3, 1) // inside Alpha
	if got := e.outlineContext(w); got != "Alpha" {
		t.Fatalf("crumb %q, want Alpha", got)
	}
	atPos(t, w, 8, 1) // inside the method
	if got := e.outlineContext(w); got != "Beta" {
		t.Fatalf("crumb %q, want Beta (receiver skipped)", got)
	}
	atPos(t, w, 0, 0) // before any definition
	if got := e.outlineContext(w); got != "" {
		t.Fatalf("crumb %q, want empty above all defs", got)
	}
}

// Python: indentation scoping chains class and method.
func TestOutlinePythonChain(t *testing.T) {
	e, w := newTestEditor(t,
		"class Cat:\n    def purr(self):\n        pass\n\ndef solo():\n    pass\n",
		"syntax=python")
	atPos(t, w, 2, 8) // inside purr's body
	if got := e.outlineContext(w); got != "Cat·purr" {
		t.Fatalf("crumb %q, want Cat·purr", got)
	}
	atPos(t, w, 5, 4)
	if got := e.outlineContext(w); got != "solo" {
		t.Fatalf("crumb %q, want solo", got)
	}
	// On the def line itself, that def is the context.
	atPos(t, w, 1, 4)
	if got := e.outlineContext(w); got != "Cat·purr" {
		t.Fatalf("crumb on def line %q, want Cat·purr", got)
	}
}

// A commented-out def is no def: context flags veto the match.
func TestOutlineSkipsCommentedDefs(t *testing.T) {
	e, w := newTestEditor(t,
		"def real():\n    x = 1\n#def fake():\n    y = 2\n",
		"syntax=python")
	atPos(t, w, 3, 4)
	if got := e.outlineContext(w); got != "real" {
		t.Fatalf("crumb %q, want real (the commented def must not win)", got)
	}
}

// Markdown: heading depth comes from the '#' run, chaining sections.
func TestOutlineMarkdownHeadings(t *testing.T) {
	e, w := newTestEditor(t,
		"# Intro\ntext\n## Setup\nmore\n### Deep\nbody\n## Next\ntail\n",
		"syntax=markdown")
	atPos(t, w, 5, 0)
	if got := e.outlineContext(w); got != "Intro·Setup·Deep" {
		t.Fatalf("crumb %q, want Intro·Setup·Deep", got)
	}
	atPos(t, w, 7, 0)
	if got := e.outlineContext(w); got != "Intro·Next" {
		t.Fatalf("crumb %q, want Intro·Next", got)
	}
}

// conf files show the enclosing [section] — including editor.conf itself.
func TestOutlineConfSection(t *testing.T) {
	e, w := newTestEditor(t,
		"[general]\ntabSize=4\n\n[colors]\ntext=\"x\"\n",
		"syntax=conf")
	atPos(t, w, 1, 0)
	if got := e.outlineContext(w); got != "general" {
		t.Fatalf("crumb %q, want general", got)
	}
	atPos(t, w, 4, 0)
	if got := e.outlineContext(w); got != "colors" {
		t.Fatalf("crumb %q, want colors", got)
	}
}

// YAML: the indentation chain gives a full key path.
func TestOutlineYamlPath(t *testing.T) {
	e, w := newTestEditor(t,
		"server:\n  http:\n    port: 8080\n",
		"syntax=yaml")
	atPos(t, w, 2, 8)
	if got := e.outlineContext(w); got != "server·http·port" {
		t.Fatalf("crumb %q, want server·http·port", got)
	}
}

// The breadcrumb lands in the modebar's context slot, replacing the spawn
// placeholder; leaving all definitions restores it.
func TestOutlineShowsInModebar(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"top\nfunc Shown() {\n\tx := 1\n}\n",
		"[options]\nsyntax=go\n")
	e.createPluginWindows()
	atPos(t, w, 2, 1)
	out.Reset()
	e.performRender()
	if !strings.Contains(stripAnsi(out.String()), "Shown") {
		t.Fatal("modebar should show the enclosing function name")
	}
	if w.Context != "Shown" {
		t.Fatalf("window context %q, want Shown", w.Context)
	}

	atPos(t, w, 0, 0)
	out.Reset()
	e.performRender()
	if w.Context != w.SpawnContext {
		t.Fatal("leaving all definitions should restore the spawn context")
	}
}

// [outline.<grammar>] user patterns extend the defaults.
func TestOutlineUserPattern(t *testing.T) {
	e, w := newTestEditor(t, "chapter one\nwords here\n", "syntax=markdown")
	e.LoadedConfig.Outline["markdown"]["chapter"] = `^chapter\s+(\w+)`
	atPos(t, w, 1, 0)
	if got := e.outlineContext(w); got != "one" {
		t.Fatalf("crumb %q, want one", got)
	}
}

// Edits invalidate the memo (ChangeSeq): renaming the function renames the
// crumb.
func TestOutlineMemoInvalidation(t *testing.T) {
	e, w := newTestEditor(t, "func Old() {\n\tx := 1\n}\n", "syntax=go")
	atPos(t, w, 1, 1)
	if got := e.outlineContext(w); got != "Old" {
		t.Fatalf("crumb %q, want Old", got)
	}
	w.Buffer.InsertText(0, 8, "er")
	if got := e.outlineContext(w); got != "Older" {
		t.Fatalf("crumb after edit %q, want Older", got)
	}
}
