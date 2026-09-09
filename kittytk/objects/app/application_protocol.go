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
//
// The type is registered as well, so the vocabulary answers for the app object
// the way it answers for a button: what properties it takes, and what events
// reach the client through it. Registration is also what lets a subscription
// on the app's ID be checked -- an event a type does not declare reads as
// misspelled and the sub is refused, which is the answer a typo deserves and
// the wrong one for an event that does exist.

func init() {
	set := func(name string) protocol.PropertyApplier {
		return func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			return target.(*Application).Set(name, v, f)
		}
	}
	protocol.RegisterType("application", &protocol.TypeSpec{
		// The connection arrives with its Application and the host registers
		// it; `new application` is refused. Registering says what the object
		// a client ALREADY HOLDS accepts and raises -- it does not offer a
		// way to make another.
		Hosted: true,
		ID:     func(target any) uint64 { return target.(*Application).ID() },
		Props: map[string]protocol.Property{
			"name": protocol.NewProperty("string", set("name")).
				Tip("What the app is called. A remote app may only keep the name it was approved under.").Def(""),
			"multiwindow": protocol.NewProperty("flag", set("multiwindow")).
				Tip("The app may open more than one top-level window.").Def("false"),
			"contextonly": protocol.NewProperty("flag", set("contextonly")).
				Tip("The app contributes context menus and no windows of its own.").Def("false"),
		},
		Events: map[string]protocol.EventDesc{
			"store_item": protocol.NewEventDesc("One stored item: what an inventory lists, and what a put or an append answers with.").
				Field("app", "uint", "The application the item is stored for.").
				Field("tree", "enum", "data or cache.").
				Field("key", "string", "What the app calls the item.").
				Field("type", "enum", "txt, psl, bin, ini or conf.").
				Field("size", "int", "The item's size in bytes."),
			"store_done": protocol.NewEventDesc("The end of an inventory: every item has been sent.").
				Field("app", "uint", "The application the inventory is of.").
				Field("tree", "enum", "data or cache.").
				Field("count", "int", "How many items were listed."),
			"store_data": protocol.NewEventDesc("One chunk of an item being read back. Ask again from the offset reached until the chunk marked last.").
				Field("app", "uint", "The application the item is stored for.").
				Field("tree", "enum", "data or cache.").
				Field("key", "string", "What the app calls the item.").
				Field("type", "enum", "txt, psl, bin, ini or conf.").
				Field("offset", "int", "Where in the item this chunk starts.").
				Field("size", "int", "The whole item's size in bytes.").
				Field("data", "string", "The chunk's bytes, every one of them escaped that is not printable ASCII.").
				Field("last", "flag", "Set on the chunk that ends the item."),
			"store_gone": protocol.NewEventDesc("An item is no longer there: what store_drop answers with.").
				Field("app", "uint", "The application the item was stored for.").
				Field("tree", "enum", "data or cache.").
				Field("key", "string", "The key that now holds nothing."),
			"store_error": protocol.NewEventDesc("A store statement the desktop refused, and why.").
				Field("app", "uint", "The application that asked.").
				Field("tree", "enum", "data or cache, where the statement named one.").
				Field("key", "string", "The key it was about, where it named one.").
				Field("reason", "string", "What was wrong with it."),
		},
	})
}

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

// Append reports that an application has no collection properties: windows
// join it by being built top-level, not by being appended here.
func (app *Application) Append(slot string, child protocol.Object) error {
	return fmt.Errorf("application has no property %q", slot)
}

// ID returns the application's stable object identity (protocol.Object).
func (app *Application) ID() uint64 {
	return uint64(app.ObjectID())
}
