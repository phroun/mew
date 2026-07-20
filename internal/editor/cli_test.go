package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/argwild"
	"github.com/phroun/mew/internal/window"
)

// newBareEditor builds a headless editor with NO pre-created document window,
// so windows created by a launch walk are the only main buffers. HOME is a
// temp dir (opening files arms backups/locks there); backups settle at
// cleanup so background writes don't race TempDir teardown.
func newBareEditor(t *testing.T) *Editor {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })
	return e
}

// launch parses argv and runs the launch walk (without the event loop),
// returning any error from planning or application.
func launch(t *testing.T, e *Editor, argv ...string) error {
	t.Helper()
	r, err := argwild.ParseArgs(argv)
	if err != nil {
		t.Fatalf("argwild parse %v: %v", argv, err)
	}
	plan, err := buildLaunchPlan(r)
	if err != nil {
		return err
	}
	_, err = e.applyLaunch(plan)
	return err
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// A per-window option before a file applies to it and persists; changing it
// before a later file changes only that file.
func TestLaunchPerFileOptions(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")

	// default showLineNumbers is true.
	if err := launch(t, e, "--showLineNumbers-", a, "--showLineNumbers", b); err != nil {
		t.Fatalf("launch: %v", err)
	}
	mb := e.getMainBuffers()
	if len(mb) != 2 {
		t.Fatalf("want 2 buffers, got %d", len(mb))
	}
	var wa, wb *window.Window
	for _, w := range mb {
		switch w.Buffer.GetFilename() {
		case a:
			wa = w
		case b:
			wb = w
		}
	}
	if wa == nil || wb == nil {
		t.Fatal("both files should open")
	}
	if v, _ := e.getOption(wa, "showlinenumbers"); v != "no" {
		t.Fatalf("a should have line numbers off, got %q", v)
	}
	if v, _ := e.getOption(wb, "showlinenumbers"); v != "yes" {
		t.Fatalf("b should have line numbers on, got %q", v)
	}
	// The first file wins focus.
	if f := e.WindowManager.GetFocusedWindow(); f != wa {
		t.Fatal("first file should be focused")
	}
}

// +N places the next file's caret; it is one-shot (does not carry to a later
// file).
func TestLaunchGotoLine(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "l1\nl2\nl3\nl4\n")
	b := writeTemp(t, "b.txt", "x\ny\n")
	if err := launch(t, e, "+3", a, b); err != nil {
		t.Fatalf("launch: %v", err)
	}
	var wa, wb *window.Window
	for _, w := range e.getMainBuffers() {
		if w.Buffer.GetFilename() == a {
			wa = w
		} else if w.Buffer.GetFilename() == b {
			wb = w
		}
	}
	if wa.CursorPos().Line != 2 {
		t.Fatalf("a caret should be on line index 2 (+3), got %d", wa.CursorPos().Line)
	}
	if wb.CursorPos().Line != 0 {
		t.Fatalf("b caret should stay at line 0 (+N is one-shot), got %d", wb.CursorPos().Line)
	}
}

// A global option must precede the first file; after one, it errors.
func TestLaunchGlobalMustComeFirst(t *testing.T) {
	a := writeTemp(t, "a.txt", "x\n")

	e := newBareEditor(t)
	if err := launch(t, e, "--wordWrap", a); err != nil {
		t.Fatalf("global before file should be fine: %v", err)
	}
	if !e.Config.WordWrap {
		t.Fatal("global --wordWrap should apply to the editor")
	}

	e2 := newBareEditor(t)
	err := launch(t, e2, a, "--wordWrap")
	if err == nil {
		t.Fatal("global option after a file should error")
	}
}

// Unknown options are rejected.
func TestLaunchUnknownOption(t *testing.T) {
	e := newBareEditor(t)
	if err := launch(t, e, "--nonesuch"); err == nil {
		t.Fatal("unknown option should error")
	}
}

// The enable/disable value grammar.
func TestLaunchEnableDisableGrammar(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{"--showInvisibles", "yes"},
		{"--showInvisibles=on", "yes"},
		{"--showInvisibles=1", "yes"},
		{"--showInvisibles=yes", "yes"},
		{"--showInvisibles-", "no"},
		{"--showInvisibles=off", "no"},
		{"--showInvisibles=false", "no"},
		{"--showInvisibles=no", "no"},
	}
	for _, tc := range cases {
		e := newBareEditor(t)
		a := writeTemp(t, "a.txt", "x\n")
		if err := launch(t, e, tc.arg, a); err != nil {
			t.Fatalf("%s: %v", tc.arg, err)
		}
		w := e.getMainBuffers()[0]
		if v, _ := e.getOption(w, "showinvisibles"); v != tc.want {
			t.Fatalf("%s: showInvisibles=%q want %q", tc.arg, v, tc.want)
		}
	}
}

// A valued (non-boolean) option passes its value through.
func TestLaunchValuedOption(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "x\n")
	if err := launch(t, e, "--tabSize=8", a); err != nil {
		t.Fatalf("launch: %v", err)
	}
	w := e.getMainBuffers()[0]
	if v, _ := e.getOption(w, "tabsize"); v != "8" {
		t.Fatalf("tabSize should be 8, got %q", v)
	}
}

// No files opens a single empty buffer.
func TestLaunchNoFiles(t *testing.T) {
	e := newBareEditor(t)
	if err := launch(t, e); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if n := len(e.getMainBuffers()); n != 1 {
		t.Fatalf("no-file launch should open one empty buffer, got %d", n)
	}
}

// cliPerWindowOptions must be a subset of cliKnownOptions, and every known
// option name must be one set_option actually accepts (not silently unknown).
// A representative valid value guards against drift from setOption.
func TestCliOptionAlignment(t *testing.T) {
	for name := range cliPerWindowOptions {
		if !cliKnownOptions[name] {
			t.Fatalf("per-window option %q is not in cliKnownOptions", name)
		}
	}
	valid := map[string]string{
		"tabsize": "4", "showlinenumbers": "true", "showinvisibles": "true",
		"showbidi": "true", "showmarks": "yes", "insertmode": "yes", "showcolumnruler": "true", "rulershowscursor": "true",
		"syntax": "", "syntaxdetect": "true", "macoptionkeys": "auto",
		"matchignoressinglequote": "true", "matchignoresdoublequote": "true",
		"matchignoresslashstar": "true", "matchignoresslashslash": "true",
		"matchignoreshash": "true", "matchignoresdoublehyphen": "true",
		"matchignoressemicolon": "true", "matchignorespercent": "true",
		"wordwrap": "true", "searchignorecase": "true", "searchwrap": "true",
		"searchregex": "true", "modebarlocation": "top", "pagesizeoptimal": "100%",
		"pageoverlapminimum": "1", "pagesizestep": "0", "maxrepeat": "100",
		"killringentries": "10", "direction": "ltr", "prompttimeout": "300",
		"scripttimeout": "300", "debouncems": "20", "maxrenderdelayms": "100",
		"modebarinner": "%FN%", "modebardefault": "%FORTUNE%",
		"modebarouter": "Line:%LINE%", "mappings": "mew",
		"flipbidiforhost": "false",
	}
	e := newBareEditor(t)
	for name := range cliKnownOptions {
		v, ok := valid[name]
		if !ok {
			t.Fatalf("no test value for known option %q (add it)", name)
		}
		if !e.setOption(nil, name, v) {
			t.Fatalf("set_option rejected known option %q=%q (drift from setOption?)", name, v)
		}
	}
	// And no stray test values for names not actually known.
	for name := range valid {
		if !cliKnownOptions[name] {
			t.Fatalf("test value for %q which is not a known option", name)
		}
	}
}
