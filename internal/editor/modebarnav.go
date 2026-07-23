package editor

import "github.com/phroun/mew/internal/plugins"

// Modebar nav-history buttons. The modebar shows [<] / [>] just before the
// filename when the focused (context) window has back / forward history. They
// behave like proper buttons: a press CAPTURES the button (without stealing
// focus — nav runs on the focused window), it paints pressed while the pointer
// is over it and reverts when dragged off, and it activates only if the
// release lands back on it. Hover (graphical all-motion builds) lights the
// button under the pointer while nothing is captured.
//
// These live outside the content-window mouse path (mouse.go), which is
// focused-window-gated and would ignore the modebar (a chrome window). The
// hit-test reads the button column ranges the modebar recorded on its last
// render.

// modebarNavHit resolves screen coordinates to a modebar nav button
// (ModebarNavBack / ModebarNavFwd), or ok=false when the point is not on one.
func (e *Editor) modebarNavHit(x, y int) (button int, ok bool) {
	if e.Modebar == nil {
		return plugins.ModebarNavNone, false
	}
	mw := e.WindowManager.GetWindow(e.Modebar.WindowID())
	if mw == nil || !mw.Visible {
		return plugins.ModebarNavNone, false
	}
	if y-1 != mw.ContentY {
		return plugins.ModebarNavNone, false
	}
	button = e.Modebar.NavButtonAtColumn(x - 1 - mw.ContentX)
	return button, button != plugins.ModebarNavNone
}

// modebarNavPressAt captures a modebar nav button under a plain left press,
// reporting whether the press was consumed (so the caller skips the ordinary
// content-window press). A modal prompt holding focus stands the buttons down.
func (e *Editor) modebarNavPressAt(x, y int) bool {
	if e.promptHasPriority() {
		return false
	}
	button, ok := e.modebarNavHit(x, y)
	if !ok {
		return false
	}
	e.modebarNavCapture = button
	e.modebarNavOn = true
	e.RequestRender()
	return true
}

// modebarNavDrag tracks the pointer while a nav button is captured: pressed
// while over the captured button, reverting to normal when dragged off (the
// capture holds until release).
func (e *Editor) modebarNavDrag(x, y int) {
	if e.modebarNavCapture == plugins.ModebarNavNone {
		return
	}
	button, ok := e.modebarNavHit(x, y)
	on := ok && button == e.modebarNavCapture
	if on != e.modebarNavOn {
		e.modebarNavOn = on
		e.RequestRender()
	}
}

// modebarNavRelease ends the capture and, when the release lands back on the
// captured button, runs the corresponding history navigation on the focused
// window.
func (e *Editor) modebarNavRelease(x, y int) {
	button := e.modebarNavCapture
	e.modebarNavCapture = plugins.ModebarNavNone
	e.modebarNavOn = false
	if button != plugins.ModebarNavNone {
		if hit, ok := e.modebarNavHit(x, y); ok && hit == button {
			switch button {
			case plugins.ModebarNavBack:
				e.navHistory(-1)
			case plugins.ModebarNavFwd:
				e.navHistory(+1)
			}
		}
	}
	e.RequestRender()
}

// modebarNavHoverAt lights the nav button under the pointer (graphical
// all-motion tracking), but only while nothing is captured and no modal
// prompt holds focus (a prompt stands the buttons — and their hover — down).
func (e *Editor) modebarNavHoverAt(x, y int) {
	hover := plugins.ModebarNavNone
	if e.modebarNavCapture == plugins.ModebarNavNone && !e.promptHasPriority() {
		if button, ok := e.modebarNavHit(x, y); ok {
			hover = button
		}
	}
	if hover != e.modebarNavHover {
		e.modebarNavHover = hover
		e.RequestRender()
	}
}
