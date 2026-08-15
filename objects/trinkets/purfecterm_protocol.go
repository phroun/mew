package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the PurfecTerm terminal surface.
//
// feed= is the streaming pseudo-property: every application writes
// APPENDS bytes to the terminal - it is a channel, not state, and is
// never read back:
//
//	term=new terminal
//	set term feed="\e[1mhello\e[0m\r\n"
//
// Arbitrary bytes travel via the \xNN string escape (and \e for ESC);
// the O6 bulk frame arrives with the transport phase as a more
// efficient encoding of the same statement.
//
// The child process runs on the CLIENT, never here on the render server:
// the terminal is a pure display+input surface. The client pumps its
// child's output in through feed=, and receives the user's input back out
// as events:
//
//   - input data="..."      bytes the user typed / mouse-reported / pasted,
//     to write to the client's PTY. Arbitrary bytes ride the \xNN escape.
//   - resize cols=N rows=M   the grid size whenever it changes, so the
//     client can set its PTY winsize. Fires once on bind with the current
//     size.
func init() {
	regTrinket("terminal",
		func() core.Trinket { return NewPurfecTerm() },
		map[string]protocol.Property{
			"feed": protocol.NewProperty("stream", wprop("feed", func(_ *protocol.BindContext, t *PurfecTerm, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("feed", v, f)
				if err != nil {
					return err
				}
				// Display direction: parsed into the screen buffer
				// like program output. (Input travels the other way,
				// out through the input event.)
				t.Feed([]byte(s))
				return nil
			})).Tip("Append bytes to the terminal display"),
			// font / font-size pick the monospace face and point size the
			// terminal's cell grid derives from on graphical targets. Text
			// mode ignores them (cells are cells).
			"font": protocol.NewProperty("string", wprop("font", func(_ *protocol.BindContext, t *PurfecTerm, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("font", v, f)
				if err != nil {
					return err
				}
				t.SetTerminalFontFamily(s)
				return nil
			})).Tip("Monospace font family for the grid").Def("Monday"),
			"font_size": protocol.NewProperty("int", wprop("font_size", func(_ *protocol.BindContext, t *PurfecTerm, v *protocol.Value, f protocol.FlagState) error {
				pt, err := protocol.AsInt("font_size", v, f)
				if err != nil {
					return err
				}
				t.SetTerminalFontSize(pt)
				return nil
			})).Tip("Font point size for the grid").Def("12"),
		},
		nil,
		func(ctx *protocol.BindContext, w core.Trinket) {
			t := w.(*PurfecTerm)
			id := trinketID(t)
			// Relay user input (keystrokes, mouse reports, paste) upstream
			// so the client can write it to its child's PTY. Bytes carry as
			// a quoted string; the wire's \xNN escape preserves them.
			t.SetInputSink(func(b []byte) {
				ctx.EmitEvent(protocol.NewEvent("input").
					WithUint("trinket", id).
					WithString("data", string(b)))
			})
			// Relay grid-size changes so the client matches its PTY winsize.
			t.SetResizeSink(func(cols, rows int) {
				ctx.EmitEvent(protocol.NewEvent("resize").
					WithUint("trinket", id).
					WithInt("cols", cols).
					WithInt("rows", rows))
			})
			// The size is emitted once, when the grid first fits its paint.
			// A client that subscribes after that (the build reply must round
			// -trip before it can) would miss it and leave its PTY at the
			// default, mis-wrapping the shell's prompt. Re-emit the current
			// size on subscribe so a late subscriber still gets it.
			ctx.OnSubscribe(id, "resize", func() { t.emitResize(t.cols, t.rows) })
		},
	)
}
