package main

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The MDI tab script must build, surface its keys, and support the
// spawn path (set-append of a window subtree).
func TestMDITabScriptBuilds(t *testing.T) {
	conn := client.NewInProcess(nil)
	ui, err := conn.Build(mdiTabScript)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, k := range []string{"sp", "mdi", "status", "dock"} {
		if ui.ID(k) == 0 {
			t.Errorf("missing surfaced key %q", k)
		}
	}
	pane, ok := ui.Object("mdi").Target().(*trinkets.MDIPane)
	if !ok {
		t.Fatal("mdi is not an MDIPane")
	}

	// Spawn two documents the way the demo does.
	mdiH := ui.Object("mdi")
	for i := 1; i <= 2; i++ {
		spawn := fmt.Sprintf(`
set mdi children={d%d=new window title="Document %d" x=8 y=16 width=240 height=128 children={
	p=new panel layout=vbox children={
		nb=new button caption="New"
	}
}}
w%d=mdi.d%d
`, i, i, i, i)
		sui, err := conn.Build(spawn)
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
		if sui.ID(fmt.Sprintf("w%d", i)) == 0 {
			t.Fatalf("spawn %d: window not surfaced", i)
		}
	}
	if n := len(pane.Windows()); n != 2 {
		t.Fatalf("pane windows = %d, want 2", n)
	}

	// Wire actions work through the handle.
	if err := mdiH.Set("tile"); err != nil {
		t.Errorf("tile: %v", err)
	}
	if err := mdiH.Set(fmt.Sprintf("remove=%d", uint64(pane.Windows()[0].ObjectID()))); err != nil {
		t.Errorf("remove: %v", err)
	}
	if n := len(pane.Windows()); n != 1 {
		t.Errorf("pane windows after remove = %d, want 1", n)
	}
}
