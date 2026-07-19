package trinkets

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

func TestTabsBuildFromProtocol(t *testing.T) {
	f, _ := buildUI(t, nil, `
tw=new tabs position=bottom children={
	new tab caption="One" children={new label caption="first"}
	new tab caption="Two" children={new panel layout=vbox children={new label caption="second"}}
} selected=1
`)
	tw := f.targets[0].(*TabTrinket)
	if tw.Count() != 2 {
		t.Fatalf("tabs = %d, want 2", tw.Count())
	}
	if tw.CurrentIndex() != 1 {
		t.Errorf("selected = %d, want 1", tw.CurrentIndex())
	}
	if tw.TabPosition() != TabsBottom {
		t.Errorf("position = %v, want TabsBottom", tw.TabPosition())
	}
}

func TestListViewBuildAndEvents(t *testing.T) {
	f, events := buildWithEvents(t, nil, `
lv=new listview children={
	new item caption="Alpha"
	new item caption="Beta"
	new item caption="Gamma"
} selected=0
`)
	lv := f.targets[0].(*ListView)
	if lv.Count() != 3 {
		t.Fatalf("items = %d, want 3", lv.Count())
	}

	*events = nil
	lv.SetCurrentIndex(2)
	got := eventsOfType(*events, "change")
	if len(got) != 1 {
		t.Fatalf("change events = %d, want 1", len(got))
	}
	if sel, ok := got[0].Int("selected"); !ok || sel != 2 {
		t.Errorf("selected = %d, want 2", sel)
	}
}

func TestTreeViewBuildsNestedItems(t *testing.T) {
	f, _ := buildUI(t, nil, `
tv=new treeview children={
	new item caption="Fruit" expanded children={
		new item caption="Apple"
		new item caption="Pear" children={new item caption="Bosc"}
	}
	new item caption="Roots"
}
`)
	tv := f.targets[0].(*TreeView)
	roots := tv.RootItems()
	if len(roots) != 2 {
		t.Fatalf("root items = %d, want 2", len(roots))
	}
	fruit := roots[0]
	if fruit.Text != "Fruit" || !fruit.Expanded {
		t.Errorf("fruit = %q expanded=%v", fruit.Text, fruit.Expanded)
	}
	if len(fruit.Children) != 2 {
		t.Fatalf("fruit children = %d, want 2", len(fruit.Children))
	}
	if len(fruit.Children[1].Children) != 1 || fruit.Children[1].Children[0].Text != "Bosc" {
		t.Errorf("nested grandchild missing")
	}
}

func TestTreeItemIdentity(t *testing.T) {
	session := protocol.NewSession()
	events := &[]*protocol.Event{}
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { *events = append(*events, ev) },
	}
	ctx.Subscribe(0, "")
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}

	script, err := protocol.Parse(`
tree=new treeview children={
	fruit=new item caption="Fruit" expanded children={
		apple=new item caption="Apple"
	}
	roots=new item caption="Roots"
}
wfruit=tree.fruit
`)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := session.Execute(script, f)
	if err != nil {
		t.Fatal(err)
	}
	tv := f.targets[0].(*TreeView)
	fruit := tv.RootItems()[0]

	// The live TreeItem carries the surfaced wire ID.
	if uint64(fruit.ID) != reply.IDs["wfruit"] || fruit.ID == 0 {
		t.Fatalf("fruit.ID = %d, surfaced = %d", fruit.ID, reply.IDs["wfruit"])
	}

	// set by key mutates the LIVE tree.
	mutate, _ := protocol.Parse(`set tree.fruit caption="Fruits & Nuts" !expanded`)
	if _, err := session.Execute(mutate, f); err != nil {
		t.Fatalf("set: %v", err)
	}
	if fruit.Text != "Fruits & Nuts" || fruit.Expanded {
		t.Errorf("after set: text=%q expanded=%v", fruit.Text, fruit.Expanded)
	}

	// set children={} appends to the live subtree.
	grow, _ := protocol.Parse(`set tree.fruit children={new item caption="Pear"}`)
	if _, err := session.Execute(grow, f); err != nil {
		t.Fatalf("set children: %v", err)
	}
	if len(fruit.Children) != 2 || fruit.Children[1].Text != "Pear" {
		t.Errorf("after append: children=%d", len(fruit.Children))
	}

	// Selection events carry the item's identity. (fruit is already
	// current - AddRootItem auto-selected it - so move to roots.)
	roots := tv.RootItems()[1]
	*events = nil
	tv.SetCurrentItem(roots)
	got := eventsOfType(*events, "change")
	if len(got) != 1 {
		t.Fatalf("change events = %d, want 1", len(got))
	}
	if id, ok := got[0].Uint("item"); !ok || id != uint64(roots.ID) {
		t.Errorf("event item = %d, want %d", id, roots.ID)
	}

	// The nested key addresses the same identity the event reported
	// (session keys aren't in the reply, but set proves resolution).
	renameRoots, _ := protocol.Parse(`set tree.roots caption="Tubers"`)
	if _, err := session.Execute(renameRoots, f); err != nil {
		t.Fatalf("set tree.roots: %v", err)
	}
	if roots.Text != "Tubers" {
		t.Errorf("roots.Text = %q", roots.Text)
	}

	// destroy removes the node from the live tree and releases keys.
	kill, _ := protocol.Parse(`destroy tree.fruit`)
	if _, err := session.Execute(kill, f); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if n := len(tv.RootItems()); n != 1 {
		t.Errorf("root items after destroy = %d, want 1", n)
	}
	if tv.RootItems()[0] != roots {
		t.Errorf("wrong item survived: %q", tv.RootItems()[0].Text)
	}
	again, _ := protocol.Parse(`set tree.fruit caption="x"`)
	if _, err := session.Execute(again, f); err == nil {
		t.Error("set on destroyed item key should fail")
	}
}

func TestScrollAreaSingleContent(t *testing.T) {
	f, _ := buildUI(t, nil, `
sa=new scrollarea resizable children={new label caption="inside"}
`)
	sa := f.targets[0].(*ScrollArea)
	if sa.Content() == nil {
		t.Fatal("no content set")
	}
	if _, err := protocol.Parse(`x=new scrollarea children={new label caption="a"; new label caption="b"}`); err != nil {
		t.Fatalf("parse: %v", err)
	} else {
		script, _ := protocol.Parse(`x=new scrollarea children={new label caption="a"; new label caption="b"}`)
		f2 := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
		if _, err := protocol.NewSession().Execute(script, f2); err == nil {
			t.Error("second content should error")
		}
	}
}

func TestRadioGroupProperty(t *testing.T) {
	f, _ := buildUI(t, nil, `
new panel layout=vbox children={
	a=new radiobutton caption="A" group=juice checked
	b=new radiobutton caption="B" group=juice
	c=new radiobutton caption="C" group=other checked
}
`)
	a := f.targets[1].(*RadioButton)
	b := f.targets[2].(*RadioButton)
	c := f.targets[3].(*RadioButton)

	b.SetChecked(true)
	if a.IsChecked() {
		t.Error("a should be unchecked after b checked (same group)")
	}
	if !c.IsChecked() {
		t.Error("c is in another group and must be unaffected")
	}
}

func TestWindowBuildFromProtocol(t *testing.T) {
	f, reply := buildUI(t, nil, `
win=new window title="Tools" x=64 y=32 width=400 height=240 no_resize children={
	new label caption="body"
}
`)
	// The window is targets[0] via the capture factory.
	w := f.targets[0].(interface {
		Title() string
		Bounds() core.UnitRect
	})
	if w.Title() != "Tools" {
		t.Errorf("title = %q", w.Title())
	}
	if b := w.Bounds(); b.X != 64 || b.Y != 32 || b.Width != 400 || b.Height != 240 {
		t.Errorf("bounds = %+v", b)
	}
	if reply.IDs["win"] == 0 {
		t.Error("window not surfaced in reply")
	}
}

func TestStretchAndAlignTravelWithChild(t *testing.T) {
	f, _ := buildUI(t, nil, `
new panel layout=hbox children={
	new label caption="fixed"
	new spacer stretch=1
	new button caption="OK" align=right
}
`)
	spacer := f.targets[2].(*Spacer)
	btn := f.targets[3].(*Button)
	if spacer.LayoutStretch() != 1 {
		t.Errorf("spacer stretch = %d, want 1", spacer.LayoutStretch())
	}
	if a, set := btn.LayoutAlignment(); !set || a != core.AlignRight {
		t.Errorf("button align = %v/%v, want AlignRight", a, set)
	}
}

func TestMDIPaneAndDockFromProtocol(t *testing.T) {
	session := protocol.NewSession()
	events := &[]*protocol.Event{}
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { *events = append(*events, ev) },
	}
	ctx.Subscribe(0, "")
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}

	script, _ := protocol.Parse(`
mdi=new mdipane fill="░" children={
	new panel layout=vbox children={new label caption="background"}
}
dock=new dockrow entry_width=20
`)
	if _, err := session.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	pane := f.targets[0].(*MDIPane)
	dock := f.targets[3].(*DockRow)

	// Spawn a document via set-append of a window subtree (D19).
	spawn, _ := protocol.Parse(`
set mdi children={d1=new window title="Doc 1" x=8 y=16 width=240 height=128 children={new label caption="body"}}
wdoc=mdi.d1
`)
	reply, err := session.Execute(spawn, f)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if len(pane.Windows()) != 1 {
		t.Fatalf("pane windows = %d", len(pane.Windows()))
	}
	docID := reply.IDs["wdoc"]

	// Minimize by id-directed action; the pane reports it with title.
	*events = nil
	min, _ := protocol.Parse(fmt.Sprintf("set mdi minimize=%d", docID))
	if _, err := session.Execute(min, f); err != nil {
		t.Fatalf("minimize: %v", err)
	}
	// D20: wire-initiated minimize is suppressed - no echo. Verify
	// the state changed instead.
	if !pane.Windows()[0].IsMinimized() {
		t.Fatal("window not minimized")
	}

	// A USER restore (imperative call = user interaction path) emits.
	pane.RestoreWindow(pane.Windows()[0])
	restores := eventsOfType(*events, "restore")
	if len(restores) != 1 {
		t.Fatalf("restore events = %d", len(restores))
	}
	if id, _ := restores[0].Uint("window"); id != docID {
		t.Errorf("restore window = %d, want %d", id, docID)
	}
	if title, _ := restores[0].Text("title"); title != "Doc 1" {
		t.Errorf("restore title = %q", title)
	}

	// Dock entries: wire objects with click events.
	addEntry, _ := protocol.Parse(fmt.Sprintf(
		`set dock children={e1=new dockentry caption="Doc 1" window=%d}
wentry=dock.e1`, docID))
	entryReply, err := session.Execute(addEntry, f)
	if err != nil {
		t.Fatalf("dock entry: %v", err)
	}
	if dock.EntryCount() != 1 {
		t.Fatalf("dock entries = %d", dock.EntryCount())
	}

	*events = nil
	dock.Entries()[0].OnClick()
	clicks := eventsOfType(*events, "click")
	if len(clicks) != 1 {
		t.Fatalf("click events = %d", len(clicks))
	}
	if id, _ := clicks[0].Trinket(); id != entryReply.IDs["wentry"] {
		t.Errorf("click trinket = %d, want %d", id, entryReply.IDs["wentry"])
	}
	if id, _ := clicks[0].Uint("window"); id != docID {
		t.Errorf("click window = %d, want %d", id, docID)
	}

	// destroy removes the entry from the live dock.
	kill, _ := protocol.Parse(`destroy dock.e1`)
	if _, err := session.Execute(kill, f); err != nil {
		t.Fatalf("destroy entry: %v", err)
	}
	if dock.EntryCount() != 0 {
		t.Errorf("dock entries after destroy = %d", dock.EntryCount())
	}

	// Flag actions parse and run (tile with one window: no crash).
	tile, _ := protocol.Parse(`set mdi tile`)
	if _, err := session.Execute(tile, f); err != nil {
		t.Fatalf("tile: %v", err)
	}
}

func TestMessageBoxFromProtocol(t *testing.T) {
	f, events := buildWithEvents(t, nil, `
dlg=new messagebox title="Confirm" text="Save changes?" yes no cancel icon=question
sub dlg finish
`)
	m := f.targets[0].(*MessageBox)
	if m.Title() != "Confirm" {
		t.Errorf("title = %q", m.Title())
	}
	want := ButtonYes | ButtonNo | ButtonCancel
	if m.Buttons() != want {
		t.Errorf("buttons = %v, want %v", m.Buttons(), want)
	}

	*events = nil
	m.done(ResultYes)
	got := eventsOfType(*events, "finish")
	if len(got) != 1 {
		t.Fatalf("finish events = %d, want 1", len(got))
	}
	if r, ok := got[0].Word("result"); !ok || r != "yes" {
		t.Errorf("result = %q, want yes", r)
	}
}

func TestStatusBarFromProtocol(t *testing.T) {
	f, _ := buildUI(t, nil, `
sb=new statusbar children={
	new section children={
		new span text="Ready - "
		new span text="F10" fg=red bg=white
	}
	new section text="plain" width=20 align=right
}
`)
	bar := f.targets[0].(interface{ Sections() []StatusSection })
	sections := bar.Sections()
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(sections))
	}
	spans := sections[0].Spans
	if len(spans) != 2 || spans[0].Text != "Ready - " {
		t.Fatalf("spans = %+v", spans)
	}
	if spans[0].Style != nil {
		t.Error("unstyled span should have nil style")
	}
	if spans[1].Style == nil || spans[1].Style.Fg != style.ColorRed || spans[1].Style.Bg != style.ColorWhite {
		t.Errorf("styled span = %+v", spans[1].Style)
	}
	if sections[1].Text != "plain" || sections[1].Width != 20 || sections[1].Alignment != 2 {
		t.Errorf("section 2 = %+v", sections[1])
	}
}

func TestTerminalFeedOverWire(t *testing.T) {
	session := protocol.NewSession()
	f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}

	script, _ := protocol.Parse(`term=new terminal`)
	if _, err := session.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.targets[0].(*PurfecTerm); !ok {
		t.Fatalf("target is %T", f.targets[0])
	}

	// Feed with escaped control bytes in a later batch, then verify
	// the text LANDED IN THE DISPLAY buffer (feed is the display
	// direction - bytes parsed as program output, not PTY input).
	feed, _ := protocol.Parse(`set term feed="\e[1;32mwire bytes\e[0m\r\n\x07"`)
	if _, err := session.Execute(feed, f); err != nil {
		t.Fatalf("feed: %v", err)
	}

	term := f.targets[0].(*PurfecTerm)
	cells := term.Terminal().GetCells()
	if len(cells) == 0 {
		t.Fatal("no cells")
	}
	var row0 []rune
	for _, c := range cells[0] {
		row0 = append(row0, c.Char)
	}
	if got := string(row0); !strings.Contains(got, "wire bytes") {
		t.Errorf("display row 0 = %q, want it to contain \"wire bytes\"", got)
	}
}

// font / font_size over the wire pick the terminal's monospace face and
// point size; on a graphical target the cell grid re-derives from them.
func TestTerminalFontOverWire(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	rb, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(rb) // graphical: grid follows the font

	session := protocol.NewSession()
	f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
	script, _ := protocol.Parse(`term=new terminal`)
	if _, err := session.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	term := f.targets[0].(*PurfecTerm)
	term.SetBounds(core.UnitRect{Width: 320, Height: 160})

	smallCW, smallCH := term.cellDims()

	set, _ := protocol.Parse(`set term font_size=24`)
	if _, err := session.Execute(set, f); err != nil {
		t.Fatalf("font_size: %v", err)
	}
	if term.TerminalFont().Size != 24 {
		t.Errorf("font_size not applied: %d", term.TerminalFont().Size)
	}
	bigCW, bigCH := term.cellDims()
	if bigCW <= smallCW || bigCH <= smallCH {
		t.Errorf("font_size=24 cell %dx%d should exceed default %dx%d", bigCW, bigCH, smallCW, smallCH)
	}
}

func TestMenuBarBuildFromProtocol(t *testing.T) {
	commands := core.NewCommandRegistry()
	opened := 0
	commands.Register("file.open", func() { opened++ })

	f, _ := buildUI(t, commands, `
bar=new menubar children={
	new menu caption="&File" children={
		new menuitem caption="&Open..." action=file.open shortcut="^O"
		new menuitem separator
		new menuitem caption="&Recent" children={
			new menuitem caption="a.txt" action=file.recent.0
			new menuitem caption="b.txt" action=file.recent.1
		}
		new menuitem caption="&Locked" !enabled
	}
	new menu caption="&View" children={
		new menuitem caption="&Toolbar" checkable checked
	}
}
`)
	bar := f.targets[0].(interface{ Menus() []*Menu })
	menus := bar.Menus()
	if len(menus) != 2 {
		t.Fatalf("menus = %d, want 2", len(menus))
	}
	items := menus[0].Items()
	if len(items) != 4 {
		t.Fatalf("file items = %d, want 4", len(items))
	}
	open := items[0]
	if open.ID() != "file.open" || open.Shortcut != core.Shortcut("^O") {
		t.Errorf("open: id=%q shortcut=%q", open.ID(), open.Shortcut)
	}
	if !items[1].Separator {
		t.Error("second item should be a separator")
	}
	recent := items[2]
	if recent.SubMenu == nil || len(recent.SubMenu.Items()) != 2 {
		t.Fatalf("submenu missing or wrong size")
	}
	if items[3].Enabled {
		t.Error("locked item should be disabled")
	}
	toolbar := menus[1].Items()[0]
	if !toolbar.Checkable || !toolbar.Checked {
		t.Errorf("toolbar checkable=%v checked=%v", toolbar.Checkable, toolbar.Checked)
	}

	// The slice-1 seam: bind and trigger dispatches by action ID.
	menus[0].BindCommands(commands)
	open.Trigger()
	if opened != 1 {
		t.Errorf("file.open dispatched %d times, want 1", opened)
	}
}

func TestColorProperties(t *testing.T) {
	f, _ := buildUI(t, nil, `
new label caption="tinted" fg=bright_yellow bg="#334455"
`)
	lbl := f.targets[0].(*Label)
	s := lbl.Style()
	if s == nil {
		t.Fatal("no custom style set")
	}
	if s.Fg != style.ColorBrightYellow {
		t.Errorf("fg = %v, want bright yellow", s.Fg)
	}
	if s.Bg != style.RGB(0x33, 0x44, 0x55) {
		t.Errorf("bg = %v, want RGB 334455", s.Bg)
	}
}
