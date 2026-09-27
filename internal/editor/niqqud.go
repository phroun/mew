package editor

import (
	"fmt"
	"strings"

	"github.com/phroun/mew/internal/plugins"
	"github.com/phroun/mew/internal/viewport"
)

// niqqudNudgeTag identifies the persistent force_ltr nudge viewport so it can be
// found for replacement/removal and exempted from age-expiry and the prompt
// priority scan.
const niqqudNudgeTag = "kitty_force_ltr_nudge"

// niqqudNudgeMessage names the one Kitty config change that fixes RTL vowel
// corruption. It sits pinned at the very bottom (just below the modebar) while a
// Hebrew cluster with unfolded niqqud is on screen and mew is flipping for Kitty.
// (Turning mew's own flip off instead would only scramble the letter order —
// force_ltr is the real fix, so the message points at that alone.)
const niqqudNudgeMessage = "* kitty terminal needs force_ltr = yes to eliminate RTL vowel corruption.  Please fix and restart mew."

// niqqudLayoutSig captures the layout facts that are independent of the nudge's
// own one-row footprint, so the hysteresis latch (updateNiqqudNudge) releases
// only on a genuine change — a resize, a scroll, an edit, or a change in the
// viewport set — and not on the nudge's own appearance.
func (e *Editor) niqqudLayoutSig() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d;", e.Renderer.Width, e.Renderer.Height)
	// Count viewports other than the nudge itself: a docked prompt/help
	// appearing or leaving is a real layout change worth re-evaluating.
	n := 0
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Tag != niqqudNudgeTag {
			n++
		}
	}
	fmt.Fprintf(&b, "n%d;", n)
	if fw := e.ViewportManager.GetFocusedViewport(); fw != nil {
		lines := 0
		if fw.Buffer != nil {
			lines = fw.Buffer.GetLineCount()
		}
		fmt.Fprintf(&b, "%s:o%d,%d:L%d", fw.ID, fw.ViewState.ViewOffsetY, fw.ViewState.ViewOffsetX, lines)
	}
	return b.String()
}

// updateNiqqudNudge shows or hides the persistent force_ltr nudge. It should
// exist exactly while mew is flipping RTL for Kitty AND the current frame holds
// a Hebrew cluster whose niqqud survive the active fold (the content Kitty may
// mis-render), and come down otherwise. Called once per render, after the frame
// is painted so the scan reads live cells.
//
// Because the nudge occupies a screen row, a niqqud line resting exactly on a
// viewport's bottom boundary can oscillate: showing the nudge scrolls the line
// off, which clears the condition, which hides the nudge, which brings the line
// back. To avoid a per-frame flicker (and the render churn it drives), a short
// run of frame-to-frame toggles latches the nudge on; the latch releases when
// niqqudLayoutSig changes — i.e. when the user resizes, scrolls, edits, or the
// viewport set changes — at which point the condition is re-evaluated fresh.
func (e *Editor) updateNiqqudNudge() {
	raw := e.kittyFlipActive && e.Renderer.FrameHasUncomposedNiqqud()

	sig := e.niqqudLayoutSig()
	if e.niqqudNudgeLatched && sig != e.niqqudNudgeLatchSig {
		e.niqqudNudgeLatched = false
		e.niqqudNudgeToggleRun = 0
	}

	want := raw
	if e.niqqudNudgeLatched {
		want = true
	} else {
		if raw != e.niqqudNudgeWantPrevious {
			e.niqqudNudgeToggleRun++
		} else {
			e.niqqudNudgeToggleRun = 0
		}
		// Three straight alternations (on/off/on) means the boundary feedback
		// loop, not real content change: pin it on until the layout moves.
		if e.niqqudNudgeToggleRun >= 3 {
			e.niqqudNudgeLatched = true
			e.niqqudNudgeLatchSig = sig
			want = true
		}
	}
	e.niqqudNudgeWantPrevious = raw

	var existing *viewport.Viewport
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == niqqudNudgeTag {
			existing = w
			break
		}
	}
	switch {
	case want && existing == nil:
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:        viewport.ToolViewport,
			ViewportSet: viewport.ViewportSetTransient,
			Class:       "warning",
			Tag:         niqqudNudgeTag,
			Dock:        viewport.DockBottom,
			// Just below the modebar: pinned to the last screen line (or just
			// above a bottom-docked modebar), under any prompt. The prompt
			// priority scan and age-expiry both exempt this tag.
			Priority:        plugins.ModebarBottomPriority - 1,
			MinHeight:       1,
			MaxHeight:       1,
			MessageTopInner: niqqudNudgeMessage,
			ShowLineNumbers: false,
		})
		e.RequestRender()
	case !want && existing != nil:
		e.ViewportManager.RemoveViewport(existing.ID)
		e.RequestRender()
	}
}
