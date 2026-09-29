package trinkets

// What an application's `do` verbs ask of a list's ticks, and what they refuse.
// A row is named by its record's identity, so one the list has not placed yet
// is ticked all the same, and turns up ticked when it arrives.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// doing parses one `do` action's arguments, as the session hands them over.
func doing(t *testing.T, l *ListView, src string) error {
	t.Helper()
	script, err := wire.Parse("do x " + src + "\n")
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	args := script.Statements[0].Args
	return l.Do(args[1].Name, args[2:])
}

func TestTheDoVerbsTickAndUntick(t *testing.T) {
	l := ticking(4)
	for _, step := range []struct{ do, want string }{
		{"check id=2", "[2]"},
		{"check id=0 checked", "[0 2]"},
		{"check id=2 checked=false", "[0]"},
		{"check_invert", "[1 2 3]"},
		{"check_none", "[]"},
		{"check_all", "[0 1 2 3]"},
	} {
		if err := doing(t, l, step.do); err != nil {
			t.Fatalf("%s: %v", step.do, err)
		}
		if got := ticked(l); got != step.want {
			t.Errorf("after %s: %s ticked, want %s", step.do, got, step.want)
		}
	}
}

func TestTheDoVerbsRefuseWhatTheyCannotDo(t *testing.T) {
	l := ticking(3)
	for _, c := range []struct{ do, says string }{
		{"check", "expected id="},
		{"check id=1 checked=maybe", "checked"},
		{"tick id=1", "nothing called"},
	} {
		err := doing(t, l, c.do)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: refused with %v, want a refusal saying %q", c.do, err, c.says)
		}
	}
	l.SetCheckboxes(false)
	l.SetCheckedID(serval.NewInt(2), true)
	if l.IsSelected(2) {
		t.Error("SetCheckedID ticked a row of a list without checkboxes")
	}
	for _, verb := range []string{"check id=1", "check_all", "check_none", "check_invert"} {
		if err := doing(t, l, verb); err == nil || !strings.Contains(err.Error(), "no checkboxes") {
			t.Errorf("%s on a list without checkboxes: %v", verb, err)
		}
	}
}

func TestARecordIsTickedBeforeItIsPlaced(t *testing.T) {
	rows := make([]serval.Row, 50)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))
	l.SetCheckboxes(true)
	l.extent(0, 5)
	var told []CheckChange
	l.SetOnCheck(func(c CheckChange) { told = append(told, c) })

	if err := doing(t, l, `check id="k40"`); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0].Row != -1 || told[0].ID == nil || told[0].ID.Str != "k40" || !told[0].Checked {
		t.Fatalf("told %+v, want one row change for k40 at no place yet", told)
	}
	l.extent(38, 5)
	if !l.IsSelected(40) || l.IsSelected(39) {
		t.Error("the record ticked before it arrived is not the one ticked now it has")
	}
	// Placed, the next change says where.
	l.SetCheckedID(serval.NewText("k40"), false)
	if last := told[len(told)-1]; last.Row != 40 || last.Checked {
		t.Errorf("unticking the placed record told %+v, want row 40 unticked", last)
	}
}

// The Do verbs are exactly what the list declares on the wire, so none of them
// is turned away before it arrives and none arrives undeclared.
func TestTheDoVerbsAreTheDeclaredOnes(t *testing.T) {
	l := ticking(2)
	does := protocol.DoNames("listview")
	if len(does) != 4 {
		t.Errorf("the listview declares %v, want the four check verbs", does)
	}
	for _, name := range does {
		args := ""
		if name == "check" {
			args = " id=0"
		}
		if err := doing(t, l, name+args); err != nil {
			t.Errorf("declared %s is refused: %v", name, err)
		}
	}
	if !protocol.TypeAnswers("listview", AskChecked) {
		t.Error("the checked question is not declared")
	}
}
