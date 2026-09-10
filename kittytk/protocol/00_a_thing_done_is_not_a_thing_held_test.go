package protocol

// `set` writes a value the object then holds; `ask` puts a question and the
// answer comes back. A thing simply DONE is neither, and was spelled as a
// property -- `set <mdi> tile` reading like state and behaving like a command.
// `do` is the verb for it, and the test between the two is whether asking for
// it afterwards means anything.

import (
	"strings"
	"testing"
)

// doneThing records what was done to it and whether emission was suppressed at
// the time, which is what D20 asks of a verb that changes the display.
type doneThing struct {
	did        []string
	arg        int
	suppressed bool
}

func (d *doneThing) ID() uint64                          { return 4242 }
func (d *doneThing) Set(string, *Value, FlagState) error { return nil }
func (d *doneThing) Append(string, Object) error         { return nil }
func (d *doneThing) Ask(question string, _ []*Arg) error {
	d.did = append(d.did, "ask:"+question)
	return nil
}

func (d *doneThing) Do(action string, args []*Arg) error {
	d.did = append(d.did, action)
	d.suppressed = suppressionDepth > 0
	for _, a := range args {
		if a.Name == "window" && a.Value != nil {
			d.arg = int(a.Value.Number)
		}
	}
	return nil
}

// idleThing is an object that does nothing at all.
type idleThing struct{}

func (idleThing) ID() uint64                          { return 99 }
func (idleThing) Set(string, *Value, FlagState) error { return nil }
func (idleThing) Append(string, Object) error         { return nil }

// suppressionDepth is what countingFactory is inside of, so an object's Do can
// see whether the verb that called it suppressed emission.
var suppressionDepth int

type countingFactory struct{}

func (countingFactory) New(string) (Object, error) { return nil, nil }
func (countingFactory) Subscribe(uint64, string)   {}
func (countingFactory) Unsubscribe(uint64, string) {}
func (countingFactory) Suppressed(f func())        { suppressionDepth++; f(); suppressionDepth-- }

func init() {
	RegisterType("testdoer", &TypeSpec{
		Virtual: true,
		New:     func() any { return &doneThing{} },
		Does: map[string]DoDesc{
			"tile": NewDoDesc("Tile them."),
			"restore": NewDoDesc("Restore one.").
				Arg("window", "uint", "Which."),
		},
		Asks: map[string]AskDesc{
			"count": NewAskDesc("How many.").Answering("counted"),
		},
	})
	RegisterType("testidler", &TypeSpec{Virtual: true, New: func() any { return &idleThing{} }})
}

// doing runs src against a session holding one thing that does things.
func doing(t *testing.T, src string) (*doneThing, error) {
	t.Helper()
	suppressionDepth = 0
	obj := &doneThing{}
	s := NewSession()
	s.Register(obj)
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	_, err = s.Execute(script, countingFactory{})
	return obj, err
}

func TestAnActionIsDoneAndItsArgumentsArrive(t *testing.T) {
	obj, err := doing(t, "do 4242 tile\ndo 4242 restore window=17")
	if err != nil {
		t.Fatalf("doing: %v", err)
	}
	if got := strings.Join(obj.did, ","); got != "tile,restore" {
		t.Errorf("the object was told %q, want tile,restore", got)
	}
	if obj.arg != 17 {
		t.Errorf("restore reached it with window=%d, want 17", obj.arg)
	}
}

// D20: a change the client asked for does not come back at it as events, so an
// action runs with emission suppressed the way `new` and `set` do.
func TestAnActionRunsWithEmissionSuppressed(t *testing.T) {
	obj, err := doing(t, "do 4242 tile")
	if err != nil {
		t.Fatalf("doing: %v", err)
	}
	if !obj.suppressed {
		t.Error("the action ran with emission open, so what it changed echoes back")
	}
	if suppressionDepth != 0 {
		t.Errorf("suppression was left %d deep", suppressionDepth)
	}
}

// An action no type declares is refused rather than accepted and quietly
// dropped -- the same rule a misspelled event name gets.
func TestAnActionNothingDeclaresIsRefused(t *testing.T) {
	_, err := doing(t, "do 4242 wibble")
	if err == nil {
		t.Fatal("an undeclared action was accepted")
	}
	if !strings.Contains(err.Error(), "wibble") {
		t.Errorf("the refusal reads %q", err)
	}
}

// The refusal from a type that does nothing says so, rather than naming a list
// it does not have.
func TestATypeThatDoesNothingSaysSo(t *testing.T) {
	s := NewSession()
	f := NewRegistryFactory(&BindContext{})
	script, err := Parse("it=new testidler\ndo it tile")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = s.Execute(script, f)
	if err == nil {
		t.Fatal("a type that does nothing accepted an action")
	}
	if !strings.Contains(err.Error(), "does nothing at all") {
		t.Errorf("the refusal reads %q", err)
	}
}

// A type that does things names them when it is asked for one it has not got.
func TestATypeThatDoesThingsNamesThem(t *testing.T) {
	s := NewSession()
	f := NewRegistryFactory(&BindContext{})
	script, _ := Parse("it=new testdoer\ndo it wibble")
	_, err := s.Execute(script, f)
	if err == nil {
		t.Fatal("an action the type does not have was accepted")
	}
	for _, want := range []string{"wibble", "tile", "restore"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q", err, want)
		}
	}
}

// The grammar: a target, then a bare action word. A named value in the action's
// place is not one, the way a named value is not an event name in `sub`.
func TestAnActionNeedsATargetAndAWord(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"do", "expected a target"},
		{"do 4242", "expected an action"},
		{"do 4242 tile=1", "expected an action"},
	} {
		if _, err := doing(t, c.src); err == nil {
			t.Errorf("%q was accepted", c.src)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q reads %q, want %q in it", c.src, err, c.want)
		}
	}
}

// Questions and actions are declared alike and reported alike, so a client
// discovers what it may do the way it discovers what it may ask.
func TestDescribeCarriesTheActions(t *testing.T) {
	enc := EncodeVocabulary(DescribeVocabulary())
	for _, want := range []string{
		`do of="testdoer" name="restore"`,
		`doarg of="testdoer" do="restore" name="window"`,
		`ask of="testdoer" name="count"`,
	} {
		if !strings.Contains(enc, want) {
			t.Errorf("the describe stream has no %s", want)
		}
	}
	// An action answers nothing, so it carries no answers= to mislead anyone.
	for _, line := range strings.Split(enc, "\n") {
		if strings.HasPrefix(line, "do of=") && strings.Contains(line, "answers=") {
			t.Errorf("an action reports answers: %s", line)
		}
	}

	// And it survives the round trip a client makes of it.
	v, err := DecodeVocabulary(strings.Split(strings.TrimRight(enc, "\n"), "\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ty := range v.Types {
		if ty.Name != "testdoer" {
			continue
		}
		var names []string
		for _, d := range ty.Does {
			names = append(names, d.Name)
		}
		if got := strings.Join(names, ","); got != "restore,tile" {
			t.Errorf("testdoer came back doing %q, want restore,tile", got)
		}
		for _, d := range ty.Does {
			if d.Name == "restore" && (len(d.Args) != 1 || d.Args[0].Name != "window") {
				t.Errorf("restore came back with args %v", d.Args)
			}
		}
		return
	}
	t.Error("testdoer did not survive the round trip")
}
