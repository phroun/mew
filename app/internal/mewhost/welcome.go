package mewhost

import (
	"os/exec"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/mew"
	"github.com/phroun/mew-app/internal/selfinstall"
)

// welcomeWrapCols is the reading width (in cells) the welcome copy is wrapped to;
// the window is sized to it so the text word-wraps rather than being hand-split.
const welcomeWrapCols = 48

// maybeShowWelcome opens the first-run welcome window when a graphical host
// starts a not-yet-installed copy of mew on a platform with a self-installer
// (Windows and macOS — elsewhere selfinstall reports the first run already done,
// so nothing shows). The window explains what mew is and offers two choices, in
// the "lame trinket" spirit (a label over a row of real Buttons):
//
//   - Install — copy mew into place (Start Menu + PATH on Windows, the
//     Applications folder on macOS), launch the freshly installed copy, and quit
//     this one.
//   - Try — dismiss the window and drop through to the normal mew editor already
//     running behind it. Nothing is written, so an uninstalled copy keeps
//     offering to install on each launch.
//
// It is presented as a WINDOW-level modal owned by the root editor: dlg.SetOwner
// (root) before AddWindow. The owner matters — the window manager scopes modal
// blocking by owner first, then app, else system (registerModalLocked). A modal
// with no owner (and no app id) lands on the SYSTEM stack, which surfaces but
// does not gate the solo editor's own surface — so it showed but didn't actually
// block. Owning it by the editor makes it (a) an owned overlay that floats above
// the editor in the z-order and (b) a window modal that blocks that editor. It is
// still WindowTypeModal, not Dialog: a Dialog floats above its owner but blocks
// nothing, and we want the first-run gate to block.
//
// Closing goes through window.Close (the manager ties modal unregistration to the
// window's close for owner/app modals, so the stack is popped cleanly) — unlike a
// system modal, which needs WindowManager.CloseModal.
func maybeShowWelcome(desktop *trinkets.Desktop, application *app.Application, root *window.Window, launchArgs []string, graphical bool) {
	if !graphical || !selfinstall.Available() || selfinstall.FirstRunDone() {
		return
	}
	dlg := newWelcomeDialog(
		"Welcome to mew",
		welcomeLines(),
		func() { // Install
			exe, err := selfinstall.Install()
			if err != nil {
				showMewError(application, root, "Install failed", err.Error())
				return
			}
			// Launch the freshly installed copy (with the same files) and bow out.
			if exe != "" {
				_ = exec.Command(exe, launchArgs...).Start()
			}
			desktop.Quit()
		},
		func() {}, // Try — dismiss (doTry closes the dialog); the editor is behind us.
	)
	dlg.SetOwner(root)
	application.AddWindow(&dlg.Window)
	desktop.RequestUpdate()
}

// welcomeLines is the explanatory copy shown in the welcome window, as
// paragraphs (welcomeContent word-wraps them to the window width). The install
// destination is platform-specific (Start Menu + PATH, or Applications).
func welcomeLines() []string {
	return []string{
		"A programmable cross-platform text, prose, and code editor in the WordStar tradition.",
		"",
		"You're running mew straight from the file you downloaded.",
		"",
		"Install adds mew to " + selfinstall.InstallLocationPhrase() + ", then opens the installed copy. Try just opens the editor now, without installing anything.",
		"",
		"mew " + mew.FullVersion(),
	}
}

// showMewError pops a simple error dialog owned by the root editor (a window
// modal that floats above and blocks it, like the welcome it replaces). The
// MessageBox unregisters its modal on close, so no explicit CloseModal is needed.
func showMewError(application *app.Application, root *window.Window, title, text string) {
	mb := trinkets.NewMessageBox(title, text, trinkets.ButtonOK)
	mb.SetIcon(trinkets.IconError)
	mb.SetOwner(root)
	application.AddWindow(&mb.Window)
	mb.ResizeToFitContent()
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

	c := &welcomeContent{paras: lines}
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

// doInstall / doTry run the wired action, then close the dialog. Closing an
// owner-scoped modal unregisters it from the manager's modal stack (the
// AddWindow path ties unregistration to the window's close), so the editor
// becomes interactive again after Try, and the install path has already quit.
func (d *welcomeDialog) doInstall() {
	if d.onInstall != nil {
		d.onInstall()
	}
	d.Close()
}

func (d *welcomeDialog) doTry() {
	if d.onTry != nil {
		d.onTry()
	}
	d.Close()
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

	// Wrap the copy to a fixed reading width, then size the window to that width
	// so Paint (which wraps to its bounds) reproduces the very same lines. Text
	// area = contentW - 2*textX (textX is a 2-column margin), so contentW is the
	// wrap width plus a 2-column margin on each side.
	textW := m.CellWidth * welcomeWrapCols
	d.content.wrap(textW, font)
	contentW := textW + m.CellWidth*4

	// Never narrower than the button row (as the content paints it) plus slack.
	var rowW core.Unit
	for _, b := range d.content.buttons {
		rowW += core.Unit(len(b.Text())+4) * m.CellWidth
	}
	if n := len(d.content.buttons); n > 1 {
		rowW += core.Unit(n-1) * m.CellWidth
	}
	if minW := rowW + m.CellWidth*4; contentW < minW {
		contentW = minW
	}

	// Height: top margin + wrapped lines + gap + button row + bottom margin.
	contentH := core.Unit(len(d.content.wrapped)+4) * m.CellHeight

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
	paras     []string   // the copy, as paragraphs (wrapped to the window width)
	wrapped   []string   // paras flowed to wrapWidth; recomputed when width changes
	wrapWidth core.Unit  // the text width wrapped was computed for
	install   *trinkets.Button
	try       *trinkets.Button
	buttons   []*trinkets.Button
}

// wrap flows the paragraphs to textWidth (cached by width), so the copy is
// written as natural paragraphs and word-wraps to the window - like the
// placeholder editor's wrapped label, not the message box's literal
// one-line-per-entry.
func (c *welcomeContent) wrap(textWidth core.Unit, font *core.Font) {
	if textWidth <= 0 || font == nil {
		if c.wrapped == nil {
			c.wrapped = c.paras
		}
		return
	}
	if c.wrapped != nil && textWidth == c.wrapWidth {
		return
	}
	c.wrapped = wrapParagraphs(c.paras, textWidth, font)
	c.wrapWidth = textWidth
}

// wrapParagraphs word-wraps each paragraph to maxWidth (measured in the given
// font), preserving blank entries as blank spacer lines. Mirrors the toolkit's
// internal wrapText, kept here so this package needn't reach into it.
func wrapParagraphs(paras []string, maxWidth core.Unit, font *core.Font) []string {
	space := font.MeasureText(" ")
	var out []string
	for _, para := range paras {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "") // blank line between paragraphs
			continue
		}
		var line strings.Builder
		var w core.Unit
		for _, word := range words {
			ww := font.MeasureText(word)
			if w > 0 && w+space+ww > maxWidth {
				out = append(out, line.String())
				line.Reset()
				w = 0
			}
			if w > 0 {
				line.WriteByte(' ')
				w += space
			}
			line.WriteString(word)
			w += ww
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return out
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

	// Message text in the proportional font, word-wrapped to the text area.
	font := c.EffectiveFont()
	textX := m.CellWidth * 2
	c.wrap(bounds.Width-textX*2, font) // no-op when already wrapped to this width
	lineY := m.CellHeight
	for _, line := range c.wrapped {
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
