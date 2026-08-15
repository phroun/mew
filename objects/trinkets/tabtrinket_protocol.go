package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for TabTrinket and the virtual `tab` type. A tab
// is a caption plus exactly one content child:
//
//	new tabs position=bottom children={
//	    new tab caption="First" children={new panel layout=vbox children={...}}
//	    new tab caption="Second" children={new label caption="hi"}
//	} selected=0
//
// Note: selected must follow the tabs that make it valid.

// wireTab is the virtual tab target: caption + content trinket.
type wireTab struct {
	caption string
	content core.Trinket
}

func init() {
	protocol.RegisterType("tab", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireTab{} },
		Props: map[string]protocol.Property{
			"caption": protocol.NewProperty("string", wprop("caption", func(_ *protocol.BindContext, t *wireTab, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("caption", v, f)
				if err != nil {
					return err
				}
				t.caption = s
				return nil
			})).Tip("Tab label text."),
		},
		Append: func(parent, child any) error {
			t := parent.(*wireTab)
			w, ok := child.(core.Trinket)
			if !ok {
				return fmt.Errorf("tab: content must be a trinket, got %T", child)
			}
			if t.content != nil {
				return fmt.Errorf("tab: only one content trinket (wrap several in a panel)")
			}
			t.content = w
			return nil
		},
	})

	protocol.RegisterType("tabs", &protocol.TypeSpec{
		New: func() any { return NewTabTrinket() },
		ID: func(t any) uint64 {
			return uint64(t.(*TabTrinket).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			tw := target.(*TabTrinket)
			id := uint64(tw.ObjectID())
			tw.SetOnCurrentChanged(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("change").
					WithUint("trinket", id).WithInt("selected", index))
			})
		},
		Props: map[string]protocol.Property{
			"selected": intProp("selected", (*TabTrinket).SetCurrentIndex).Tip("Active tab index.").Def("0"),
			"movable":  boolProp("movable", (*TabTrinket).SetMovable).Tip("Allow reordering tabs by drag.").Def("false"),
			"closable": boolProp("closable", (*TabTrinket).SetClosable).Tip("Show per-tab close buttons.").Def("false"),
			// background paints the tab body; unlike the common `bg`
			// style override, this drives the color the TabTrinket reports
			// to its children. The word "default" clears it (inherit).
			"background": protocol.NewProperty("color", wprop("background", func(_ *protocol.BindContext, tw *TabTrinket, v *protocol.Value, f protocol.FlagState) error {
				if v != nil && v.Kind == protocol.WordValue && v.Word == "default" {
					tw.SetBackgroundColor(nil)
					tw.Update()
					return nil
				}
				c, err := parseColor("background", v, f)
				if err != nil {
					return err
				}
				tw.SetBackgroundColor(&c)
				tw.Update()
				return nil
			})).Tip("Tab body background color."),
			"position": protocol.NewProperty("enum", wprop("position", func(_ *protocol.BindContext, tw *TabTrinket, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("position", v, f)
				if err != nil {
					return err
				}
				pos, ok := map[string]TabPosition{
					"top":    TabsTop,
					"bottom": TabsBottom,
					"left":   TabsLeft,
					"right":  TabsRight,
				}[w]
				if !ok {
					return fmt.Errorf("position: unknown value %q", w)
				}
				tw.SetTabPosition(pos)
				return nil
			})).OneOf("top", "bottom", "left", "right").Tip("Tab strip edge."),
		},
		Append: func(parent, child any) error {
			tw, ok := parent.(*TabTrinket)
			if !ok {
				return fmt.Errorf("tabs: wrong parent type %T", parent)
			}
			t, ok := child.(*wireTab)
			if !ok {
				return fmt.Errorf("tabs: children must be tab, got %T", child)
			}
			if t.content == nil {
				return fmt.Errorf("tabs: tab %q has no content", t.caption)
			}
			tw.AddTab(t.caption, t.content)
			return nil
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*TabTrinket))
		},
	})
}
