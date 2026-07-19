package protocol

import (
	"fmt"
	"strings"
	"testing"
)

// mockObject records everything the session applies to it.
type mockObject struct {
	id       uint64
	typeName string
	sets     []string // "name=value" / "name!" / "name?" / "name~" (false)
	children []*mockObject
}

func (m *mockObject) Set(name string, v *Value, flag FlagState) error {
	switch flag {
	case FlagTrue:
		m.sets = append(m.sets, name+"!")
	case FlagFalse:
		m.sets = append(m.sets, name+"~")
	case FlagIndeterminate:
		m.sets = append(m.sets, name+"?")
	default:
		switch v.Kind {
		case StringValue:
			m.sets = append(m.sets, fmt.Sprintf("%s=%q", name, v.Str))
		case WordValue:
			m.sets = append(m.sets, name+"="+v.Word)
		case NumberValue:
			m.sets = append(m.sets, fmt.Sprintf("%s=%v", name, v.Number))
		default:
			return fmt.Errorf("unexpected value kind")
		}
	}
	return nil
}

func (m *mockObject) Append(child Object) error {
	m.children = append(m.children, child.(*mockObject))
	return nil
}

func (m *mockObject) ID() uint64 { return m.id }

type mockFactory struct {
	nextID  uint64
	created []*mockObject
	known   map[string]bool
}

func newMockFactory(types ...string) *mockFactory {
	known := map[string]bool{}
	for _, t := range types {
		known[t] = true
	}
	return &mockFactory{known: known}
}

func (f *mockFactory) New(typeName string) (Object, error) {
	if !f.known[typeName] {
		return nil, fmt.Errorf("unknown trinket type %q", typeName)
	}
	f.nextID++
	obj := &mockObject{id: f.nextID, typeName: typeName}
	f.created = append(f.created, obj)
	return obj, nil
}

func exec(t *testing.T, s *Session, f Factory, src string) *Reply {
	t.Helper()
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reply, err := s.Execute(script, f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return reply
}

func execErr(t *testing.T, s *Session, f Factory, src string) error {
	t.Helper()
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = s.Execute(script, f)
	if err == nil {
		t.Fatalf("Execute(%q): expected error", src)
	}
	return err
}

func TestExecuteNewWithProperties(t *testing.T) {
	f := newMockFactory("button")
	s := NewSession()
	exec(t, s, f, `new button caption="Hi" action=file.open default !visible ?checked x=4.2`)

	if len(f.created) != 1 {
		t.Fatalf("created = %d", len(f.created))
	}
	got := strings.Join(f.created[0].sets, " ")
	want := `caption="Hi" action=file.open default! visible~ checked? x=4.2`
	if got != want {
		t.Errorf("sets:\n got  %s\n want %s", got, want)
	}
}

func TestAliasDeclareAndUse(t *testing.T) {
	f := newMockFactory("button")
	s := NewSession()
	exec(t, s, f, `
alias C="caption" V="visible"
new button C="Aliased" !V
`)
	got := strings.Join(f.created[0].sets, " ")
	want := `caption="Aliased" visible~`
	if got != want {
		t.Errorf("sets:\n got  %s\n want %s", got, want)
	}
}

func TestAliasRules(t *testing.T) {
	s := NewSession()
	f := newMockFactory("button")

	// Lowercase alias name violates D18.
	execErr(t, s, f, `alias c="caption"`)
	// Non-string target violates the lexical-macro ruling.
	execErr(t, s, f, `alias C=caption`)
	// Undeclared uppercase property name is an unknown alias.
	execErr(t, s, f, `new button X="boom"`)
}

func TestTemplateInstantiationWithOverrides(t *testing.T) {
	f := newMockFactory("button")
	s := NewSession()
	exec(t, s, f, `
template MyBtn=button align=right caption="Click Me" visible
new MyBtn caption="Other" !visible
`)
	got := strings.Join(f.created[0].sets, " ")
	// Template properties first, instance overrides after (later wins
	// at the object layer); !visible un-sets the template's flag (D12).
	want := `align=right caption="Click Me" visible! caption="Other" visible~`
	if got != want {
		t.Errorf("sets:\n got  %s\n want %s", got, want)
	}
}

func TestTransitiveTemplatesAndCycleGuard(t *testing.T) {
	f := newMockFactory("button")
	s := NewSession()
	exec(t, s, f, `
template Base=button caption="base"
template Big=Base font_size=20
new Big
`)
	got := strings.Join(f.created[0].sets, " ")
	want := `caption="base" font_size=20`
	if got != want {
		t.Errorf("sets:\n got  %s\n want %s", got, want)
	}

	// Cycles cannot be declared forward (unknown base), and re-pointing
	// a base later cannot create one either without redeclaration - but
	// guard the walker regardless by wiring one manually.
	s.templates["A"] = &templateDef{base: "B"}
	s.templates["B"] = &templateDef{base: "A"}
	execErr(t, s, f, `new A`)
}

func TestTemplateRules(t *testing.T) {
	s := NewSession()
	f := newMockFactory("button")

	execErr(t, s, f, `template lowercase=button`) // D18
	execErr(t, s, f, `template Orphan=Missing`)   // unknown base template
	execErr(t, s, f, `new Undeclared`)            // unknown template
}

func TestTemplateWithChildrenComponent(t *testing.T) {
	f := newMockFactory("panel", "label", "textinput")
	s := NewSession()
	reply := exec(t, s, f, `
template LabeledInput=panel layout=hbox children={lbl=new label caption="?"; input=new textinput}
k1=new LabeledInput children={new label caption="extra"}
mine=k1.input
`)
	root := f.created[0]
	if root.typeName != "panel" {
		t.Fatalf("root type = %s", root.typeName)
	}
	// Template children first, instance children appended (D14).
	if len(root.children) != 3 {
		t.Fatalf("children = %d, want 3", len(root.children))
	}
	if root.children[0].typeName != "label" || root.children[1].typeName != "textinput" ||
		root.children[2].typeName != "label" {
		t.Errorf("child types = %s, %s, %s",
			root.children[0].typeName, root.children[1].typeName, root.children[2].typeName)
	}

	// D15: instance key namespaces template-body keys; surfacing works.
	if reply.IDs["k1"] != root.id {
		t.Errorf("reply k1 = %d, want %d", reply.IDs["k1"], root.id)
	}
	if reply.IDs["mine"] != root.children[1].id {
		t.Errorf("reply mine = %d, want %d (the textinput)", reply.IDs["mine"], root.children[1].id)
	}
	// Non-surfaced nested keys stay out of the reply.
	if _, ok := reply.IDs["k1.lbl"]; ok {
		t.Error("nested key leaked into reply")
	}
	if len(reply.IDs) != 2 {
		t.Errorf("reply size = %d, want 2", len(reply.IDs))
	}
}

func TestScopedKeysUnkeyedParentStaysInternal(t *testing.T) {
	f := newMockFactory("panel", "button")
	s := NewSession()
	script, err := Parse(`
new panel children={sk=new button}
g=sk
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(script, f); err == nil {
		t.Fatal("expected error: child key of an unkeyed parent must not be addressable")
	}
}

func TestNestedChildrenPaths(t *testing.T) {
	f := newMockFactory("panel", "button")
	s := NewSession()
	reply := exec(t, s, f, `
k1=new panel children={inner=new panel children={btn=new button}}
deep=k1.inner.btn
`)
	if len(f.created) != 3 {
		t.Fatalf("created = %d", len(f.created))
	}
	btn := f.created[2]
	if reply.IDs["deep"] != btn.id {
		t.Errorf("deep = %d, want %d", reply.IDs["deep"], btn.id)
	}
}

func TestUnknownVerbAndBadSurfacing(t *testing.T) {
	s := NewSession()
	f := newMockFactory("button")
	execErr(t, s, f, `destroy id=4`)
	execErr(t, s, f, `g=nothing.here`)
}
