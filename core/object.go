// Package core provides fundamental types for KittyTK.
package core

import "sync/atomic"

// ObjectID is the stable identity of a UI object (trinket, window).
// Under the display protocol (D2), objects are addressed by ID across
// the wire; pointer identity remains an in-process convenience only.
//
// In-process, IDs are allocated from a process-wide counter. Under the
// protocol, allocation becomes session-scoped and the display service
// is authoritative; the per-session object table lives with the
// service's connection state (deliberately NOT a process-global
// registry here - that would bake in the wrong lifecycle).
type ObjectID uint64

var objectIDCounter atomic.Uint64

// NextObjectID allocates a process-unique object ID.
func NextObjectID() ObjectID {
	return ObjectID(objectIDCounter.Add(1))
}
