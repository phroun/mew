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

// wireTab is the virtual tab target: caption + content trinket, and its own
// say over its close button. Once the strip has taken it, strip and tab say
// where it went, so a later set reaches the tab it built.
type wireTab struct {
	caption  string
	content  core.Trinket
	closable Closability
	strip    *TabTrinket
	tab      *Tab
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
			"closable": protocol.NewProperty("flag", wprop("closable", func(_ *protocol.BindContext, t *wireTab, v *protocol.Value, f protocol.FlagState) error {
				c := ClosableDefault
				switch f {
				case protocol.FlagTrue:
					c = ClosableOn
				case protocol.FlagFalse:
					c = ClosableOff
				case protocol.FlagIndeterminate:
				default:
					w, err := protocol.AsWord("closable", v, f)
					if err != nil {
						return err
					}
					switch w {
					case "true":
						c = ClosableOn
					case "false":
						c = ClosableOff
					case "default":
					default:
						return fmt.Errorf("closable: unknown value %q", w)
					}
				}
				t.closable = c
				if t.strip != nil {
					t.strip.setClosableOf(t.tab, c)
				}
				return nil
			})).Tip("This tab's own say over its close button: closable gives it one and !closable " +
				"keeps it without, whatever the strip's closable says. ?closable, or closable=default, " +
				"hands it back to the strip, which is where a tab starts."),
			"children": protocol.NewCollection(func(parent, child any) error {
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
			}).Tip("The one trinket this tab shows."),
		},
	})

	protocol.RegisterType("tabs", &protocol.TypeSpec{
		Events: map[string]protocol.EventDesc{
			"change": protocol.NewEventDesc("A different tab was selected.").
				Field("trinket", "uint", "The tab strip's object ID.").
				Field("selected", "int", "Index of the newly selected tab."),
			"close": protocol.NewEventDesc("A tab's close button was activated: pressed and let go over it, or pressed "+
				"from the keyboard. The tab is still there; taking it away is the application's decision.").
				Field("trinket", "uint", "The tab strip's object ID.").
				Field("index", "int", "Index of the tab whose close button was activated."),
		},
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
			tw.SetOnTabCloseRequested(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("close").
					WithUint("trinket", id).WithInt("index", index))
			})
		},
		Props: map[string]protocol.Property{
			"selected": intProp("selected", (*TabTrinket).SetCurrentIndex).Tip("Active tab index.").Def("0"),
			"movable":  boolProp("movable", (*TabTrinket).SetMovable).Tip("Allow reordering tabs by drag.").Def("false"),
			"closable": boolProp("closable", (*TabTrinket).SetClosable).Tip("Show per-tab close buttons.").Def("false"),
			"close_side": protocol.NewProperty("enum", wprop("close_side", func(_ *protocol.BindContext, tw *TabTrinket, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("close_side", v, f)
				if err != nil {
					return err
				}
				switch w {
				case "leading":
					tw.SetCloseLeading(true)
				case "trailing":
					tw.SetCloseLeading(false)
				default:
					return fmt.Errorf("close_side: unknown value %q", w)
				}
				return nil
			})).OneOf("leading", "trailing").Def("leading").
				Tip("Which end of its label a tab's close button stands at: leading, the end the " +
					"run starts from, or trailing, the end it reaches last."),
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
			"align": protocol.NewProperty("enum", wprop("align", func(_ *protocol.BindContext, tw *TabTrinket, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("align", v, f)
				if err != nil {
					return err
				}
				a, ok := map[string]TabAlign{
					"natural":  TabsAlignNatural,
					"center":   TabsAlignCenter,
					"opposite": TabsAlignOpposite,
				}[w]
				if !ok {
					return fmt.Errorf("align: unknown value %q", w)
				}
				tw.SetTabAlign(a)
				return nil
			})).OneOf("natural", "center", "opposite").Def("natural").
				Tip("Where the tabs sit along a strip with room to spare: packed at the end " +
					"the run starts from, centred, or packed at the far end. A strip that has " +
					"to scroll has no slack to place and ignores this."),
			"position": protocol.NewProperty("enum", wprop("position", func(_ *protocol.BindContext, tw *TabTrinket, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("position", v, f)
				if err != nil {
					return err
				}
				pos, ok := map[string]TabPosition{
					"top":          TabsTop,
					"bottom":       TabsBottom,
					"side":         TabsSide,
					"sideopposite": TabsSideOpposite,
					"opticalleft":  TabsOpticalLeft,
					"opticalright": TabsOpticalRight,
				}[w]
				if !ok {
					return fmt.Errorf("position: unknown value %q", w)
				}
				tw.SetTabPosition(pos)
				return nil
			})).OneOf("top", "bottom", "side", "sideopposite", "opticalleft", "opticalright").
				Tip("Which edge the tab strip stands on. side is the edge the direction reads from " +
					"and sideopposite the far one; the optical pair names a side of the screen outright."),
			"children": protocol.NewCollection(func(parent, child any) error {
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
				i := tw.AddTab(t.caption, t.content)
				t.strip, t.tab = tw, tw.Tab(i)
				tw.SetTabClosable(i, t.closable)
				return nil
			}).Members("tab").Tip("The tabs on the strip, in order."),
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*TabTrinket))
		},
	})
}
