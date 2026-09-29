package display_test

// A list's checkboxes cross the wire both ways: an application turns them on
// with `checkboxes=true`, and each change to what is ticked comes back as a
// `check` event -- one row with its position, identity and state, or every row
// at once -- so the application can keep the same record of the ticks the list
// does without ever being sent a list of rows.

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

func TestAListSaysWhatItTicked(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Ticking App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ui, err := conn.Build(`
w=new window title="Ticks" width=320 height=200 children={
	lv=new listview checkboxes=true items={
		new item caption="Alpha"
		new item caption="Beta"
		new item caption="Gamma"
	}
}
wlv=w.lv
`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	told := make(chan string, 8)
	ui.Object("wlv").On("check", func(ev *wire.Event) {
		change, _ := ev.Word("change")
		at, _ := ev.Int("at")
		key, _ := ev.Text("key")
		told <- fmt.Sprintf("%s at=%d key=%q checked=%v", change, at, key, ev.Flag("checked"))
	})

	lv := waitForList(t, desktop, 5*time.Second)
	onUI(desktop, func() {
		if !lv.Checkboxes() {
			t.Error("checkboxes=true did not give the list its boxes")
		}
		lv.SetSelected(1, true)
		lv.InvertSelection()
	})
	want := []string{
		// The row's identity as serval spells it, as a tree's key= is.
		fmt.Sprintf("row at=1 key=%q checked=%v", serval.Key(serval.NewInt(1)), wire.FlagTrue),
		fmt.Sprintf("invert at=-1 key=%q checked=%v", "", wire.FlagNone),
	}
	for _, w := range want {
		select {
		case got := <-told:
			if got != w {
				t.Errorf("told %s, want %s", got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no check event arrived; want %s", w)
		}
	}

	// And off again from the application's side.
	if err := ui.Object("wlv").Set("checkboxes=false"); err != nil {
		t.Fatalf("set: %v", err)
	}
	onUI(desktop, func() {
		if lv.Checkboxes() {
			t.Error("checkboxes=false left the boxes on")
		}
	})
}
