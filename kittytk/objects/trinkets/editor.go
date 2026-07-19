//go:build !mew

package trinkets

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/style"
)

// Editor is vanilla KittyTK's placeholder "editor" trinket: a deliberately
// minimal, functional-but-lame stand-in for a real monospaced multiline editor
// (mew ships the real one under -tags mew). It honors the editor trinket
// contract (docs/editor-trinket.md) just enough that an app needing a text
// editor still works on a stock build.
//
// It composes real trinkets in a vertical box, so behavior and accessibility
// match the rest of the toolkit rather than being hand-rolled:
//
//   - top (stretched): a ScrollArea holding a word-wrapped Label — a scrollable
//     preview of the text, so long content wraps and scrolls instead of
//     overflowing; and
//   - bottom: a real Button ("Edit") that owns focus, Enter/Space activation,
//     and screen-reader announcements.
//
// Clicking Edit hands the text to the user's external OS editor via a temp file;
// the next click reads it back and emits `commit`. GUI editors don't block
// per-document (modern Notepad is tabbed; macOS `open` and Linux `xdg-open`
// return at once), so this uses an explicit click-again-to-commit flow rather
// than detecting exit — lame but robust on every platform.
//
// The rich contract properties (wrap, tab_size, syntax, line_numbers, caret)
// are accepted and ignored; mew honors them. See editor_protocol.go.
type Editor struct {
	core.TrinketBase
	core.AccessibleTrinket

	scroll    *ScrollArea       // holds the preview label (top of the box)
	label     *Label            // word-wrapped preview of the text
	button    *Button           // the interactive surface (bottom of the box)
	box       *layout.BoxLayout // vertical layout: scroll (stretch) over button
	laidOutAt core.UnitRect     // bounds the children were last laid out for

	value       string
	placeholder string
	caption     string
	readonly    bool
	filename    string

	// Click-to-edit state — mutated only on the UI/event goroutine.
	editing     bool
	tmpPath     string
	editInitial string
	status      string

	// Event hooks, wired by the protocol bind (editor_protocol.go).
	onCommit func(value string)
	onCancel func()
	onDirty  func(dirty bool)
}

// The placeholder must be a real Container so the framework's focus and
// accessibility traversal descends into the preview and the Button.
var _ core.Container = (*Editor)(nil)

// NewEditor builds a placeholder editor trinket.
func NewEditor() *Editor {
	e := &Editor{}
	e.TrinketBase = *core.NewTrinketBase()
	e.Init(e)
	e.SetAccessibleRole(core.RoleGroup)
	e.SetAccessibleName("editor")

	// Preview: a word-wrapped label in a scroll area.
	e.label = NewLabel("")
	e.label.SetWordWrap(true)
	e.scroll = NewScrollArea()
	e.scroll.SetContent(e.label)
	e.scroll.SetParent(e)

	// The real Button is the focusable, keyboard-activatable, announced surface.
	e.button = NewButton("Edit")
	e.button.SetParent(e)
	e.button.SetOnClick(e.activate)

	// Vertical box: the scroll area stretches to fill, the button takes its
	// natural height at the bottom.
	e.box = layout.NewVBoxLayout()
	e.box.SetMetricsSource(e)
	e.box.AddTrinketWithStretch(e.scroll, 1)
	e.box.AddTrinket(e.button)

	e.refreshPreview()
	return e
}

// --- Container: the preview scroll area and the Button, exposed so the
// framework routes focus, keyboard, and accessibility to them natively. ---

func (e *Editor) Children() []core.Trinket { return []core.Trinket{e.scroll, e.button} }
func (e *Editor) AddChild(core.Trinket)    {}
func (e *Editor) RemoveChild(core.Trinket) {}

func (e *Editor) ChildAt(pos core.UnitPoint) core.Trinket {
	c, _ := e.childHit(pos.X, pos.Y)
	return c
}

// Layout positions the children inside the frame using the vertical box.
func (e *Editor) Layout() {
	b := e.Bounds()
	m := e.EffectiveCellMetrics()
	interior := core.UnitRect{
		X:      m.CellWidth,
		Y:      m.CellHeight,
		Width:  b.Width - 2*m.CellWidth,
		Height: b.Height - 2*m.CellHeight,
	}
	e.box.Layout(e, interior)
	e.laidOutAt = b
}

func (e *Editor) LayoutManager() core.LayoutManager   { return e.box }
func (e *Editor) SetLayoutManager(core.LayoutManager) {} // fixed internal layout

// childHit returns the child under (x,y) in editor-local coordinates.
func (e *Editor) childHit(x, y core.Unit) (core.Trinket, core.UnitRect) {
	for _, c := range []core.Trinket{e.button, e.scroll} {
		b := c.Bounds()
		if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
			return c, b
		}
	}
	return nil, core.UnitRect{}
}

// --- Property setters (public: bound by editor_protocol.go) ---

func (e *Editor) SetValue(s string)       { e.value = s; e.refreshPreview(); e.Update() }
func (e *Editor) Value() string           { return e.value }
func (e *Editor) SetPlaceholder(s string) { e.placeholder = s; e.refreshPreview(); e.Update() }
func (e *Editor) SetCaption(s string)     { e.caption = s; e.SetAccessibleName(s); e.Update() }
func (e *Editor) SetReadOnly(b bool)      { e.readonly = b; e.refreshButton(); e.Update() }
func (e *Editor) SetFilename(s string)    { e.filename = s }

// --- Event-hook setters (bind) ---

func (e *Editor) SetOnCommit(fn func(string)) { e.onCommit = fn }
func (e *Editor) SetOnCancel(fn func())       { e.onCancel = fn }
func (e *Editor) SetOnDirty(fn func(bool))    { e.onDirty = fn }

// refreshPreview syncs the preview label to editor state. Called on state
// changes, never from Paint (SetText triggers a repaint).
func (e *Editor) refreshPreview() {
	var t string
	switch {
	case e.status != "":
		t = e.status
	case e.editing:
		t = "editing externally…"
	case e.value != "":
		t = e.value
	case e.placeholder != "":
		t = e.placeholder
	default:
		t = "(empty)"
	}
	e.label.SetText(t)
}

// refreshButton syncs the button's label and enabled state to editor state.
func (e *Editor) refreshButton() {
	switch {
	case e.readonly:
		e.button.SetText("View")
		e.button.SetEnabled(false)
	case e.editing:
		e.button.SetText("Done")
		e.button.SetEnabled(true)
	default:
		e.button.SetText("Edit")
		e.button.SetEnabled(true)
	}
}

// activate is the button's action: start an external edit, or finish one.
func (e *Editor) activate() {
	switch {
	case e.readonly:
		return
	case e.editing:
		e.finishEdit()
	default:
		e.startEdit()
	}
}

func (e *Editor) startEdit() {
	argv, ok := externalEditorArgv()
	if !ok {
		e.status = "no external editor found"
		e.refreshPreview()
		e.Update()
		return
	}
	f, err := os.CreateTemp("", "mew-edit-*"+editorTempExt(e.filename))
	if err != nil {
		e.status = "cannot create temp file"
		e.refreshPreview()
		e.Update()
		return
	}
	_, _ = f.WriteString(e.value)
	_ = f.Close()

	cmd := exec.Command(argv[0], append(argv[1:], f.Name())...)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(f.Name())
		e.status = "cannot launch editor"
		e.refreshPreview()
		e.Update()
		return
	}
	go func() { _ = cmd.Wait() }() // reap the launcher; the real editor may outlive it

	e.editing = true
	e.tmpPath = f.Name()
	e.editInitial = e.value
	e.status = ""
	e.refreshButton()
	e.refreshPreview()
	if e.onDirty != nil {
		e.onDirty(true)
	}
	e.Update()
}

func (e *Editor) finishEdit() {
	data, err := os.ReadFile(e.tmpPath)
	_ = os.Remove(e.tmpPath)
	e.editing = false
	e.tmpPath = ""
	committed := err == nil && string(data) != e.editInitial
	if committed {
		e.value = string(data)
	}
	e.editInitial = ""
	e.refreshButton()
	e.refreshPreview()
	if e.onDirty != nil {
		e.onDirty(false)
	}
	if committed {
		if e.onCommit != nil {
			e.onCommit(e.value)
		}
	} else if e.onCancel != nil {
		e.onCancel()
	}
	e.Update()
}

// editorTempExt keeps the temp file's extension so the external editor picks
// sensible syntax highlighting; defaults to .txt.
func editorTempExt(filename string) string {
	if i := strings.LastIndexByte(filename, '.'); i >= 0 && i > strings.LastIndexByte(filename, '/') {
		return filename[i:]
	}
	return ".txt"
}

// externalEditorArgv returns the command that opens a text file in the OS's
// default editor. These launchers do not block until the document is closed,
// which is why the placeholder commits on the next click rather than on exit.
func externalEditorArgv() ([]string, bool) {
	switch runtime.GOOS {
	case "windows":
		return []string{"notepad"}, true
	case "darwin":
		return []string{"open", "-t"}, true // -t: the default text editor
	default: // linux, *bsd — a desktop's default text app
		if p, err := exec.LookPath("xdg-open"); err == nil {
			return []string{p}, true
		}
		return nil, false
	}
}

// --- Rendering: frame + caption, then the laid-out children. ---

func (e *Editor) Paint(p *core.Painter) {
	b := e.Bounds()
	scheme := e.GetScheme()
	s := scheme.GetNormal(true).WithBg(e.EffectiveBackgroundColor())

	p.FillRect(core.UnitRect{Width: b.Width, Height: b.Height}, ' ', s)
	p.DrawBox(core.UnitRect{Width: b.Width, Height: b.Height}, style.BorderSingle, e.caption, s)

	// (Re)position the children when the frame size changed.
	if b != e.laidOutAt {
		e.Layout()
	}

	for _, c := range []core.Trinket{e.scroll, e.button} {
		if !c.IsVisible() {
			continue
		}
		cb := c.Bounds()
		c.Paint(p.WithOffset(cb.X, cb.Y))
	}
}

func (e *Editor) SizeHint() core.UnitSize {
	m := e.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  m.TextWidth(40) + m.CellWidth*4,
		Height: m.CellHeight * 8, // border + a few preview rows + button + border
	}
}

// --- Mouse: forward to the child under the pointer in its local coordinates.
// Keyboard, focus, and accessibility are handled by the framework via
// Children(). ---

func (e *Editor) HandleMousePress(ev core.MousePressEvent) bool {
	c, b := e.childHit(ev.X, ev.Y)
	if c == nil {
		return false
	}
	local := ev
	local.X -= b.X
	local.Y -= b.Y
	return c.HandleMousePress(local)
}

func (e *Editor) HandleMouseRelease(ev core.MouseReleaseEvent) bool {
	handled := false
	if e.button.pressed { // the button may have captured the press
		bb := e.button.Bounds()
		local := ev
		local.X -= bb.X
		local.Y -= bb.Y
		handled = e.button.HandleMouseRelease(local) || handled
	}
	if c, b := e.childHit(ev.X, ev.Y); c != nil && c != e.button {
		local := ev
		local.X -= b.X
		local.Y -= b.Y
		handled = c.HandleMouseRelease(local) || handled
	}
	return handled
}

func (e *Editor) HandleMouseMove(ev core.MouseMoveEvent) bool {
	if e.button.pressed { // a pressed button keeps receiving moves
		bb := e.button.Bounds()
		local := ev
		local.X -= bb.X
		local.Y -= bb.Y
		e.button.HandleMouseMove(local)
		return true
	}
	if c, b := e.childHit(ev.X, ev.Y); c != nil {
		local := ev
		local.X -= b.X
		local.Y -= b.Y
		return c.HandleMouseMove(local)
	}
	bb := e.button.Bounds() // off both: let the button clear hover state
	local := ev
	local.X -= bb.X
	local.Y -= bb.Y
	e.button.HandleMouseMove(local)
	return false
}

func (e *Editor) HandleMouseWheel(ev core.MouseWheelEvent) bool {
	b := e.scroll.Bounds()
	local := ev
	local.X -= b.X
	local.Y -= b.Y
	return e.scroll.HandleMouseWheel(local)
}
