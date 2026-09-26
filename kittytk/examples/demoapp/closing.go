package main

// Two windows that are asked whether they may close: the MDI documents, which
// answer, and the Sulking Window, which does not.
//
// The pair is the point. Both subscribe to `window_closing`, so from the display
// they start out indistinguishable -- and the display's five-second deadline
// exists precisely because it cannot tell them apart. A document that puts a
// question in front of somebody and a window that will never speak again look the
// same from the outside for as long as the person takes to read the question.
//
// So confirmClose is what an application is expected to do, and the Sulking Window
// is what happens to one that does not.
//
// ---
//
// A window that is asked whether it may close, and says nothing.
//
// An application that subscribes to `window_closing` is consulted before one of its
// windows closes, and answers `do <decision> allow` or `deny`. This one subscribes
// and then deliberately never answers -- which is what a hung application looks like
// from the display, and is indistinguishable from one that is putting a
// save-your-work dialog in front of somebody.
//
// So after about five seconds the display stops guessing and asks the one party who
// can tell: "KittyTK Demo did not respond while trying to close ... Force the window
// closed?" That dialog is what this window exists to make happen.
//
// It also shows the two ways out that are NOT the question:
//
//	[x] on the title bar asks, and this window never answers -- so it is the
//	force-close path, and the one to press
//
//	the Close button inside it sends `destroy`, which does not ask at all: the
//	statement came from the application, and putting its own order back to it as a
//	question would want an answer inside the batch that gave the order

import (
	"fmt"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

// sulkingWindowScript is the window itself. It says what to do with it, because a
// window whose whole behaviour is a five-second silence explains nothing on its own.
func sulkingWindowScript(n int) string {
	return fmt.Sprintf(`
sw%d=new window title="Sulking Window" x=%d y=%d width=440 height=240 tearable children={
	swp=new panel layout=vbox spacing=8 children={
		new label caption="This window's application asked to be consulted about closing it, and will never answer." wrap
		new label caption="Press [x] on the title bar. Nothing happens for about five seconds — that is the display waiting for an answer. Then it asks YOU whether to force the window closed, because it cannot tell a thoughtful application from a hung one." wrap
		new label caption="Answer No and the window stays, and pressing [x] again asks again. Answer Yes and it goes." wrap
		new spacer
		new label caption="The button below is not the same thing: it sends destroy, which never asks." wrap
		swclose=new button caption="Close (destroy — no question asked)"
	}
}
swin=sw%d
swcloser=sw%d.swp.swclose
swstate=sw%d.swp
`, n, 72+n*16, 56+n*16, n, n, n)
}

// openSulkingWindow builds it and subscribes to its close WITHOUT ever answering.
//
// The subscription is per WINDOW, which is the point of doing it here rather than on
// the connection: this application's main window and everything else it owns go on
// closing at once, and only this one hangs.
func (a *app) openSulkingWindow() {
	a.mdiCount++ // reuse the counter for a unique key and offset per window
	n := a.mdiCount
	ui, err := a.conn.Build(sulkingWindowScript(n))
	if err != nil {
		a.setStatus("Sulking window: " + err.Error())
		return
	}
	win := ui.Window("swin")

	// **Subscribed, and never answered.** A real application answers here, or says
	// it is still asking somebody -- the documents do both, and Conn.Asking is the
	// helper for the second. Saying neither is the demonstration, and now the only
	// way left to reach that dialog.
	win.On("window_closing", func(ev *wire.Event) {
		id, _ := ev.Uint(wire.DecisionField)
		a.setStatus(fmt.Sprintf(
			"Sulking window: asked to allow the close (decision=%d) and saying nothing"+
				" — the display will ask you in about five seconds.", id))
	})
	// And when it does close, however it was got rid of, say so: the difference
	// between forcing it and destroying it is visible in what came before, not here.
	win.OnClosed(func() { a.setStatus("Sulking window: closed.") })

	ui.Button("swcloser").OnClick(func() {
		a.setStatus("Sulking window: destroy sent — no question asked.")
		_ = win.Close()
	})
}

// wireSulking wires the menu item that opens it.
func (a *app) wireSulking(c *client.Conn) {
	c.OnCommand("demo.file.sulking", func() { a.openSulkingWindow() })
}

// confirmClose subscribes one window's close and puts a question in front of the
// person before allowing it.
//
// `what` names the window the way the question should read, and `unsaved` is asked
// at the moment of the question rather than remembered: whether there is work in
// the document is exactly what changes between opening it and closing it.
//
// **This is per WINDOW, and that is the reason to do it here.** The subscription
// belongs to this document, so the application's other windows -- its main window,
// the protocol companion, the bounded windows -- go on closing without a word.
// Nothing is made askable by an application being willing to be asked about one
// thing.
func (a *app) confirmClose(win client.Window, what string, unsaved func() bool) {
	win.On("window_closing", func(ev *wire.Event) {
		decision, ok := ev.Uint(wire.DecisionField)
		if !ok {
			// No decision named, so there is nothing to answer and nothing
			// waiting on an answer. Putting a dialog up here would ask a
			// question whose reply reaches nobody.
			return
		}
		// **Said before the question goes up, not after.** Putting a dialog on
		// the screen and reading it takes longer than the display's patience,
		// and what it does when that runs out is ask about this very window --
		// so the keep-alive starts before anything slow happens.
		a.askBeforeClosing(decision, a.conn.Asking(ev), what, unsaved())
	})
	// No `window_closed` handler to go with it: the MDI pane owns the
	// close-complete hook of the windows it hosts, so a child's own
	// `window_closed` is superseded by the pane's `remove` -- which wireMDI is
	// already listening to. Subscribing here would be a handler that never fires.
}

// askBeforeClosing puts the question on the screen and answers the decision with
// whatever comes back.
//
// `stillAsking` is what keeps the display from asking its own question on top of
// this one -- see Conn.Asking. It is stopped on every way out of here, because a
// keep-alive for a question nobody is being asked any more is a lie about a person
// who has gone back to work.
func (a *app) askBeforeClosing(decision uint64, stillAsking func(), what string, unsaved bool) {
	a.closeAsks++
	key := fmt.Sprintf("cq%d", a.closeAsks)
	ui, err := a.conn.Build(closeConfirmScript(key, what, unsaved))
	if err != nil {
		// **Unable to ask is not a reason to say nothing.** Silence is the
		// Sulking Window, and it ends in the display asking somebody whether to
		// force the window closed. Deny instead: the window stays, which is the
		// answer that loses nothing, and pressing [x] again asks again.
		stillAsking()
		a.setStatus(what + ": could not ask about closing (" + err.Error() + ") — keeping it open.")
		_ = a.conn.Decide(decision, false)
		return
	}
	ui.Object(key).On("finish", func(ev *wire.Event) {
		stillAsking()
		result, _ := ev.Word("result")
		allow := closeAnswer(result)
		if err := a.conn.Decide(decision, allow); err != nil {
			a.setStatus(what + ": answering the close failed: " + err.Error())
			return
		}
		if allow {
			a.setStatus(what + ": allowed to close.")
		} else {
			a.setStatus(what + ": kept open. Press [x] again to be asked again.")
		}
	})
}

// closeAnswer translates between two vocabularies: a messagebox finishes with a
// word of its own -- `yes`, `no`, and whatever a dialog dismissed some other way
// reports -- and a decision is answered `allow` or `deny`.
//
// **Only yes closes.** Everything else keeps the window, dismissal included: a
// dialog that went away without being answered has not been answered, and the
// answer that loses nothing is the one to give in its place.
func closeAnswer(result string) bool { return result == "yes" }

// closeConfirmScript is the question. Two of them, because a document with
// something in it is asking about losing that, and an empty one is only asking
// whether you meant it -- and a warning about nothing teaches people to dismiss
// warnings.
func closeConfirmScript(key, what string, unsaved bool) string {
	text, icon := "Close "+what+"?", "question"
	if unsaved {
		text = what + " has content that is not saved anywhere.\n\nClose it and lose what is in it?"
		icon = "warning"
	}
	return fmt.Sprintf("%s=new messagebox title=%s icon=%s yes no text=%s",
		key, wire.Quote("Close "+what), icon, wire.Quote(text))
}
