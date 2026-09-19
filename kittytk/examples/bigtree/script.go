package main

// The window, written in the KittyTK Wire Language.
//
// It is the demo's Details tab with nothing else around it: the same tree, the
// same five columns, the same hidden sort proxy behind Size, the same toggle row.
// Deliberately the same, because the point of this app is to change ONE thing --
// where the rows come from -- and see what that costs. A tree that looked
// different would leave the difference to argue about.
//
// What is added is a row of buttons that name a source, and a label that says
// which one is being read.

import (
	"fmt"
	"strings"
)

// buildScript is the whole window.
//
// The tree starts on its own AUTHORED items, which is what a treeview has always
// been: fifteen rows written down here, expanded, with a hierarchy in the writing.
// The buttons then point it at a hundred thousand rows instead.
func buildScript() string {
	return `
w=new window title="KittyTK Big Tree" width=1000 height=560 children={
	box=new panel layout=vbox spacing=0 children={
		tree=new treeview caption="Name" showheader sorted sortedby=-1 editable treelines stretch=1 display="name" columns={
			sizec=new column id=size caption="Size" width=80 align=layoutopposite sortable sortproxy=4
			kindc=new column id=kind caption="Kind" width=112 sortable editable
			modc=new column id=modified caption="Date Modified" width=192 sortable
			tagsc=new column id=tags caption="Tags" width=64 editable
			rawc=new column id=rawsize caption="Raw Size" width=80 align=layoutopposite numeric hidden !optional
		} items={
			s1=new item caption="Screenshot 2026-07-10 at 1.21.28 AM.png"
			s2=new item caption="Screenshot 2026-07-10 at 12.24.05 AM.png"
			pc=new item caption="PC12" expanded items={
				pcin=new item caption="pc12" expanded items={
					src=new item caption="src" expanded items={
						fmain=new item caption="main.go"
						futil=new item caption="util.go"
					}
					fbuild=new item caption="build.log"
				}
				fread=new item caption="readme.txt"
			}
			docs=new item caption="Documents" expanded items={
				fnotes=new item caption="notes.txt"
				arch=new item caption="archive" expanded items={
					ffin=new item caption="final-report.txt"
					fold=new item caption="old-report.txt"
				}
			}
			farj=new item caption="pc12.arj"
		}
		srcrow=new panel layout=hbox spacing=8 children={
			bflat=new button caption="Flat 100,000"
			bdeep=new button caption="Deep 100,000"
			said=new label caption="Reading: the tree's own items"
		}
		togrow=new panel layout=hbox spacing=8 children={
			showkey=new checkbox caption="Name column" checked
			hscroll=new checkbox caption="H-scroll (fit off)"
			pinl=new checkbox caption="Pin first 2"
			pinr=new checkbox caption="Pin last"
			ledger=new checkbox caption="Ledger"
			lines=new checkbox caption="Tree lines" checked
		}
	}
}

tree=w.box.tree
said=w.box.srcrow.said
bflat=w.box.srcrow.bflat
bdeep=w.box.srcrow.bdeep
sizec=w.box.tree.sizec
kindc=w.box.tree.kindc
modc=w.box.tree.modc
tagsc=w.box.tree.tagsc
rawc=w.box.tree.rawc
showkey=w.box.togrow.showkey
hscroll=w.box.togrow.hscroll
pinl=w.box.togrow.pinl
pinr=w.box.togrow.pinr
ledger=w.box.togrow.ledger
lines=w.box.togrow.lines
s1=w.box.tree.s1
s2=w.box.tree.s2
pc=w.box.tree.pc
pcin=w.box.tree.pc.pcin
src=w.box.tree.pc.pcin.src
fmain=w.box.tree.pc.pcin.src.fmain
futil=w.box.tree.pc.pcin.src.futil
fbuild=w.box.tree.pc.pcin.fbuild
fread=w.box.tree.pc.fread
docs=w.box.tree.docs
fnotes=w.box.tree.docs.fnotes
arch=w.box.tree.docs.arch
ffin=w.box.tree.docs.arch.ffin
fold=w.box.tree.docs.arch.fold
farj=w.box.tree.farj
`
}

// The authored rows, in the order the columns want them read. One list, used to
// write every column, so a row cannot end up with its Kind against somebody
// else's Size.
var authored = []struct {
	item, size, kind, modified, tags, rawsize string
}{
	{"s1", "311 KB", "PNG image", "Yesterday at 1:21 AM", "screen", "318464"},
	{"s2", "1 MB", "PNG image", "Yesterday at 12:24 AM", "screen", "1048576"},
	{"pc", "--", "Folder", "Yesterday at 12:23 AM", "project", "--"},
	{"pcin", "--", "Folder", "Yesterday at 12:28 AM", "project", "--"},
	{"src", "--", "Folder", "Yesterday at 12:29 AM", "code", "--"},
	{"fmain", "6 KB", "Text", "Yesterday at 12:30 AM", "code", "6144"},
	{"futil", "3 KB", "Text", "Yesterday at 12:31 AM", "code", "3072"},
	{"fbuild", "42 KB", "Text", "Today at 9:02 AM", "log", "43008"},
	{"fread", "2 KB", "Text", "Yesterday at 12:32 AM", "readme", "2048"},
	{"docs", "--", "Folder", "Today at 8:15 AM", "docs", "--"},
	{"fnotes", "1 KB", "Text", "Today at 8:16 AM", "notes", "1024"},
	{"arch", "--", "Folder", "Today at 8:20 AM", "archive", "--"},
	{"ffin", "88 KB", "Text", "Today at 8:21 AM", "final", "90112"},
	{"fold", "74 KB", "Text", "Today at 8:22 AM", "old", "75776"},
	{"farj", "99 KB", "ARJ Archive", "Yesterday at 12:17 AM", "archive", "101376"},
}

// valuesScript fills the authored rows' cells, column by column.
//
// The two-batch pattern the demo uses: the build surfaced the item IDs, and this
// references them, because a cell names the item it belongs to and that item has
// to exist first. `id` turns a correlation name from the build into its wire ID.
func valuesScript(id func(name string) uint64) string {
	var b strings.Builder
	col := func(key string, pick func(int) string) {
		fmt.Fprintf(&b, "set %s children={\n", key)
		for i := range authored {
			fmt.Fprintf(&b, "\tnew cell item=%d value=%q\n", id(authored[i].item), pick(i))
		}
		b.WriteString("}\n")
	}
	col("sizec", func(i int) string { return authored[i].size })
	col("kindc", func(i int) string { return authored[i].kind })
	col("modc", func(i int) string { return authored[i].modified })
	col("tagsc", func(i int) string { return authored[i].tags })
	col("rawc", func(i int) string { return authored[i].rawsize })
	return b.String()
}
