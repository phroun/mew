package mewhost

import (
	"os/exec"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/mew"
	"github.com/phroun/mew-app/internal/wininstall"
)

// maybeShowWelcome opens the first-run welcome window when a graphical host
// starts a copy of mew that has not been installed yet (Windows only — off
// Windows wininstall reports the first run already done). The window explains
// what mew is and offers two choices, in the "lame trinket" spirit (a label over
// a row of real Buttons):
//
//   - Install — copy the binaries into place, add the Start Menu shortcut, set
//     the registry flag, launch the freshly installed copy, and quit this one.
//   - Try — dismiss the window and drop through to the normal mew editor already
//     running behind it. Nothing is written, so an uninstalled copy keeps
//     offering to install on each launch.
//
// It is presented as a desktop system modal via WindowManager.ShowModal — the
// same mechanism the display service's authorization prompt uses — so it always
// surfaces on top and blocks the surface beneath it; it can't be ignored or lost
// behind the solo editor. mew-sdl is the desktop owner here, so this host-level
// gate is on the same footing as that prompt (not an app-scoped dialog).
//
// Both choices tear the modal down with CloseModal (not window.Close): the
// window manager refuses to front a window while a modal is live, so a plain
// close would leave the editor blocked and unfrontable after "Try".
func maybeShowWelcome(desktop *trinkets.Desktop, application *app.Application, launchArgs []string, graphical bool) {
	if !graphical || !wininstall.Available() || wininstall.FirstRunDone() {
		return
	}
	wm := desktop.WindowManager()
	if wm == nil {
		return
	}
	dlg := newWelcomeDialog(
		"Welcome to mew",
		welcomeLines(),
		func() { // Install
			exe, err := wininstall.Install()
			wm.CloseModal() // tear the welcome down first, either way
			if err != nil {
				showMewError(wm, desktop, "Install failed", err.Error())
				return
			}
			// Launch the freshly installed copy (with the same files) and bow out.
			if exe != "" {
				_ = exec.Command(exe, launchArgs...).Start()
			}
			desktop.Quit()
		},
		func() { wm.CloseModal() }, // Try — dismiss; the editor is already behind us.
	)
	wm.ShowModal(&dlg.Window)
	desktop.RequestUpdate()
}

// welcomeLines is the explanatory copy shown in the welcome window. Pre-split
// into lines (the content trinket draws one line per entry, like the message
// box), kept comfortably narrow.
func welcomeLines() []string {
	return []string{
		"mew edits words — a small, fast editor.",
		"",
		"You're running mew straight from the file you downloaded.",
		"",
		"Install adds mew to your Start Menu and PATH, then opens the",
		"installed copy. Try just opens the editor now, without",
		"installing anything.",
		"",
		"mew " + mew.FullVersion(),
	}
}

// showMewError pops a simple error dialog as a desktop system modal (so it
// surfaces on top like the welcome it replaces), wiring OK to CloseModal so the
// modal stack is popped cleanly and the editor becomes frontable again.
func showMewError(wm *window.WindowManager, desktop *trinkets.Desktop, title, text string) {
	mb := trinkets.NewMessageBox(title, text, trinkets.ButtonOK)
	mb.SetIcon(trinkets.IconError)
	mb.SetOnFinished(func(trinkets.DialogResult) { wm.CloseModal() })
	wm.ShowModal(&mb.Window)
	mb.ResizeToFitContent()
	desktop.RequestUpdate()
}

// welcomeDialog is a modal window whose content is a paragraph of text over a
// row of two buttons (Install / Try). It mirrors the toolkit's MessageBox: a
// content trinket (welcomeContent) implementing Container so the framework routes
// focus, keyboard, and mouse to the real Buttons.
type welcomeDialog struct {
	window.Window
	content   *welcomeContent
	onInstall func()
	onTry     func()
}

func newWelcomeDialog(title string, lines []string, onInstall, onTry func()) *welcomeDialog {
	d := &welcomeDialog{onInstall: onInstall, onTry: onTry}
	d.Window = *window.NewWindow(title)
	d.SetType(window.WindowTypeModal)
	d.SetFlags(window.WindowFlagNoResize)

	c := &welcomeContent{lines: lines}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	c.SetFocusPolicy(core.StrongFocus)

	c.install = trinkets.NewButton("Install")
	c.install.SetParent(c)
	c.install.SetOnClick(d.doInstall)
	c.try = trinkets.NewButton("Try")
	c.try.SetParent(c)
	c.try.SetOnClick(d.doTry)
	c.buttons = []*trinkets.Button{c.install, c.try}

	d.content = c
	d.SetContent(c)
	d.calculateSize()
	return d
}

// doInstall / doTry invoke the wired action, which owns tearing the modal down
// (via WindowManager.CloseModal) — the dialog does not close itself, so the
// modal stack is popped cleanly and the editor becomes frontable again.
func (d *welcomeDialog) doInstall() {
	if d.onInstall != nil {
		d.onInstall()
	}
}

func (d *welcomeDialog) doTry() {
	if d.onTry != nil {
		d.onTry()
	}
}

// HandleKeyPress maps Enter to Install (the primary action) and Escape to Try
// (dismiss), falling back to the window's default handling otherwise.
func (d *welcomeDialog) HandleKeyPress(ev core.KeyPressEvent) bool {
	switch ev.Key {
	case "Enter":
		d.doInstall()
		return true
	case "Escape":
		d.doTry()
		return true
	}
	return d.Window.HandleKeyPress(ev)
}

// calculateSize sizes the dialog to its text and button row, then adds the
// window chrome — the same two-pass measure MessageBox uses so the content area
// holds every line plus the buttons.
func (d *welcomeDialog) calculateSize() {
	m := d.EffectiveCellMetrics()
	font := d.content.EffectiveFont()

	var maxLineW core.Unit
	for _, line := range d.content.lines {
		if w := font.MeasureText(line); w > maxLineW {
			maxLineW = w
		}
	}
	contentW := m.CellWidth*4 + maxLineW // a 2-column margin on each side

	// Never narrower than the button row (as the content paints it) plus slack.
	var rowW core.Unit
	for _, b := range d.content.buttons {
		rowW += core.Unit(len(b.Text())+4) * m.CellWidth
	}
	if n := len(d.content.buttons); n > 1 {
		rowW += core.Unit(n-1) * m.CellWidth
	}
	minW := rowW + m.CellWidth*4
	if floor := m.CellWidth * 28; minW < floor {
		minW = floor
	}
	if contentW < minW {
		contentW = minW
	}
	if maxW := m.CellWidth * 72; contentW > maxW {
		contentW = maxW
	}

	// Height: top margin + text lines + gap + button row + bottom margin.
	contentH := core.Unit(len(d.content.lines)+4) * m.CellHeight

	d.SetBounds(core.UnitRect{Width: contentW, Height: contentH})
	cb := d.ContentBounds()
	chromeW := contentW - cb.Width
	chromeH := contentH - cb.Height
	if chromeW < 0 {
		chromeW = 0
	}
	if chromeH < 0 {
		chromeH = 0
	}
	d.SetBounds(core.UnitRect{Width: contentW + chromeW, Height: contentH + chromeH})
}

// welcomeContent paints the explanatory text and lays out the two buttons, and
// implements Container so the framework routes input to them (mirrors the
// toolkit's messageBoxContent, using only its exported Button API).
type welcomeContent struct {
	core.TrinketBase
	lines   []string
	install *trinkets.Button
	try     *trinkets.Button
	buttons []*trinkets.Button
}

var _ core.Container = (*welcomeContent)(nil)

func (c *welcomeContent) Children() []core.Trinket {
	out := make([]core.Trinket, len(c.buttons))
	for i, b := range c.buttons {
		out[i] = b
	}
	return out
}

func (c *welcomeContent) AddChild(core.Trinket)    {}
func (c *welcomeContent) RemoveChild(core.Trinket) {}

func (c *welcomeContent) ChildAt(pos core.UnitPoint) core.Trinket {
	for _, b := range c.buttons {
		bb := b.Bounds()
		if pos.X >= bb.X && pos.X < bb.X+bb.Width && pos.Y >= bb.Y && pos.Y < bb.Y+bb.Height {
			return b
		}
	}
	return nil
}

func (c *welcomeContent) Layout()                             {}
func (c *welcomeContent) LayoutManager() core.LayoutManager   { return nil }
func (c *welcomeContent) SetLayoutManager(core.LayoutManager) {}

func (c *welcomeContent) Paint(p *core.Painter) {
	bounds := c.Bounds()
	m := c.EffectiveCellMetrics()
	st := c.GetScheme().GetNormal(true).WithBg(c.EffectiveBackgroundColor())

	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', st)

	// Message text in the proportional font, one DrawText per line.
	font := c.EffectiveFont()
	textX := m.CellWidth * 2
	lineY := m.CellHeight
	for _, line := range c.lines {
		p.DrawText(textX, lineY, line, st, font)
		lineY += m.CellHeight
	}

	// Buttons centered as a row at the bottom.
	c.layoutButtons(bounds, m)
	for _, b := range c.buttons {
		if !b.IsVisible() {
			continue
		}
		bb := b.Bounds()
		b.Paint(p.WithOffset(bb.X, bb.Y))
	}
}

// layoutButtons positions the button row centered along the bottom, snapping the
// origin to a whole column on cell surfaces so painted and hit-test bounds agree.
func (c *welcomeContent) layoutButtons(bounds core.UnitRect, m core.CellMetrics) {
	widths := make([]core.Unit, len(c.buttons))
	var row core.Unit
	for i, b := range c.buttons {
		widths[i] = core.Unit(len(b.Text())+4) * m.CellWidth
		row += widths[i]
	}
	if n := len(c.buttons); n > 1 {
		row += core.Unit(n-1) * m.CellWidth
	}
	x := (bounds.Width - row) / 2
	if x < m.CellWidth {
		x = m.CellWidth
	}
	if m.CellWidth > 0 && !core.FindSmoothPositioning(c.Self()) {
		x = (x / m.CellWidth) * m.CellWidth
	}
	y := bounds.Height - m.CellHeight*2
	for i, b := range c.buttons {
		b.SetBounds(core.UnitRect{X: x, Y: y, Width: widths[i], Height: m.CellHeight * 2})
		x += widths[i] + m.CellWidth
	}
}

func (c *welcomeContent) HandleMousePress(ev core.MousePressEvent) bool {
	for _, b := range c.buttons {
		bb := b.Bounds()
		if ev.X >= bb.X && ev.X < bb.X+bb.Width && ev.Y >= bb.Y && ev.Y < bb.Y+bb.Height {
			l := ev
			l.X -= bb.X
			l.Y -= bb.Y
			return b.HandleMousePress(l)
		}
	}
	return false
}

// HandleMouseMove forwards motion to every button (translated), so the one under
// the pointer hovers and a pressed button still learns the pointer left its
// bounds (and can drop its pressed look) even without access to its private
// pressed flag.
func (c *welcomeContent) HandleMouseMove(ev core.MouseMoveEvent) bool {
	for _, b := range c.buttons {
		bb := b.Bounds()
		l := ev
		l.X -= bb.X
		l.Y -= bb.Y
		b.HandleMouseMove(l)
	}
	return false
}

func (c *welcomeContent) HandleMouseRelease(ev core.MouseReleaseEvent) bool {
	for _, b := range c.buttons {
		bb := b.Bounds()
		l := ev
		l.X -= bb.X
		l.Y -= bb.Y
		if b.HandleMouseRelease(l) {
			return true
		}
	}
	return false
}
