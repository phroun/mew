package wire

// A number crossing the wire keeps every digit it was sent with.
//
// A float64 stops being able to tell two integers apart at 2^53, and the
// things that travel as integers here go well past it: a record key, a hash, a
// nanosecond timestamp (~1.7e18). PSL already carries a full int64 in and out
// unharmed, so a wire that rounded one would be the narrow part of the path.

import (
	"strings"
	"testing"
)

// The two integers in each pair are a single step apart and land on the same
// float64, so any step that widens loses the difference.
const (
	justPast    = int64(9007199254740993) // 2^53 + 1, the first integer a float64 cannot hold
	justBelow   = int64(9007199254740992) // 2^53, which it can
	nanoseconds = int64(1700000000123456789)
)

func TestALargeIntegerArrivesWithEveryDigit(t *testing.T) {
	script, err := Parse("set 7 key=9007199254740993 stamp=1700000000123456789")
	if err != nil {
		t.Fatal(err)
	}
	stmt := script.Statements[0]
	for _, c := range []struct {
		name string
		want int64
	}{{"key", justPast}, {"stamp", nanoseconds}} {
		v := argValue(t, stmt, c.name)
		if !v.IsInt {
			t.Errorf("%s did not arrive as an integer: %#v", c.name, v)
			continue
		}
		if v.Int != c.want {
			t.Errorf("%s arrived as %d, want %d", c.name, v.Int, c.want)
		}
	}
}

// A fractional number is a float and says so.
func TestAFractionalNumberIsNotAnInteger(t *testing.T) {
	script, err := Parse("set 7 size=1.5")
	if err != nil {
		t.Fatal(err)
	}
	v := argValue(t, script.Statements[0], "size")
	if v.IsInt {
		t.Errorf("1.5 arrived as an integer: %#v", v)
	}
	if v.Number != 1.5 {
		t.Errorf("1.5 arrived as %v", v.Number)
	}
}

// A whole number too large even for an int64 is a float, and says that too --
// IsInt means the exact value is there, not that a dot was left out.
func TestAWholeNumberTooLargeToHoldIsAFloat(t *testing.T) {
	script, err := Parse("set 7 huge=99999999999999999999999")
	if err != nil {
		t.Fatal(err)
	}
	v := argValue(t, script.Statements[0], "huge")
	if v.IsInt {
		t.Errorf("a number past int64 claimed to be an exact integer: %#v", v)
	}
	if v.Number < 9.9e22 {
		t.Errorf("a number past int64 arrived as %v", v.Number)
	}
}

// An id surfaced as `key=<id>` is the id it was written as.
func TestAnIdSurfacedByNameKeepsItsDigits(t *testing.T) {
	script, err := Parse("app=9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	if got := script.Statements[0].RefID; got != uint64(justPast) {
		t.Errorf("the surfaced id is %d, want %d", got, justPast)
	}
}

// An event carries an id out and back without rounding it on either leg.
func TestAnEventCarriesALargeIdBothWays(t *testing.T) {
	text := NewEvent("thing_moved").WithUint("id", uint64(justPast)).Encode()
	if !strings.Contains(text, "9007199254740993") {
		t.Fatalf("the encoded event lost the id: %s", text)
	}

	ev, err := ParseEvent(text)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ev.Uint("id")
	if !ok {
		t.Fatalf("the event has no id: %s", text)
	}
	if got != uint64(justPast) {
		t.Errorf("the id came back as %d, want %d", got, justPast)
	}
}

// Two ids a single step apart stay apart, which is the whole point: through a
// float64 they are the same number.
func TestTwoAdjacentIdsStayApart(t *testing.T) {
	script, err := Parse("set 7 a=9007199254740992 b=9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	stmt := script.Statements[0]
	a, b := argValue(t, stmt, "a"), argValue(t, stmt, "b")
	if a.Int == b.Int {
		t.Errorf("two ids one apart arrived as the same number: %d", a.Int)
	}
	if a.Int != justBelow || b.Int != justPast {
		t.Errorf("ids arrived as %d and %d, want %d and %d", a.Int, b.Int, justBelow, justPast)
	}
}

// argValue is the value of one named argument of a statement.
func argValue(t *testing.T, stmt *Statement, name string) *Value {
	t.Helper()
	for _, a := range stmt.Args {
		if a.Name == name {
			if a.Value == nil {
				t.Fatalf("%s has no value", name)
			}
			return a.Value
		}
	}
	t.Fatalf("no argument named %s", name)
	return nil
}
