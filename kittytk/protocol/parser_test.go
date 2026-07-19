package protocol

import "testing"

func mustParse(t *testing.T, src string) *Script {
	t.Helper()
	s, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return s
}

func TestParseNewButton(t *testing.T) {
	s := mustParse(t, `new button caption="Caption Here" action=action_id_here some_float=4.2`)
	if len(s.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(s.Statements))
	}
	st := s.Statements[0]
	if st.Verb != "new" || st.Key != "" {
		t.Fatalf("verb=%q key=%q", st.Verb, st.Key)
	}
	if len(st.Args) != 4 {
		t.Fatalf("args = %d, want 4", len(st.Args))
	}
	// Positional-by-convention type word parses as a bare flag arg;
	// the `new` interpreter treats the leading one as the type.
	if st.Args[0].Name != "button" || st.Args[0].Flag != FlagTrue {
		t.Errorf("arg0 = %+v", st.Args[0])
	}
	if st.Args[1].Name != "caption" || st.Args[1].Value.Kind != StringValue ||
		st.Args[1].Value.Str != "Caption Here" {
		t.Errorf("caption arg = %+v", st.Args[1])
	}
	if st.Args[2].Name != "action" || st.Args[2].Value.Kind != WordValue ||
		st.Args[2].Value.Word != "action_id_here" {
		t.Errorf("action arg = %+v", st.Args[2])
	}
	v := st.Args[3].Value
	if v.Kind != NumberValue || v.Number != 4.2 || v.IsInt {
		t.Errorf("float arg = %+v", v)
	}
}

func TestParseFlagStates(t *testing.T) {
	s := mustParse(t, `new checkbox checked !visible ?checked tristate`)
	st := s.Statements[0]
	want := []struct {
		name string
		flag FlagState
	}{
		{"checkbox", FlagTrue}, {"checked", FlagTrue}, {"visible", FlagFalse},
		{"checked", FlagIndeterminate}, {"tristate", FlagTrue},
	}
	if len(st.Args) != len(want) {
		t.Fatalf("args = %d, want %d", len(st.Args), len(want))
	}
	for i, w := range want {
		if st.Args[i].Name != w.name || st.Args[i].Flag != w.flag {
			t.Errorf("arg %d = %q/%v, want %q/%v",
				i, st.Args[i].Name, st.Args[i].Flag, w.name, w.flag)
		}
	}
}

func TestParseAliasTargetsAreStrings(t *testing.T) {
	// Aliases are lexical macros: targets are strings (owner ruling).
	s := mustParse(t, `alias c="caption" a="action"`)
	st := s.Statements[0]
	if st.Verb != "alias" {
		t.Fatalf("verb = %q", st.Verb)
	}
	for i, wantName := range []string{"c", "a"} {
		if st.Args[i].Name != wantName || st.Args[i].Value.Kind != StringValue {
			t.Errorf("arg %d = %+v", i, st.Args[i])
		}
	}
}

func TestParseKeyedChildrenAndSurfacing(t *testing.T) {
	src := `
# build a subtree, then surface a grandchild (D15)
k1=new thing children={sk1=new subthing; sk2=new subthing caption="Two"}
globalsk1=k1.sk1
`
	s := mustParse(t, src)
	if len(s.Statements) != 2 {
		t.Fatalf("statements = %d, want 2", len(s.Statements))
	}

	create := s.Statements[0]
	if create.Key != "k1" || create.Verb != "new" {
		t.Fatalf("create = key %q verb %q", create.Key, create.Verb)
	}
	var children *Value
	for _, a := range create.Args {
		if a.Name == "children" {
			children = a.Value
		}
	}
	if children == nil || children.Kind != BlockValue {
		t.Fatalf("children arg missing or not a block")
	}
	inner := children.Block.Statements
	if len(inner) != 2 {
		t.Fatalf("inner statements = %d, want 2", len(inner))
	}
	if inner[0].Key != "sk1" || inner[0].Verb != "new" {
		t.Errorf("inner0 = %+v", inner[0])
	}
	if inner[1].Key != "sk2" {
		t.Errorf("inner1 key = %q", inner[1].Key)
	}

	surf := s.Statements[1]
	if surf.Key != "globalsk1" || surf.Verb != "" || surf.Ref != "k1.sk1" {
		t.Errorf("surfacing = %+v", surf)
	}
}

func TestParseTemplate(t *testing.T) {
	s := mustParse(t, `template MyBtn=button align=right caption="Click Me"`)
	st := s.Statements[0]
	if st.Verb != "template" || st.Key != "" {
		t.Fatalf("template stmt = %+v", st)
	}
	// Template target is an identifier (semantic), unlike alias targets.
	if st.Args[0].Name != "MyBtn" || st.Args[0].Value.Kind != WordValue ||
		st.Args[0].Value.Word != "button" {
		t.Errorf("template target = %+v", st.Args[0])
	}
	if st.Args[1].Name != "align" || st.Args[1].Value.Word != "right" {
		t.Errorf("align = %+v", st.Args[1])
	}
}

func TestParseSeparatorsAndComments(t *testing.T) {
	src := "new label caption=\"a\"; new label caption=\"b\" # trailing\n# full-line comment\nnew label caption=\"c\""
	s := mustParse(t, src)
	if len(s.Statements) != 3 {
		t.Fatalf("statements = %d, want 3", len(s.Statements))
	}
}

func TestParseNumbers(t *testing.T) {
	s := mustParse(t, `new spacer width=-8 height=16`)
	st := s.Statements[0]
	w, h := st.Args[1].Value, st.Args[2].Value
	if w.Number != -8 || !w.IsInt {
		t.Errorf("width = %+v", w)
	}
	if h.Number != 16 || !h.IsInt {
		t.Errorf("height = %+v", h)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		`new button caption="unterminated`,
		`new button "positional string"`, // D10: values must be named
		`new panel children={new button`, // unterminated block
		`new button x=`,                  // missing value
	}
	for _, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q): expected error, got none", src)
		}
	}
}

func TestParseByteEscapes(t *testing.T) {
	script, err := Parse(`set term feed="\e[1mA\x00B\x7f\xff"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := script.Statements[0].Args[1].Value.Str
	want := "\x1b[1mA\x00B\x7f\xff"
	if got != want {
		t.Errorf("feed bytes = %q, want %q", got, want)
	}

	for _, bad := range []string{`x="\x1"`, `x="\xzz"`, `x="\q"`} {
		if _, err := Parse("new t " + bad); err == nil {
			t.Errorf("Parse(%s): expected error", bad)
		}
	}
}

func TestEventEncodesControlBytes(t *testing.T) {
	ev := NewEvent("data").WithUint("trinket", 5).WithString("text", "\x1b[2Jok\x07")
	back, err := ParseEvent(ev.Encode())
	if err != nil {
		t.Fatalf("ParseEvent(%q): %v", ev.Encode(), err)
	}
	if s, _ := back.Text("text"); s != "\x1b[2Jok\x07" {
		t.Errorf("round-trip = %q", s)
	}
}
