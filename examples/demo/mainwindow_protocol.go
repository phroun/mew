package main

import (
	"fmt"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// The KittyTK Demo window is built from protocol text: the script
// below IS the window. Only the MDI Demo tab stays imperative (the
// MDIPane still has G1 boundary residuals and embeds PurfecTerm),
// attached through the surfaced TabTrinket - the supported hybrid.
//
// The demo also registers its own wire type (fixedbox): trinket-owned
// registration is a public API, so apps can extend the vocabulary
// with their local trinkets.

func init() {
	// fixedbox: a bordered panel with pinned width (word wrap only
	// happens under real width constraint). Demo-local type over the
	// demo-local fixedWidthBox trinket.
	protocol.RegisterType("fixedbox", &protocol.TypeSpec{
		New: func() any {
			f := &fixedWidthBox{Panel: trinkets.NewPanel()}
			f.SetBorder(true)
			f.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
			return f
		},
		ID: func(t any) uint64 {
			return uint64(t.(*fixedWidthBox).ObjectID())
		},
		Props: map[string]protocol.Property{
			"width": protocol.NewProperty("int", func(_ *protocol.BindContext, target any, v *protocol.Value, fl protocol.FlagState) error {
				n, err := protocol.AsInt("width", v, fl)
				if err != nil {
					return err
				}
				target.(*fixedWidthBox).width = core.Unit(n)
				return nil
			}).Tip("Fixed box width in units"),
		},
		Append: func(parent, child any) error {
			w, ok := child.(core.Trinket)
			if !ok {
				return fmt.Errorf("fixedbox: children must be trinkets, got %T", child)
			}
			parent.(*fixedWidthBox).AddChild(w)
			return nil
		},
	})
}

// treeItemsScript is the demo tree, shared by the Lists and Scroll
// Lists tabs (D13: nested children blocks ARE the tree).
const treeItemsScript = `
new item caption="Documents" expanded children={
	new item caption="Work" expanded children={
		new item caption="Report.txt"
		new item caption="Presentation.pptx"
		new item caption="Budget.xlsx"
		new item caption="Meeting Notes.md"
	}
	new item caption="Personal" children={
		new item caption="Notes.txt"
		new item caption="Journal.md"
		new item caption="Ideas.txt"
	}
	new item caption="Projects" children={
		new item caption="Alpha"
		new item caption="Beta"
		new item caption="Gamma"
	}
}
new item caption="Pictures" children={
	new item caption="Vacation"
	new item caption="Family"
	new item caption="Pets"
	new item caption="Events"
	new item caption="Screenshots"
}
new item caption="Downloads" children={
	new item caption="Software"
	new item caption="Documents"
	new item caption="Music"
}
new item caption="Music" children={
	new item caption="Rock"
	new item caption="Jazz"
	new item caption="Classical"
	new item caption="Electronic"
}
new item caption="Videos" children={
	new item caption="Movies"
	new item caption="TV Shows"
	new item caption="Tutorials"
}
new item caption="Code" children={
	new item caption="Go" children={
		new item caption="main.go"
		new item caption="utils.go"
	}
	new item caption="Python" children={
		new item caption="script.py"
	}
}
`

// mainWindowScript assembles the whole demo window. Repetitive runs
// (numbered items, alphabet combo, filler tabs) are generated - it is
// still protocol text, just not hand-typed.
func mainWindowScript() string {
	var b strings.Builder

	b.WriteString(`
w=new window title="KittyTK Demo" width=480 height=288 children={
t=new tabs children={

b=new tab caption="Basic Trinkets" children={
	bw=new panel layout=vbox spacing=0 children={
		new label caption="This is a demo of basic trinkets:"
		brow=new panel layout=hbox spacing=8 children={
			input=new textinput placeholder="Enter text here..." stretch=1
			new button caption="Browse..."
		}
		new spacer
		new panel layout=hbox spacing=8 children={
			new button caption="OK" action=demo.basic.ok
			new button caption="Cancel" action=demo.basic.cancel
			new button caption="Apply" action=demo.basic.apply
		}
		new button caption="Disabled" !enabled
	}
}

s=new tab caption="Selection" children={
	o=new panel layout=vbox spacing=0 children={
		new panel layout=hbox spacing=8 align=fill children={
			new fixedbox width=256 children={
				new label caption="The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence" wrap align=fill
			}
			new fixedbox width=256 children={
				new label caption="Pack my box with five dozen liquor jugs before the Tuesday checkbox below doubles every letter" wrap align=fill
			}
			new fixedbox width=288 children={
				new panel layout=vbox align=fill children={
					new checkbox caption="Enable the experimental feature that reticulates splines while the moon is full" wrap
					new radiobutton caption="Prefer the long-form explanation whenever the assistant answers a question" wrap
				}
			}
		}
		sp=new splitter orientation=vertical position=0.4 stretch=1 align=fill children={
			c=new panel layout=vbox spacing=0 children={
				new label caption="Checkboxes:"
				new checkbox caption="Enable feature A" checked
				new checkbox caption="Enable feature B"
				new checkbox caption="Tri-state checkbox" tristate
				new label caption="Font Options:"
				wfont=new checkbox caption="Window: Tuesday (double-width)"
				dfont=new checkbox caption="Desktop: Tuesday (double-width)"
				grid=new checkbox caption="Window: 32-unit rows (denomination test)"
			}
			r=new panel layout=vbox spacing=0 children={
				new label caption="Radio buttons:"
				new radiobutton caption="Option 1" group=selopts
				new radiobutton caption="Option 2" group=selopts
				new radiobutton caption="Option 3" group=selopts
				new label caption="Tab Background Color:"
				bgdef=new radiobutton caption="Default" group=selbg checked
				bggreen=new radiobutton caption="Dark Green" group=selbg
				bggray=new radiobutton caption="TrueColor #333" group=selbg
				new label caption="ComboBox:"
				new combobox children={
					new item caption="First item"
					new item caption="Second item"
					new item caption="Third item"
					new item caption="Fourth item"
				}
				new label caption="Alphabet ComboBox:"
				new combobox children={`)
	for i := 0; i < 26; i++ {
		letter := string(rune('A' + i))
		fmt.Fprintf(&b, "\n\t\t\t\t\tnew item caption=%q", letter+" - Letter "+letter)
	}
	b.WriteString(`
				}
			}
		}
	}
}

new tab caption="Lists" children={
	new splitter orientation=horizontal position=0.5 children={
		new panel layout=vbox children={
			new label caption="ListView:"
			new listview children={`)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "\n\t\t\t\tnew item caption=\"Item %d\"", i)
	}
	b.WriteString(`
			}
		}
		new panel layout=vbox children={
			new label caption="TreeView:"
			new treeview children={` + indent(treeItemsScript, "\t\t\t\t") + `}
		}
	}
}

ss=new tab caption="Scroll Selection" children={
	sp=new splitter orientation=vertical position=0.4 children={
		new scrollarea children={
			new panel layout=vbox spacing=0 children={
				new label caption="Checkboxes (scrollable):"`)
	for i := 1; i <= 15; i++ {
		checked := ""
		if i%3 == 0 {
			checked = " checked"
		}
		fmt.Fprintf(&b, "\n\t\t\t\tnew checkbox caption=\"Feature option %d\"%s", i, checked)
	}
	b.WriteString(`
			}
		}
		sa=new scrollarea children={
			sr=new panel layout=vbox spacing=0 children={
				new label caption="Radio buttons (scrollable):"`)
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "\n\t\t\t\tnew radiobutton caption=\"Radio option %d with longer text\" group=scrollopts", i)
	}
	b.WriteString(`
				new label caption="Tab Background Color:"
				sbgdef=new radiobutton caption="Default" group=scrollbg checked
				sbggreen=new radiobutton caption="Dark Green" group=scrollbg
				sbggray=new radiobutton caption="TrueColor #333" group=scrollbg
				new label caption="ComboBox:"
				new combobox children={
					new item caption="First item"
					new item caption="Second item"
					new item caption="Third item"
					new item caption="Fourth item"
				}
			}
		}
	}
}

new tab caption="Scroll Lists" children={
	new splitter orientation=horizontal position=0.5 children={
		new scrollarea children={
			new panel layout=vbox children={
				new label caption="ListView (scrollable container):"
				new listview children={`)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "\n\t\t\t\t\tnew item caption=\"Item %d\"", i)
	}
	b.WriteString(`
				}
				new label caption="Extra content below ListView:"`)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "\n\t\t\t\tnew button caption=\"Button %d\"", i)
	}
	b.WriteString(`
			}
		}
		new scrollarea children={
			new panel layout=vbox children={
				new label caption="TreeView (scrollable container):"
				new treeview children={` + indent(treeItemsScript, "\t\t\t\t\t") + `}
				new label caption="Extra content below TreeView:"
				new textinput placeholder="Type something..."
			}
		}
	}
}

new tab caption="Progress" children={
	new panel layout=vbox spacing=16 children={
		new label caption="Horizontal Progress Bars:"
		new progress value=25
		new progress value=50
		new progress value=75
		new progress value=100
		new label caption="Indeterminate Progress:"
		new progress indeterminate
	}
}

new tab caption="Bottom Tabs" children={
	new tabs position=bottom children={
		new tab caption="First" children={
			new panel layout=vbox children={
				new label caption="This TabTrinket has tabs at the bottom."
				new label caption="Notice how the tab connectors are inverted:"
				new label caption="  Top tabs use: _/ and \\_"
				new label caption="  Bottom tabs use: \\_ and _/"
			}
		}
		new tab caption="Second" children={
			new panel layout=vbox children={
				new label caption="Second tab content"
				new button caption="Click me"
			}
		}
		new tab caption="Third" children={
			new panel layout=vbox children={
				new label caption="Third tab with an input field:"
				new textinput placeholder="Type here..."
			}
		}
	}
}

new tab caption="Vertical Tabs" children={
	new splitter orientation=horizontal position=0.5 children={
		new tabs position=left children={
			new tab caption="First" children={
				new panel layout=vbox children={
					new label caption="This is the first tab in a\nTabsLeft layout."
					new label caption="Tabs are displayed vertically\nalong the left edge."
				}
			}
			new tab caption="Second" children={
				new panel layout=vbox children={
					new label caption="Second tab content"
					new button caption="A Button"
				}
			}
			new tab caption="Third" children={
				new panel layout=vbox children={
					new textinput placeholder="Type here..."
				}
			}`)
	for _, name := range []string{"Fourth", "Fifth", "Sixth", "Seventh", "Eighth", "Ninth", "Tenth", "Eleventh", "Twelfth", "Thirteenth"} {
		fmt.Fprintf(&b, `
			new tab caption=%q children={
				new panel layout=vbox children={
					new label caption="%s tab content\nin TabsLeft layout."
				}
			}`, name, name)
	}
	b.WriteString(`
		}
		new tabs position=right children={
			new tab caption="Alpha" children={
				new panel layout=vbox children={
					new label caption="This is the first tab in a\nTabsRight layout."
					new label caption="Tabs are displayed vertically\nalong the right edge."
				}
			}
			new tab caption="Beta" children={
				new panel layout=vbox children={
					new label caption="Beta tab content"
					new checkbox caption="Enable option"
				}
			}
			new tab caption="Gamma" children={
				new panel layout=vbox children={
					new label caption="Gamma tab content"
				}
			}`)
	for _, name := range []string{"Delta", "Epsilon", "Zeta", "Eta", "Theta", "Iota", "Kappa", "Lambda", "Mu", "Nu"} {
		fmt.Fprintf(&b, `
			new tab caption=%q children={
				new panel layout=vbox children={
					new label caption="%s tab content\nin TabsRight layout."
				}
			}`, name, name)
	}
	b.WriteString(`
		}
	}
}

}
}

# Surface what the app-side handlers address, then open the event
# flows they listen to (D20 default-closed; command flows regardless).
tabs=w.t
binput=w.t.b.bw.brow.input
wfont=w.t.s.o.sp.c.wfont
dfont=w.t.s.o.sp.c.dfont
grid=w.t.s.o.sp.c.grid
bgdef=w.t.s.o.sp.r.bgdef
bggreen=w.t.s.o.sp.r.bggreen
bggray=w.t.s.o.sp.r.bggray
sbgdef=w.t.ss.sp.sa.sr.sbgdef
sbggreen=w.t.ss.sp.sa.sr.sbggreen
sbggray=w.t.ss.sp.sa.sr.sbggray

sub binput change
sub wfont toggle
sub dfont toggle
sub grid toggle
sub bgdef toggle
sub bggreen toggle
sub bggray toggle
sub sbgdef toggle
sub sbggreen toggle
sub sbggray toggle
`)
	return b.String()
}

// indent re-indents a script fragment for readability of the composed
// script (whitespace is insignificant to the parser).
func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+prefix) + "\n"
}

// createMainWindow builds the KittyTK Demo window by executing
// the protocol script, then wires app-side behavior: commands into
// the registry, event handlers by surfaced ObjectID, and the one
// imperative tab (MDI).
func createMainWindow(desktop *trinkets.Desktop, application *app.Application) *window.Window {
	dispatcher := protocol.NewEventDispatcher()
	ctx := &protocol.BindContext{
		Dispatch: func(id string) { application.Commands().Dispatch(id) },
		Emit:     func(ev *protocol.Event) { dispatcher.Dispatch(ev) },
	}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}

	script, err := protocol.Parse(mainWindowScript())
	if err != nil {
		panic(fmt.Sprintf("main window script: %v", err))
	}
	reply, err := protocol.NewSession().Execute(script, factory)
	if err != nil {
		panic(fmt.Sprintf("main window script: %v", err))
	}

	mainWindow := factory.byID[reply.IDs["w"]].(*window.Window)
	mainWindow.SetTearable(true) // main window shows the %/# tear handle
	tabTrinket := factory.byID[reply.IDs["tabs"]].(*trinkets.TabTrinket)

	// Basic Trinkets: buttons dispatch commands (slice-1 seam); the
	// text input's change events narrate to the status bar.
	setStatus := func(text string) {
		if sb := desktop.StatusBar(); sb != nil {
			sb.SetText(text)
		}
	}
	application.Commands().Register("demo.basic.ok", func() { setStatus("OK button clicked!") })
	application.Commands().Register("demo.basic.cancel", func() { setStatus("Cancel button clicked!") })
	application.Commands().Register("demo.basic.apply", func() { setStatus("Apply button clicked!") })
	dispatcher.On(reply.IDs["binput"], "change", func(ev *protocol.Event) {
		if s, ok := ev.Text("text"); ok {
			setStatus(fmt.Sprintf("Text: %s", s))
		}
	})

	// Selection: font / denomination toggles.
	onToggle := func(key string, fn func(checked bool)) {
		dispatcher.On(reply.IDs[key], "toggle", func(ev *protocol.Event) {
			fn(ev.Flag("checked") == protocol.FlagTrue)
		})
	}
	onToggle("wfont", func(checked bool) {
		if checked {
			mainWindow.SetFont(core.FontTuesday12)
		} else {
			mainWindow.SetFont(nil) // Inherit from desktop
		}
	})
	onToggle("dfont", func(checked bool) {
		if checked {
			desktop.SetFont(core.FontTuesday12)
		} else {
			desktop.SetFont(nil) // Use default (Monday)
		}
	})
	onToggle("grid", func(checked bool) {
		if checked {
			mainWindow.SetCellMetrics(&core.CellMetrics{CellWidth: 8, CellHeight: 32})
		} else {
			mainWindow.SetCellMetrics(nil) // Inherit from desktop
		}
	})

	// Tab background color radios (Selection and Scroll Selection
	// carry the same three options).
	setTabBG := func(c *style.Color) {
		tabTrinket.SetBackgroundColor(c)
		tabTrinket.Update()
	}
	bgHandler := func(c func() *style.Color) func(bool) {
		return func(checked bool) {
			if checked {
				setTabBG(c())
			}
		}
	}
	defaultBG := bgHandler(func() *style.Color { return nil })
	greenBG := bgHandler(func() *style.Color { g := style.ColorGreen; return &g })
	grayBG := bgHandler(func() *style.Color { g := style.RGB(0x33, 0x33, 0x33); return &g })
	onToggle("bgdef", defaultBG)
	onToggle("bggreen", greenBG)
	onToggle("bggray", grayBG)
	onToggle("sbgdef", defaultBG)
	onToggle("sbggreen", greenBG)
	onToggle("sbggray", grayBG)

	// The one imperative tab: MDIPane (G1 residuals; embeds
	// PurfecTerm). Hybrid by design - the surfaced TabTrinket is real.
	tabTrinket.AddTab("MDI Demo", createMDIDemo(desktop, application, mainWindow))

	return mainWindow
}
