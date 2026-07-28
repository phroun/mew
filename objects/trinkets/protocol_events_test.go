package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// buildWithEvents builds protocol UI with an event-collecting context,
// subscribed wildcard (D20 default-closed: tests opt into everything).
func buildWithEvents(t *testing.T, commands *core.CommandRegistry, src string) (*captureFactory, *[]*protocol.Event) {
	t.Helper()
	events := &[]*protocol.Event{}
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { *events = append(*events, ev) },
	}
	if commands != nil {
		ctx.Dispatch = func(id string) { commands.Dispatch(id) }
	}
	ctx.Subscribe(0, "")
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return f, events
}

func eventsOfType(events []*protocol.Event, typ string) []*protocol.Event {
	var out []*protocol.Event
	for _, ev := range events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

func TestCheckboxTogglesEmitTriStateEvents(t *testing.T) {
	f, events := buildWithEvents(t, nil, `new checkbox caption="t" tristate`)
	cb := f.targets[0].(*Checkbox)
	*events = nil // ignore construction-time emissions

	// Toggle cycle: Unchecked -> Checked -> Partially -> Unchecked.
	cb.Toggle()
	cb.Toggle()
	cb.Toggle()

	got := eventsOfType(*events, "toggle")
	if len(got) != 3 {
		t.Fatalf("toggle events = %d, want 3", len(got))
	}
	want := []protocol.FlagState{protocol.FlagTrue, protocol.FlagIndeterminate, protocol.FlagFalse}
	for i, ev := range got {
		if id, ok := ev.Trinket(); !ok || id != uint64(cb.ObjectID()) {
			t.Errorf("event %d trinket = %d", i, id)
		}
		if ev.Flag("checked") != want[i] {
			t.Errorf("event %d checked = %v, want %v", i, ev.Flag("checked"), want[i])
		}
	}
}

func TestButtonClickEmitsClickAndCommandEvents(t *testing.T) {
	commands := core.NewCommandRegistry()
	fired := 0
	commands.Register("do.it", func() { fired++ })

	f, events := buildWithEvents(t, commands, `new button caption="Go" action=do.it`)
	btn := f.targets[0].(*Button)
	*events = nil

	btn.Click()

	if fired != 1 {
		t.Errorf("registry fired = %d, want 1", fired)
	}
	cmds := eventsOfType(*events, "command")
	clicks := eventsOfType(*events, "click")
	if len(cmds) != 1 || len(clicks) != 1 {
		t.Fatalf("command=%d click=%d, want 1 each", len(cmds), len(clicks))
	}
	if action, ok := cmds[0].Word("action"); !ok || action != "do.it" {
		t.Errorf("command action = %q", action)
	}
	if id, ok := clicks[0].Trinket(); !ok || id != uint64(btn.ObjectID()) {
		t.Errorf("click trinket = %d", id)
	}
}

func TestTextInputChangeEvents(t *testing.T) {
	f, events := buildWithEvents(t, nil, `new textinput text="start"`)
	ti := f.targets[0].(*TextInput)

	// D20: wire-initiated property application never echoes - even
	// with a wildcard subscription, construction emits nothing.
	if built := eventsOfType(*events, "change"); len(built) != 0 {
		t.Fatalf("construction change events = %d, want 0 (D20 no echo)", len(built))
	}

	ti.SetText("edited")

	got := eventsOfType(*events, "change")
	if len(got) != 1 {
		t.Fatalf("change events = %d, want 1", len(got))
	}
	if s, ok := got[0].Text("text"); !ok || s != "edited" {
		t.Errorf("text = %q", s)
	}
}

func TestComboBoxSelectionChangeEvents(t *testing.T) {
	f, events := buildWithEvents(t, nil, `
new combobox children={new item caption="A"; new item caption="B"} selected=1
`)
	combo := f.targets[0].(*ComboBox)
	*events = nil

	combo.SetCurrentIndex(0)

	got := eventsOfType(*events, "change")
	if len(got) != 1 {
		t.Fatalf("change events = %d, want 1", len(got))
	}
	if sel, ok := got[0].Int("selected"); !ok || sel != 0 {
		t.Errorf("selected = %d", sel)
	}
}

func TestEventsRouteThroughDispatcher(t *testing.T) {
	// The full loop: protocol-built trinket + sub verb -> event record
	// -> dispatcher -> app handler keyed by ObjectID. The sub verb is
	// what opens the flow (D20 default-closed).
	dispatcher := protocol.NewEventDispatcher()
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { dispatcher.Dispatch(ev) },
	}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, err := protocol.Parse(`
cb=new checkbox caption="c"
sub cb toggle
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatal(err)
	}
	cb := f.targets[0].(*Checkbox)

	toggled := 0
	dispatcher.On(uint64(cb.ObjectID()), "toggle", func(ev *protocol.Event) {
		if ev.Flag("checked") == protocol.FlagTrue {
			toggled++
		}
	})

	cb.Toggle()
	if toggled != 1 {
		t.Errorf("handler toggled = %d, want 1", toggled)
	}
}

func TestDefaultClosedExceptCommand(t *testing.T) {
	// No sub statements at all: state events are filtered, but a
	// button's action still dispatches and its command event flows.
	commands := core.NewCommandRegistry()
	fired := 0
	commands.Register("go.go", func() { fired++ })

	events := &[]*protocol.Event{}
	ctx := &protocol.BindContext{
		Emit:     func(ev *protocol.Event) { *events = append(*events, ev) },
		Dispatch: func(id string) { commands.Dispatch(id) },
	}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, _ := protocol.Parse(`
cb=new checkbox caption="c"
btn=new button caption="b" action=go.go
`)
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatal(err)
	}

	f.targets[0].(*Checkbox).Toggle()
	f.targets[1].(*Button).Click()

	if fired != 1 {
		t.Errorf("registry fired = %d, want 1", fired)
	}
	if got := eventsOfType(*events, "toggle"); len(got) != 0 {
		t.Errorf("unsubscribed toggle events = %d, want 0", len(got))
	}
	if got := eventsOfType(*events, "command"); len(got) != 1 {
		t.Errorf("command events = %d, want 1 (command always flows)", len(got))
	}
}

func TestSetVerbMutatesWithoutEcho(t *testing.T) {
	session := protocol.NewSession()
	events := &[]*protocol.Event{}
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { *events = append(*events, ev) },
	}
	ctx.Subscribe(0, "") // even wildcard-subscribed: set must not echo
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}

	script, _ := protocol.Parse(`inp=new textinput text="one"`)
	reply, err := session.Execute(script, f)
	if err != nil {
		t.Fatal(err)
	}
	ti := f.targets[0].(*TextInput)

	// Mutate by key in a LATER batch (D19: session-persistent keys).
	setByKey, _ := protocol.Parse(`set inp text="two"`)
	if _, err := session.Execute(setByKey, f); err != nil {
		t.Fatalf("set by key: %v", err)
	}
	if ti.Text() != "two" {
		t.Errorf("after set by key: text = %q", ti.Text())
	}

	// Mutate by numeric ID.
	setByID, _ := protocol.Parse(fmt.Sprintf(`set %d text="three"`, reply.IDs["inp"]))
	if _, err := session.Execute(setByID, f); err != nil {
		t.Fatalf("set by id: %v", err)
	}
	if ti.Text() != "three" {
		t.Errorf("after set by id: text = %q", ti.Text())
	}

	if got := eventsOfType(*events, "change"); len(got) != 0 {
		t.Errorf("set echoed %d change events, want 0 (D20)", len(got))
	}

	// A real user edit still flows.
	ti.SetText("typed")
	if got := eventsOfType(*events, "change"); len(got) != 1 {
		t.Errorf("user change events = %d, want 1", len(got))
	}
}

func TestDestroyVerbRemovesFromParent(t *testing.T) {
	session := protocol.NewSession()
	ctx := &protocol.BindContext{}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}

	script, _ := protocol.Parse(`
root=new panel layout=vbox children={
	a=new label caption="a"
	b=new label caption="b"
}
`)
	if _, err := session.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	panel := f.targets[0].(*Panel)
	if n := len(panel.Children()); n != 2 {
		t.Fatalf("children before destroy = %d, want 2", n)
	}

	destroy, _ := protocol.Parse(`destroy root.a`)
	if _, err := session.Execute(destroy, f); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if n := len(panel.Children()); n != 1 {
		t.Errorf("children after destroy = %d, want 1", n)
	}

	// The key is gone with the object.
	again, _ := protocol.Parse(`set root.a caption="x"`)
	if _, err := session.Execute(again, f); err == nil {
		t.Error("set on destroyed key should fail")
	}
}
