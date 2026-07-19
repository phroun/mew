//go:build !mew

package trinkets

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Editor is vanilla KittyTK's placeholder "editor" trinket: a deliberately
// minimal, functional-but-lame stand-in for a real monospaced multiline editor
// (mew ships the real one under -tags mew). It honors the editor trinket
// contract (docs/editor-trinket.md) just enough that an app needing a text
// editor still works on a stock build.
//
// It is a COMPOSITE: a framed preview of the text plus a real Button child
// ("Edit"). The Button — exposed through Children() — owns focus, Enter/Space
// activation, and screen-reader announcements, so keyboard accessibility
// matches any other button rather than being hand-rolled. Clicking it hands the
// text to the user's external OS editor via a temp file; the next click reads
// the file back and emits `commit`. GUI editors don't block per-document
// (modern Notepad is tabbed; macOS `open` and Linux `xdg-open` return at once),
// so this uses an explicit click-again-to-commit flow rather than detecting
// exit — lame but robust on every platform.
//
// The rich contract properties (wrap, tab_size, syntax, line_numbers, caret)
// are accepted and ignored; mew honors them. See editor_protocol.go.
type Editor struct {
	core.TrinketBase
	core.AccessibleTrinket

	button *Button // the interactive surface (focus/keyboard/a11y live here)

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
// accessibility traversal descends into the Button child.
var _ core.Container = (*Editor)(nil)

// NewEditor builds a placeholder editor trinket.
func NewEditor() *Editor {
	e := &Editor{}
	e.TrinketBase = *core.NewTrinketBase()
	e.Init(e)
	e.SetAccessibleRole(core.RoleGroup)
	e.SetAccessibleName("editor")

	// The real Button is the focusable, keyboard-activatable, announced surface.
	e.button = NewButton("Edit")
	e.button.SetParent(e)
	e.button.SetOnClick(e.activate)

	return e
}

// --- Container: one internal Button child, exposed so the framework routes
// focus, keyboard activation, and accessibility to it natively. ---

func (e *Editor) Children() []core.Trinket { return []core.Trinket{e.button} }
func (e *Editor) AddChild(core.Trinket)    {}
func (e *Editor) RemoveChild(core.Trinket) {}

func (e *Editor) ChildAt(pos core.UnitPoint) core.Trinket {
	b := e.button.Bounds()
	if pos.X >= b.X && pos.X < b.X+b.Width && pos.Y >= b.Y && pos.Y < b.Y+b.Height {
		return e.button
	}
	return nil
}

func (e *Editor) Layout()                             {} // button positioned in Paint
func (e *Editor) LayoutManager() core.LayoutManager   { return nil }
func (e *Editor) SetLayoutManager(core.LayoutManager) {}

// --- Property setters (public: bound by editor_protocol.go) ---

func (e *Editor) SetValue(s string)       { e.value = s; e.Update() }
func (e *Editor) Value() string           { return e.value }
func (e *Editor) SetPlaceholder(s string) { e.placeholder = s; e.Update() }
func (e *Editor) SetCaption(s string)     { e.caption = s; e.SetAccessibleName(s); e.Update() }
func (e *Editor) SetReadOnly(b bool)      { e.readonly = b; e.refreshButton(); e.Update() }
func (e *Editor) SetFilename(s string)    { e.filename = s }

// --- Event-hook setters (bind) ---

func (e *Editor) SetOnCommit(fn func(string)) { e.onCommit = fn }
func (e *Editor) SetOnCancel(fn func())       { e.onCancel = fn }
func (e *Editor) SetOnDirty(fn func(bool))    { e.onDirty = fn }

// refreshButton syncs the button's label and enabled state to editor state.
// (Called on state changes, never from Paint — SetText triggers a repaint.)
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
		e.Update()
		return
	}
	f, err := os.CreateTemp("", "mew-edit-*"+editorTempExt(e.filename))
	if err != nil {
		e.status = "cannot create temp file"
		e.Update()
		return
	}
	_, _ = f.WriteString(e.value)
	_ = f.Close()

	cmd := exec.Command(argv[0], append(argv[1:], f.Name())...)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(f.Name())
		e.status = "cannot launch editor"
		e.Update()
		return
	}
	go func() { _ = cmd.Wait() }() // reap the launcher; the real editor may outlive it

	e.editing = true
	e.tmpPath = f.Name()
	e.editInitial = e.value
	e.status = ""
	e.refreshButton()
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
	e.refreshButton()
	if e.onDirty != nil {
		e.onDirty(false)
	}
	if err == nil && string(data) != e.editInitial {
		e.value = string(data)
		if e.onCommit != nil {
			e.onCommit(e.value)
		}
	} else if e.onCancel != nil {
		e.onCancel()
	}
	e.editInitial = ""
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

// --- Rendering: frame + preview text, then the Button child (painted via a
// translated painter, its bounds recorded for hit-testing). ---

func (e *Editor) Paint(p *core.Painter) {
	b := e.Bounds()
	m := e.EffectiveCellMetrics()
	font := e.EffectiveFont()
	scheme := e.GetScheme()
	s := scheme.GetNormal(true).WithBg(e.EffectiveBackgroundColor())

	p.FillRect(core.UnitRect{Width: b.Width, Height: b.Height}, ' ', s)
	p.DrawBox(core.UnitRect{Width: b.Width, Height: b.Height}, style.BorderSingle, e.caption, s)

	var line string
	switch {
	case e.status != "":
		line = e.status
	case e.editing:
		line = "editing externally…"
	case e.value != "":
		line = editorFirstLine(e.value)
	case e.placeholder != "":
		line = e.placeholder
	default:
		line = "(empty)"
	}
	p.DrawText(m.CellWidth*2, m.CellHeight, line, s, font)

	// The Edit button, bottom-left inside the border.
	btnW := core.Unit(len(e.button.Text())+4) * m.CellWidth
	btnH := m.CellHeight * 2 // face + shadow
	btnX := m.CellWidth * 2
	btnY := b.Height - btnH - m.CellHeight
	if btnY < m.CellHeight*2 {
		btnY = m.CellHeight * 2
	}
	e.button.SetBounds(core.UnitRect{X: btnX, Y: btnY, Width: btnW, Height: btnH})
	e.button.Paint(p.WithOffset(btnX, btnY))
}

func (e *Editor) SizeHint() core.UnitSize {
	m := e.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  m.TextWidth(40) + m.CellWidth*4,
		Height: m.CellHeight * 5, // border + preview + gap + 2-row button + border
	}
}

func editorFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// --- Mouse: forward to the button in its local coordinates. Keyboard, focus,
// and accessibility are handled by the framework via Children(). ---

func (e *Editor) HandleMousePress(ev core.MousePressEvent) bool {
	b := e.button.Bounds()
	if ev.X < b.X || ev.X >= b.X+b.Width || ev.Y < b.Y || ev.Y >= b.Y+b.Height {
		return false
	}
	local := ev
	local.X -= b.X
	local.Y -= b.Y
	return e.button.HandleMousePress(local)
}

func (e *Editor) HandleMouseMove(ev core.MouseMoveEvent) bool {
	b := e.button.Bounds()
	local := ev
	local.X -= b.X
	local.Y -= b.Y
	captured := e.button.pressed // a pressed button keeps receiving moves
	e.button.HandleMouseMove(local)
	return captured
}

func (e *Editor) HandleMouseRelease(ev core.MouseReleaseEvent) bool {
	b := e.button.Bounds()
	local := ev
	local.X -= b.X
	local.Y -= b.Y
	return e.button.HandleMouseRelease(local)
}
