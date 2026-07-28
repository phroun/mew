package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// captureFactory records constructed targets so tests can reach the
// real trinkets behind protocol objects.
type captureFactory struct {
	inner   protocol.Factory
	targets []any
}

func (f *captureFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	if tg, ok := o.(interface{ Target() any }); ok {
		f.targets = append(f.targets, tg.Target())
	}
	return o, nil
}

// Forward EventControl so sub/unsub and echo suppression reach the
// wrapped RegistryFactory (a wrapper must not hide the capability).
func (f *captureFactory) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *captureFactory) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *captureFactory) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}

func buildUI(t *testing.T, commands *core.CommandRegistry, src string) (*captureFactory, *protocol.Reply) {
	t.Helper()
	ctx := &protocol.BindContext{}
	if commands != nil {
		ctx.Dispatch = func(id string) { commands.Dispatch(id) }
	}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reply, err := protocol.NewSession().Execute(script, f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return f, reply
}

func TestProtocolBuildsRealTrinketTree(t *testing.T) {
	commands := core.NewCommandRegistry()
	fired := 0
	commands.Register("do.it", func() { fired++ })

	f, reply := buildUI(t, commands, `
alias C="caption"
template Wrapped=label wrap
root=new panel layout=vbox border children={
	new Wrapped C="hello world"
	cb=new checkbox C="tri" tristate ?checked
	btn=new button C="Go" action=do.it default
}
grab=root.cb
press=root.btn
`)

	root, ok := f.targets[0].(*Panel)
	if !ok {
		t.Fatalf("root is %T, want *Panel", f.targets[0])
	}
	if len(root.Children()) != 3 {
		t.Fatalf("children = %d, want 3", len(root.Children()))
	}

	lbl, ok := root.Children()[0].(*Label)
	if !ok || lbl.Text() != "hello world" || !lbl.WordWrap() {
		t.Errorf("label = %T text=%q wrap=%v", root.Children()[0], lbl.Text(), lbl.WordWrap())
	}

	cb, ok := root.Children()[1].(*Checkbox)
	if !ok || cb.Text() != "tri" || !cb.IsTriState() || cb.CheckState() != PartiallyChecked {
		t.Errorf("checkbox = %T state=%v tristate=%v", root.Children()[1], cb.CheckState(), cb.IsTriState())
	}

	btn, ok := root.Children()[2].(*Button)
	if !ok || btn.Text() != "Go" {
		t.Fatalf("button = %T text=%q", root.Children()[2], btn.Text())
	}

	// action= wiring: clicking dispatches the command through the
	// registry - the slice-1 seam driven from protocol-built UI.
	btn.Click()
	if fired != 1 {
		t.Errorf("fired = %d, want 1", fired)
	}

	// D15 surfacing resolves to the real trinkets' ObjectIDs.
	if reply.IDs["grab"] != uint64(cb.ObjectID()) {
		t.Errorf("grab = %d, want %d", reply.IDs["grab"], cb.ObjectID())
	}
	if reply.IDs["press"] != uint64(btn.ObjectID()) {
		t.Errorf("press = %d, want %d", reply.IDs["press"], btn.ObjectID())
	}
}

func TestProtocolComboBoxItems(t *testing.T) {
	f, _ := buildUI(t, nil, `
new combobox children={new item caption="Alpha"; new item caption="Beta"} selected=1
`)
	combo := f.targets[0].(*ComboBox)
	if combo.Count() != 2 || combo.ItemText(1) != "Beta" {
		t.Fatalf("combo items = %d, [1]=%q", combo.Count(), combo.ItemText(1))
	}
	if combo.CurrentIndex() != 1 {
		t.Errorf("selected = %d, want 1", combo.CurrentIndex())
	}
}

func TestProtocolSplitterPanes(t *testing.T) {
	f, _ := buildUI(t, nil, `
new splitter orientation=horizontal position=0.3 children={new panel; new panel}
`)
	s := f.targets[0].(*Splitter)
	if s.First() == nil || s.Second() == nil {
		t.Fatal("splitter panes not set")
	}
	if s.Position() != 0.3 {
		t.Errorf("position = %v", s.Position())
	}

	// A third pane is an error.
	ctx := &protocol.BindContext{}
	script, _ := protocol.Parse(`new splitter children={new panel; new panel; new panel}`)
	_, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(ctx))
	if err == nil || !strings.Contains(err.Error(), "two panes") {
		t.Errorf("expected two-panes error, got %v", err)
	}
}

func TestProtocolCommonProperties(t *testing.T) {
	f, _ := buildUI(t, nil, `
new label caption="dense" row_units=32 min_width=80 !enabled
`)
	lbl := f.targets[0].(*Label)
	if lbl.IsEnabled() {
		t.Error("label should be disabled")
	}
	if got := lbl.EffectiveCellMetrics().CellHeight; got != 32 {
		t.Errorf("row_units: CellHeight = %d, want 32", got)
	}
	if got := lbl.MinimumSize().Width; got != 80 {
		t.Errorf("min_width = %d, want 80", got)
	}
}

func TestProtocolUnknownPropertyError(t *testing.T) {
	script, _ := protocol.Parse(`new button bogus=1`)
	_, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected bogus-property error, got %v", err)
	}
}

func TestProtocolActionWithoutDispatcher(t *testing.T) {
	script, _ := protocol.Parse(`new button action=do.it`)
	_, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
	if err == nil || !strings.Contains(err.Error(), "dispatcher") {
		t.Errorf("expected no-dispatcher error, got %v", err)
	}
}
