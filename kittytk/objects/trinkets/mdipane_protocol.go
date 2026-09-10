package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for MDIPane. Child windows are ordinary protocol
// windows appended as children (spawning later is `set pane
// children={new window …}` - D19's append); a non-window child
// becomes the pane's background content. Window-management verbs are
// action properties:
//
//	set pane tile            # flag actions
//	set pane restore=1042    # id-directed actions
//
// Events (all carry window= and, where useful, title=): minimize,
// restore, remove, active (window=0 means none). Note: an MDI child's
// window_closed emission is superseded by the pane's remove event
// (the pane owns the close-complete hook of hosted windows).
// Do performs one of the pane's actions (protocol.doer). Arranging windows and
// moving between them are things done, not values the pane then holds, so they
// arrive as `do <mdi> tile` rather than as properties that read like state.
func (m *MDIPane) Do(action string, args []*protocol.Arg) error {
	switch action {
	case "tile":
		m.TileWindows()
		return nil
	case "cascade":
		m.CascadeWindows()
		return nil
	case "next":
		m.NextWindow()
		return nil
	case "prior":
		m.PriorWindow()
		return nil
	case "restore", "minimize", "remove":
		w, err := m.hostedWindow(action, args)
		if err != nil {
			return err
		}
		switch action {
		case "restore":
			m.RestoreWindow(w)
		case "minimize":
			m.MinimizeWindow(w)
		case "remove":
			m.RemoveWindow(w)
		}
		return nil
	}
	return fmt.Errorf("an mdipane does nothing called %q", action)
}

// hostedWindow resolves the window= argument the id-directed actions take.
func (m *MDIPane) hostedWindow(action string, args []*protocol.Arg) (*window.Window, error) {
	for _, a := range args {
		if a.Name != "window" {
			continue
		}
		if a.Value == nil || a.Value.Kind != protocol.NumberValue || !a.Value.IsInt {
			return nil, fmt.Errorf("%s: window= expects an object id", action)
		}
		id := uint64(a.Value.Number)
		w := findMDIWindow(m, id)
		if w == nil {
			return nil, fmt.Errorf("%s: no window %d in this pane", action, id)
		}
		return w, nil
	}
	return nil, fmt.Errorf("%s: expected window=", action)
}

func init() {
	protocol.RegisterType("mdipane", &protocol.TypeSpec{
		// Arranging windows and moving between them are things DONE. There is
		// no "is it tiled" to ask for or to set, so they are actions rather
		// than the flag and int properties they used to be spelled as.
		//
		// "prior", not "prev": the vocabulary pairs next with prior everywhere
		// else it makes the distinction -- item_prior, page_prior, del_prior,
		// sort_mode_prior -- and "prev" appeared here alone, on the wire and on
		// the Go method both. Renamed together so the two never disagree.
		Does: map[string]protocol.DoDesc{
			"tile":    protocol.NewDoDesc("Tile the hosted windows."),
			"cascade": protocol.NewDoDesc("Cascade the hosted windows."),
			"next":    protocol.NewDoDesc("Activate the next window."),
			"prior":   protocol.NewDoDesc("Activate the prior window."),
			"restore": protocol.NewDoDesc("Restore a hosted window.").
				Arg("window", "uint", "The window, by the id it was built under."),
			"minimize": protocol.NewDoDesc("Minimize a hosted window.").
				Arg("window", "uint", "The window, by the id it was built under."),
			"remove": protocol.NewDoDesc("Close a hosted window.").
				Arg("window", "uint", "The window, by the id it was built under."),
		},
		Events: map[string]protocol.EventDesc{
			"active": protocol.NewEventDesc("The active child window changed, including to none.").
				Field("trinket", "uint", "The pane's object ID.").
				Field("window", "uint", "The newly active window, or 0 when none is.").
				Field("title", "string", "The active window's title; absent when none is active."),
			"minimize": protocol.NewEventDesc("A child window was minimized to the dock.").
				Field("trinket", "uint", "The pane's object ID.").
				Field("window", "uint", "The minimized window.").
				Field("title", "string", "The window's title."),
			"restore": protocol.NewEventDesc("A minimized child window was restored.").
				Field("trinket", "uint", "The pane's object ID.").
				Field("window", "uint", "The restored window.").
				Field("title", "string", "The window's title."),
			"remove": protocol.NewEventDesc("A child window left the pane.").
				Field("trinket", "uint", "The pane's object ID.").
				Field("window", "uint", "The window that left.").
				Field("title", "string", "The window's title."),
		},
		New: func() any { return NewMDIPane() },
		ID: func(t any) uint64 {
			return uint64(t.(*MDIPane).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			m := target.(*MDIPane)
			id := uint64(m.ObjectID())
			winEvent := func(evType string, win *window.Window) {
				if win == nil {
					return
				}
				ctx.EmitEvent(protocol.NewEvent(evType).
					WithUint("trinket", id).
					WithUint("window", uint64(win.ObjectID())).
					WithString("title", win.Title()))
			}
			m.SetOnWindowMinimized(func(w *window.Window) { winEvent("minimize", w) })
			m.SetOnWindowRestored(func(w *window.Window) { winEvent("restore", w) })
			m.SetOnWindowRemoved(func(w *window.Window) { winEvent("remove", w) })
			m.SetOnActiveWindowChanged(func(w *window.Window) {
				ev := protocol.NewEvent("active").WithUint("trinket", id)
				if w != nil {
					ev = ev.WithUint("window", uint64(w.ObjectID())).
						WithString("title", w.Title())
				} else {
					ev = ev.WithUint("window", 0)
				}
				ctx.EmitEvent(ev)
			})
		},
		Props: map[string]protocol.Property{
			// background_char, not fill: fill is the common property for
			// which axes an item grows to, and this names a character.
			"background_char": protocol.NewProperty("string", wprop("background_char", func(_ *protocol.BindContext, m *MDIPane, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("background_char", v, f)
				if err != nil {
					return err
				}
				runes := []rune(s)
				if len(runes) != 1 {
					return fmt.Errorf("background_char: expected exactly one character")
				}
				m.SetBackgroundChar(runes[0])
				return nil
			})).Tip("Background fill character"),
			"pattern": boolProp("pattern", (*MDIPane).SetDrawPattern).Tip("Draw a pattern background").Def("false"),
			"children": protocol.NewCollection(func(parent, child any) error {
				m := parent.(*MDIPane)
				switch c := child.(type) {
				case *window.Window:
					m.AddWindow(c)
					return nil
				case core.Trinket:
					if m.Content() != nil {
						return fmt.Errorf("mdipane: background content already set")
					}
					m.SetContent(c)
					return nil
				default:
					return fmt.Errorf("mdipane: children must be windows or a content trinket, got %T", child)
				}
			}).Tip("The windows this pane hosts, and one trinket behind them."),
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*MDIPane))
		},
	})
}

// findMDIWindow locates a hosted window by its wire identity.
func findMDIWindow(m *MDIPane, id uint64) *window.Window {
	for _, w := range m.Windows() {
		if uint64(w.ObjectID()) == id {
			return w
		}
	}
	return nil
}
