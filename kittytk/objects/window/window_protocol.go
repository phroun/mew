package window

import (
	"fmt"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// closeDecision is how long an application gets to answer whether one of its
// windows may close, before the display asks the person instead.
//
// **There is no such thing as waiting long enough.** An application that
// subscribed to `window_closing` and has not answered may be holding a
// save-your-work dialog in front of somebody, in which case no deadline is long
// enough; or it may be in a loop and never going to answer, in which case no
// deadline is short enough. The two are indistinguishable from here.
//
// So this is not a guess at how long deciding takes. It is how long the display
// waits before admitting it cannot tell, and asking somebody who can -- see
// Desktop.AskForceClose. Long enough that an application which is merely thinking
// is never interrupted, short enough that a hung one does not look like a window
// that simply will not close.
//
// A var, so a test of what happens when it passes need not take five seconds.
var closeDecision = 5 * time.Second

// Wire registration for Window. Per D12, behavior flags are
// individual named flags, never bitsets:
//
//	win=new window title="Tools" x=64 y=64 width=448 height=256 no_resize children={
//	    new panel layout=vbox children={...}
//	}
//
// Coordinates and sizes are in the desktop denomination (D8). The
// single child is the window content; wrap several in a panel.
func init() {
	// Each flag carries its own description: these reach clients through
	// `describe` and are what the documentation tables print, so "frameless
	// behavior flag" tells a reader nothing they could not guess.
	windowFlagProps := map[string]struct {
		flag WindowFlags
		doc  string
	}{
		"frameless":    {WindowFlagFrameless, "Draw no frame around the window."},
		"no_title":     {WindowFlagNoTitle, "Draw no title bar."},
		"no_resize":    {WindowFlagNoResize, "The user cannot resize the window."},
		"no_move":      {WindowFlagNoMove, "The user cannot move the window."},
		"no_close":     {WindowFlagNoClose, "The user cannot close the window."},
		"no_minimize":  {WindowFlagNoMinimize, "The user cannot minimize the window."},
		"no_maximize":  {WindowFlagNoMaximize, "The user cannot maximize the window."},
		"stays_on_top": {WindowFlagStaysOnTop, "Keep the window above its peers."},
		"tearable":     {WindowFlagTearable, "The window may be torn off to its own OS surface."},
	}

	props := map[string]protocol.Property{
		"title": protocol.NewProperty("string", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString("title", v, f)
			if err != nil {
				return err
			}
			target.(*Window).SetTitle(s)
			return nil
		}).Tip("Window title bar text"),
		// native requests an OS window when the platform can create
		// surfaces (G4 dual mode); single-surface platforms keep the
		// window in-surface.
		"native": protocol.NewProperty("flag", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			b, err := protocol.AsBool("native", v, f)
			if err != nil {
				return err
			}
			target.(*Window).SetNativeRequested(b)
			return nil
		}).Tip("Request a native OS window").Def("false"),
		// main marks this window as its application's main window: its
		// menu/status chrome detaches with it on tear-off. The host acts
		// on it when adopting the window.
		"main": protocol.NewProperty("flag", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			b, err := protocol.AsBool("main", v, f)
			if err != nil {
				return err
			}
			target.(*Window).SetMainRequested(b)
			return nil
		}).Tip("Mark as the application's main window").Def("false"),
		// type classifies the window's role: main, normal, mdichild, dialog,
		// modal, or toolpalette. Creation gating (multiwindow for normal, and
		// the requirements for dialog/modal/toolpalette) is enforced by the
		// display layer when it adopts the window.
		"type": protocol.NewProperty("string", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString("type", v, f)
			if err != nil {
				return err
			}
			wt, ok := WindowTypeFromString(s)
			if !ok {
				return fmt.Errorf("window: unknown type %q", s)
			}
			target.(*Window).SetType(wt)
			return nil
		}).Tip("Window role: main|normal|mdichild|dialog|modal|toolpalette").Def("normal"),
		// owner is the object id of the window a dialog/modal/toolpalette
		// floats above. The display layer resolves it (up the ownership chain)
		// when the window is adopted. 0 = application-level.
		"owner": protocol.NewProperty("int", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			n, err := protocol.AsInt("owner", v, f)
			if err != nil {
				return err
			}
			target.(*Window).SetOwnerRequestID(uint64(n))
			return nil
		}).Tip("Owning window's object id for a dialog/modal/toolpalette (0 = app-level)").Def("0"),
		// font overrides the window's font (its content inherits it);
		// empty / "default" clears the override back to the desktop's.
		"font": protocol.NewProperty("string", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString("font", v, f)
			if err != nil {
				return err
			}
			target.(*Window).SetFont(namedFont(s))
			return nil
		}).Tip("Window font override (\"default\" clears)"),
		// denomination overrides the window's row height in units (its
		// content lays out against it); 0 clears the override.
		"denomination": protocol.NewProperty("int", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			n, err := protocol.AsInt("denomination", v, f)
			if err != nil {
				return err
			}
			w := target.(*Window)
			if n <= 0 {
				w.SetCellMetrics(nil)
			} else {
				m := core.DefaultCellMetrics()
				m.UnitsPerCellHeight = core.Unit(n)
				w.SetCellMetrics(&m)
			}
			return nil
		}).Tip("Row height override in units (0 clears)"),
	}

	for _, dim := range []string{"x", "y", "width", "height"} {
		dim := dim
		props[dim] = protocol.NewProperty("int", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			n, err := protocol.AsInt(dim, v, f)
			if err != nil {
				return err
			}
			w := target.(*Window)
			b := w.Bounds()
			switch dim {
			case "x":
				b.X = core.Unit(n)
			case "y":
				b.Y = core.Unit(n)
			case "width":
				b.Width = core.Unit(n)
			case "height":
				b.Height = core.Unit(n)
			}
			w.SetBounds(b)
			return nil
		}).Tip("Window " + dim + " in desktop units")
	}

	for name, descriptor := range windowFlagProps {
		name, flag, doc := name, descriptor.flag, descriptor.doc
		props[name] = protocol.NewProperty("flag", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			b, err := protocol.AsBool(name, v, f)
			if err != nil {
				return err
			}
			w := target.(*Window)
			if b {
				w.SetFlags(w.Flags() | flag)
			} else {
				w.SetFlags(w.Flags() &^ flag)
			}
			return nil
		}).Tip(doc).Def("false")
	}

	props["children"] = protocol.NewCollection(func(parent, child any) error {
		w, ok := parent.(*Window)
		if !ok {
			return fmt.Errorf("window: wrong parent type %T", parent)
		}
		cw, ok := child.(core.Trinket)
		if !ok {
			return fmt.Errorf("window: content must be a trinket, got %T", child)
		}
		if w.Content() != nil {
			return fmt.Errorf("window: only one content trinket (wrap several in a panel)")
		}
		w.SetContent(cw)
		return nil
	}).Tip("The one trinket this window shows.")

	protocol.RegisterType("window", &protocol.TypeSpec{
		Events: map[string]protocol.EventDesc{
			"window_closing": protocol.NewEventDesc("The window is closing and has NOT closed: the application decides whether it may, because it is the only one that knows there is unsaved work. Subscribing is what makes a close askable at all — a window nobody is listening about closes at once.").
				Field("window", "uint", "The window asking.").
				Field(protocol.DecisionField, "uint", "The decision to answer: `do <id> allow` and it closes, `do <id> deny` and it stays. Answer within about five seconds — not because deciding must be quick, but because the display cannot tell an application that is still thinking from one that is never going to answer. Past that it asks the person whether to force the window closed, and says which application did not respond. Answering promptly, even to say `deny`, is what keeps your name out of that dialog."),
			"window_closed": protocol.NewEventDesc("The window finished closing. It carries no trinket field because the window IS the subject.").
				Field("window", "uint", "The closed window's object ID."),
		},
		New: func() any { return NewWindow("") },
		ID: func(t any) uint64 {
			return uint64(t.(*Window).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			w := target.(*Window)
			id := uint64(w.ObjectID())
			w.SetOnCloseComplete(func() {
				ctx.EmitEvent(protocol.NewEvent("window_closed").
					WithUint("window", id))
			})

			// **A close the application may refuse**, which it could not before:
			// a Go handler returning false is a veto for the host, and an
			// application reaches the display only over the wire.
			//
			// Close() answers now and the application answers later, so this
			// declines the close it cannot yet allow, and closes the window
			// itself when the answer comes. `allowing` is how the second Close
			// gets past this handler rather than asking the same question
			// again; both it and the verdict are on the desktop thread, which
			// is where a close happens and where a batch executes.
			allowing := false
			w.SetOnClose(func() bool {
				if allowing {
					allowing = false
					return true
				}
				d := ctx.Deciding(
					protocol.NewEvent("window_closing").WithUint("window", id),
					closeDecision,
					func(v protocol.Verdict) {
						switch {
						case v.Allowed:
							allowing = true
							w.Close()
						case v.Said:
							// Denied. It stays open, which is what was asked
							// for.
						default:
							// **Nobody answered, and the display cannot tell
							// why.** An application that subscribed may be
							// holding a save-your-work dialog in front of
							// somebody, or may be in a loop and never going to
							// answer. Both look exactly alike from here.
							//
							// So it stops guessing and asks the one party that
							// can tell, who is looking at the screen.
							w.AskForceClose(func(force bool) {
								if !force {
									return
								}
								allowing = true
								w.Close()
							})
						}
					})
				// Nobody is listening, so nobody is deciding: close at once,
				// exactly as a window with no wire binding at all does.
				return d == nil
			})
		},
		Props: props,
		Destroy: func(t any) error {
			// **Destroying is not asking.** The statement came from the
			// application, and putting its own order back to it as a question
			// would want an answer inside the batch that gave the order. The
			// handler goes first, so this close is the plain one.
			w := t.(*Window)
			w.SetOnClose(nil)
			w.Close()
			return nil
		},
	})
}

// namedFont maps a protocol font name to a built-in font, or nil (inherit
// from the desktop) for empty / "default".
func namedFont(name string) *core.Font {
	switch name {
	case "monday", "monday12", "mono":
		return core.FontMonday12
	case "tuesday", "tuesday12":
		return core.FontTuesday12
	case "uitext", "uitext12", "ui":
		return core.FontUIText12
	default:
		return nil
	}
}
