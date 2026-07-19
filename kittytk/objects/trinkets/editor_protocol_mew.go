//go:build mew

package trinkets

import (
	"github.com/phroun/kittytk/core"
)

// Wire registration for the mew-backed Editor surface.
//
// Build-tagged `mew`: a desktop host must be built with `-tags mew` for
// the "editor" type to exist. Unlike "terminal" - whose child process
// lives on the CLIENT, so it relays input/resize as events - the editor
// runs its mew session HERE, server-side, driving the internal
// PurfecTerm directly. Keys the (focused) editor receives are handled in
// process and the resulting display repaints through the normal cell
// stream, so there is no client-side wiring: no input relay, no resize
// relay, nil bind. The editor is simply placed in a window and fills it,
// like any leaf trinket:
//
//	w=new window title="Editor" width=640 height=400 children={ ed=new editor }
//
// The session starts empty at construction. Opening a specific file or
// seeding initial content is future work: properties are applied AFTER
// construction (and after Bind), whereas NewEditor starts the mew
// session at construction, so a `filename=`/`content=` property would
// arrive too late to feed the session. A post-properties finalize hook
// (or a deferred start) is the seam for that once the baseline lands.
func init() {
	regTrinket("editor",
		func() core.Trinket { return NewEditor(EditorOptions{}) },
		nil, // no editor-specific properties yet (see above)
		nil, // leaf: no children
		nil, // no client-side relay: the mew session runs server-side
	)
}
