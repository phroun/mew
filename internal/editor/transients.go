package editor

import (
	"fmt"

	"strings"
	"time"

	"github.com/phroun/mew/internal/plugins"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// transientNotificationClasses are the viewport classes used for the transient
// bottom-docked message viewports created by ShowNotification/ShowError/
// ShowWarning. They share a single display slot and are auto-expired.
var transientNotificationClasses = map[string]bool{
	"notification": true,
	"error":        true,
	"warning":      true,
}

// showTransient creates a one-line bottom-docked message viewport of the given
// class; the class drives its colors. Transient viewports are not cleared on
// creation; they stack and are removed purely by age via
// expireStaleNotifications.
//
// A non-empty tag makes the message REPLACE its predecessor rather than stack:
// any existing transient carrying the same tag is removed first. A rapid
// series of same-tag toasts (e.g. "Switched to …" as focus cycles) collapses
// to just the latest instead of piling up. The tag rides ViewportOptions and
// is otherwise inert — colors, chrome set, and age-expiry are unchanged.
func (e *Editor) showTransient(message, class, tag string, tfc bool) {
	if tfc {
		message = e.expandTransientTFC(message, class)
	}
	if tag != "" {
		for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
			if w.Tag == tag {
				e.ViewportManager.RemoveViewport(w.ID)
			}
		}
	}
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.ToolViewport,
		ViewportSet:     viewport.ViewportSetTransient,
		Class:           class,
		Tag:             tag,
		Dock:            viewport.DockBottom,
		Priority:        0, // Very low priority - below everything else
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})

	e.RequestRender()
}

// expandTransientTFC expands TFC codes in a transient's message (opt-in, per
// notification). A %keys#…% / %keys_verbose#…% badge is wrapped in the "key"
// color and closed with the transient class's own "messages" color, so the
// badge stands out and the surrounding text returns to the bar's color — the
// renderer paints the whole bar in that "messages" color, so the close restores
// it exactly.
func (e *Editor) expandTransientTFC(message, class string) string {
	const typ = "tool" // transients are ToolViewports
	keyColor := e.LoadedConfig.Colors.Resolve(class, typ, "key")
	barColor := e.LoadedConfig.Colors.Resolve(class, typ, "messages")
	return plugins.ExpandTFC(message, nil, e.tfcKeyResolver(keyColor, barColor))
}

// ShowNotification creates a transient notification viewport at the bottom of the
// screen (informational messages, command confirmations).
func (e *Editor) ShowNotification(message string) {
	e.showTransient(message, "notification", "", false)
}

// ShowNotificationTagged is ShowNotification for a message that should REPLACE
// its predecessor instead of stacking: a new toast with the same tag removes
// the old one first. For repeated messages that would otherwise pile up — the
// "Switched to …" focus toast, the "Buffer is read-only" warning.
func (e *Editor) ShowNotificationTagged(message, tag string) {
	e.showTransient(message, "notification", tag, false)
}

// ShowNotificationTFC is ShowNotification for a message that should have its TFC
// codes expanded — e.g. %keys_verbose#help_toggle|^Q H% resolved to the live
// binding and colored. TFC support is opt-in per notification.
func (e *Editor) ShowNotificationTFC(message string) {
	e.showTransient(message, "notification", "", true)
}

// clearTaggedTransient removes any transient carrying tag, for a progress
// message whose work has finished (or been cancelled) and which should come
// down NOW rather than linger out its five-second expiry — the incremental
// search's "Searching…" toast.
func (e *Editor) clearTaggedTransient(tag string) {
	if tag == "" {
		return
	}
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == tag {
			e.ViewportManager.RemoveViewport(w.ID)
			e.RequestRender()
		}
	}
}

// showTaggedTransient is showTransient for messages that should replace their
// predecessor instead of stacking: any existing transient carrying the same
// tag is removed first. The class still drives colors and age-expiry (so pass
// "notification"); the tag only groups the message for replacement. Used by
// filename completion so re-completing does not pile up option lists.
func (e *Editor) showTaggedTransient(message, class, tag string) {
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == tag {
			e.ViewportManager.RemoveViewport(w.ID)
		}
	}
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.ToolViewport,
		ViewportSet:     "help",
		Class:           class,
		Tag:             tag,
		Dock:            viewport.DockBottom,
		Priority:        0,
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})
	e.RequestRender()
}

// appendVerboseLog appends lines to the shared verbose-log viewport (class
// "verboseLog"), creating it on first use: a new empty buffer viewport like
// buffer_new makes, but in the background — it never takes focus or the
// painted main area; the user reaches it later via viewport cycling.
func (e *Editor) appendVerboseLog(lines ...string) {
	var vw *viewport.Viewport
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Class == "verboseLog" {
			vw = w
			break
		}
	}
	if vw == nil {
		id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:            viewport.DocViewport,
			Class:           "verboseLog",
			Dock:            viewport.DockNone,
			Priority:        0,
			Buffer:          e.lib.New(),
			ShowLineNumbers: true,
			ProtectNewlines: !e.Config.DeleteNewlineAsChar,
			TabSize:         e.Config.TabSize,
			Visible:         true,
		})
		vw = e.ViewportManager.GetViewport(id)
	}
	if vw == nil || vw.Buffer == nil {
		return
	}
	for _, line := range lines {
		if vw.Buffer.GetLineCount() == 1 && strings.TrimRight(vw.Buffer.GetLine(0), "\n\r") == "" {
			// First write into the fresh, empty buffer: insert at the start of
			// line 0 rather than rewriting it.
			vw.Buffer.InsertText(0, 0, line)
		} else {
			vw.Buffer.InsertLine(vw.Buffer.GetLineCount(), line)
		}
	}
}

// announceFocusedViewport shows a "Switched to ..." notification naming the
// newly focused viewport: its top-center message, else its buffer's filename,
// else its ID.
func (e *Editor) announceFocusedViewport() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	name := w.MessageTopCenter
	if name == "" && w.Buffer != nil {
		name = w.Buffer.GetFilename()
	}
	if name == "" {
		name = w.ID
	}
	// Tagged "navigate" so a rapid cycle (^B N / mouse focus) collapses to the
	// latest "Switched to …", and so it shares the navigation family — a switch
	// followed by a link jump replaces rather than stacks.
	e.ShowNotificationTagged("Switched to "+name, "navigate")
}

// ShowError shows a transient error viewport at the bottom of the screen.
func (e *Editor) ShowError(message string) {
	e.showTransient(message, "error", "", false)
}

// ShowWarning shows a transient warning viewport at the bottom of the screen.
func (e *Editor) ShowWarning(message string) {
	e.showTransient(message, "warning", "", false)
}

// ShowWarningTagged is ShowWarning for a warning that should REPLACE its
// predecessor instead of stacking — e.g. the "Buffer is read-only" warning,
// which fires on every rejected edit and would otherwise pile up.
func (e *Editor) ShowWarningTagged(message, tag string) {
	e.showTransient(message, "warning", tag, false)
}

// expireStaleNotifications removes transient notification/error/warning viewports
// that are older than 5 seconds. Called before each command executes.
func (e *Editor) expireStaleNotifications() {
	now := time.Now()
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == niqqudNudgeTag {
			continue // condition-driven; managed by updateNiqqudNudge, never aged out
		}
		if transientNotificationClasses[w.Class] && now.Sub(w.SpawnedAt) > 5*time.Second {
			e.ViewportManager.RemoveViewport(w.ID)
		}
	}
}

// registerMessageCommands registers verbose_log and status.
func (e *Editor) registerMessageCommands(ps *pawscript.PawScript) {
	// verbose_log appends text to the shared verbose-log viewport (class
	// "verboseLog"), creating it in the background on first use - the
	// logging counterpart of insert. Each argument becomes its own line.
	ps.RegisterCommand("verbose_log", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) == 0 {
			e.ShowWarning("Usage: verbose_log <text>")
			return pawscript.BoolStatus(false)
		}
		for i := range ctx.Args {
			if text, ok := argString(ctx, i); ok {
				e.appendVerboseLog(text)
			}
		}
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Status command
	ps.RegisterCommand("status", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			e.ShowNotification(fmt.Sprintf("%v", ctx.Args[0]))
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})
}
