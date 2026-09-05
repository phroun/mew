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
// KittyTK Demo window and its tab gallery, its menu bar and its status bar.
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
		new panel layout=hbox spacing=8 children={
			new panel border layout=vbox fixed_width=256 children={
				new label caption="The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence" wrap
			}
			new panel border layout=vbox fixed_width=256 children={
				new label caption="Pack my box with five dozen liquor jugs before the Tuesday checkbox below doubles every letter" wrap
			}
			new panel border layout=vbox fixed_width=288 children={
				new panel layout=vbox children={
					new checkbox caption="Enable the experimental feature that reticulates splines while the moon is full" wrap
					new radiobutton caption="Prefer the long-form explanation whenever the assistant answers a question" wrap
				}
			}
		}
		sp=new splitter orientation=vertical position=0.4 stretch=1 children={
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
			new listview min_width=160 min_height=160 children={`)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "\n\t\t\t\tnew item caption=\"Item %d\"", i)
	}
	b.WriteString(`
			}
		}
		new panel layout=vbox children={
			new label caption="TreeView:"
			new treeview min_width=160 min_height=160 children={` + indent(treeItemsScript, "\t\t\t\t") + `}
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
				new listview min_width=160 min_height=160 children={`)
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
				new treeview min_width=160 min_height=160 children={` + indent(treeItemsScript, "\t\t\t\t\t") + `}
				new label caption="Extra content below TreeView:"
				new textinput min_width=160 placeholder="Type something..."
			}
		}
	}
}

tf=new tab caption="Text Fields" children={
	tfp=new panel layout=vbox spacing=0 children={
		new label caption="Every option a textinput has, one per row."
		tfrow=new panel layout=hbox spacing=8 children={
			tfl=new panel layout=vbox spacing=0 stretch=1 children={
				new label caption="Plain, with a placeholder:"
				new textinput min_width=160 placeholder="Type here..."

				new label caption="Pre-filled, from text=:"
				new textinput min_width=160 text="already typed"

				new label caption="readonly - selectable, not editable:"
				new textinput min_width=160 text="you can copy this but not change it" readonly

				new label caption="!enabled - not reachable at all:"
				new textinput min_width=160 text="disabled" !enabled

				new label caption="max_length=8 - stops accepting at 8:"
				new textinput min_width=160 placeholder="8 max" max_length=8
			}
			tfr=new panel layout=vbox spacing=0 stretch=1 children={
				new label caption="echo=password - masked with the default bullet:"
				new textinput min_width=160 text="hunter2" echo=password

				new label caption="echo=password mask=\"*\" - masked with a star:"
				new textinput min_width=160 text="hunter2" echo=password mask="*"

				new label caption="echo=none - accepts typing, shows nothing:"
				new textinput min_width=160 text="invisible" echo=none

				new label caption="Live: change fires per edit, complete on Return."
				tfwatch=new textinput min_width=160 placeholder="Type, then press Return..."
				tfecho=new label caption="(nothing yet)"

				new label caption="Masked, and you choose the mask:"
				tfmask=new textinput min_width=160 text="secret" echo=password
				tfmrow=new panel layout=hbox spacing=8 children={
					tfmb=new radiobutton caption="bullet" group=tfmaskg checked
					tfms=new radiobutton caption="star" group=tfmaskg
					tfmh=new radiobutton caption="hash" group=tfmaskg
					tfmn=new radiobutton caption="show" group=tfmaskg
				}
			}
		}
	}
}

dn=new tab caption="Denomination" children={
	dnp=new panel layout=vbox spacing=8 children={
		new label caption="Denomination is how many units make one character cell -- 8 wide by 16 tall by default. Setting it on this window re-expresses every unit inside it, so the whole page below changes size while the window does not."
		new label caption="column_units is the X axis, row_units the Y. They are independent: a tall thin cell is 8 by 32, a squat one 16 by 8."
		dnrow=new panel layout=hbox spacing=8 children={
			new label caption="X:"
			dnx=new textinput min_width=160 text="8" max_length=2
			new label caption="Y:"
			dny=new textinput min_width=160 text="16" max_length=2
			dnap=new button caption="Apply" default
		}
		new label caption="Type in either field and press Return, or use Apply. Presets:"
		dnpre=new panel layout=hbox spacing=8 children={
			dnd=new button caption="8 x 16 (default)"
			dnh=new button caption="4 x 8 (half)"
			dnt=new button caption="16 x 32 (double)"
			dns=new button caption="16 x 16 (square)"
			dnn=new button caption="8 x 32 (narrow)"
		}
		dnecho=new label caption="Currently 8 x 16 (inherited -- no override set)."
		new spacer
	}
}

new tab caption="Grid" children={
	new panel layout=vbox spacing=8 children={
		new label caption="A form: labels in a column that sizes to them, fields in one that takes the rest."
		new panel border layout=grid spacing=8 children={
			new label caption="Name:" row=0 column=0 halign=textend fill=none
			new textinput row=0 column=1 column_stretch=1 placeholder="Ada Lovelace"
			new label caption="Address:" row=1 column=0 halign=textend fill=none
			new textinput row=1 column=1 column_stretch=1 placeholder="12 Marylebone Road"
			new label caption="Notes:" row=2 column=0 halign=textend valign=top fill=none
			new textinput row=2 column=1 column_stretch=1 placeholder="anything at all"
			new panel layout=hbox spacing=8 row=3 column=0 column_span=2 halign=textend fill=none children={
				new button caption="Save" action=demo.grid.save
				new button caption="Cancel" action=demo.grid.cancel
			}
		}
		new label caption="A span: the button row above covers both columns and sits at the trailing edge."
		new panel border layout=grid spacing=4 children={
			new button caption="1" row=0 column=0 column_stretch=1
			new button caption="2" row=0 column=1 column_stretch=1
			new button caption="3" row=0 column=2 column_stretch=1
			new button caption="tall" row=0 column=3 row_span=2 column_stretch=1 fill=both
			new button caption="4" row=1 column=0 column_stretch=1
			new button caption="wide" row=1 column=1 column_span=2 column_stretch=1
		}
		new spacer
	}
}

new tab caption="Flex" children={
	new panel layout=vbox spacing=8 children={
		new label caption="Wrapping: eight fixed cards in a run that breaks when it runs out of room."
		new panel border layout=flex flex_wrap=wrap spacing=4 align_items=begin children={
			new button caption="Alpha"
			new button caption="Bravo"
			new button caption="Charlie"
			new button caption="Delta"
			new button caption="Echo"
			new button caption="Foxtrot"
			new button caption="Golf"
			new button caption="Hotel"
		}
		new label caption="Justify: the same three buttons, with the leftover space spread between them."
		new panel border layout=flex justify=space_between spacing=0 align_items=center children={
			new button caption="Left"
			new button caption="Middle"
			new button caption="Right"
		}
		new label caption="Grow: one part to three, so the second field takes three times the leftover."
		new panel border layout=flex spacing=8 align_items=center children={
			new textinput grow=1 placeholder="one part"
			new textinput grow=3 placeholder="three parts"
		}
		new spacer
	}
}

new tab caption="Progress" children={
	new panel layout=vbox spacing=16 children={
		new label caption="Horizontal Progress Bars:"
		new progress min_width=160 value=25
		new progress min_width=160 value=50
		new progress min_width=160 value=75
		new progress min_width=160 value=100
		new label caption="Indeterminate Progress:"
		new progress min_width=160 indeterminate
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
				new textinput min_width=160 placeholder="Type here..."
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
					new textinput min_width=160 placeholder="Type here..."
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
		dtree=new treeview caption="Name" showheader sorted sortedby=-1 editable stretch=1 children={
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
			mdi=new mdipane background_char="░" min_width=640 min_height=400 max_width=640 max_height=400 children={
				mdicp=new panel layout=vbox spacing=8 children={
					new label caption="MDIPane Trinket Demo"
					new label caption="This MDIPane trinket manages floating windows.\nClick [_] to minimize windows to the dock below."
					new button caption="Spawn Window in MDIPane" action=demo.mdi.spawn
					new panel layout=hbox spacing=8 children={
						new button caption="Tile" action=demo.mdi.tile
						new button caption="Cascade" action=demo.mdi.cascade
						new button caption="Next" action=demo.mdi.next
						new button caption="Prior" action=demo.mdi.prior
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

mt=new tab caption="Terminal" children={
	mtsp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
		mtp=new panel layout=vbox spacing=8 children={
			new label caption="A terminal surface in a tab, the same trinket the Demo Window and each secondary application embed." wrap
			new label caption="Keystrokes leave as input events and the application writes them to a PTY it owns; the child's output comes back through feed=. The shell starts the first time this tab is shown." wrap
			mtrow=new panel layout=hbox spacing=8 children={
				mtclear=new button caption="Clear"
				new spacer
			}
		}
		mterm=new terminal
	}
}

df=new tab caption="Defaults" children={
	dfsa=new scrollarea stretch=1 children={
		dfv=new panel layout=vbox spacing=0 children={
			new label halign=textbegin fill=none caption="One of each trinket, with nothing setting a size on any of them.\nWhat you see is what each one asks for when nobody tells it.\n\nA trinket that can size itself from its own content does: a button\nfrom its caption, a label from its text, a combo box from its items.\nOne that cannot falls back to three cells, which is meant to look\nwrong -- it is how a trinket says nobody gave it a size."

			new label halign=textbegin fill=none caption="--- button"
			new button halign=textbegin fill=none caption="Button"
			new label halign=textbegin fill=none caption="--- checkbox"
			new checkbox halign=textbegin fill=none caption="Checkbox"
			new label halign=textbegin fill=none caption="--- radiobutton"
			new radiobutton halign=textbegin fill=none caption="Radio button" group=dfgroup
			new label halign=textbegin fill=none caption="--- label"
			new label halign=textbegin fill=none caption="Label"
			new label halign=textbegin fill=none caption="--- textinput"
			new textinput halign=textbegin fill=none placeholder="Text input"
			new label halign=textbegin fill=none caption="--- combobox"
			new combobox halign=textbegin fill=none children={
				new item caption="Combo item one"
				new item caption="Combo item two"
			}
			new label halign=textbegin fill=none caption="--- progress"
			new progress halign=textbegin fill=none value=60
			new label halign=textbegin fill=none caption="--- separator"
			new separator halign=textbegin fill=none caption="Separator"
			new label halign=textbegin fill=none caption="--- spacer"
			new spacer halign=textbegin fill=none
			new label halign=textbegin fill=none caption="--- listview"
			new listview halign=textbegin fill=none children={
				new item caption="List item one"
				new item caption="List item two"
				new item caption="List item three"
			}
			new label halign=textbegin fill=none caption="--- treeview"
			new treeview halign=textbegin fill=none children={
				new item caption="Tree item" children={
					new item caption="Tree child"
				}
			}
			new label halign=textbegin fill=none caption="--- panel (no layout, bordered)"
			new panel halign=textbegin fill=none border
			new label halign=textbegin fill=none caption="--- scrollarea"
			new scrollarea halign=textbegin fill=none children={
				new label caption="Scroll area content"
			}
			new label halign=textbegin fill=none caption="--- splitter"
			new splitter halign=textbegin fill=none orientation=vertical children={
				new label caption="Splitter first"
				new label caption="Splitter second"
			}
			new label halign=textbegin fill=none caption="--- tabs"
			new tabs halign=textbegin fill=none children={
				new tab caption="One" children={new label caption="Tab one"}
				new tab caption="Two" children={new label caption="Tab two"}
			}
			new label halign=textbegin fill=none caption="--- editor"
			new editor halign=textbegin fill=none
			new label halign=textbegin fill=none caption="--- terminal"
			new terminal halign=textbegin fill=none
			new label halign=textbegin fill=none caption="--- dockrow"
			new dockrow halign=textbegin fill=none
			new label halign=textbegin fill=none caption="--- mdipane"
			new mdipane halign=textbegin fill=none
		}
	}
}

}
}

# Surface what the app-side handlers address, then open the event flows
# they listen to (command flows regardless; toggles/changes need a sub).
tabs=w.t
mterm=w.t.mt.mtsp.mterm
mtclear=w.t.mt.mtsp.mtp.mtrow.mtclear
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
tfwatch=w.t.tf.tfp.tfrow.tfr.tfwatch
tfecho=w.t.tf.tfp.tfrow.tfr.tfecho
tfmask=w.t.tf.tfp.tfrow.tfr.tfmask
tfmb=w.t.tf.tfp.tfrow.tfr.tfmrow.tfmb
tfms=w.t.tf.tfp.tfrow.tfr.tfmrow.tfms
tfmh=w.t.tf.tfp.tfrow.tfr.tfmrow.tfmh
tfmn=w.t.tf.tfp.tfrow.tfr.tfmrow.tfmn
dnx=w.t.dn.dnp.dnrow.dnx
dny=w.t.dn.dnp.dnrow.dny
dnap=w.t.dn.dnp.dnrow.dnap
dnd=w.t.dn.dnp.dnpre.dnd
dnh=w.t.dn.dnp.dnpre.dnh
dnt=w.t.dn.dnp.dnpre.dnt
dns=w.t.dn.dnp.dnpre.dns
dnn=w.t.dn.dnp.dnpre.dnn
dnecho=w.t.dn.dnp.dnecho
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
	new menu caption="&Nested" children={`)
	b.WriteString(nestedMenuBody())
	b.WriteString(`
	}
	new menu caption="&Help" wellknown="help" children={
		new menuitem caption="&About" action=demo.help.about
	}
}
`)
	return b.String()
}

// nestedMenuBody is the body of the Nested menu: the submenu exercise.
//
// A submenu has no verb and no property of its own on the wire. An item that
// is given children BECOMES one — menuitem's Append makes the Menu on the
// first child and names it after the item — so the whole feature is spelled
// by nesting children={} one level deeper than a menu already does.
//
// Four things worth having a case of:
//
//   - Plenty. Forty items is more than a submenu can show at once on a short
//     display, which is where the height clamp and its scroll are.
//   - Depth. Four levels, to see one submenu open from inside another and
//     Left arrow walk back up the chain.
//   - The ordinary item furniture — checkables, shortcuts, separators, a
//     disabled item — INSIDE a submenu, since none of that is special-cased
//     for the top level.
//   - Something that actually fires, so a trigger from four levels down is
//     seen to arrive as the same command event as one from the menu bar.
func nestedMenuBody() string {
	var b strings.Builder

	b.WriteString("\n\t\tnew menuitem caption=\"&Ordinary Item\" action=demo.nested.pick")
	b.WriteString("\n\t\tnew menuitem separator")

	// A submenu long enough to need the height clamp.
	b.WriteString("\n\t\tnew menuitem caption=\"&Many Items\" children={")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "\n\t\t\tnew menuitem caption=\"Item %d\"", i)
		if i%8 == 0 && i != 40 {
			b.WriteString("\n\t\t\tnew menuitem separator")
		}
	}
	b.WriteString("\n\t\t}")

	// Depth. The innermost item is the one wired to a handler.
	b.WriteString(`
		new menuitem caption="&Deeper" children={
			new menuitem caption="Level &2" children={
				new menuitem caption="Level &3" children={
					new menuitem caption="Level &4 - the bottom" action=demo.nested.deep
					new menuitem separator
					new menuitem caption="Also at level 4"
				}
				new menuitem caption="Also at level 3"
			}
			new menuitem caption="Also at level 2"
		}
		new menuitem separator
		new menuitem caption="&Furniture" children={
			new menuitem caption="&Checkable" checkable
			new menuitem caption="Checkable, &Checked" checkable checked
			new menuitem separator
			new menuitem caption="With a &Shortcut" shortcut="^9"
			new menuitem caption="Shortcut &Text Only" shortcuttext="Cmd-Whatever"
			new menuitem separator
			new menuitem caption="&Disabled" enabled=false
		}`)

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
		inp=new textinput min_width=160 placeholder="Type here..."
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
			new textinput min_width=160 placeholder="Type something..."
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
			new textinput min_width=160 placeholder="Enter text here..."
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
		new textinput min_width=160 placeholder="Enter document content..."
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
