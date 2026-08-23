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

// maybeShowWelcome shows the first-run welcome IN the app's own window when a
// graphical host starts a not-yet-installed copy of mew on a platform with a
// self-installer (Windows and macOS — elsewhere selfinstall reports the first
// run already done, so nothing shows), and reports whether it took the window
// over. It offers two choices, in the "lame trinket" spirit (wrapped copy over
// a row of real Buttons):
//
//   - Install — copy mew into place (Start Menu + PATH on Windows, the
//     Applications folder on macOS), launch the freshly installed copy, and quit
//     this one.
//   - Try — put the editor back and get out of the way. Nothing is written, so
//     an uninstalled copy keeps offering to install on each launch.
//
// It is the window's CONTENT, not a dialog over it. mew runs solo — its window
// IS the display — so a welcome floating above the editor had mew's own window
// behind it, answering the question it was asking: someone who chooses Install
// never wanted this session at all, and a window of the throwaway copy showing
// underneath is at best noise. Replacing the content instead means one window
// with nothing behind it, and the editor trinket never paints, so the mew
// session inside it never starts until Try asks for it.
//
// The window keeps its own chrome - it IS mew's window - but not mew's menus:
// the caller holds those back until Try (see welcomeWanted), so the welcome
// carries the minimal bar the desktop synthesizes, with Quit on it. declareMenus
// is what brings the real ones in, alongside the editor.
func maybeShowWelcome(desktop *trinkets.Desktop, application *app.Application, root *window.Window, launchArgs []string, graphical bool, declareMenus func()) bool {
	if !welcomeWanted(graphical) || root == nil {
		return false
	}
	showWelcomeIn(desktop, application, root, launchArgs, declareMenus)
	return true
}

// welcomeWanted reports whether this launch shows the first-run welcome: a
// graphical host running a not-yet-installed copy on a platform with a
// self-installer. Asked BEFORE anything is built, because what it answers
// changes what gets built - the app declares no menus while the welcome holds
// the window.
func welcomeWanted(graphical bool) bool {
	return graphical && selfinstall.Available() && !selfinstall.FirstRunDone()
}

// showWelcomeIn is the takeover itself, split from the first-run test above so
// what it does to the window can be exercised on any platform. It remembers
// what the window held and hands both answers a way back to it.
func showWelcomeIn(desktop *trinkets.Desktop, application *app.Application, root *window.Window, launchArgs []string, declareMenus func()) *welcomeContent {
	editor := root.Content()
	title := root.Title()

	// Show mew: the window goes back to exactly what it was built as. Posted
	// rather than run inline, so the welcome content is off the window before
	// the editor takes its place (this runs from a button's own click).
	showEditor := func() {
		desktop.Post(func() {
			root.SetTitle(title)
			root.SetContent(editor)
			if declareMenus != nil {
				declareMenus()
			}
			desktop.RequestUpdate()
		})
	}

	c := newWelcomeContent(welcomeLines(),
		func() { // Install
			exe, err := selfinstall.Install()
			if err != nil {
				// The install failed, so this copy IS the session after all:
				// show the editor behind the error rather than leaving the
				// welcome up with nothing it can still do.
				showMewError(application, root, "Install failed", err.Error())
				showEditor()
				return
			}
			// Launch the freshly installed copy (with the same files) and bow
			// out. Nothing was started here, so there is nothing to close --
			// and nothing to ask, which is why this quit is the forced one.
			if exe != "" {
				_ = exec.Command(exe, launchArgs...).Start()
			}
			desktop.ForceQuit()
		},
		showEditor) // Try

	root.SetTitle("Welcome to mew")
	root.SetContent(c)
	desktop.RequestUpdate()
	return c
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

// showMewError pops a simple error dialog owned by the app's window (a window
// modal that floats above and blocks it). The MessageBox unregisters its modal
// on close, so no explicit CloseModal is needed.
func showMewError(application *app.Application, root *window.Window, title, text string) {
	mb := trinkets.NewMessageBox(title, text, trinkets.ButtonOK)
	mb.SetIcon(trinkets.IconError)
	mb.SetOwner(root)
	application.AddWindow(&mb.Window)
	mb.ResizeToFitContent()
}

// welcomeContent paints the explanatory text and lays out the two buttons, and
// implements Container so the framework routes input to them (mirrors the
// toolkit's messageBoxContent, using only its exported Button API).
type welcomeContent struct {
	core.TrinketBase
	paras     []string  // the copy, as paragraphs (wrapped to the window width)
	wrapped   []string  // paras flowed to wrapWidth; recomputed when width changes
	wrapWidth core.Unit // the text width wrapped was computed for
	install   *trinkets.Button
	try       *trinkets.Button
	buttons   []*trinkets.Button

	onInstall func()
	onTry     func()
	answered  bool // the question is answered once, whichever route answers it
}

// newWelcomeContent builds the welcome as a content trinket: the copy over a
// row of two real Buttons, so focus, keyboard activation and screen-reader
// announcements are the toolkit's rather than hand-rolled.
func newWelcomeContent(lines []string, onInstall, onTry func()) *welcomeContent {
	c := &welcomeContent{paras: lines, onInstall: onInstall, onTry: onTry}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	c.SetFocusPolicy(core.StrongFocus)

	c.install = trinkets.NewButton("Install")
	c.install.SetParent(c)
	c.install.SetOnClick(func() { c.answer(c.onInstall) })
	c.try = trinkets.NewButton("Try")
	c.try.SetParent(c)
	c.try.SetOnClick(func() { c.answer(c.onTry) })
	c.buttons = []*trinkets.Button{c.install, c.try}
	return c
}

// answer runs the chosen action, once: the two buttons and the keyboard reach
// the same two answers, and the window is replaced by whichever runs first.
func (c *welcomeContent) answer(fn func()) {
	if c.answered {
		return
	}
	c.answered = true
	if fn != nil {
		fn()
	}
}

// HandleKeyPress answers from the keyboard: the accept key installs (the
// primary action) and Escape tries. Both spellings of accept — Return is the
// home-row key, Enter the keypad one — since naming only one would leave the
// other dead. A focused Button answers for itself; this is the fallback for
// when the content holds the focus.
func (c *welcomeContent) HandleKeyPress(ev core.KeyPressEvent) bool {
	switch ev.Key {
	case "Return", "Enter":
		c.answer(c.onInstall)
		return true
	case "Escape":
		c.answer(c.onTry)
		return true
	}
	return false
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

// Paint centers the copy and the button row in the window. This IS the window
// now rather than a dialog sized to its text, so the block sits in the middle
// of whatever mew's window happens to be, at a fixed reading width.
func (c *welcomeContent) Paint(p *core.Painter) {
	bounds := c.Bounds()
	m := c.EffectiveCellMetrics()
	st := c.GetScheme().GetNormal(true).WithBg(c.EffectiveBackgroundColor())

	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', st)

	// Message text in the proportional font, wrapped to a reading width (or to
	// the window, when the window is the narrower of the two).
	font := c.EffectiveFont()
	textW := m.CellWidth * welcomeWrapCols
	if avail := bounds.Width - m.CellWidth*4; avail > 0 && avail < textW {
		textW = avail
	}
	c.wrap(textW, font) // no-op when already wrapped to this width

	// The block is the copy, a blank line, and the button row (two rows tall).
	blockH := core.Unit(len(c.wrapped)+3) * m.CellHeight
	top := (bounds.Height - blockH) / 2
	if top < m.CellHeight {
		top = m.CellHeight
	}
	textX := (bounds.Width - textW) / 2
	if textX < m.CellWidth {
		textX = m.CellWidth
	}
	if m.CellWidth > 0 && !core.FindSmoothPositioning(c.Self()) {
		textX = (textX / m.CellWidth) * m.CellWidth
		top = (top / m.CellHeight) * m.CellHeight
	}

	lineY := top
	for _, line := range c.wrapped {
		p.DrawText(textX, lineY, line, st, font)
		lineY += m.CellHeight
	}

	// Buttons centered as a row under the copy.
	c.layoutButtons(bounds, m, lineY+m.CellHeight)
	for _, b := range c.buttons {
		if !b.IsVisible() {
			continue
		}
		bb := b.Bounds()
		b.Paint(p.WithOffset(bb.X, bb.Y))
	}
}

// layoutButtons positions the button row centered at y, snapping the origin to a
// whole column on cell surfaces so painted and hit-test bounds agree. y is
// clamped so the row never runs off the bottom of a short window.
func (c *welcomeContent) layoutButtons(bounds core.UnitRect, m core.CellMetrics, y core.Unit) {
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
	if max := bounds.Height - m.CellHeight*2; y > max {
		y = max
	}
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
