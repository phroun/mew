package protocol

// A property always travels under its name.
//
// The grammar now lets a verb take operands -- values written with no name, in
// the order they were written -- because a filter is an operator, a field and
// what it is matched against, with no room for invented argument names. But
// that is a VERB's business. A property statement still refuses an operand:
// the name is what the alias dictionaries are for, and a value that arrived
// without one names nothing.

import (
	"strings"
	"testing"
)

func TestAPropertyStatementRefusesAnOperand(t *testing.T) {
	for _, src := range []string{
		`it=new testleaf "positional string"`,
		`it=new testleaf 42`,
		`it=new testform children={ new testleaf "stray" }`,
	} {
		script, err := Parse(src)
		if err != nil {
			t.Errorf("%q: the grammar refused it, which is the verb's job now: %v", src, err)
			continue
		}
		f := NewRegistryFactory(&BindContext{})
		if _, err := NewSession().Execute(script, f); err == nil {
			t.Errorf("%q: an operand was applied as a property", src)
		} else if !strings.Contains(err.Error(), "must be named") {
			t.Errorf("%q: refused with %v, which does not say why", src, err)
		}
	}
}

// The target of a verb is an operand, and still reaches the object it names.
func TestAVerbStillFindsItsTarget(t *testing.T) {
	f := NewRegistryFactory(&BindContext{})
	s := NewSession()

	script, err := Parse(`it=new testleaf name="first"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	id := s.keys["it"]
	if id == 0 {
		t.Fatal("nothing was keyed `it`")
	}

	set, err := Parse("set " + itoa(id) + ` name="second"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(set, f); err != nil {
		t.Fatalf("set by target reference: %v", err)
	}
	obj, _ := s.Object(id)
	if got := obj.(*registryObject).target.(*testLeaf).name; got != "second" {
		t.Errorf("the target's name is %q, want %q", got, "second")
	}
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
