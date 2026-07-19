package client_test

// The MDI dock choreography the demo uses, end to end. The subtle
// part: minimize arrives as a UI-initiated event, but restoring over
// the wire (set mdi restore=N) is suppressed per D20 - it never
// echoes a restore event - so the click handler that initiated the
// restore must drop its own dock entry. This test locks in that a
// minimize/restore cycle leaves the dock clean (no duplicate entries
// accumulating across cycles).

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

func TestMDIDockChoreographyNoDuplicateEntries(t *testing.T) {
	conn, ui := build(t, nil, `
mdi=new mdipane children={d1=new window title="Doc 1" width=240 height=128}
dock=new dockrow entry_width=20
wwin=mdi.d1
`)
	mdiH := ui.Object("mdi")
	pane := mdiH.Target().(*trinkets.MDIPane)
	dock := ui.Object("dock").Target().(*trinkets.DockRow)
	win := ui.Object("wwin").Target().(*window.Window)

	// The demo's choreography.
	entries := map[uint64]client.Handle{}
	drop := func(id uint64) {
		if h, ok := entries[id]; ok {
			_ = h.Destroy()
			delete(entries, id)
		}
	}
	seq := 0
	mdiH.On("minimize", func(ev *protocol.Event) {
		id, _ := ev.Uint("window")
		drop(id)
		seq++
		key := fmt.Sprintf("e%d", seq)
		entryUI, err := conn.Build(fmt.Sprintf(
			"set dock children={%s=new dockentry caption=\"d\" window=%d}\nwe=dock.%s",
			key, id, key))
		if err != nil {
			t.Fatalf("dock entry build: %v", err)
		}
		entry := entryUI.Object("we")
		entry.On("click", func(*protocol.Event) {
			// D20: our own set never echoes; drop the entry here.
			if mdiH.Set(fmt.Sprintf("restore=%d", id)) == nil {
				drop(id)
			}
		})
		entries[id] = entry
	})
	mdiH.On("restore", func(ev *protocol.Event) {
		if id, ok := ev.Uint("window"); ok {
			drop(id)
		}
	})

	for cycle := 1; cycle <= 3; cycle++ {
		pane.MinimizeWindow(win) // UI path: [_] button
		if n := dock.EntryCount(); n != 1 {
			t.Fatalf("cycle %d: %d dock entries after minimize, want 1", cycle, n)
		}
		dock.Entries()[0].OnClick() // UI path: click the dock entry
		if n := dock.EntryCount(); n != 0 {
			t.Fatalf("cycle %d: %d dock entries after restore, want 0", cycle, n)
		}
		if win.IsMinimized() {
			t.Fatalf("cycle %d: window still minimized after dock click", cycle)
		}
	}
}
