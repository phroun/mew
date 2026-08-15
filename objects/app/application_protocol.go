package app

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Wire binding for Application: a running app is a protocol object (it carries
// an ObjectID, see ObjectID) and accepts property sets like any window or
// trinket. The app is never constructed over the wire - the connection
// already has one - so there is no `new application`; instead the host
// registers the existing instance into the session (Session.Register) and
// hands the client its ID in the handshake. The client can then address it:
//
//	set <appID> multiwindow contextonly name="Tools"
//
// These three methods make *Application satisfy protocol.Object.

// SetWireNameChangeAllowed marks whether this connection is trusted to change
// the app's name over the protocol independently of the name it was approved
// under. The display host sets it true for connections whose trust is not
// name-specific - a local app, or an "Always for All Apps" client - and false
// otherwise, so a remote name-specific app can only keep its authorized name
// (a change to a different name is rejected). It does not affect the
// in-process SetName path.
func (app *Application) SetWireNameChangeAllowed(allowed bool) {
	app.mu.Lock()
	app.wireNameAllowed = allowed
	app.mu.Unlock()
}

// Set applies one wire property to the application (protocol.Object).
func (app *Application) Set(name string, v *protocol.Value, flag protocol.FlagState) error {
	switch name {
	case "name":
		s, err := protocol.AsString("name", v, flag)
		if err != nil {
			return err
		}
		app.mu.RLock()
		nameIndependent := app.wireNameAllowed
		current := app.name
		app.mu.RUnlock()
		// The connect-time name is the authorized one. A later change to a
		// DIFFERENT name doesn't match the approval and is rejected, unless the
		// connection is trusted independently of the name (a local app, or an
		// "Always for All Apps" client). Setting the name to its current value
		// always matches.
		if !nameIndependent && s != current {
			return fmt.Errorf("name change to %q is not authorized for this connection", s)
		}
		app.SetName(s)
	case "multiwindow":
		b, err := protocol.AsBool("multiwindow", v, flag)
		if err != nil {
			return err
		}
		app.SetMultiWindow(b)
	case "contextonly":
		b, err := protocol.AsBool("contextonly", v, flag)
		if err != nil {
			return err
		}
		app.SetContextOnly(b)
	default:
		return fmt.Errorf("application has no property %q", name)
	}
	return nil
}

// Append reports that an application takes no children over the wire: windows
// join it by being built top-level, not by being appended here.
func (app *Application) Append(child protocol.Object) error {
	return fmt.Errorf("application does not accept children")
}

// ID returns the application's stable object identity (protocol.Object).
func (app *Application) ID() uint64 {
	return uint64(app.ObjectID())
}
