package display_test

// A list's checkboxes cross the wire both ways. An application turns them on
// with `checkboxes=true`, ticks rows with `do <list> check id=...` and the three
// bulk verbs, and asks `checked` for what is ticked. A change made on the
// display's side comes back as a `check` event; one the application made itself
// does not echo back to it (D20), and asking is how it reads the result. A row
// is named by its record's identity throughout, so what an application hears is
// what it can send back, and no answer ever lists every row.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

// spell is an answer or event field as the wire writes it, or "-" for none.
func spell(v *wire.Value) string {
	if v == nil {
		return "-"
	}
	return wire.EncodeValue(v)
}

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
	list := ui.Object("wlv")
	told := make(chan string, 8)
	list.On("check", func(ev *wire.Event) {
		change, _ := ev.Word("change")
		at, _ := ev.Int("at")
		told <- fmt.Sprintf("%s at=%d id=%s checked=%v", change, at, spell(ev.Value("id")), ev.Flag("checked"))
	})
	hear := func(want string) {
		t.Helper()
		select {
		case got := <-told:
			if got != want {
				t.Errorf("told %s, want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no check event arrived; want %s", want)
		}
	}
	ask := func() string {
		t.Helper()
		g := newGathered()
		if err := list.AskFor("checked", g.take); err != nil {
			t.Fatalf("ask: %v", err)
		}
		var parts []string
		for _, a := range g.wait(t) {
			for _, c := range a.Carries {
				if c.Value != nil {
					parts = append(parts, c.Name+"="+spell(c.Value))
				} else if c.Flag == wire.FlagTrue {
					parts = append(parts, c.Name)
				}
			}
		}
		return strings.Join(parts, " ")
	}

	lv := waitForList(t, desktop, 5*time.Second)
	onUI(desktop, func() {
		if !lv.Checkboxes() {
			t.Error("checkboxes=true did not give the list its boxes")
		}
	})

	// One row, by the identity the list reports it under.
	if err := list.Do("check id=1"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if got := ask(); got != "id=1 count=1" {
		t.Errorf("asked with Beta ticked: %q", got)
	}

	// Turned over: the one named is now the one spared out of every row.
	if err := list.Do("check_invert"); err != nil {
		t.Fatalf("check_invert: %v", err)
	}
	if got := ask(); got != "id=1 count=1 all" {
		t.Errorf("asked after inverting: %q", got)
	}

	// A row ticked from the display's side reports the same identity -- and is
	// the first event heard, so none of the application's own changes echoed.
	onUI(desktop, func() { lv.SetSelected(1, true) })
	hear(fmt.Sprintf("row at=1 id=1 checked=%v", wire.FlagTrue))
	if got := ask(); got != "count=0 all" {
		t.Errorf("asked with everything ticked: %q", got)
	}

	if err := list.Do("check id=2 checked=false"); err != nil {
		t.Fatalf("uncheck: %v", err)
	}
	if got := ask(); got != "id=2 count=1 all" {
		t.Errorf("asked with Gamma unticked out of every row: %q", got)
	}
	if err := list.Do("check_none"); err != nil {
		t.Fatalf("check_none: %v", err)
	}
	if got := ask(); got != "count=0" {
		t.Errorf("asked after check_none: %q", got)
	}
	if err := list.Do("check_all"); err != nil {
		t.Fatalf("check_all: %v", err)
	}
	if got := ask(); got != "count=0 all" {
		t.Errorf("asked after check_all: %q", got)
	}
	// And a bulk change from the display's side is told as one.
	onUI(desktop, func() { lv.InvertSelection() })
	hear(fmt.Sprintf("invert at=-1 id=- checked=%v", wire.FlagNone))

	// Without boxes: the verbs are refused, and nothing is ticked.
	if err := list.Set("checkboxes=false"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := list.Do("check_all"); err == nil {
		t.Error("check_all was accepted by a list without checkboxes")
	}
	if got := ask(); got != "count=0" {
		t.Errorf("asked a list without checkboxes: %q", got)
	}
}
