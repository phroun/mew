package main

import (
	"fmt"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/protocol"
)

// The MDI Demo tab, converted: the pane, its background control
// panel, and the dock are protocol objects; spawning documents is
// `set mdi children={new window …}`; Tile/Cascade/Next/Prev are
// action properties; and the dock choreography (minimize -> entry,
// click -> restore) runs entirely over pane/dock events.

const mdiTabScript = `
sp=new splitter orientation=vertical position=0.9 caption="Dock" children={
	sa=new scrollarea children={
		mdi=new mdipane fill="░" min_width=640 min_height=400 max_width=640 max_height=400 children={
			cp=new panel layout=vbox spacing=8 children={
				new label caption="MDIPane Trinket Demo"
				new label caption="This MDIPane trinket manages floating windows.\nClick [_] to minimize windows to the dock below."
				new button caption="Spawn Window in MDIPane" action=demo.mdi.spawn
				new panel layout=hbox spacing=8 children={
					new button caption="Tile" action=demo.mdi.tile
					new button caption="Cascade" action=demo.mdi.cascade
					new button caption="Next" action=demo.mdi.next
					new button caption="Prev" action=demo.mdi.prev
				}
				status=new label caption="Active: none"
				new spacer
				new label caption="Tips:"
				new label caption="- Click [_] to minimize to dock"
				new label caption="- Click dock entry to restore"
				new label caption="- Double-click title to maximize"
			}
		}
	}
	dock=new dockrow entry_width=16
}
mdi=sp.sa.mdi
status=sp.sa.mdi.cp.status
dock=sp.dock
`

// mdiChildCount numbers spawned documents across the session.
var mdiChildCount int

// mdiDockSeq numbers dock entry keys (keys release on destroy, but
// unique names keep batches self-describing).
var mdiDockSeq int

func createMDIDemo(desktop *trinkets.Desktop, application *app.Application, _ any) core.Trinket {
	conn := client.NewInProcess(func(id string) { application.Commands().Dispatch(id) })
	ui, err := conn.Build(mdiTabScript)
	if err != nil {
		panic(fmt.Sprintf("mdi tab script: %v", err))
	}

	mdiH := ui.Object("mdi")
	status := ui.Label("status")

	// Window-management buttons: pure wire actions.
	commands := application.Commands()
	commands.Register("demo.mdi.spawn", func() { spawnMDIChild(conn, mdiH) })
	commands.Register("demo.mdi.tile", func() { _ = mdiH.Set("tile") })
	commands.Register("demo.mdi.cascade", func() { _ = mdiH.Set("cascade") })
	commands.Register("demo.mdi.next", func() { _ = mdiH.Set("next") })
	commands.Register("demo.mdi.prev", func() { _ = mdiH.Set("prev") })

	// Dock choreography over events: minimize -> add an entry;
	// restore/remove -> destroy it; entry click -> restore.
	entries := make(map[uint64]client.Handle) // window id -> entry handle
	dropEntry := func(winID uint64) {
		if h, ok := entries[winID]; ok {
			_ = h.Destroy()
			delete(entries, winID)
		}
	}

	mdiH.On("minimize", func(ev *protocol.Event) {
		winID, _ := ev.Uint("window")
		title, _ := ev.Text("title")
		dropEntry(winID) // never two entries for one window
		mdiDockSeq++
		key := fmt.Sprintf("e%d", mdiDockSeq)
		entryUI, err := conn.Build(fmt.Sprintf(
			"set dock children={%s=new dockentry caption=%s window=%d}\nwentry=dock.%s",
			key, protocol.Quote(title), winID, key))
		if err != nil {
			return
		}
		entry := entryUI.Object("wentry")
		entry.On("click", func(*protocol.Event) {
			// D20: our own set never echoes a restore event, so the
			// initiator drops its dock entry itself.
			if mdiH.Set(fmt.Sprintf("restore=%d", winID)) == nil {
				dropEntry(winID)
			}
		})
		entries[winID] = entry
	})
	// UI-initiated restores (not echoes of our own set= above).
	mdiH.On("restore", func(ev *protocol.Event) {
		if id, ok := ev.Uint("window"); ok {
			dropEntry(id)
		}
	})
	mdiH.On("remove", func(ev *protocol.Event) {
		if id, ok := ev.Uint("window"); ok {
			dropEntry(id)
		}
	})
	mdiH.On("active", func(ev *protocol.Event) {
		if title, ok := ev.Text("title"); ok && title != "" {
			_ = status.SetCaption("Active: " + title)
		} else {
			_ = status.SetCaption("Active: none")
		}
	})

	// Spawn the initial document.
	spawnMDIChild(conn, mdiH)

	root, ok := ui.Object("sp").Target().(core.Trinket)
	if !ok {
		panic("mdi tab: no root trinket")
	}
	return root
}

// spawnMDIChild creates one document window inside the pane - a
// set-append of a whole window subtree, with its own buttons wired
// through click events (no per-child command IDs to collide).
func spawnMDIChild(conn *client.Conn, mdiH client.Handle) {
	mdiChildCount++
	n := mdiChildCount
	offset := (n - 1) % 5

	ui, err := conn.Build(fmt.Sprintf(`
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
`, n, n, (offset*2+1)*8, (offset+1)*16, n, n, n, n))
	if err != nil {
		return
	}

	winID := ui.ID("wwin")
	ui.Button("wnew").OnClick(func() { spawnMDIChild(conn, mdiH) })
	ui.Button("wclose").OnClick(func() {
		_ = mdiH.Set(fmt.Sprintf("remove=%d", winID))
	})
}
