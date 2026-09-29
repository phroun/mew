package trinkets

// PerformEdit is what the Edit menu's items do and what the display's do verbs
// do, so the two cannot drift: both act on a tree's row editor while one is
// open, and both say why a concealed or empty Cut or Copy did nothing.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// focusedOn builds a desktop with a status bar and a window holding content,
// focused.
func focusedOn(t *testing.T, content core.Trinket) (*Desktop, *StatusBar) {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	sb := NewStatusBar()
	sb.SetText("Ready")
	d.SetStatusBar(sb)
	win := window.NewWindow("host")
	win.SetContent(content)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: 640, Height: 320})
	win.Layout()
	d.WindowManager().ActivateWindow(win)
	content.SetFocus()
	return d, sb
}

// hostOfEditor is a focusable trinket that hands the Edit acts to an editor
// it hosts, the way a tree does while its row editor is open.
type hostOfEditor struct {
	*TextInput
	inner editActor
}

func (h *hostOfEditor) editActorTarget() (editActor, bool) { return h.inner, true }

// Where the focused trinket hands the acts to an editor it hosts, they go to
// that editor and not to the trinket.
func TestAnEditReachesTheHostedEditor(t *testing.T) {
	outer, inner := NewTextInput(), NewTextInput()
	outer.SetText("outer")
	inner.HandleKeyPress(core.KeyPressEvent{Key: "x"})
	host := &hostOfEditor{TextInput: outer, inner: inner}
	outer.Init(host)
	d, _ := focusedOn(t, host)
	if !d.PerformEdit(ItemIDUndo) {
		t.Fatal("undo found nothing to act on")
	}
	if inner.Text() != "" {
		t.Errorf("undo did not reach the hosted editor, which holds %q", inner.Text())
	}
	if outer.Text() != "outer" {
		t.Errorf("undo reached the host, which now holds %q", outer.Text())
	}
}

// A concealed field's Cut and Copy say why they did nothing, and an empty
// selection says there was nothing to cut or copy.
func TestAnEditSaysWhyItDidNothing(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("secret")
	ti.SetEchoMode(EchoPassword)
	d, sb := focusedOn(t, ti)
	ti.SelectAll()
	d.PerformEdit(ItemIDCopy)
	if sb.Text() != concealedNotice {
		t.Errorf("copy on a password field: the bar reads %q, want %q", sb.Text(), concealedNotice)
	}

	plain := NewTextInput()
	plain.SetText("hello")
	d, sb = focusedOn(t, plain)
	for verb, want := range map[string]string{
		ItemIDCut:  "Nothing was selected to cut.",
		ItemIDCopy: "Nothing was selected to copy.",
	} {
		sb.SetText("Ready")
		d.PerformEdit(verb)
		if sb.Text() != want {
			t.Errorf("%s with nothing selected: the bar reads %q, want %q", verb, sb.Text(), want)
		}
	}
	if plain.Text() != "hello" {
		t.Errorf("an empty cut changed the field to %q", plain.Text())
	}
}

// recorder is an edit target that notes which act it was asked for.
type recorder struct{ did []string }

func (r *recorder) Undo()             { r.did = append(r.did, ItemIDUndo) }
func (r *recorder) Redo()             { r.did = append(r.did, ItemIDRedo) }
func (r *recorder) UndoEnabled() bool { return true }
func (r *recorder) RedoEnabled() bool { return true }
func (r *recorder) Cut()              { r.did = append(r.did, ItemIDCut) }
func (r *recorder) Copy()             { r.did = append(r.did, ItemIDCopy) }
func (r *recorder) Paste()            { r.did = append(r.did, ItemIDPaste) }
func (r *recorder) SelectAll()        { r.did = append(r.did, ItemIDSelectAll) }

// Each verb does its own act, and one that is not a verb, or no edit target,
// is reported rather than ignored.
func TestEachEditVerbDoesItsAct(t *testing.T) {
	rec := &recorder{}
	outer := NewTextInput()
	host := &hostOfEditor{TextInput: outer, inner: rec}
	outer.Init(host)
	d, _ := focusedOn(t, host)
	verbs := []string{ItemIDUndo, ItemIDRedo, ItemIDCut, ItemIDCopy, ItemIDPaste, ItemIDSelectAll}
	for _, v := range verbs {
		if !d.PerformEdit(v) {
			t.Errorf("%s reported nothing to act on", v)
		}
	}
	if len(rec.did) != len(verbs) {
		t.Fatalf("the verbs did %v, want %v", rec.did, verbs)
	}
	for i := range verbs {
		if rec.did[i] != verbs[i] {
			t.Fatalf("the verbs did %v, want %v", rec.did, verbs)
		}
	}
	if d.PerformEdit("nonsense") {
		t.Error("an unknown verb reported that it acted")
	}

	nothing, _ := focusedOn(t, NewButton("b"))
	if nothing.PerformEdit(ItemIDUndo) {
		t.Error("undo reported acting with no edit target focused")
	}
}

// Each standard Edit item does its own act when chosen.
func TestEachEditItemDoesItsAct(t *testing.T) {
	rec := &recorder{}
	outer := NewTextInput()
	host := &hostOfEditor{TextInput: outer, inner: rec}
	outer.Init(host)
	d, _ := focusedOn(t, host)
	menu := d.systemEditMenu(nil, nil)
	want := map[string]string{
		core.CmdTrinketUndo: ItemIDUndo, core.CmdTrinketRedo: ItemIDRedo,
		core.CmdTrinketCut: ItemIDCut, core.CmdTrinketCopy: ItemIDCopy,
		core.CmdTrinketPaste: ItemIDPaste, core.CmdTrinketSelectAll: ItemIDSelectAll,
	}
	for command, verb := range want {
		rec.did = nil
		itemNamed(t, menu, command).Trigger()
		if len(rec.did) != 1 || rec.did[0] != verb {
			t.Errorf("the %s item did %v, want %s", command, rec.did, verb)
		}
	}
}
