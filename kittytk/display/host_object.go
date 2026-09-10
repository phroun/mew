package display

// The display itself as an object the app can address.
//
// The terminal's theme, the font the desktop draws in, what the status bar
// says, whether the desktop is showing: one of each exists and everyone
// connected shares it. The connection is given a host object under the name
// `host`, alongside its application and its store:
//
//	set host dark / set host !dark    the terminal's theme, dark or light
//	set host desktop                  show the desktop (mew's show_desktop)
//	set host !desktop                 hide it again    (mew's hide_desktop)
//	set host desktopfont=tuesday      tuesday, or default
//	set host status="Ready"           the desktop's status bar
//	ask host dark / ask host desktop  which way either of those is set
//
// And the things it DOES, which hold no value afterwards to set or to ask for:
//
//	do host tile / do host cascade    arrange the desktop's windows
//	do host rawkey                    pass the next key straight through
//	do host cut / copy / paste / selectall
//
// The edit actions reach whatever has the focus, which may belong to another
// app -- as the bare verbs they replace always did.
//
// Any app connected can set or do any of these. There is no surface to
// configure who may, so it stays what the bare verbs already allowed.

import (
	"fmt"
	"sync/atomic"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// The questions the display answers, and the event it answers them with.
const (
	AskDark    = "dark"    // is the terminal in the dark theme
	AskDesktop = "desktop" // is the desktop showing

	EventHostState = "host_state" // how the display stands
)

// The things the display does. None of them is a value it then holds: there is
// no "is it tiled" to ask for, and pasting is over the moment it happens.
const (
	DoTile      = "tile"
	DoCascade   = "cascade"
	DoRawKey    = "rawkey"
	DoCut       = "cut"
	DoCopy      = "copy"
	DoPaste     = "paste"
	DoSelectAll = "selectall"
)

// hostObject is a connection's handle on the display it is connected to. Like
// the store, it is registered rather than built: `new host` is refused, and the
// handshake hands the client the id of the one it has.
type hostObject struct {
	conn *conn
	id   uint64
}

func newHostObject(c *conn, id uint64) *hostObject {
	return &hostObject{conn: c, id: id}
}

// ID implements protocol.Object.
func (h *hostObject) ID() uint64 { return h.id }

// Append reports that the display adopts nothing here: windows join it by being
// built top-level.
func (h *hostObject) Append(slot string, _ protocol.Object) error {
	return fmt.Errorf("the host has no property %q", slot)
}

// Set applies one property (protocol.Object).
func (h *hostObject) Set(name string, v *protocol.Value, flag protocol.FlagState) error {
	d := h.conn.server.desktop

	switch name {
	case "dark":
		b, err := protocol.AsBool("dark", v, flag)
		if err != nil {
			return err
		}
		setTerminalTheme(d, b)
		return nil
	case "desktop":
		// mew's show_desktop and hide_desktop, on the wire. Showing gives the
		// primary surface back to the desktop and re-homes the window that
		// filled it as a torn-off, dockable one; hiding promotes a detached
		// window to fill the display again.
		if v != nil {
			return fmt.Errorf("desktop: asserted or negated, not set")
		}
		switch flag {
		case protocol.FlagTrue:
			d.ExitSoloMode()
			return nil
		case protocol.FlagFalse:
			d.EnterSoloFromDesktop()
			return nil
		}
		return fmt.Errorf("desktop: asserted or negated, not left indeterminate")
	case "status":
		s, err := protocol.AsString("status", v, flag)
		if err != nil {
			return err
		}
		if sb := d.StatusBar(); sb != nil {
			sb.SetText(s)
		}
		return nil
	case "desktopfont":
		if v == nil || v.Kind != protocol.WordValue {
			return fmt.Errorf("desktopfont: expected a font name")
		}
		d.SetFont(namedDesktopFont(v.Word))
		return nil
	}
	return fmt.Errorf("the host has no property %q", name)
}

// Do performs one of the display's actions. Each reaches the desktop everyone
// shares, and the edit actions reach whatever has the focus -- which may belong
// to another app, the way the bare verbs they replace always did.
func (h *hostObject) Do(action string, _ []*protocol.Arg) error {
	d := h.conn.server.desktop
	switch action {
	case DoTile:
		if wm := d.WindowManager(); wm != nil {
			wm.TileWindows()
		}
		return nil
	case DoCascade:
		if wm := d.WindowManager(); wm != nil {
			wm.CascadeWindows()
		}
		return nil
	case DoRawKey:
		d.ActivatePassNextKeyToTrinket()
		return nil
	case DoCut, DoCopy, DoPaste, DoSelectAll:
		editAction(d.FocusedTrinket(), action)
		return nil
	}
	return fmt.Errorf("the host does nothing called %q", action)
}

// Ask answers a question put to the display. Nothing else reads these back, so
// an app that means to turn one of them over has to be told which way it is
// first. Every question is answered with the same event, carrying all of it.
func (h *hostObject) Ask(question string, _ []*protocol.Arg) error {
	switch question {
	case AskDark, AskDesktop:
		h.answer()
		return nil
	}
	return fmt.Errorf("the host answers no question called %q", question)
}

// answer says how the display stands, naming itself as the source so one
// subscription hears it.
func (h *hostObject) answer() {
	d := h.conn.server.desktop
	h.conn.queueAnswer(protocol.NewEvent(EventHostState).
		WithUint("host", h.id).
		WithFlag("dark", flagOf(style.ActiveTermTheme() == style.TermThemeDark)).
		WithFlag("desktop", flagOf(d.IsDesktopEnvironment())))
}

// hostObjectIDs numbers the host objects, one per connection, kept clear of
// every other kind of wire id the way the store's are.
var hostObjectIDs atomic.Uint64

func hostObjectID() uint64 { return hostIDBase + hostObjectIDs.Add(1) }

// hostIDBase keeps host ids clear of trinket ids and of store ids both.
const hostIDBase = 1 << 41

func init() {
	prop := func(kind, name, tip string) protocol.Property {
		return protocol.NewProperty(kind, func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			return target.(*hostObject).Set(name, v, f)
		}).Tip(tip)
	}

	protocol.RegisterType("host", &protocol.TypeSpec{
		// The connection arrives with its host object and the display
		// registers it; `new host` is refused. There is one display, and it
		// is not something a client makes another of.
		Hosted: true,
		ID:     func(target any) uint64 { return target.(*hostObject).ID() },
		Props: map[string]protocol.Property{
			"dark": prop("flag", "dark",
				"The terminal's theme: dark, or light when negated."),
			"desktop": prop("flag", "desktop",
				"Show the desktop, docking the window that filled the display; "+
					"negated, hide it again."),
			"status": prop("string", "status",
				"What the desktop's status bar says."),
			"desktopfont": prop("enum", "desktopfont",
				"The font the desktop draws in: tuesday, or default."),
		},
		Does: map[string]protocol.DoDesc{
			DoTile:      protocol.NewDoDesc("Arrange the desktop's windows side by side."),
			DoCascade:   protocol.NewDoDesc("Arrange the desktop's windows in a stack."),
			DoRawKey:    protocol.NewDoDesc("Pass the next key straight to the focused trinket."),
			DoCut:       protocol.NewDoDesc("Cut, on whatever has the focus."),
			DoCopy:      protocol.NewDoDesc("Copy, on whatever has the focus."),
			DoPaste:     protocol.NewDoDesc("Paste, on whatever has the focus."),
			DoSelectAll: protocol.NewDoDesc("Select all, on whatever has the focus."),
		},
		Asks: map[string]protocol.AskDesc{
			AskDark: protocol.NewAskDesc("Which way the terminal's theme is set.").
				Answering(EventHostState),
			AskDesktop: protocol.NewAskDesc("Whether the desktop is showing.").
				Answering(EventHostState),
		},
		Events: map[string]protocol.EventDesc{
			EventHostState: protocol.NewEventDesc(
				"How the display stands. Every question is answered with this, so one "+
					"answer says all of it.").
				Field("host", "uint", "The display answering.").
				Field("dark", "flag", "The terminal's theme: asserted for dark, negated for light.").
				Field("desktop", "flag", "Whether the desktop is showing."),
		},
	})
}
