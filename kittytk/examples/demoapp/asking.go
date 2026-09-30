package main

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/wire"
)

// askForName puts an input dialog up for a name, offering the last one given,
// and says what came back: the name, a Cancel, or a window closed without an
// answer.
func (a *app) askForName() {
	a.nameAsks++
	key := fmt.Sprintf("nq%d", a.nameAsks)
	ui, err := a.conn.Build(fmt.Sprintf(
		`%s=new inputdialog title="Ask for a Name" prompt="What should this demo call you?" placeholder="a name" text=%s`,
		key, protocol.Quote(a.name)))
	if err != nil {
		a.setStatus("Could not ask for a name: " + err.Error())
		return
	}
	ui.Object(key).On("finish", func(ev *wire.Event) {
		result, _ := ev.Word("result")
		switch result {
		case "ok":
			a.name, _ = ev.Text("text")
			caption := "No name given yet."
			if a.name != "" {
				caption = "Name: " + a.name
			}
			_ = a.ui.Object("bname").Set("caption=" + protocol.Quote(caption))
			a.setStatus(fmt.Sprintf("Input dialog: OK, with %q.", a.name))
		case "cancel":
			a.setStatus("Input dialog: cancelled; the name is unchanged.")
		default:
			a.setStatus("Input dialog: closed without an answer; the name is unchanged.")
		}
	})
}
