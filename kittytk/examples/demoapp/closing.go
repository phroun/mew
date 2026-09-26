package main

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

	// **Subscribed, and never answered.** A real application would answer here,
	// with the client's own helper for it. Saying nothing is the demonstration.
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
