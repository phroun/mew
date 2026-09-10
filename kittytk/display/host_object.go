package display

// The display itself as an object the app can address.
//
// The terminal's theme, the font the desktop draws in, what the status bar
// says, whether the desktop is showing: one of each exists and everyone
// connected shares it. The connection is given a host object under the name
// `host`, alongside its application and its store:
//
//	set host theme                 toggle the terminal's dark/light theme
//	set host desktop               show the desktop  (mew's show_desktop)
//	set host !desktop              hide it again     (mew's hide_desktop)
//	set host desktopfont=tuesday   tuesday, or default
//	set host status="Ready"        the desktop's status bar
//
// Any app connected can set any of these. There is no surface to configure who
// may, so it stays what the bare verbs it replaces already allowed.

import (
	"fmt"
	"sync/atomic"

	"github.com/phroun/kittytk/protocol"
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

	// theme is an action rather than a state: asserting it is the whole of the
	// request, so `!theme` is refused rather than quietly doing nothing.
	action := func(fn func()) error {
		if v != nil || flag != protocol.FlagTrue {
			return fmt.Errorf("%s: an action is asserted, not set", name)
		}
		fn()
		return nil
	}

	switch name {
	case "theme":
		return action(func() { toggleTerminalTheme(d) })
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
			"theme": prop("flag", "theme",
				"Turn the terminal's theme over, dark to light or back."),
			"desktop": prop("flag", "desktop",
				"Show the desktop, docking the window that filled the display; "+
					"negated, hide it again."),
			"status": prop("string", "status",
				"What the desktop's status bar says."),
			"desktopfont": prop("enum", "desktopfont",
				"The font the desktop draws in: tuesday, or default."),
		},
	})
}
