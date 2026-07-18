package jsf

import (
	"fmt"
	"strings"
	"testing"
)

// testGrammar is a small C-like grammar exercising the core machinery:
// recolor, multi-line comments, strings with escapes, keyword collection.
const testGrammar = `# test grammar
=Idle
=Comment	green
=String		cyan
=Keyword	bold
=Number		red

:idle Idle
	*	idle
	"/"	slash		recolor=-1
	"\""	string		recolor=-1
	"0-9"	number		recolor=-1
	"a-z"	ident		buffer recolor=-1

:slash Idle
	*	idle		noeat recolor=-1
	"*"	comment		recolor=-2

:comment Comment
	*	comment
	"*"	maybe_end

:maybe_end Comment
	*	comment		noeat
	"/"	idle

:string String
	*	string
	"\""	idle
	"\\"	str_esc

:str_esc String
	*	string

:number Number
	*	idle		noeat
	"0-9"	number

:ident Idle
	*	idle		noeat strings
	"if"	kw
	"while"	kw
done
	"a-z"	ident

:kw Keyword
	*	idle		noeat
`

func loaderFor(files map[string]string) *Loader {
	return NewLoader(func(name string) ([]byte, error) {
		if src, ok := files[name]; ok {
			return []byte(src), nil
		}
		return nil, fmt.Errorf("not found")
	})
}

// classes renders a highlighted line as one class-name letter per rune, using
// the first letter of each class ("." for unstyled/Idle-like empty).
func classes(attrs []*ColorRef) string {
	var b strings.Builder
	for _, a := range attrs {
		if a == nil {
			b.WriteByte('?')
		} else {
			b.WriteByte(a.Class[0])
		}
	}
	return b.String()
}

func highlightAll(t *testing.T, l *Loader, in *Instance, lines ...string) []string {
	t.Helper()
	var st LineState
	out := make([]string, len(lines))
	for i, line := range lines {
		var attrs []*ColorRef
		attrs, st = l.HighlightLine(in, st, []rune(line))
		out[i] = classes(attrs)
	}
	return out
}

func TestKeywordRecolor(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in, "ab if x")
	// "ab" and "x" are plain identifiers (Idle); "if" matches the keyword
	// table and takes the target state's Keyword color.
	if got[0] != "IIIKKII" {
		t.Fatalf("keyword coloring: %q", got[0])
	}
	// A keyword terminated by end-of-line still matches (lookup happens on
	// the synthetic newline).
	if got := highlightAll(t, l, in, "while"); got[0] != "KKKKK" {
		t.Fatalf("eol keyword: %q", got[0])
	}
	// Not a keyword: prefix only.
	if got := highlightAll(t, l, in, "ifx"); got[0] != "III" {
		t.Fatalf("keyword prefix must not match: %q", got[0])
	}
}

func TestMultilineCommentStateCarry(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in,
		"9 /* c",
		"still */ 9",
		"9")
	if got[0] != "NICCCC" {
		t.Fatalf("comment open line: %q", got[0])
	}
	// The whole tail of the comment, including the closing */, is Comment;
	// after it the number colors again.
	if got[1] != "CCCCCCCCIN" {
		t.Fatalf("comment close line: %q", got[1])
	}
	if got[2] != "N" {
		t.Fatalf("after comment: %q", got[2])
	}
}

func TestStringsAndEscapes(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in, `9"s\"t"9`)
	if got[0] != `NSSSSSSN` {
		t.Fatalf("string with escaped quote: %q", got[0])
	}
}

func TestRecolorTargetColor(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	// "/*" is recognized on the '*' and recolor=-2 paints both chars with
	// the comment color; a lone '/' falls back to Idle.
	got := highlightAll(t, l, in, "/*x*/ /")
	if got[0] != "CCCCCII" {
		t.Fatalf("recolor=-2: %q", got[0])
	}
}

// A save_c pair: the "&" rule matches the partner of the saved delimiter.
const pairGrammar = `=Idle
=Vec	magenta
:idle Idle
	*	idle
	"<([{"	vec	save_c recolor=-1
:vec Vec
	*	vec
	&	idle
`

func TestSaveCPartnerMatch(t *testing.T) {
	l := loaderFor(map[string]string{"p": pairGrammar})
	in, err := l.Load("p")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in, "<ab> x", "(c) y")
	// The closer is eaten by the vec state (Vec color) and returns to idle.
	if got[0] != "VVVVII" {
		t.Fatalf("angle pair: %q", got[0])
	}
	if got[1] != "VVVII" {
		t.Fatalf("paren pair: %q", got[1])
	}
	// A mismatched closer does not end the region.
	if got := highlightAll(t, l, in, "<ab) x"); got[0] != "VVVVVV" {
		t.Fatalf("mismatched closer must not match &: %q", got[0])
	}
}

// save_s + a strings "&" entry: the here-document pattern. The terminator
// word is captured with save_s and matched later by "&".
const heredocGrammar = `=Idle
=Here	green
:idle Idle
	*	idle
	"<"	langle
:langle Idle
	*	idle	noeat
	"<"	collect
:collect Idle
	*	here	noeat save_s
	"A-Z"	collect2	buffer
:collect2 Idle
	*	here	noeat save_s
	"A-Z"	collect2
:here Here
	*	here
	"\n"	here_bol
:here_bol Here
	*	here	noeat
	"A-Z"	here_word	buffer
:here_word Here
	*	here	noeat strings
	"&"	idle
done
	"A-Z"	here_word
`

func TestSaveSHeredoc(t *testing.T) {
	l := loaderFor(map[string]string{"h": heredocGrammar})
	in, err := l.Load("h")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in,
		"a<<EOS",
		"hi END",
		"EOS",
		"b")
	if got[1] != "HHHHHH" {
		t.Fatalf("heredoc body: %q", got[1])
	}
	// "END" is not the saved terminator; "EOS" is.
	if got[3] != "I" {
		t.Fatalf("after terminator the machine must be idle again: %q", got[3])
	}
}

// mark/markend/recolormark: a call-like identifier recolors only when the
// next character is a parenthesis.
const markGrammar = `=Idle
=Fn	bold
:idle Idle
	*	idle
	"a-z"	word	mark buffer recolor=-1
:word Idle
	*	word_end	noeat markend strings
done
	"a-z"	word
:word_end Idle
	*	idle	noeat
	"("	fn	noeat recolormark
:fn Fn
	*	idle	noeat recolor=-1
`

func TestRecolorMark(t *testing.T) {
	l := loaderFor(map[string]string{"m": markGrammar})
	in, err := l.Load("m")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in, "foo(x) bar y")
	// "foo" turns Fn because "(" follows; "bar" stays Idle.
	if got[0] != "FFFIIIIIIIII" {
		t.Fatalf("recolormark: %q", got[0])
	}
}

// istrings folds case for the lookup.
const foldGrammar = `=Idle
=Kw	bold
:idle Idle
	*	idle
	"a-zA-Z"	word	buffer
:word Idle
	*	idle	noeat istrings
	"select"	kw
done
	"a-zA-Z"	word
:kw Kw
	*	idle	noeat
`

func TestIstringsFoldsCase(t *testing.T) {
	l := loaderFor(map[string]string{"f": foldGrammar})
	in, err := l.Load("f")
	if err != nil {
		t.Fatal(err)
	}
	got := highlightAll(t, l, in, "SeLeCt x")
	if got[0] != "KKKKKKII" {
		t.Fatalf("istrings: %q", got[0])
	}
}

// Subroutines: a .subr instantiated with different .ifdef parameters from
// different call sites.
const subrGrammar = `=Idle
=A	red
=B	blue
:begin Idle
	*	begin	noeat call=.sub(x)
.subr sub
:s Idle
.ifdef x
	*	a	noeat
.else
	*	b	noeat
.endif
:a A
	*	a
:b B
	*	b
.end
`

const subrCaller = `=Idle
:begin Idle
	*	begin	noeat call=sub1.sub()
`

func TestSubroutineParams(t *testing.T) {
	l := loaderFor(map[string]string{"sub1": subrGrammar, "caller": subrCaller})

	in, err := l.Load("sub1")
	if err != nil {
		t.Fatal(err)
	}
	// Root grammar calls .sub(x): the .ifdef x branch colors A.
	if got := highlightAll(t, l, in, "ab"); got[0] != "AA" {
		t.Fatalf("subr with param: %q", got[0])
	}

	// Another file calls sub1.sub() without the param: the .else branch.
	in2, err := l.Load("caller")
	if err != nil {
		t.Fatal(err)
	}
	if got := highlightAll(t, l, in2, "ab"); got[0] != "BB" {
		t.Fatalf("subr without param: %q", got[0])
	}
}

// return pops back to the state named on the calling rule.
const returnGrammar = `=Idle
=R	green
=X	red
:begin Idle
	*	rest	call=.sub()
:rest R
	*	rest
.subr sub
:s X
	*	s
	";"	s	return
.end
`

func TestCallAndReturn(t *testing.T) {
	l := loaderFor(map[string]string{"r": returnGrammar})
	in, err := l.Load("r")
	if err != nil {
		t.Fatal(err)
	}
	// First char eaten by begin (Idle) while calling; the subroutine colors
	// X until ";" returns to the call rule's target state.
	if got := highlightAll(t, l, in, "a;cd"); got[0] != "IXRR" {
		t.Fatalf("call/return: %q", got[0])
	}
}

func TestUnknownCallTargetFails(t *testing.T) {
	l := loaderFor(map[string]string{"bad": `=Idle
:x Idle
	*	x	call=missing()
`})
	if _, err := l.Load("bad"); err == nil {
		t.Fatal("call to a missing file must fail to load")
	}
}

// push_c/pop_c nest saved delimiters: an inner pair overrides the "&" match
// and popping restores the outer pair (perl's s{...}{...} shape).
const delimStackGrammar = `=Idle
=D	cyan
:idle Idle
	*	idle
	"<"	outer	save_c recolor=-1
:outer D
	*	outer
	"("	inner	push_c
	&	idle
:inner D
	*	inner
	&	outer	pop_c
`

func TestDelimiterStack(t *testing.T) {
	l := loaderFor(map[string]string{"d": delimStackGrammar})
	in, err := l.Load("d")
	if err != nil {
		t.Fatal(err)
	}
	// "<a(b)c>d": everything through the ">" is inside delimiters; the ")"
	// pops back to the outer "<>" pair so ">" still closes it.
	got := highlightAll(t, l, in, "<a(b)c>d")
	if got[0] != "DDDDDDDI" {
		t.Fatalf("delimiter nesting: %q", got[0])
	}
	// Without the pop the ">" would not close: prove ")" was what restored
	// it by checking a mismatched inner close keeps everything inside.
	if got := highlightAll(t, l, in, "<a(b]c>d"); got[0] != "DDDDDDDD" {
		t.Fatalf("unpopped delimiter must keep the region open: %q", got[0])
	}
	// LineStates with delimiter stacks compare.
	var s1, s2 LineState
	_, s1 = l.HighlightLine(in, LineState{}, []rune("<a(b"))
	_, s2 = l.HighlightLine(in, LineState{}, []rune("<a(b"))
	if s1 != s2 {
		t.Fatal("delimiter frames must intern for comparable states")
	}
}

// push_s / pop_s parse (word-delimiter stacking).
func TestPushPopWordOptionsParse(t *testing.T) {
	l := loaderFor(map[string]string{"p": `=Idle
:a Idle
	*	a
	"x"	a	push_s
	"y"	a	pop_s
`})
	if _, err := l.Load("p"); err != nil {
		t.Fatalf("push_s/pop_s should parse: %v", err)
	}
}

// Entry states are comparable and converge: identical content yields
// identical LineState values, so caches can stop re-highlighting.
func TestLineStateConvergence(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	var st1, st2 LineState
	lines := []string{"/* c", "x */ 9", "if x"}
	for _, line := range lines {
		_, st1 = l.HighlightLine(in, st1, []rune(line))
	}
	for _, line := range lines {
		_, st2 = l.HighlightLine(in, st2, []rune(line))
	}
	if st1 != st2 {
		t.Fatal("identical input must produce equal LineStates")
	}
	if !st1.Valid() {
		t.Fatal("resulting state should be valid")
	}
}

// A grammar whose noeat rules form a cycle must not hang.
func TestNoeatCycleGuard(t *testing.T) {
	l := loaderFor(map[string]string{"loop": `=Idle
:a Idle
	*	b	noeat
:b Idle
	*	a	noeat
`})
	in, err := l.Load("loop")
	if err != nil {
		t.Fatal(err)
	}
	attrs, _ := l.HighlightLine(in, LineState{}, []rune("xyz"))
	if len(attrs) != 3 {
		t.Fatalf("guard must force progress, got %d attrs", len(attrs))
	}
}

// Context flags: comment/string states (derived from their class names here)
// flag their characters, and recolored delimiters take the context along
// with the color.
func TestContextFlags(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	line := []rune(`9 /* c */ "s" 9`)
	_, ctx, _ := l.HighlightLineFull(in, LineState{}, line)
	if ctx[0] != 0 {
		t.Fatalf("number has no context, got %d", ctx[0])
	}
	// The /* delimiter itself carries the comment context (recolor=-2).
	if ctx[2]&CtxComment == 0 || ctx[5]&CtxComment == 0 {
		t.Fatal("comment (including its delimiter) should flag CtxComment")
	}
	// The quotes and the content carry the string context.
	if ctx[10]&CtxString == 0 || ctx[11]&CtxString == 0 {
		t.Fatal("string (including its quote) should flag CtxString")
	}
	if ctx[14] != 0 {
		t.Fatal("trailing code is plain again")
	}
}

// ContextAt reports the machine state, class, flags, and (via a cross-file
// subroutine call) the embedded-language stack.
func TestContextAt(t *testing.T) {
	l := loaderFor(map[string]string{"t": testGrammar})
	in, err := l.Load("t")
	if err != nil {
		t.Fatal(err)
	}
	c := l.ContextAt(in, LineState{}, []rune("9 /* c"), 5)
	if !c.Comment || c.String || c.Class != "Comment" || c.State != "comment" {
		t.Fatalf("comment context: %+v", c)
	}

	l2 := loaderFor(map[string]string{"sub1": subrGrammar, "caller": subrCaller})
	root, err := l2.Load("caller")
	if err != nil {
		t.Fatal(err)
	}
	c2 := l2.ContextAt(root, LineState{}, []rune("ab"), 1)
	if c2.Syntax != "sub1" {
		t.Fatalf("innermost grammar should be the called sub1, got %q", c2.Syntax)
	}
	if len(c2.Stack) != 2 || c2.Stack[0] != "sub1" || c2.Stack[1] != "caller" {
		t.Fatalf("stack should walk out to the root: %v", c2.Stack)
	}
}

// An explicit context word on a state overrides the class-name derivation.
func TestExplicitContextWord(t *testing.T) {
	l := loaderFor(map[string]string{"g": `=Idle
=Doc	green
:idle Idle
	*	idle
	";"	doc	recolor=-1
:doc Doc comment
	*	doc
	"\n"	idle
`})
	in, err := l.Load("g")
	if err != nil {
		t.Fatal(err)
	}
	_, ctx, _ := l.HighlightLineFull(in, LineState{}, []rune("x ; y"))
	if ctx[2]&CtxComment == 0 || ctx[4]&CtxComment == 0 {
		t.Fatal("explicit 'comment' context word should flag the state's chars")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"rule before state": "\t*\tx\n:x Idle\n",
		"unknown option":    "=Idle\n:x Idle\n\t*\tx\tfrobnicate\n",
		"open strings":      "=Idle\n:x Idle\n\t*\tx\tstrings\n",
		"open ifdef":        "=Idle\n.ifdef y\n:x Idle\n",
		"no states":         "=Idle\n",
	}
	for name, src := range cases {
		l := loaderFor(map[string]string{"g": src})
		if _, err := l.Load("g"); err == nil {
			t.Errorf("%s: expected a load error", name)
		}
	}
}

func TestAttrSGR(t *testing.T) {
	cases := []struct {
		attrs []string
		want  string
	}{
		{nil, ""},
		{[]string{"bold", "red"}, "\x1b[0;1;31m"},
		{[]string{"green"}, "\x1b[0;32m"},
		{[]string{"WHITE"}, "\x1b[0;97m"},
		{[]string{"bg_white", "black"}, "\x1b[0;47;30m"},
		{[]string{"BG_CYAN"}, "\x1b[0;106m"},
		{[]string{"underline", "inverse"}, "\x1b[0;4;7m"},
		{[]string{"fg_500"}, "\x1b[0;38;5;196m"},
		{[]string{"bg_012"}, "\x1b[0;48;5;24m"},
		{[]string{"fg_12"}, "\x1b[0;38;5;244m"},
		{[]string{"mystery"}, ""},
	}
	for _, c := range cases {
		if got := attrSGR(c.attrs); got != c.want {
			t.Errorf("attrSGR(%v) = %q, want %q", c.attrs, got, c.want)
		}
	}
}
