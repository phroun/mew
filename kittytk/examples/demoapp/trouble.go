package main

// A button that makes a little trouble, so the two paths out of a complaint can
// be watched.
//
// A refusal is what a batch is answered WITH: the statement is turned down and
// nothing is made. A COMPLAINT is not that. The load finished, the records are all
// there, and something about them is worth the author knowing -- so the batch gets
// its reply and the complaint rides along beside it.
//
// It goes two ways, and the button shows both at once:
//
//	to the APPLICATION, on the reply to the batch that caused it -- which this
//	reads off `Reply.Trouble` and puts in the status bar, because the author is
//	the one who can fix it
//
//	to the DISPLAY's own log, where Ψ > Desktop Accessories > Event Viewer shows
//	it as an `Error` row keyed by the name that was being loaded -- which is the
//	floor under the whole thing, for a load with no statement behind it
//
// What makes the trouble is a bundle that cannot mean what it says: its `tree:`
// block names a parent field AND a location field, which is two ways down at
// once. The records are fine, so it loads and reads as a flat list, and the
// loader says why it is not a tree.

import (
	"fmt"

	"github.com/phroun/kittytk/client"
)

// troubleBundleKey is what the bundle is filed under, and troubleBundleName is
// how a statement asks for it. One constant each, because a name spelled twice is
// the misspelling all of this exists to make visible.
const (
	troubleBundleKey  = "muddle-1.0.0"
	troubleBundleName = "bundle:muddle"
)

// troubleBundle is a document that loads and has something said about it.
const troubleBundle = `(
  _bundle: (
    key: "muddle", version: "1.0.0",
    tree: ( parent: "up", location: "where", delimiter: "/" )
  ),
  ("first record"),
  ("second record")
)`

// makeTrouble names the muddled bundle and says what came back.
//
// The statement is answered: a list IS made, over records that are all there. So
// it is made and let go again in the same breath -- what is being demonstrated is
// the complaint, not the list.
func (a *app) makeTrouble() {
	reply, err := a.conn.Exec(`muddle=new listview source="` + troubleBundleName + `"`)
	if err != nil {
		// A refusal, which is the other thing entirely: nothing was made, and the
		// batch is answered with the reason instead of a reply.
		a.setStatus("Trouble: the statement was refused -- " + err.Error())
		return
	}
	if id := reply.IDs["muddle"]; id != 0 {
		// Made and let go in the same breath: what is being demonstrated is what
		// came back with the reply, not the list.
		_, _ = a.conn.Exec(fmt.Sprintf("destroy %d", id))
	}
	if len(reply.Trouble) == 0 {
		a.setStatus("Trouble: the bundle loaded with nothing said about it" +
			" -- is " + troubleBundleKey + " on the shelf?")
		return
	}
	// The first of them, with the count where there are more: a status bar is one
	// line, and the Event Viewer has them all.
	t := reply.Trouble[0]
	more := ""
	if len(reply.Trouble) > 1 {
		more = fmt.Sprintf(" (and %d more)", len(reply.Trouble)-1)
	}
	a.setStatus(fmt.Sprintf("Trouble about %s: %s%s -- and in the Event Viewer",
		t.About, t.Text, more))
}

// troubleSample is the muddled bundle, for the shelf the demo stocks at startup.
func troubleSample() sample {
	return sample{troubleBundleKey, "psl", []byte(troubleBundle)}
}

// wireTrouble wires the button on the Lists tab.
func (a *app) wireTrouble(c *client.Conn) {
	c.OnCommand("demo.trouble", func() { a.makeTrouble() })
}
