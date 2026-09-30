package display_test

// An application asks for a line of text with `new inputdialog`, and the
// display puts the dialog on the screen as it does a message box. What the
// person does with it comes back as one `finish` event: result=ok with the
// text, result=cancel, or result=none for a window closed without choosing. An
// application's own `destroy` sends nothing back.

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/wire"
)

// waitForWindow finds a window on the desktop by its title.
func waitForWindow(t *testing.T, desktop *trinkets.Desktop, title string) *window.Window {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var found *window.Window
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if w.Title() == title && !w.IsClosed() {
						found = w
					}
				}
			}
		})
		if found != nil {
			return found
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no window titled %q on the desktop", title)
	return nil
}

func TestAnInputDialogAnswersItsApplication(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Asking App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	told := make(chan string, 8)
	ask := func(n int) client.Handle {
		t.Helper()
		name := fmt.Sprintf("q%d", n)
		ui, err := conn.Build(fmt.Sprintf(`%s=new inputdialog title="Rename %d" prompt="New name:" text="draft.txt"`, name, n))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		h := ui.Object(name)
		h.On("finish", func(ev *wire.Event) {
			r, _ := ev.Word("result")
			if s, ok := ev.Text("text"); ok {
				r += " " + s
			}
			told <- fmt.Sprintf("%d %s", n, r)
		})
		return h
	}
	hear := func(want string) {
		t.Helper()
		select {
		case got := <-told:
			if got != want {
				t.Errorf("told %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no finish arrived; want %q", want)
		}
	}
	typeAt := func(w *window.Window, keys ...string) {
		onUI(desktop, func() {
			for _, k := range keys {
				text := ""
				if len([]rune(k)) == 1 {
					text = k
				}
				w.HandleKeyPress(core.KeyPressEvent{Key: k, Text: text})
			}
		})
	}

	// Typed over the offer, and Return in the field.
	ask(1)
	w := waitForWindow(t, desktop, "Rename 1")
	typeAt(w, "n", "o", "t", "e", "s", "Return")
	hear("1 ok notes")

	// Closed without choosing.
	ask(2)
	w = waitForWindow(t, desktop, "Rename 2")
	onUI(desktop, func() { w.Close() })
	hear("2 none")

	// Taken away by the application: nothing comes back. The next dialog's
	// answer is the next thing heard.
	h := ask(3)
	waitForWindow(t, desktop, "Rename 3")
	if err := h.Destroy(); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	ask(4)
	w = waitForWindow(t, desktop, "Rename 4")
	typeAt(w, "Tab", "Tab", "Return")
	hear("4 cancel")
}
