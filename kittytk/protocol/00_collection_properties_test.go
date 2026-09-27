package protocol

import (
	"fmt"
	"strings"
	"testing"
)

// A {} block is a value kind like any other, and the property it was written
// on decides what becomes of it. These types exercise that: a form with two
// separate collections, one open and one naming its members.

type testForm struct {
	leaves []string
	rails  []string
}

type testLeaf struct{ name string }
type testRail struct{ name string }

// testNameProp registers a name= on any of the three targets, so a test can
// tell one child from another once it has been adopted.
func testNameProp[T any](set func(*T, string)) Property {
	return NewProperty("string", func(_ *BindContext, target any, v *Value, f FlagState) error {
		s, err := AsString("name", v, f)
		if err != nil {
			return err
		}
		set(target.(*T), s)
		return nil
	})
}

func init() {
	RegisterType("testleaf", &TypeSpec{
		Virtual: true,
		New:     func() any { return &testLeaf{} },
		Props: map[string]Property{
			"name": testNameProp(func(l *testLeaf, s string) { l.name = s }),
		},
	})
	RegisterType("testrail", &TypeSpec{
		Virtual: true,
		New:     func() any { return &testRail{} },
		Props: map[string]Property{
			"name": testNameProp(func(r *testRail, s string) { r.name = s }),
		},
	})
	RegisterType("testform", &TypeSpec{
		Virtual: true,
		New:     func() any { return &testForm{} },
		Props: map[string]Property{
			"caption": NewProperty("string", func(_ *BindContext, _ any, v *Value, f FlagState) error {
				_, err := AsString("caption", v, f)
				return err
			}),
			"children": NewCollection(func(parent, child any) error {
				l, ok := child.(*testLeaf)
				if !ok {
					return fmt.Errorf("testform: children must be leaves, got %T", child)
				}
				p := parent.(*testForm)
				p.leaves = append(p.leaves, l.name)
				return nil
			}).Tip("Leaves."),
			"rails": NewCollection(func(parent, child any) error {
				r := child.(*testRail)
				p := parent.(*testForm)
				p.rails = append(p.rails, r.name)
				return nil
			}).Members("testrail").Tip("Rails."),
		},
	})
	// A type with no collection at all: children are not a thing it has.
	RegisterType("testplain", &TypeSpec{
		Virtual: true,
		New:     func() any { return &testLeaf{} },
	})
}

// buildTarget runs src, which must key one object as `it`, and returns the
// target that object was built around.
func buildTarget(t *testing.T, src string) any {
	t.Helper()
	f := NewRegistryFactory(&BindContext{})
	s := NewSession()
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	obj, ok := s.Object(s.keys["it"])
	if !ok {
		t.Fatalf("%q built nothing keyed `it`", src)
	}
	return obj.(*registryObject).target
}

// buildErr runs src and returns the error it must produce.
func buildErr(t *testing.T, src string) string {
	t.Helper()
	f := NewRegistryFactory(&BindContext{})
	s := NewSession()
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.Execute(script, f); err != nil {
		return err.Error()
	}
	t.Fatalf("executing %q: expected an error", src)
	return ""
}

// Each block goes to the property it was written on, so a type can accept
// several and keep them apart. Nothing here knows the name `children`.
func TestEachCollectionAdoptsIntoTheProperyItWasWrittenOn(t *testing.T) {
	form := buildTarget(t, `it=new testform children={
	new testleaf name="first"
	new testleaf name="second"
} rails={
	new testrail name="rail"
}`).(*testForm)

	if got := strings.Join(form.leaves, ","); got != "first,second" {
		t.Errorf("children= adopted %q, want first,second", got)
	}
	if got := strings.Join(form.rails, ","); got != "rail" {
		t.Errorf("rails= adopted %q, want rail", got)
	}
}

// Members are checked against the child's own type name, so the refusal is
// written in the vocabulary of the script rather than in Go types.
func TestACollectionRefusesAMemberItDoesNotName(t *testing.T) {
	err := buildErr(t, `it=new testform rails={ new testleaf name="wrong" }`)
	for _, want := range []string{"rails", "testrail", "testleaf"} {
		if !strings.Contains(err, want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}

	// A collection that names no members takes whatever its appender
	// accepts, and says so in the appender's own words.
	err = buildErr(t, `it=new testform children={ new testrail name="wrong" }`)
	if !strings.Contains(err, "children must be leaves") {
		t.Errorf("open collection refused with %q, want the appender's message", err)
	}
}

// The two shapes do not cross: a value property will not take a block, and a
// collection will not take a value.
func TestAPropertyTakesEitherAValueOrABlockAndSaysWhich(t *testing.T) {
	err := buildErr(t, `it=new testform caption={ new testleaf }`)
	if !strings.Contains(err, "caption") || !strings.Contains(err, "value") {
		t.Errorf("a block on a value property: %q", err)
	}

	err = buildErr(t, `it=new testform rails="not a block"`)
	if !strings.Contains(err, "rails") || !strings.Contains(err, "{} block") {
		t.Errorf("a value on a collection property: %q", err)
	}
}

// A type that registers no collection refuses children the same way it
// refuses any property it does not have -- one answer, not two.
func TestATypeWithNoCollectionHasNoChildrenProperty(t *testing.T) {
	err := buildErr(t, `it=new testplain children={ new testleaf }`)
	if !strings.Contains(err, `"children"`) || !strings.Contains(err, "not supported") {
		t.Errorf("refusal %q, want the same answer any unknown property gets", err)
	}
}

// A registration is one shape or the other. Both at once, or neither, is a
// property the session cannot route, and it is caught where it is written.
func TestAPropertyRegistrationMustBeOneShapeOrTheOther(t *testing.T) {
	apply := func(*BindContext, any, *Value, FlagState) error { return nil }
	accept := func(any, any) error { return nil }

	for _, c := range []struct {
		name string
		p    Property
	}{
		{"neither", Property{Desc: PropDesc{Kind: "string"}}},
		{"both", Property{Apply: apply, Accept: accept, Desc: PropDesc{Kind: "collection"}}},
		{"collection kind with an applier", Property{Apply: apply, Desc: PropDesc{Kind: "collection"}}},
		{"appender under a value kind", Property{Accept: accept, Desc: PropDesc{Kind: "string"}}},
		{"members on a value property", NewProperty("string", apply).Members("testrail")},
	} {
		if err := checkPropertyShape(c.p); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}

	for _, c := range []struct {
		name string
		p    Property
	}{
		{"a value property", NewProperty("string", apply)},
		{"an open collection", NewCollection(accept)},
		{"a collection naming members", NewCollection(accept).Members("testrail")},
	} {
		if err := checkPropertyShape(c.p); err != nil {
			t.Errorf("%s: rejected with %v", c.name, err)
		}
	}
}

// A collection describes itself like any other property, and the members it
// names survive the round trip to a client.
func TestACollectionDescribesItsMembers(t *testing.T) {
	v := DescribeVocabulary()
	var rails, children *PropInfo
	for i := range v.Types {
		if v.Types[i].Name != "testform" {
			continue
		}
		for j := range v.Types[i].Props {
			switch v.Types[i].Props[j].Name {
			case "rails":
				rails = &v.Types[i].Props[j]
			case "children":
				children = &v.Types[i].Props[j]
			}
		}
	}
	if rails == nil || children == nil {
		t.Fatal("testform's collections are missing from the described vocabulary")
	}
	if rails.Kind != "collection" || children.Kind != "collection" {
		t.Errorf("kinds are %q and %q, want collection", rails.Kind, children.Kind)
	}
	if strings.Join(rails.Members, ",") != "testrail" {
		t.Errorf("rails members = %v, want [testrail]", rails.Members)
	}
	if len(children.Members) != 0 {
		t.Errorf("an open collection named members %v", children.Members)
	}

	decoded, err := DecodeVocabulary(strings.Split(strings.TrimSuffix(EncodeVocabulary(v), "\n"), "\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ty := range decoded.Types {
		if ty.Name != "testform" {
			continue
		}
		for _, p := range ty.Props {
			if p.Name != "rails" {
				continue
			}
			if strings.Join(p.Members, ",") != "testrail" {
				t.Errorf("after the round trip rails members = %v, want [testrail]", p.Members)
			}
			return
		}
	}
	t.Error("rails did not survive the describe round trip")
}
