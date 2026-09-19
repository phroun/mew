package display_test

// An application asking a tree what it holds, and being answered.
//
// The first question a trinket answers and the first use of the `answer` verb, over a
// real socket -- so what is checked here is the whole path: the key the ask carried
// comes back on the answers, they arrive without anything having subscribed, and the
// completion arrives whether or not anything came before it.
//
// What makes it worth the round trip is that nothing Go-side is reachable from an
// application. A reader edits a cell, the edit is held against the source so it
// survives the next read, and until this it was real, durable for the session, and
// invisible.

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// gathered is what an application collected from one question.
type gathered struct {
	mu    sync.Mutex
	got   []*wire.Answer
	ended chan struct{}
}

func newGathered() *gathered { return &gathered{ended: make(chan struct{})} }

func (g *gathered) take(a *wire.Answer) {
	g.mu.Lock()
	g.got = append(g.got, a)
	done := a.Complete
	g.mu.Unlock()
	if done {
		close(g.ended)
	}
}

func (g *gathered) wait(t *testing.T) []*wire.Answer {
	t.Helper()
	select {
	case <-g.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the question was never completed")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.got
}

// theDesktopTree finds the tree the script built.
func theDesktopTree(t *testing.T, desktop *trinkets.Desktop) *trinkets.TreeView {
	t.Helper()
	var found *trinkets.TreeView
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if tv, ok := w.Content().(*trinkets.TreeView); ok {
						found = tv
					}
				}
			}
		})
		if found != nil {
			return found
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no tree on the desktop")
	return nil
}

func TestAnApplicationAsksWhatATreeHolds(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Asking App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ProvideSource("papers", serveAll); err != nil {
		t.Fatalf("providing: %v", err)
	}

	ui, err := conn.Build(`
w=new window title="Papers" width=420 height=240 children={
	tv=new treeview caption="Name" showheader display="name" editable source="source:papers" columns={
		c=new column id=size caption="Size" width=80 editable
	}
}
wtv=w.tv
`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// **Nothing held yet is an ANSWER and not a refusal.** An application has to be
	// able to tell "nothing to save" from "I cannot tell you", and a question that
	// was understood and found nothing is the first of those.
	empty := newGathered()
	if err := ui.Object("wtv").AskFor("amendments", empty.take); err != nil {
		t.Fatalf("asking: %v", err)
	}
	got := empty.wait(t)
	if len(got) != 1 || !got[0].Complete {
		t.Fatalf("an unamended tree answered %d times, want one completion", len(got))
	}
	if arg := got[0].Arg("count"); arg == nil || arg.Value == nil || arg.Value.Int != 0 {
		t.Errorf("it says it holds %v", arg)
	}
	if got[0].Error != "" {
		t.Errorf("it refused with %q; holding nothing is not a refusal", got[0].Error)
	}

	// Now edit two cells of one row, which is two alterations of one record -- and
	// the one thing an application could not see until this.
	tv := theDesktopTree(t, desktop)
	var key string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			if rows := tv.RootItems(); len(rows) > 0 {
				key = rows[0].Key()
			}
		})
		if key != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if key == "" {
		t.Fatal("the tree never read a row to edit")
	}
	onUI(desktop, func() {
		row := tv.RootItems()[0]
		if !tv.WriteCell(row, "size", "1 KB") {
			t.Error("writing the size cell changed nothing")
		}
		if !tv.WriteCell(row, "", "renamed.txt") {
			t.Error("writing the key cell changed nothing")
		}
	})

	held := newGathered()
	if err := ui.Object("wtv").AskFor("amendments", held.take); err != nil {
		t.Fatalf("asking again: %v", err)
	}
	answers := held.wait(t)
	if len(answers) != 2 {
		t.Fatalf("it answered %d times, want one amendment and a completion", len(answers))
	}

	// The amendment: the record's identity, which of the four things is held, and the
	// members -- as `fields=` and not `record=`, an alteration being partial.
	one := answers[0]
	if one.To == "" {
		t.Error("the answer does not say which question it is answering")
	}
	if one.To != answers[1].To {
		t.Errorf("the two answers quote %q and %q", one.To, answers[1].To)
	}
	if arg := one.Arg("how"); arg == nil || arg.Value == nil ||
		arg.Value.Word != serval.Altered.String() {
		t.Errorf("it is held as %v, want altered", arg)
	}
	id, fields, whole, ok := one.Record()
	if !ok {
		t.Fatal("the answer carries no record")
	}
	if whole {
		t.Error("an alteration came as a whole record; the members that stand are the source's")
	}
	if id == nil {
		t.Error("the answer names no record")
	}
	if v := fields.Get("size"); v == nil || !serval.Equal(v, serval.NewText("1 KB")) {
		t.Errorf("the edited size reads %v", v)
	}
	if v := fields.Get("name"); v == nil || !serval.Equal(v, serval.NewText("renamed.txt")) {
		t.Errorf("the edited name reads %v; the second edit lost the first", v)
	}

	// And the completion says how many amendments, not how many members among them.
	if arg := answers[1].Arg("count"); arg == nil || arg.Value == nil || arg.Value.Int != 1 {
		t.Errorf("the completion says %v, want the one record that was amended", arg)
	}
}
