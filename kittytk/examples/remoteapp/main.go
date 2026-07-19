// Command remoteapp is a display-protocol APPLICATION: it contains
// no rendering code at all - it links only client and protocol,
// dials a running KittyTK display service (start examples/demo in
// another terminal), and drives its UI over the socket.
//
//	terminal 1:  go run ./examples/demo
//	terminal 2:  go run ./examples/remoteapp
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
)

func main() {
	path := client.DefaultSocketPath()

	commands := make(chan string, 8)
	conn, err := client.Dial(path, "Remote App", func(id string) { commands <- id })
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach display service at %s: %v\n", path, err)
		fmt.Fprintln(os.Stderr, "start the desktop first: go run ./examples/demo")
		os.Exit(1)
	}
	defer conn.Close()

	ui, err := conn.Build(`
w=new window title="Remote App" x=96 y=96 width=400 height=224 children={
	p=new panel layout=vbox spacing=0 children={
		new label caption="This window's process is NOT the desktop's process." wrap
		status=new label caption="Interact - events cross the socket."
		new separator
		cb=new checkbox caption="Remote tri-state checkbox" tristate
		inp=new textinput placeholder="Typed text travels as events..."
		btn=new button caption="Quit Remote App" action=remote.quit
	}
}
watch=w.p.status
wcb=w.p.cb
winp=w.p.inp
`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n", err)
		os.Exit(1)
	}

	status := ui.Label("watch")
	ui.Checkbox("wcb").OnToggle(func(s protocol.FlagState) {
		state := map[protocol.FlagState]string{
			protocol.FlagTrue:          "on",
			protocol.FlagFalse:         "off",
			protocol.FlagIndeterminate: "mixed",
		}[s]
		status.SetCaption("toggle -> " + state + " (round-tripped the socket)")
	})
	ui.TextInput("winp").OnChange(func(s string) {
		status.SetCaption(fmt.Sprintf("change -> %q", s))
	})

	// Block until the quit button dispatches remote.quit.
	for id := range commands {
		if id == "remote.quit" {
			return
		}
	}
}
