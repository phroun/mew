package client_test

// The veneer tested against the real in-process trinket vocabulary:
// replica reads, write-through without echo, event folding order.

import (
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/protocol"
)

func build(t *testing.T, dispatch func(string), src string) (*client.Conn, *client.UI) {
	t.Helper()
	conn := client.NewInProcess(dispatch)
	ui, err := conn.Build(src)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return conn, ui
}

func TestReplicaMirrorsUserChanges(t *testing.T) {
	_, ui := build(t, nil, `
root=new panel layout=vbox children={
	cb=new checkbox caption="c" tristate
	inp=new textinput text="start"
	combo=new combobox children={new item caption="A"; new item caption="B"} selected=0
}
wcb=root.cb
winp=root.inp
wcombo=root.combo
`)
	cb := ui.Checkbox("wcb")
	inp := ui.TextInput("winp")
	combo := ui.Selector("wcombo")

	// Baseline replica (construction is write-through-equivalent:
	// no echo happened, state starts at defaults).
	if cb.State() != protocol.FlagFalse {
		t.Errorf("initial checkbox state = %v", cb.State())
	}

	// USER interactions on the real trinkets update the replica.
	realCb := cb.Target().(*trinkets.Checkbox)
	realCb.Toggle() // -> checked
	if cb.State() != protocol.FlagTrue || !cb.Checked() {
		t.Errorf("after toggle: state = %v", cb.State())
	}
	realCb.Toggle() // tristate -> partial
	if cb.State() != protocol.FlagIndeterminate {
		t.Errorf("after 2nd toggle: state = %v", cb.State())
	}

	realInp := inp.Target().(*trinkets.TextInput)
	realInp.SetText("typed by user")
	if inp.Text() != "typed by user" {
		t.Errorf("replica text = %q", inp.Text())
	}

	realCombo := combo.Target().(*trinkets.ComboBox)
	realCombo.SetCurrentIndex(1)
	if combo.Selected() != 1 {
		t.Errorf("replica selected = %d", combo.Selected())
	}
}

func TestWriteThroughDoesNotEcho(t *testing.T) {
	_, ui := build(t, nil, `
inp=new textinput
cb=new checkbox caption="x"
`)
	inp := ui.TextInput("inp")
	cb := ui.Checkbox("cb")

	changes := 0
	inp.OnChange(func(string) { changes++ })
	toggles := 0
	cb.OnToggle(func(protocol.FlagState) { toggles++ })

	// Writes through the veneer: replica updated, display updated,
	// NO events back (D20).
	if err := inp.SetText("programmatic"); err != nil {
		t.Fatal(err)
	}
	if err := cb.SetChecked(true); err != nil {
		t.Fatal(err)
	}

	if inp.Text() != "programmatic" {
		t.Errorf("replica text = %q", inp.Text())
	}
	if !cb.Checked() {
		t.Errorf("replica checked = false")
	}
	// The display side really changed.
	if got := inp.Target().(*trinkets.TextInput).Text(); got != "programmatic" {
		t.Errorf("display text = %q", got)
	}
	if !cb.Target().(*trinkets.Checkbox).IsChecked() {
		t.Errorf("display checkbox unchecked")
	}
	// And nothing echoed.
	if changes != 0 || toggles != 0 {
		t.Errorf("echo: changes=%d toggles=%d, want 0", changes, toggles)
	}

	// A subsequent USER edit still flows.
	inp.Target().(*trinkets.TextInput).SetText("user")
	if changes != 1 || inp.Text() != "user" {
		t.Errorf("user edit: changes=%d text=%q", changes, inp.Text())
	}
}

func TestReplicaFoldsBeforeHandlers(t *testing.T) {
	_, ui := build(t, nil, `cb=new checkbox caption="c"`)
	cb := ui.Checkbox("cb")

	// Inside the handler, the replica must already hold the new state.
	var observed protocol.FlagState
	cb.OnToggle(func(protocol.FlagState) { observed = cb.State() })

	cb.Target().(*trinkets.Checkbox).Toggle()
	if observed != protocol.FlagTrue {
		t.Errorf("replica inside handler = %v, want FlagTrue", observed)
	}
}

func TestCommandsAndClicks(t *testing.T) {
	commands := core.NewCommandRegistry()
	dispatched := 0
	commands.Register("do.it", func() { dispatched++ })

	conn, ui := build(t, func(id string) { commands.Dispatch(id) }, `
btn=new button caption="Go" action=do.it
`)
	btn := ui.Button("btn")

	clicks, observed := 0, 0
	btn.OnClick(func() { clicks++ })
	conn.OnCommand("do.it", func() { observed++ })

	btn.Target().(*trinkets.Button).Click()

	if dispatched != 1 {
		t.Errorf("registry dispatched = %d", dispatched)
	}
	if clicks != 1 {
		t.Errorf("clicks = %d", clicks)
	}
	if observed != 1 {
		t.Errorf("command observations = %d", observed)
	}
}

func TestSetEscapeHatchAndDestroy(t *testing.T) {
	_, ui := build(t, nil, `
root=new panel layout=vbox children={
	a=new label caption="a"
	b=new label caption="b"
}
wa=root.a
`)
	a := ui.Label("wa")
	if err := a.SetCaption(`quoted "text" with \ back`); err != nil {
		t.Fatal(err)
	}
	realA := a.Target().(*trinkets.Label)
	if realA.Text() != `quoted "text" with \ back` {
		t.Errorf("caption = %q", realA.Text())
	}

	panel := ui.Object("root").Target().(*trinkets.Panel)
	if len(panel.Children()) != 2 {
		t.Fatalf("children = %d", len(panel.Children()))
	}
	if err := a.Destroy(); err != nil {
		t.Fatal(err)
	}
	if len(panel.Children()) != 1 {
		t.Errorf("children after destroy = %d", len(panel.Children()))
	}
}

func TestConnectionsAreIndependent(t *testing.T) {
	// Multi-display guardrail: two connections, separate sessions,
	// separate replicas and subscriptions.
	_, ui1 := build(t, nil, `cb=new checkbox caption="one"`)
	_, ui2 := build(t, nil, `cb=new checkbox caption="two"`)

	cb1 := ui1.Checkbox("cb")
	cb2 := ui2.Checkbox("cb")
	if cb1.ID() == cb2.ID() {
		t.Fatal("distinct objects share an ID")
	}

	cb1.Target().(*trinkets.Checkbox).Toggle()
	if !cb1.Checked() {
		t.Error("conn1 replica missed its own toggle")
	}
	if cb2.Checked() {
		t.Error("conn2 replica polluted by conn1's toggle")
	}
}
