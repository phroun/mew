package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

func atPos(t *testing.T, w *window.Window, line, r int) {
	t.Helper()
	w.SetCursorPos(window.Position{Line: line, Rune: r})
}

func expectMatch(t *testing.T, e *Editor, w *window.Window, line, r int) {
	t.Helper()
	if !e.gotoMatchingBracket() {
		t.Fatal("go_match failed")
	}
	if got := w.CursorPos(); got.Line != line || got.Rune != r {
		t.Fatalf("matched at %d:%d, want %d:%d", got.Line, got.Rune, line, r)
	}
}

// Context-aware brackets: a bracket inside a string never answers a code
// bracket; from inside the string, matching stays inside strings.
func TestGoMatchSkipsStringBrackets(t *testing.T) {
	e, w := newTestEditor(t, `f("(", 1);`+"\n", "syntax=cpp")
	atPos(t, w, 0, 1) // the ( of f(
	expectMatch(t, e, w, 0, 8)

	// From the quoted "(" a match would need a quoted ")"; none here.
	atPos(t, w, 0, 3)
	if e.gotoMatchingBracket() {
		t.Fatal("a lone quoted bracket must not match code brackets")
	}

	// Within one string, brackets pair normally.
	e2, w2 := newTestEditor(t, `s = "(x)";`+"\n", "syntax=cpp")
	atPos(t, w2, 0, 5)
	expectMatch(t, e2, w2, 0, 7)
}

// A comment's bracket is equally invisible to code.
func TestGoMatchSkipsCommentBrackets(t *testing.T) {
	e, w := newTestEditor(t, "( /* ) */ x )\n", "syntax=cpp")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 0, 12)
	_ = e
}

// Without a grammar, matching is plain-text (previous behavior).
func TestGoMatchNoGrammarStillWorks(t *testing.T) {
	e, w := newTestEditor(t, "a (b [c] d) e\n")
	atPos(t, w, 0, 2)
	expectMatch(t, e, w, 0, 10)
}

// Token pairs: shell if/fi nest, and a "fi" inside a string doesn't count.
func TestGoMatchShellIfFi(t *testing.T) {
	e, w := newTestEditor(t, "if a; then\nif b; then\nfi\nfi\n", "syntax=shell")
	atPos(t, w, 0, 0) // outer if
	expectMatch(t, e, w, 3, 0)

	// Backward from the inner fi to the inner if.
	atPos(t, w, 2, 0)
	expectMatch(t, e, w, 1, 0)

	e2, w2 := newTestEditor(t, "if a; then\necho \"fi\"\nfi\n", "syntax=shell")
	atPos(t, w2, 0, 0)
	expectMatch(t, e2, w2, 2, 0)
}

// Lua's if/do/function share the closer "end", so intervening blocks count
// as one nesting family.
func TestGoMatchLuaEndFamily(t *testing.T) {
	e, w := newTestEditor(t, "if x then\nfor i in y do\nend\nend\n", "syntax=lua")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 3, 0)
}

// Preprocessor conditionals in C: #if pairs with #endif across #ifdef nests.
func TestGoMatchCppPreproc(t *testing.T) {
	e, w := newTestEditor(t, "#if A\n#ifdef B\n#endif\n#endif\n", "syntax=cpp")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 3, 0)
}

// TeX environments pair \begin with \end.
func TestGoMatchTexBeginEnd(t *testing.T) {
	e, w := newTestEditor(t, "\\begin{doc}\ntext\n\\end{doc}\n", "syntax=tex")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 2, 0)
}

// HTML tags: same-name nesting, self-closing skipped, closing tags match
// backward, and jumps land one rune past the "<" — on the name of an
// opening tag or the "/" of a closing one.
func TestGoMatchHTMLTags(t *testing.T) {
	e, w := newTestEditor(t, "<div><div>a</div><br/>x</div>\n", "syntax=html")
	atPos(t, w, 0, 1)           // the outer div's NAME
	expectMatch(t, e, w, 0, 24) // the "/" of the final </div>

	// Backward from the "/" of the inner </div> to the inner div's name.
	atPos(t, w, 0, 12)
	expectMatch(t, e, w, 0, 6)
}

// On "<" or ">" the tag's own delimiters pair with each other, superseding
// the begin/end-tag jump.
func TestGoMatchAngleDelimiters(t *testing.T) {
	e, w := newTestEditor(t, "<div class=\"a>b\"><br/>x</div>\n", "syntax=html")
	atPos(t, w, 0, 0) // the opening "<"
	// The ">" inside the quoted attribute has string context: skipped.
	expectMatch(t, e, w, 0, 16)

	// And back again from the ">".
	atPos(t, w, 0, 16)
	expectMatch(t, e, w, 0, 0)

	// The self-closing tag's ">" pairs with its own "<".
	atPos(t, w, 0, 21)
	expectMatch(t, e, w, 0, 17)
}

func TestGoMatchHTMLTagAcrossCommentAndLines(t *testing.T) {
	e, w := newTestEditor(t, "<ul>\n<!-- </ul> -->\n<li>x</li>\n</ul>\n", "syntax=html")
	atPos(t, w, 0, 1)          // the ul's name
	expectMatch(t, e, w, 3, 1) // the "/" of the real </ul>
}

// Tag matching works in ANY document as a last resort, confined to the
// caret's own comment/string region by the context filter.
func TestGoMatchTagFallbackAnywhere(t *testing.T) {
	// Plain text, no grammar at all.
	e, w := newTestEditor(t, "docs <b>hi</b> end\n")
	atPos(t, w, 0, 6) // the b's name
	expectMatch(t, e, w, 0, 11)
	// Angle delimiters pair here too.
	atPos(t, w, 0, 5)
	expectMatch(t, e, w, 0, 7)

	// A tag inside a C comment matches within the comment.
	e2, w2 := newTestEditor(t, "int x; /* <note>see</note> */ (y)\n", "syntax=cpp")
	atPos(t, w2, 0, 11) // "note" name, comment context
	expectMatch(t, e2, w2, 0, 20)
	// Ordinary brackets in the same buffer still work.
	atPos(t, w2, 0, 30)
	expectMatch(t, e2, w2, 0, 32)
}

// A quote at either end of a string matches its mate — inside a tag's
// attributes this supersedes the tag jump.
func TestGoMatchQuoteMates(t *testing.T) {
	// html attribute string: " at 11 pairs with " at 15.
	e, w := newTestEditor(t, "<div class=\"a>b\"><br/>x</div>\n", "syntax=html")
	atPos(t, w, 0, 11)
	expectMatch(t, e, w, 0, 15)
	atPos(t, w, 0, 15)
	expectMatch(t, e, w, 0, 11)

	// C strings: opening quote to closing quote and back.
	e2, w2 := newTestEditor(t, "s = \"hello\";\n", "syntax=cpp")
	atPos(t, w2, 0, 4)
	expectMatch(t, e2, w2, 0, 10)
	atPos(t, w2, 0, 10)
	expectMatch(t, e2, w2, 0, 4)

	// An escaped quote mid-string is neither end: falls through.
	e3, w3 := newTestEditor(t, "s = \"a\\\"b\";\n", "syntax=cpp")
	atPos(t, w3, 0, 7)
	if e3.gotoMatchingBracket() {
		t.Fatal("an escaped quote must not quote-match")
	}

	// An unterminated string warns rather than landing on a non-quote.
	e4, w4 := newTestEditor(t, "s = \"abc\n", "syntax=cpp")
	atPos(t, w4, 0, 4)
	if e4.gotoMatchingBracket() {
		t.Fatal("an unterminated string has no mate")
	}
	if !hasWarning(e4, "No matching quote") {
		t.Fatal("expected the quote warning")
	}
}

// Python triple-quoted strings mate across lines (first quote of the opener
// to last quote of the closer).
func TestGoMatchTripleQuoteAcrossLines(t *testing.T) {
	e, w := newTestEditor(t, "x = \"\"\"a\nbb\n\"\"\"\n", "syntax=python")
	atPos(t, w, 0, 4)
	expectMatch(t, e, w, 2, 2)
	atPos(t, w, 2, 2)
	expectMatch(t, e, w, 0, 4)
}

// The C++ generics compromise, locked in: "<" pairs with ">", while sitting
// on the type name honestly reports no tag.
func TestGoMatchAngleInGenerics(t *testing.T) {
	e, w := newTestEditor(t, "std::vector<int> v;\n", "syntax=cpp")
	atPos(t, w, 0, 11) // the <
	expectMatch(t, e, w, 0, 15)
	atPos(t, w, 0, 15) // the >
	expectMatch(t, e, w, 0, 11)

	atPos(t, w, 0, 12) // "int" itself
	if e.gotoMatchingBracket() {
		t.Fatal("no <int> tag exists to match")
	}
	if !hasWarning(e, "No matching tag found") {
		t.Fatal("expected the tag warning on the name")
	}
}

// The matchIgnores* flags give go_match string/comment awareness in buffers
// with NO grammar (JOE's per-filetype ^G skip flags).
func TestMatchIgnoresFallback(t *testing.T) {
	// Default: double quotes ignored, so the quoted ")" doesn't answer.
	e, w := newTestEditor(t, "( \")\" x )\n")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 0, 8)

	// Quote mates work in a plain buffer through the same pseudo-context.
	atPos(t, w, 0, 2)
	expectMatch(t, e, w, 0, 4)

	// Single quotes are off by default: prose apostrophes are harmless.
	e2, w2 := newTestEditor(t, "(don't panic)\n")
	atPos(t, w2, 0, 0)
	expectMatch(t, e2, w2, 0, 12)

	// /* */ blocks (multi-line) once enabled.
	e3, w3 := newTestEditor(t, "( /* )\n*/ x )\n")
	e3.PawScript.ExecuteAsync("set_option 'matchIgnoresSlashStar', 'true'")
	atPos(t, w3, 0, 0)
	expectMatch(t, e3, w3, 1, 5)

	// # line comments once enabled.
	e4, w4 := newTestEditor(t, "( # )\n)\n")
	e4.PawScript.ExecuteAsync("set_option 'matchIgnoresHash', 'true'")
	atPos(t, w4, 0, 0)
	expectMatch(t, e4, w4, 1, 0)
}

// When a grammar applies, the highlighter context supersedes the flags: cpp
// preprocessor lines are not comments, whatever matchIgnoresHash says.
func TestMatchIgnoresSupersededByGrammar(t *testing.T) {
	e, w := newTestEditor(t, "( #define X 1 )\n", "syntax=cpp")
	e.PawScript.ExecuteAsync("set_option 'matchIgnoresHash', 'true'")
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 0, 14)
}

// Typographic prose pairs match like brackets: curly quotes, guillemets,
// CJK and fullwidth forms — with nesting.
func TestGoMatchProsePairs(t *testing.T) {
	e, w := newTestEditor(t, "she said “he said “hi” to me” then\n")
	atPos(t, w, 0, 9) // outer “
	expectMatch(t, e, w, 0, 28)
	atPos(t, w, 0, 28)
	expectMatch(t, e, w, 0, 9)

	e2, w2 := newTestEditor(t, "il a dit « bonjour ‹ oui › monde » ici\n")
	atPos(t, w2, 0, 9) // «
	expectMatch(t, e2, w2, 0, 33)
	atPos(t, w2, 0, 25) // ›
	expectMatch(t, e2, w2, 0, 19)

	e3, w3 := newTestEditor(t, "本を『読む「こと」が』好き\n")
	atPos(t, w3, 0, 2) // 『
	expectMatch(t, e3, w3, 0, 10)
	atPos(t, w3, 0, 8) // 」
	expectMatch(t, e3, w3, 0, 5)

	// Fullwidth parens.
	e4, w4 := newTestEditor(t, "值（ａ（ｂ）ｃ）尾\n")
	atPos(t, w4, 0, 1)
	expectMatch(t, e4, w4, 0, 7)

	// German-style „…“ matches forward from the opener.
	e5, w5 := newTestEditor(t, "er sagte „hallo“ dann\n")
	atPos(t, w5, 0, 9)
	expectMatch(t, e5, w5, 0, 15)
}

// [match.<grammar>] user config can add and remove pairs.
func TestGoMatchConfigOverride(t *testing.T) {
	e, w := newTestEditor(t, "startx a endx\n", "syntax=lua")
	e.LoadedConfig.MatchPairs["lua"] = map[string]string{"startx": "endx"}
	atPos(t, w, 0, 0)
	expectMatch(t, e, w, 0, 9)
}

// syntax_context reports comment/string/code, plus state/class/stack details.
func TestSyntaxContextCommand(t *testing.T) {
	e, w := newTestEditor(t, "int x; // note\n", "syntax=cpp")
	log := func(script string) string {
		e.PawScript.ExecuteAsync(script)
		return verboseLogContent(e)
	}
	atPos(t, w, 0, 10) // inside the comment
	if !strings.Contains(log(`verbose_log "c={syntax_context}"`), "c=comment") {
		t.Fatal("expected comment context at the caret")
	}
	atPos(t, w, 0, 0)
	if !strings.Contains(log(`verbose_log "k={syntax_context}"`), "k=code") {
		t.Fatal("expected code context on 'int'")
	}
	if !strings.Contains(log(`verbose_log "s={syntax_context 'syntax'}"`), "s=cpp") {
		t.Fatal("expected the grammar name detail")
	}
}
