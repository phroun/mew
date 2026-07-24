package editor

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// Default systematic palette entries (from the config color defaults).
const (
	sgrKeyword  = "\x1b[0;1;97;40m"
	sgrComment  = "\x1b[0;32;40m"
	sgrString   = "\x1b[0;36;40m"
	sgrConstant = "\x1b[0;91;40m"
	sgrType     = "\x1b[0;93;40m"
)

// escEnd returns the index just past the terminal escape sequence at s[i:]
// (i points at ESC): a CSI (ESC [ … final) or a two/three-byte ESC form.
func escEnd(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		return j + 1 // include the final byte
	case '#', '(', ')', '*', '+':
		return i + 3
	default:
		return i + 2
	}
}

// expandSGR rewrites a (now SGR-coalesced) terminal stream back into the
// one-SGR-per-glyph form the highlight assertions were written against: it
// tracks the pen (the last CSI '…m') and re-emits it before every printable
// rune, dropping other escapes. So a "<sgr><glyph>" check verifies the glyph's
// EFFECTIVE style regardless of whether present() actually repeated the SGR.
func expandSGR(raw string) string {
	var out strings.Builder
	pen := ""
	for i := 0; i < len(raw); {
		if raw[i] == 0x1b {
			j := escEnd(raw, i)
			if seq := raw[i:j]; strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				pen = seq
			}
			i = j
			continue
		}
		_, sz := utf8.DecodeRuneInString(raw[i:])
		if sz == 0 {
			sz = 1
		}
		out.WriteString(pen)
		out.WriteString(raw[i : i+sz])
		i += sz
	}
	return out.String()
}

// renderedEditorWithConfig is newRenderedEditor with a full custom config
// text (for [syntax.*] sections and the syntax option).
func renderedEditorWithConfig(t *testing.T, content, configText string) (*Editor, *window.Window, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.ConfigText = &configText
	cfg.Terminal = &TerminalIO{
		Input:  bytes.NewReader(nil),
		Output: &out,
		Size:   func() (int, int, error) { return 80, 24, nil },
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString(content), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})
	return e, e.WindowManager.GetWindow("doc"), &out
}

// syntax=cpp colors keywords, types, comments and strings in the rendered
// output using the systematic palette.
func TestSyntaxCppRenders(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"return 42; // done\n",
		"[options]\nsyntax=cpp\n")
	out.Reset()
	e.performRender()
	raw := out.String()

	if !strings.Contains(raw, sgrKeyword+"r") {
		t.Fatal("keyword 'return' should render in the syntaxKeyword color")
	}
	if !strings.Contains(raw, sgrComment+"/") {
		t.Fatal("the // comment should render in the syntaxComment color")
	}
	if !strings.Contains(raw, sgrConstant+"4") {
		t.Fatal("the number should render in the syntaxConstant color")
	}
}

// A /* comment continues across lines: the machine state carries over.
func TestSyntaxMultilineComment(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"int a; /* open\nstill inside\n*/ int b;\n",
		"[options]\nsyntax=cpp\n")
	out.Reset()
	e.performRender()
	raw := out.String()

	if !strings.Contains(raw, sgrComment+"s") {
		t.Fatal("the middle line should be entirely comment-colored")
	}
	if !strings.Contains(raw, sgrType+"i") {
		t.Fatal("'int' outside the comment should color as a type")
	}
}

// [syntax.<grammar>] remaps a jsf class to another mew color name.
func TestSyntaxPerGrammarColorMap(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"int x;\n",
		"[options]\nsyntax=cpp\n\n[syntax.cpp]\nType = syntaxConstant\n")
	out.Reset()
	e.performRender()
	raw := out.String()

	if !strings.Contains(raw, sgrConstant+"i") {
		t.Fatal("mapped Type class should render in syntaxConstant")
	}
	if strings.Contains(raw, sgrType+"i") {
		t.Fatal("the default Type color should no longer apply")
	}
}

// [syntax] (global) remaps a class for every grammar.
func TestSyntaxGlobalColorMap(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"// hey\n",
		"[options]\nsyntax=cpp\n\n[syntax]\nComment = syntaxString\n")
	out.Reset()
	e.performRender()
	raw := out.String()

	if !strings.Contains(raw, sgrString+"/") {
		t.Fatal("globally mapped Comment class should render in syntaxString")
	}
}

// Edits invalidate the highlight cache: completing a keyword recolors it.
func TestSyntaxRecomputesAfterEdit(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"retur x;\n",
		"[options]\nsyntax=cpp\n")
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), sgrKeyword+"r") {
		t.Fatal("'retur' is not a keyword yet")
	}

	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.executeCommand("insert 'n'")
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"r") {
		t.Fatal("after inserting 'n', 'return' should color as a keyword")
	}
}

// The syntax option is runtime-switchable and validates its grammar.
func TestSyntaxOption(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t, "func main() {}\n", "[general]\n")

	if v, _ := e.getOption(nil, "syntax"); v != "" {
		t.Fatalf("default syntax should be empty, got %q", v)
	}
	e.PawScript.ExecuteAsync("set_option 'syntax', 'go'")
	if v, _ := e.getOption(nil, "syntax"); v != "go" {
		t.Fatalf("syntax after set_option: %q", v)
	}
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"f") {
		t.Fatal("go keyword 'func' should highlight after enabling syntax=go")
	}

	// Unknown grammars are rejected and leave the option unchanged.
	if e.setSyntax("no_such_grammar") {
		t.Fatal("unknown grammar must fail")
	}
	if v, _ := e.getOption(nil, "syntax"); v != "go" {
		t.Fatalf("failed switch must keep the old grammar, got %q", v)
	}

	// none/empty disables highlighting again.
	e.PawScript.ExecuteAsync("set_option 'syntax', 'none'")
	if v, _ := e.getOption(nil, "syntax"); v != "" {
		t.Fatalf("syntax after disabling: %q", v)
	}
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), sgrKeyword+"f") {
		t.Fatal("highlighting should be off after syntax=none")
	}
}

// Prompt windows never syntax-highlight (only main buffers do).
func TestSyntaxSkipsPrompts(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t, "x\n", "[options]\nsyntax=cpp\n")
	e.PromptForInput("Cmd: ", "", func(string, bool) {})
	e.PawScript.ExecuteAsync("insert 'return'")
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), sgrKeyword+"r") {
		t.Fatal("prompt content must not be syntax-highlighted")
	}
}

// Every builtin grammar loads and colors a representative sample: the given
// SGR must appear immediately before the given rune in the rendered stream.
func TestBuiltinGrammarsSmoke(t *testing.T) {
	cases := []struct {
		grammar string
		content string
		wantSGR string
		before  string
	}{
		{"cpp", "if (x) {}\n", sgrKeyword, "i"},
		{"go", "var x int\n", sgrKeyword, "v"},
		{"pawscript", "macro greet(echo 'hi')\n", sgrKeyword, "m"},
		{"conf", "# note\nkey=value\n", sgrComment, "#"},
		{"toml", "name = \"mew\"\n", sgrString, "\""},
		{"yaml", "key: true\n", sgrConstant, "t"},
		{"python", "def f():\n", sgrKeyword, "d"},
		{"javascript", "const x = 1;\n", sgrKeyword, "c"},
		{"typescript", "interface A {}\n", sgrKeyword, "i"},
		{"shell", "if true; then\n", sgrKeyword, "i"},
		{"sql", "SeLeCt 1;\n", sgrKeyword, "S"},
		{"lua", "local x = nil\n", sgrKeyword, "l"},
		{"elisp", "(defun f () nil)\n", sgrKeyword, "d"},
		{"scheme", "(define (f) #t)\n", sgrKeyword, "d"},
		{"markdown", "# Title\n", sgrKeyword, "#"},
		{"make", "all: build\n", sgrKeyword, "a"},
		{"dockerfile", "FROM alpine\n", sgrKeyword, "F"},
		{"java", "public class A {}\n", sgrKeyword, "p"},
		{"csharp", "namespace A {}\n", sgrKeyword, "n"},
		{"rust", "fn main() {}\n", sgrKeyword, "f"},
		{"html", "<div id=\"a\">hi</div>\n", sgrKeyword, "d"},
		{"php", "<?php echo $x; ?>\n", sgrKeyword, "e"},
		{"css", "a { color: red; }\n", sgrComment, ""}, // load-only below
		{"tex", "\\section{Intro}\n", sgrKeyword, "\\"},
	}
	for _, c := range cases {
		t.Run(c.grammar, func(t *testing.T) {
			e, _, out := renderedEditorWithConfig(t, c.content,
				"[options]\nsyntax="+c.grammar+"\n")
			if e.syntaxGrammar == nil {
				t.Fatalf("grammar %s failed to load", c.grammar)
			}
			if c.before == "" {
				return // load-only check
			}
			out.Reset()
			e.performRender()
			if !strings.Contains(expandSGR(out.String()), c.wantSGR+c.before) {
				t.Fatalf("expected %q before %q in rendered %s output",
					c.wantSGR, c.before, c.grammar)
			}
		})
	}
}

// A class with no mapping anywhere (tex's MathCommand) falls back to the
// grammar file's own "=Class attrs" colors (bold cyan).
func TestUnmappedClassFallsBackToFileAttrs(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t, "$\\frac{a}{b}$\n",
		"[options]\nsyntax=tex\n")
	_ = e
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[0;1;36m\\") {
		t.Fatal("unmapped MathCommand class should use the grammar's own bold cyan attrs")
	}
}

// [formats] aliases resolve to their builtin grammars (canonicalized before
// loading, so [syntax.<grammar>] maps key on the real grammar name).
func TestSyntaxFormatsAliases(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "x\n", "[general]\n")
	for alias, want := range map[string]string{
		"c": "cpp", "js": "javascript", "py": "python", "sh": "shell",
		"md": "markdown", "el": "elisp", "rs": "rust", "mew": "pawscript",
	} {
		if !e.setSyntax(alias) {
			t.Fatalf("alias %q failed to load", alias)
		}
		if e.syntaxGrammar == nil || e.syntaxGrammar.Name != want {
			t.Fatalf("alias %q loaded wrong grammar, want %s", alias, want)
		}
		if e.Config.Syntax != want {
			t.Fatalf("alias %q should canonicalize the option to %s, got %s", alias, want, e.Config.Syntax)
		}
	}
}

// A user [formats] section adds new aliases and can override or remove the
// built-in ones.
func TestSyntaxFormatsUserSection(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "x\n",
		"[general]\n\n[formats]\npawthing = pawscript\nc = go\ninc =\n")

	if !e.setSyntax("pawthing") || e.syntaxGrammar.Name != "pawscript" {
		t.Fatal("user alias pawthing -> pawscript should load")
	}
	if !e.setSyntax("c") || e.syntaxGrammar.Name != "go" {
		t.Fatal("user override c -> go should beat the built-in c -> cpp")
	}
	if e.setSyntax("inc") {
		t.Fatal("a blanked alias must no longer resolve")
	}
}

// shebangName parses interpreter names from #! lines.
func TestShebangName(t *testing.T) {
	cases := map[string]string{
		"#!/bin/bash":                  "bash",
		"#!/bin/sh":                    "sh",
		"#!/usr/bin/env python3":       "python",
		"#!/bin/env php":               "php",
		"#!/bin/env php8.2":            "php",
		"#!/usr/bin/env -S node --exp": "node",
		"#!/usr/bin/env FOO=1 lua5.4":  "lua",
		"#!/usr/bin/python3.11":        "python",
		"#! /bin/dash":                 "dash",
		"#!/usr/bin/env":               "",
		"# not a shebang":              "",
		"plain text":                   "",
	}
	for line, want := range cases {
		if got := shebangName(line); got != want {
			t.Errorf("shebangName(%q) = %q, want %q", line, got, want)
		}
	}
}

// syntaxDetect: a shebang picks the grammar even without a filename.
func TestSyntaxDetectShebang(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"#!/usr/bin/env python3\ndef f():\n",
		"[options]\nsyntaxDetect=true\n")
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"d") {
		t.Fatal("shebang should select the python grammar (def as keyword)")
	}
}

// syntaxDetect: the short env form (#!/bin/env php) detects php, and the
// grammar only highlights inside the <?php tags.
func TestSyntaxDetectEnvPhp(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"#!/bin/env php\n<?php echo $greeting; ?>\n",
		"[options]\nsyntaxDetect=true\n")
	out.Reset()
	e.performRender()
	raw := out.String()
	if !strings.Contains(raw, sgrKeyword+"e") {
		t.Fatal("php 'echo' should color as a keyword via the shebang")
	}
	// $greeting: the Var class maps to syntaxEscape (bright cyan on black).
	if !strings.Contains(raw, "\x1b[0;96;40m$") {
		t.Fatal("php variables should color via the Var class")
	}
}

// syntaxDetect: the file extension picks the grammar when there is no
// shebang; the shebang wins over a conflicting extension.
func TestSyntaxDetectExtension(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, "var x int\n",
		"[options]\nsyntaxDetect=true\n")
	w.Buffer.SetFilename("main.go")
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"v") {
		t.Fatal(".go extension should select the go grammar")
	}

	// Shebang beats extension: a .py file carrying a shell shebang.
	e2, w2, out2 := renderedEditorWithConfig(t, "#!/bin/sh\nif true; then\n",
		"[options]\nsyntaxDetect=true\n")
	w2.Buffer.SetFilename("script.py")
	out2.Reset()
	e2.performRender()
	if !strings.Contains(out2.String(), sgrKeyword+"i") {
		t.Fatal("the shebang should win over the extension")
	}
}

// syntaxDetect: an extensionless basename (Makefile) resolves via [formats],
// and a buffer that detects nothing falls back to the global syntax option.
func TestSyntaxDetectBasenameAndFallback(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, "all: build\n",
		"[options]\nsyntaxDetect=true\n")
	w.Buffer.SetFilename("Makefile")
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"a") {
		t.Fatal("Makefile basename should select the make grammar")
	}

	// No shebang, unknown extension: the global option's grammar applies.
	e2, w2, out2 := renderedEditorWithConfig(t, "return 1;\n",
		"[options]\nsyntaxDetect=true\nsyntax=cpp\n")
	w2.Buffer.SetFilename("notes.xyz")
	out2.Reset()
	e2.performRender()
	if !strings.Contains(out2.String(), sgrKeyword+"r") {
		t.Fatal("undetected buffers should fall back to the syntax option")
	}
}

// The syntaxDetect option round-trips and takes effect at runtime.
func TestSyntaxDetectOption(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, "#!/bin/bash\nif true; then\n", "[general]\n")
	if v, _ := e.getOption(nil, "syntaxDetect"); v != "no" {
		t.Fatalf("default syntaxDetect: %q", v)
	}
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), sgrKeyword+"i") {
		t.Fatal("no highlighting expected while detection is off")
	}

	e.PawScript.ExecuteAsync("set_option 'syntaxDetect', 'true'")
	if v, _ := e.getOption(nil, "syntaxDetect"); v != "yes" {
		t.Fatalf("syntaxDetect after set_option: %q", v)
	}
	_ = w
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrKeyword+"i") {
		t.Fatal("enabling syntaxDetect should highlight via the shebang")
	}
}

// Path-conditional formats: .txt highlights as dokuwiki only inside a
// wiki-looking tree ([formats.txt] path rules), other .txt stays plain.
func TestSyntaxDetectDokuwikiByPath(t *testing.T) {
	crumbles := func(filename string) *Editor {
		e, w, _ := renderedEditorWithConfig(t, "====== Title ======\n",
			"[options]\nsyntaxDetect=true\n")
		w.Buffer.SetFilename(filename)
		e.performRender()
		_ = w
		return e
	}

	e := crumbles("/home/u/mywiki/start.txt")
	if c := e.ensureSynCache(mainBuf(e), 0); c == nil || c.grammar == nil || c.grammar.Name != "dokuwiki" {
		t.Fatal("a .txt inside a wiki path should detect dokuwiki")
	}
	e = crumbles("/srv/doku/data/pages/ns/start.txt")
	if c := e.ensureSynCache(mainBuf(e), 0); c == nil || c.grammar == nil || c.grammar.Name != "dokuwiki" {
		t.Fatal("a .txt under a pages folder should detect dokuwiki")
	}
	e = crumbles("/home/u/notes/start.txt")
	if c := e.ensureSynCache(mainBuf(e), 0); c != nil && c.grammar != nil {
		t.Fatal("an ordinary .txt should stay plain")
	}
}

// A relative filename tests the working directory too.
func TestSyntaxDetectDokuwikiByCwd(t *testing.T) {
	dir := t.TempDir() + "/mywiki"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	e, w, _ := renderedEditorWithConfig(t, "x\n", "[options]\nsyntaxDetect=true\n")
	w.Buffer.SetFilename("start.txt")
	e.performRender()
	if c := e.ensureSynCache(w.Buffer, 0); c == nil || c.grammar == nil || c.grammar.Name != "dokuwiki" {
		t.Fatal("a relative .txt in a wiki cwd should detect dokuwiki")
	}
}

// [formats.txt] user rules extend/override; blanking removes a default.
func TestFormatPathsUserRules(t *testing.T) {
	e, w, _ := renderedEditorWithConfig(t, "x\n",
		"[options]\nsyntaxDetect=true\n\n[formats.txt]\n*scrolls* = markdown\npages =\n")
	w.Buffer.SetFilename("/home/u/scrolls/notes.txt")
	if c := e.ensureSynCache(w.Buffer, 0); c == nil || c.grammar == nil || c.grammar.Name != "markdown" {
		t.Fatal("a user path rule should apply")
	}

	e2, w2, _ := renderedEditorWithConfig(t, "x\n",
		"[options]\nsyntaxDetect=true\n\n[formats.txt]\npages =\n")
	w2.Buffer.SetFilename("/srv/w/data/pages/p.txt")
	if c := e2.ensureSynCache(w2.Buffer, 0); c != nil && c.grammar != nil {
		t.Fatal("a blanked default rule must no longer fire")
	}
}

// mainBuf returns the "doc" window's buffer.
func mainBuf(e *Editor) *buffer.Buffer {
	return e.WindowManager.GetWindow("doc").Buffer
}

// The dokuwiki grammar colors the core constructs.
func TestDokuwikiGrammar(t *testing.T) {
	const sgrEscape = "\x1b[0;96;40m"
	e, w, out := renderedEditorWithConfig(t,
		"====== Head ======\n**bold** and [[wiki:page|x]]\n<code>\nraw < stuff\n</code>\nafter\n",
		"[options]\nsyntax=dokuwiki\n")
	w.BrowseAutoArmed = true // caret-mode colors: model a user who ^C'd out of browse
	out.Reset()
	e.performRender()
	raw := expandSGR(out.String()) // present() coalesces SGR; expand to check effective styles
	if !strings.Contains(raw, sgrKeyword+"=") {
		t.Fatal("headings should color via Heading -> syntaxKeyword")
	}
	if !strings.Contains(raw, "\x1b[1m*") && !strings.Contains(raw, "\x1b[1mb") {
		t.Fatal("bold spans should use the grammar's bold attr (layered, no reset)")
	}
	// Grammar-recognized links now paint in the "link" color (caret mode):
	// the followable-link affordance overrides the Link class's syntax color.
	if !strings.Contains(raw, "\x1b[0;4;93;40m[") {
		t.Fatal("links should paint in the link color in caret mode")
	}
	if !strings.Contains(raw, sgrString+"r") {
		t.Fatal("code block content should color via Code -> syntaxString")
	}
	// The line after </code> is plain again (default text color).
	if !strings.Contains(raw, "\x1b[0;37;40ma") {
		t.Fatal("text after the code block should be plain")
	}
}

// DokuWiki headings outline with INVERTED depth: more '=' is shallower.
func TestOutlineDokuwikiHeadings(t *testing.T) {
	e, w := newTestEditor(t,
		"====== Top ======\ntext\n==== Mid ====\nmore\n== Deep ==\nbody\n",
		"syntax=dokuwiki")
	atPos(t, w, 5, 0)
	if got := e.outlineContext(w); got != "Top·Mid·Deep" {
		t.Fatalf("crumb %q, want Top·Mid·Deep", got)
	}
}

// Vim and emacs modelines pick the grammar when the shebang doesn't.
func TestSyntaxDetectModelines(t *testing.T) {
	grammarOf := func(content string) string {
		e, w, _ := renderedEditorWithConfig(t, content, "[options]\nsyntaxDetect=true\n")
		e.performRender()
		if c := e.ensureSynCache(w.Buffer, 0); c != nil && c.grammar != nil {
			return c.grammar.Name
		}
		return ""
	}

	if g := grammarOf("# vim: set ft=python :\nx = 1\n"); g != "python" {
		t.Fatalf("vim modeline: got %q", g)
	}
	if g := grammarOf("// -*- mode: c++ -*-\nint x;\n"); g != "cpp" {
		t.Fatalf("emacs modeline (c++ mode name): got %q", g)
	}
	if g := grammarOf("// -*- Go -*-\nvar x int\n"); g != "go" {
		t.Fatalf("short emacs modeline: got %q", g)
	}
	// Vim honors the LAST five lines too.
	if g := grammarOf("plain\ntext\nhere\n# vi:ft=lua\n"); g != "lua" {
		t.Fatalf("trailing vim modeline: got %q", g)
	}
	// A shebang wins over a modeline.
	if g := grammarOf("#!/bin/sh\n# vim: ft=python\n"); g != "shell" {
		t.Fatalf("shebang should beat the modeline: got %q", g)
	}
	// A modeline outside the honored lines is ignored.
	long := "a\nb\nc\nd\ne\nf\n# vim: ft=python\ng\nh\ni\nj\nk\nl\n"
	if g := grammarOf(long); g != "" {
		t.Fatalf("mid-file modeline must not fire: got %q", g)
	}
}

// JavaScript regex literals color as strings; division does not.
func TestJavascriptRegexLiterals(t *testing.T) {
	render := func(content string) string {
		e, _, out := renderedEditorWithConfig(t, content, "[options]\nsyntax=javascript\n")
		out.Reset()
		e.performRender()
		return expandSGR(out.String()) // coalesced SGR -> per-glyph, for effective-style checks
	}

	// Expression position: /ab+c/g is a regex (Regexp -> syntaxString).
	if raw := render("x = /ab+c/g;\n"); !strings.Contains(raw, sgrString+"/") {
		t.Fatal("a regex literal should color via the Regexp class")
	}
	// After a term, / is division — and a division-context comment works.
	raw := render("y = a / b; // note\n")
	if strings.Contains(raw, sgrString+"/") {
		t.Fatal("division must not color as a regex")
	}
	if !strings.Contains(raw, sgrComment+"/") {
		t.Fatal("the comment after division context should still color")
	}
	// A regex may follow a keyword (return /re/), and a / inside [...] does
	// not end it.
	if raw := render("return /a[/]b/;\n"); !strings.Contains(raw, sgrString+"b") {
		t.Fatal("a / inside a regex character class must not terminate it")
	}
	// Division by a number after a paren: (a + b) / 2.
	if raw := render("z = (a + b) / 2;\n"); strings.Contains(raw, sgrString+"/") {
		t.Fatal("/ after a closing paren is division")
	}
}

// Project .mew/syntax directories join the grammar search path, nearest
// project first.
func TestProjectSyntaxDir(t *testing.T) {
	proj := t.TempDir()
	mew := proj + "/.mew"
	if err := os.MkdirAll(mew+"/syntax", 0o755); err != nil {
		t.Fatal(err)
	}
	grammar := "=Idle\n=Zap\tbold red\n:idle Idle\n\t*\tidle\n\t\"z\"\tzap\trecolor=-1\n:zap Zap\n\t*\tidle\tnoeat\n"
	if err := os.WriteFile(mew+"/syntax/zzgrammar.jsf", []byte(grammar), 0o644); err != nil {
		t.Fatal(err)
	}

	e, _, _ := renderedEditorWithConfig(t, "z\n", "[general]\n")
	e.LoadedConfig.ProjectDirs = []string{mew}
	if !e.setSyntax("zzgrammar") {
		t.Fatal("a grammar in a project .mew/syntax dir should load")
	}
	if e.syntaxGrammar == nil || e.syntaxGrammar.Name != "zzgrammar" {
		t.Fatal("wrong grammar loaded")
	}
}

// parseSyntaxOverrides accepts space-, comma-, and semicolon-separated flavor
// lists and lowercases them.
func TestParseSyntaxOverrides(t *testing.T) {
	for _, in := range []string{"go conf", "go,conf", "  Go ; CONF ", "go\tconf"} {
		set := parseSyntaxOverrides(in)
		if !set["go"] || !set["conf"] || len(set) != 2 {
			t.Fatalf("parseSyntaxOverrides(%q) = %v", in, set)
		}
	}
	if parseSyntaxOverrides("") != nil || parseSyntaxOverrides("   ") != nil {
		t.Fatal("empty input should yield a nil set")
	}
}

// syntaxOverrides makes a listed flavor skip the document's project .mew/syntax
// folder, so a project-only grammar no longer resolves under that name.
func TestSyntaxOverridesSkipsProjectDir(t *testing.T) {
	proj := t.TempDir()
	mew := proj + "/.mew"
	if err := os.MkdirAll(mew+"/syntax", 0o755); err != nil {
		t.Fatal(err)
	}
	grammar := "=Idle\n=Zap\tbold red\n:idle Idle\n\t*\tidle\n\t\"z\"\tzap\trecolor=-1\n:zap Zap\n\t*\tidle\tnoeat\n"
	if err := os.WriteFile(mew+"/syntax/zzgrammar.jsf", []byte(grammar), 0o644); err != nil {
		t.Fatal(err)
	}

	// Baseline: the project grammar resolves through the normal cascade.
	e, _, _ := renderedEditorWithConfig(t, "z\n", "[general]\n")
	e.LoadedConfig.ProjectDirs = []string{mew}
	if _, err := e.resolveSyntaxFile("zzgrammar", false); err != nil {
		t.Fatalf("project grammar should resolve with the project layer: %v", err)
	}
	// Skipping the project layer removes the only source, so it no longer loads.
	if _, err := e.resolveSyntaxFile("zzgrammar", true); err == nil {
		t.Fatal("skipProject should bypass the project .mew/syntax dir")
	}

	// End to end through the override loader: with zzgrammar overridden, the
	// grammar (project-only) no longer loads.
	if !e.setSyntax("zzgrammar") {
		t.Fatal("baseline: project grammar should load")
	}
	e.Config.SyntaxOverrides = "zzgrammar"
	if e.setSyntax("zzgrammar") {
		t.Fatal("an overridden project-only grammar should fail to resolve")
	}
}

// The conf grammar follows mew's own editor.conf rules.
func TestConfGrammarMewRules(t *testing.T) {
	const sgrEscape = "\x1b[0;96;40m" // syntaxEscape
	render := func(content string) string {
		e, _, out := renderedEditorWithConfig(t, content, "[options]\nsyntax=conf\n")
		out.Reset()
		e.performRender()
		return expandSGR(out.String()) // coalesced SGR -> per-glyph, for effective-style checks
	}

	// A mid-line ';' is value text (a mappings command separator), not a
	// comment; a mid-line unescaped '#' is one.
	raw := render("^C\t=cancel|close; other_cmd # tail\n")
	if strings.Contains(raw, sgrComment+";") {
		t.Fatal("mid-line ';' must not open a comment")
	}
	if !strings.Contains(raw, sgrComment+"#") {
		t.Fatal("mid-line unescaped '#' must open a comment")
	}

	// Full-line ';' comments still work.
	if raw := render("; note\nkey=1\n"); !strings.Contains(raw, sgrComment+";") {
		t.Fatal("full-line ';' comment should color as comment")
	}

	// \#if = \#endif — the escaped hashes are key and value, never comments.
	raw = render("\\#if = \\#endif\n")
	if strings.Contains(raw, sgrComment) {
		t.Fatal("escaped '#' must not open a comment on either side of '='")
	}
	if !strings.Contains(raw, sgrType+"\\") {
		t.Fatal("the escaped key should color as a key")
	}
	if !strings.Contains(raw, sgrEscape+"\\") {
		t.Fatal("the value's escape should color as an escape")
	}

	// A '#' inside quotes is literal string content; one after the closing
	// quote is a comment.
	raw = render("name=\"a#b\" # real\n")
	if !strings.Contains(raw, sgrString+"#") {
		t.Fatal("a quoted '#' is string content")
	}
	if !strings.Contains(raw, sgrComment+"#") {
		t.Fatal("the '#' after the quotes is a comment")
	}

	// A '#' inside a [section] header is part of the name.
	if raw := render("[weird#name]\nkey=1\n"); strings.Contains(raw, sgrComment+"#") {
		t.Fatal("a '#' inside a section header is not a comment")
	}

	// \= stays inside the key: the key spans it and the value follows the
	// real '='.
	raw = render("a\\=b = value # c\n")
	if !strings.Contains(raw, sgrComment+"#") {
		t.Fatal("the tail comment should still be found after an escaped '='")
	}

	// An @include directive colors as a preprocessor at-rule — distinct from a
	// '#' comment, so it stands out (and is never mistaken for one).
	const sgrPreproc = "\x1b[0;94;40m" // syntaxPreproc
	raw = render("# a comment\n@include \"team.conf\"\n")
	if !strings.Contains(raw, sgrPreproc+"@") {
		t.Fatal("@include should color as a preprocessor directive")
	}
	if strings.Contains(raw, sgrComment+"@") {
		t.Fatal("@include must not color as a comment")
	}
}

// fsMap is a virtual FileSystem for sandboxed-host tests.
type fsMap map[string]string

func (f fsMap) ReadFile(name string) ([]byte, error) {
	if s, ok := f[name]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}
func (f fsMap) WriteFile(name string, data []byte) error { f[name] = string(data); return nil }
func (f fsMap) Glob(pattern string) ([]string, error)    { return nil, nil }

// A sandboxed host's config text can @include further files, served back
// through the host's own FileSystem.
func TestConfigIncludeThroughHostFS(t *testing.T) {
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	text := "@include \"team.conf\"\n[options]\ntabSize=3\n"
	cfg.ConfigText = &text
	cfg.FS = fsMap{"team.conf": "[options]\nsyntax=go\n"}
	cfg.Terminal = &TerminalIO{
		Input:  bytes.NewReader(nil),
		Output: &out,
		Size:   func() (int, int, error) { return 80, 24, nil },
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.Config.TabSize != 3 {
		t.Fatalf("main config text should still apply, tabSize %d", e.Config.TabSize)
	}
	if e.Config.Syntax != "go" {
		t.Fatalf("included config should apply through the host FS, syntax %q", e.Config.Syntax)
	}
}

// Go raw strings span lines (backquote to backquote).
func TestSyntaxGoRawString(t *testing.T) {
	e, _, out := renderedEditorWithConfig(t,
		"x := `raw\nzz more\n` + y\n",
		"[options]\nsyntax=go\n")
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), sgrString+"z") {
		t.Fatal("the middle of a raw string should be string-colored")
	}
}
