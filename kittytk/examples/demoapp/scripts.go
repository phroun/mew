package main

import (
	"fmt"
	"strings"

	"github.com/phroun/kittytk/core"
)

// This file holds the display-protocol scripts that BUILD the demo's
// UI over the socket. They are the backendless mirror of the in-process
// examples/demo: the same windows, tabs, menus and dialogs, expressed
// as protocol text a pure client sends to the display service.

// treeItemsScript is the demo tree, shared by the Lists and Scroll
// Lists tabs (nested children blocks ARE the tree).
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

// indent re-indents a script fragment (whitespace is insignificant to
// the parser; this is only for readability of the composed script).
func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+prefix) + "\n"
}

// mainBuildScript is the whole primary application in one build: the
// KittyTK Demo window (nine tabs), its menu bar and its status bar.
// The display adopts each top-level object - window, menubar, statusbar -
// as the connection's application chrome.
func mainBuildScript() string {
	var b strings.Builder

	b.WriteString(`
w=new window title="KittyTK Demo" width=480 height=288 tearable main children={
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
			new panel border layout=vbox fixed_width=256 children={
				new label caption="The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence" wrap align=fill
			}
			new panel border layout=vbox fixed_width=256 children={
				new label caption="Pack my box with five dozen liquor jugs before the Tuesday checkbox below doubles every letter" wrap align=fill
			}
			new panel border layout=vbox fixed_width=288 children={
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

det=new tab caption="Details" children={
	dbox=new panel layout=vbox spacing=0 children={
		dtree=new treeview caption="Name" showheader sorted sortedby=-1 editable stretch=1 align=fill children={
			dsizec=new column id=size caption="Size" width=10 align=right sortable sortproxy=4
			dkindc=new column id=kind caption="Kind" width=14 sortable editable
			dmodc=new column id=modified caption="Date Modified" width=24 sortable
			dtagsc=new column id=tags caption="Tags" width=8 editable
			drawc=new column id=rawsize caption="Raw Size" width=10 align=right numeric hidden !optional
			ds1=new item caption="Screenshot 2026-07-10 at 1.21.28 AM.png"
			ds2=new item caption="Screenshot 2026-07-10 at 12.24.05 AM.png"
			dpc=new item caption="PC12" expanded children={
				dpcin=new item caption="pc12" expanded children={
					dsrc=new item caption="src" expanded children={
						dmain=new item caption="main.go"
						dutil=new item caption="util.go"
					}
					dbuild=new item caption="build.log"
				}
				dread=new item caption="readme.txt"
			}
			ddocs=new item caption="Documents" expanded children={
				dnotes=new item caption="notes.txt"
				darch=new item caption="archive" expanded children={
					dfin=new item caption="final-report.txt"
					dold=new item caption="old-report.txt"
				}
			}
			darj=new item caption="pc12.arj"
		}
		drow=new panel layout=hbox spacing=8 children={
			dshowkey=new checkbox caption="Name column" checked
			dhscroll=new checkbox caption="H-scroll (fit off)"
			dpinl=new checkbox caption="Pin first 2"
			dpinr=new checkbox caption="Pin last"
			dledger=new checkbox caption="Ledger"
			dlines=new checkbox caption="Tree lines"
		}
	}
}

mtab=new tab caption="MDI Demo" children={
	mdisp=new splitter orientation=vertical position=0.9 caption="Dock" children={
		mdisa=new scrollarea children={
			mdi=new mdipane fill="░" min_width=640 min_height=400 max_width=640 max_height=400 children={
				mdicp=new panel layout=vbox spacing=8 children={
					new label caption="MDIPane Trinket Demo"
					new label caption="This MDIPane trinket manages floating windows.\nClick [_] to minimize windows to the dock below."
					new button caption="Spawn Window in MDIPane" action=demo.mdi.spawn
					new panel layout=hbox spacing=8 children={
						new button caption="Tile" action=demo.mdi.tile
						new button caption="Cascade" action=demo.mdi.cascade
						new button caption="Next" action=demo.mdi.next
						new button caption="Prev" action=demo.mdi.prev
					}
					mdistatus=new label caption="Active: none"
					new spacer
					new label caption="Tips:"
					new label caption="- Click [_] to minimize to dock"
					new label caption="- Click dock entry to restore"
					new label caption="- Double-click title to maximize"
				}
			}
		}
		mdidock=new dockrow entry_width=20
	}
}

}
}

# Surface what the app-side handlers address, then open the event flows
# they listen to (command flows regardless; toggles/changes need a sub).
tabs=w.t
dtree=w.t.det.dbox.dtree
dsizec=w.t.det.dbox.dtree.dsizec
dkindc=w.t.det.dbox.dtree.dkindc
dmodc=w.t.det.dbox.dtree.dmodc
dtagsc=w.t.det.dbox.dtree.dtagsc
drawc=w.t.det.dbox.dtree.drawc
kinds=new collection of=options children={
	new option key=png value="PNG image"
	new option key=folder value="Folder"
	new option key=arj value="ARJ Archive"
	new option key=txt value="Text"
}
ds1=w.t.det.dbox.dtree.ds1
ds2=w.t.det.dbox.dtree.ds2
dpc=w.t.det.dbox.dtree.dpc
dpcin=w.t.det.dbox.dtree.dpc.dpcin
dsrc=w.t.det.dbox.dtree.dpc.dpcin.dsrc
dmain=w.t.det.dbox.dtree.dpc.dpcin.dsrc.dmain
dutil=w.t.det.dbox.dtree.dpc.dpcin.dsrc.dutil
dbuild=w.t.det.dbox.dtree.dpc.dpcin.dbuild
dread=w.t.det.dbox.dtree.dpc.dread
ddocs=w.t.det.dbox.dtree.ddocs
dnotes=w.t.det.dbox.dtree.ddocs.dnotes
darch=w.t.det.dbox.dtree.ddocs.darch
dfin=w.t.det.dbox.dtree.ddocs.darch.dfin
dold=w.t.det.dbox.dtree.ddocs.darch.dold
darj=w.t.det.dbox.dtree.darj
dshowkey=w.t.det.dbox.drow.dshowkey
dhscroll=w.t.det.dbox.drow.dhscroll
dpinl=w.t.det.dbox.drow.dpinl
dpinr=w.t.det.dbox.drow.dpinr
dledger=w.t.det.dbox.drow.dledger
dlines=w.t.det.dbox.drow.dlines
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
mdi=w.t.mtab.mdisp.mdisa.mdi
mdistatus=w.t.mtab.mdisp.mdisa.mdi.mdicp.mdistatus
mdidock=w.t.mtab.mdisp.mdidock
`)

	// The menu bar and status bar are adopted as this application's
	// chrome by the display when the build's targets are taken.
	b.WriteString(mainMenuScript())
	b.WriteString(mainStatusScript)
	return b.String()
}

// mainMenuScript is the primary application's menu bar (Demo, Edit,
// View, Window, Alphabet, Help). action= IDs dispatch back as command
// events the client handles.
func mainMenuScript() string {
	var b strings.Builder
	b.WriteString(`
mb=new menubar children={
	new menu caption="&Demo" wellknown="app" children={
		new menuitem caption="&New" shortcut="^N" action=demo.file.new
		new menuitem caption="&Open..." shortcut="^O"
		new menuitem caption="&Save" shortcut="^S"
	}
	new menu caption="&Edit" wellknown="edit" children={
		new menuitem caption="&Raw Key Input" shortcut="^\\" action=demo.edit.rawkey
	}
	new menu caption="&View" wellknown="view" children={
		new menuitem caption="&Toolbar" checkable checked
		new menuitem caption="&Status Bar" checkable checked
		new menuitem separator
		new menuitem caption="&Light/Dark Theme" shortcut="^T" action=demo.view.theme
		new menuitem separator
		new menuitem caption="Show A&nnouncements in Status Bar" checkable action=demo.view.announce
		new menuitem caption="Speak Announcements" checkable action=demo.view.speak
	}
	new menu caption="&Window" wellknown="window" children={
		new menuitem caption="&New Window" action=demo.window.new
	}
	new menu caption="&Alphabet" children={`)
	for i := 0; i < 26; i++ {
		letter := string(rune('A' + i))
		fmt.Fprintf(&b, "\n\t\tnew menuitem caption=\"&%s - Letter %s\"", letter, letter)
		if i == 2 { // separator after "Letter C" (demo of the thin separator)
			b.WriteString("\n\t\tnew menuitem separator")
		}
	}
	b.WriteString(`
	}
	new menu caption="&Help" wellknown="help" children={
		new menuitem caption="&About" action=demo.help.about
	}
}
`)
	return b.String()
}

// mainStatusScript is the primary application's status bar.
const mainStatusScript = `
sb=new statusbar children={
	new section children={
		new span text="Ready - Press "
		new span text="F10" fg=red bg=white
		new span text=" for menu, Tab to navigate, "
		new span text="Ctrl+Q" fg=red bg=white
		new span text=" to quit"
	}
}
`

// protocolWindowScript is a second window built entirely from protocol
// text, its interactions narrated to its own label.
const protocolWindowScript = `
alias C="caption"
pw=new window title="Protocol Demo" x=64 y=64 width=448 height=256 children={
	root=new panel layout=vbox children={
		new label C="This window's content was built from protocol text." wrap
		pstatus=new label C="Interact below; events appear here."
		new separator
		cb=new checkbox C="Tri-state checkbox (watch the label above)" tristate
		inp=new textinput placeholder="Type here..."
		combo=new combobox children={new item C="Alpha"; new item C="Beta"; new item C="Gamma"} selected=0
		btn=new button C="Dispatch demo.hello" action=demo.hello
	}
}
pstatus=pw.root.pstatus
pcb=pw.root.cb
pinp=pw.root.inp
pcombo=pw.root.combo
`

// demoTerminalScript builds the "Demo Window" (opened from Demo > New):
// a splitter with a control panel over an embedded shell terminal. The
// feed= pseudo-property streams a banner in before the shell starts.
func demoTerminalScript(n int) string {
	return fmt.Sprintf(`
dw%d=new window title="Demo Window" x=%d y=%d width=480 height=320 tearable children={
	dsp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
		dtp=new panel layout=vbox spacing=8 children={
			new label caption="This is a child window."
			new textinput placeholder="Type something..."
			dclose=new button caption="Close"
		}
		dterm=new terminal
	}
}
dwin=dw%d
dcloser=dw%d.dsp.dtp.dclose
dterm=dw%d.dsp.dterm
set dterm feed="\e[1;36mThis banner arrived as protocol text.\e[0m\r\n\r\n"
`, n, 40+n*16, 40+n*16, n, n, n)
}

// aboutDialogScript is the About message box. The name and version come from
// the core package's single source of truth.
var aboutDialogScript = fmt.Sprintf(`
dlg=new messagebox title="About %s" icon=information ok text="%s Demo\n\nA comprehensive cross-surface UI toolkit.\n\nVersion %s"
`, core.Name, core.Name, core.Version)

// secondaryBuildScript is a whole secondary application: a window with a
// control panel over a PurfecTerm, its own menu bar and status bar.
func secondaryBuildScript(n int) string {
	offset := (n - 1) % 5
	x := (offset*3 + 5) * 8
	y := (offset*2 + 3) * 16
	return fmt.Sprintf(`
w=new window title="App %d Window" x=%d y=%d width=480 height=320 tearable main children={
	sp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
		tp=new panel layout=vbox spacing=8 children={
			new label caption="This window belongs to Application #%d"
			new label caption="Notice the menu bar and status bar change\nwhen this window is focused."
			new textinput placeholder="Enter text here..."
			closebtn=new button caption="Close Window"
		}
		term=new terminal
	}
}
closer=w.sp.tp.closebtn
term=w.sp.term
mb=new menubar children={
	new menu caption="&App %d" wellknown="app" children={
		new menuitem caption="&Close Window" shortcut="^W" action=demo.app.close
	}
	new menu caption="&Edit" wellknown="edit" children={
		new menuitem caption="&Raw Key Input" shortcut="^\\" action=demo.app.rawkey
	}
	new menu caption="&Info" children={
		new menuitem caption="&About This App" action=demo.app.info
	}
	new menu caption="&Help" wellknown="help" children={
		new menuitem caption="&About" action=demo.app.about
	}
}
sb=new statusbar children={new section children={new span text="Secondary Application #%d"}}
`, n, x, y, n, n, n)
}

// mdiChildScript spawns one document window inside the MDI pane, wired
// through click events (no per-child command IDs to collide).
func mdiChildScript(n int) string {
	offset := (n - 1) % 5
	return fmt.Sprintf(`
set mdi children={d%d=new window title="Document %d" x=%d y=%d width=240 height=128 children={
	p=new panel layout=vbox spacing=8 children={
		new label caption="Document #%d"
		new textinput placeholder="Enter document content..."
		bp=new panel layout=hbox spacing=8 children={
			nb=new button caption="New"
			cl=new button caption="Close"
		}
	}
}}
wwin=mdi.d%d
wnew=mdi.d%d.p.bp.nb
wclose=mdi.d%d.p.bp.cl
`, n, n, (offset*2+1)*8, (offset+1)*16, n, n, n, n)
}

// detailsValuesScript fills the Details tab's cell values column-major
// (each column owns its data, keyed by item), per the two-batch
// pattern: items build first so their IDs exist, then the values
// reference them. id resolves a surfaced correlation key to its wire ID.
func detailsValuesScript(id func(name string) uint64) string {
	col := func(colKey string, vals map[string]string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "set %s children={\n", colKey)
		for _, item := range []string{
			"ds1", "ds2", "dpc", "dpcin", "dsrc", "dmain", "dutil",
			"dbuild", "dread", "ddocs", "dnotes", "darch", "dfin",
			"dold", "darj",
		} {
			if v, ok := vals[item]; ok {
				fmt.Fprintf(&b, "\tnew cell item=%d value=%q\n", id(item), v)
			}
		}
		b.WriteString("}\n")
		return b.String()
	}
	return col("dsizec", map[string]string{
		"ds1": "311 KB", "ds2": "1 MB", "dpc": "--", "dpcin": "--",
		"dsrc": "--", "dmain": "6 KB", "dutil": "3 KB", "dbuild": "42 KB",
		"dread": "2 KB", "ddocs": "--", "dnotes": "1 KB", "darch": "--",
		"dfin": "88 KB", "dold": "74 KB", "darj": "99 KB",
	}) + col("dkindc", map[string]string{
		"ds1": "PNG image", "ds2": "PNG image", "dpc": "Folder", "dpcin": "Folder",
		"dsrc": "Folder", "dmain": "Text", "dutil": "Text", "dbuild": "Text",
		"dread": "Text", "ddocs": "Folder", "dnotes": "Text", "darch": "Folder",
		"dfin": "Text", "dold": "Text", "darj": "ARJ Archive",
	}) + col("dmodc", map[string]string{
		"ds1": "Yesterday at 1:21 AM", "ds2": "Yesterday at 12:24 AM",
		"dpc": "Yesterday at 12:23 AM", "dpcin": "Yesterday at 12:28 AM",
		"dsrc": "Yesterday at 12:29 AM", "dmain": "Yesterday at 12:30 AM",
		"dutil": "Yesterday at 12:31 AM", "dbuild": "Today at 9:02 AM",
		"dread": "Yesterday at 12:32 AM", "ddocs": "Today at 8:15 AM",
		"dnotes": "Today at 8:16 AM", "darch": "Today at 8:20 AM",
		"dfin": "Today at 8:21 AM", "dold": "Today at 8:22 AM",
		"darj": "Yesterday at 12:17 AM",
	}) + col("dtagsc", map[string]string{}) + col("drawc", map[string]string{
		// The Size column's sort proxy: the same sizes expanded to
		// plain byte counts, so "sort by Size" compares 2048-style
		// numbers while the visible cells keep their "2 KB" captions.
		"ds1": "318464", "ds2": "1048576", "dpc": "--", "dpcin": "--",
		"dsrc": "--", "dmain": "6144", "dutil": "3072", "dbuild": "43008",
		"dread": "2048", "ddocs": "--", "dnotes": "1024", "darch": "--",
		"dfin": "90112", "dold": "75776", "darj": "101376",
	}) + fmt.Sprintf(
		// Kind becomes a CHOICE column: its cell editor is a combo
		// over the kinds collection (the values above are option
		// values, so no magic entry appears).
		"set dkindc enum=%d enum_store=value\n", id("kinds"))
}
