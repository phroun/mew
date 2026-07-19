package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for menus (G6: menus are data trees). A menubar
// collects menus; a menu collects menuitems; an item with menuitem
// children grows a submenu:
//
//	bar=new menubar children={
//	    new menu caption="&File" children={
//	        new menuitem caption="&Open..." action=file.open shortcut="^O"
//	        new menuitem separator
//	        new menuitem caption="&Recent" children={
//	            new menuitem caption="a.txt" action=file.recent.0
//	        }
//	    }
//	}
//
// Activation is the slice-1 seam: action= is the item's command ID;
// binding to the application registry happens when the app installs
// the bar (Application.SetMenuBarContent), and the app registers
// handlers under those IDs. No closures cross the wire.

// wireMenuBar is the virtual menubar target: an ordered menu list.
type wireMenuBar struct {
	menus []*Menu
}

// Menus returns the collected menus (for SetMenuBarContent).
func (b *wireMenuBar) Menus() []*Menu { return b.menus }

func init() {
	protocol.RegisterType("menubar", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireMenuBar{} },
		Append: func(parent, child any) error {
			b := parent.(*wireMenuBar)
			m, ok := child.(*Menu)
			if !ok {
				return fmt.Errorf("menubar: children must be menus, got %T", child)
			}
			b.menus = append(b.menus, m)
			return nil
		},
	})

	protocol.RegisterType("menu", &protocol.TypeSpec{
		New: func() any { return NewMenu("") },
		ID: func(t any) uint64 {
			return uint64(t.(*Menu).ObjectID())
		},
		Props: map[string]protocol.Property{
			"caption": protocol.NewProperty("string", wprop("caption", func(_ *protocol.BindContext, m *Menu, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("caption", v, f)
				if err != nil {
					return err
				}
				m.SetTitle(s)
				return nil
			})).Tip("Menu title (& marks accelerator)"),
			"wellknown": protocol.NewProperty("string", wprop("wellknown", func(_ *protocol.BindContext, m *Menu, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("wellknown", v, f)
				if err != nil {
					return err
				}
				m.SetWellKnownID(s)
				return nil
			})).Tip("System role tag: app/file/edit/format/view/window/help"),
		},
		Append: func(parent, child any) error {
			m := parent.(*Menu)
			it, ok := child.(*MenuItem)
			if !ok {
				return fmt.Errorf("menu: children must be menuitems, got %T", child)
			}
			m.AddItem(it)
			return nil
		},
	})

	protocol.RegisterType("menuitem", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return NewMenuItem("") },
		// Over a connection that consumes events (the display protocol),
		// triggering a menu item emits a command event the app handles -
		// the same seam buttons use through FireAction. In-process menus
		// build with no event sink (Emit is nil) and dispatch through a
		// real command registry instead, so there we stay out of the way
		// and let the app's registered handlers own activation.
		Bind: func(ctx *protocol.BindContext, target any) {
			if ctx == nil || ctx.Emit == nil {
				return
			}
			m := target.(*MenuItem)
			m.SetOnTriggered(func() {
				if id := m.ID(); id != "" {
					ctx.EmitEvent(protocol.NewEvent("command").WithWord("action", id))
				}
			})
		},
		Props: map[string]protocol.Property{
			"caption": protocol.NewProperty("string", wprop("caption", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("caption", v, f)
				if err != nil {
					return err
				}
				m.SetText(s)
				return nil
			})).Tip("Item label (& marks accelerator)"),
			"action": protocol.NewProperty("word", wprop("action", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				id, err := protocol.AsWord("action", v, f)
				if err != nil {
					return err
				}
				m.SetID(id)
				return nil
			})).Tip("Command id dispatched on activation"),
			"shortcut": protocol.NewProperty("string", wprop("shortcut", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("shortcut", v, f)
				if err != nil {
					return err
				}
				m.SetShortcut(core.NewShortcut(s))
				return nil
			})).Tip("Keyboard shortcut (e.g. \"^N\")"),
			"checkable": protocol.NewProperty("flag", wprop("checkable", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("checkable", v, f)
				if err != nil {
					return err
				}
				m.SetCheckable(b)
				return nil
			})).Tip("Item can be checked").Def("false"),
			"checked": protocol.NewProperty("flag", wprop("checked", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("checked", v, f)
				if err != nil {
					return err
				}
				m.SetChecked(b)
				return nil
			})).Tip("Checked state").Def("false"),
			"inplace": protocol.NewProperty("flag", wprop("inplace", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("inplace", v, f)
				if err != nil {
					return err
				}
				m.SetInPlace(b)
				return nil
			})).Tip("Activation acts in place: the menu stays open.").Def("false"),
			"enabled": protocol.NewProperty("flag", wprop("enabled", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("enabled", v, f)
				if err != nil {
					return err
				}
				m.SetEnabled(b)
				return nil
			})).Tip("Item is enabled").Def("true"),
			"separator": protocol.NewProperty("flag", wprop("separator", func(_ *protocol.BindContext, m *MenuItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("separator", v, f)
				if err != nil {
					return err
				}
				m.Separator = b
				return nil
			})).Tip("Render as a separator line").Def("false"),
		},
		Append: func(parent, child any) error {
			it := parent.(*MenuItem)
			c, ok := child.(*MenuItem)
			if !ok {
				return fmt.Errorf("menuitem: submenu children must be menuitems, got %T", child)
			}
			if it.SubMenu == nil {
				it.SetSubMenu(NewMenu(it.Text))
			}
			it.SubMenu.AddItem(c)
			return nil
		},
	})
}
