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
// editor still works on a stock build:
//
//   - it holds the text (the `value` property), and
//   - "click to edit" hands that text to the user's external OS editor through
//     a temp file; a second click reads the file back and emits `commit`.
//
// GUI editors don't block per-document (modern Notepad is tabbed; macOS `open`
// and Linux `xdg-open` return immediately), so rather than trying to detect
// when editing finished, the placeholder uses an explicit click-again-to-commit
// flow — peak "functional but lame," but robust on every platform.
//
// The rich contract properties (wrap, tab_size, syntax, line_numbers, caret)
// are accepted and ignored; mew honors them. See editor_protocol.go.
type Editor struct {
	core.TrinketBase
	core.AccessibleTrinket

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
	pressed     bool

	// Event hooks, wired by the protocol bind (editor_protocol.go).
	onCommit func(value string)
	onCancel func()
	onDirty  func(dirty bool)
}

// NewEditor builds a placeholder editor trinket.
func NewEditor() *Editor {
	e := &Editor{}
	e.TrinketBase = *core.NewTrinketBase()
	e.Init(e)
	e.SetFocusPolicy(core.StrongFocus)
	e.SetAccessibleRole(core.RoleTextInput)
	e.SetAccessibleName("editor")
	return e
}

// --- Property setters (public: bound by editor_protocol.go) ---

func (e *Editor) SetValue(s string)       { e.value = s; e.Update() }
func (e *Editor) Value() string           { return e.value }
func (e *Editor) SetPlaceholder(s string) { e.placeholder = s; e.Update() }
func (e *Editor) SetCaption(s string)     { e.caption = s; e.SetAccessibleName(s); e.Update() }
func (e *Editor) SetReadOnly(b bool)      { e.readonly = b; e.Update() }
func (e *Editor) SetFilename(s string)    { e.filename = s }

// --- Event-hook setters (bind) ---

func (e *Editor) SetOnCommit(fn func(string)) { e.onCommit = fn }
func (e *Editor) SetOnCancel(fn func())       { e.onCancel = fn }
func (e *Editor) SetOnDirty(fn func(bool))    { e.onDirty = fn }

// activate is the click/Enter action: start an external edit, or (if one is in
// progress) finish it by reading the file back.
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

// --- Rendering ---

func (e *Editor) Paint(p *core.Painter) {
	b := e.Bounds()
	rect := core.UnitRect{Width: b.Width, Height: b.Height}
	scheme := e.GetScheme()
	s := style.DefaultStyle().WithFg(scheme.GetLabelFG(true)).WithBg(e.EffectiveBackgroundColor())

	p.FillRect(rect, ' ', s)
	p.DrawBox(rect, style.BorderSingle, e.caption, s)

	m := e.EffectiveCellMetrics()
	font := e.EffectiveFont()
	x := m.CellWidth
	y := m.CellHeight

	var line1 string
	switch {
	case e.status != "":
		line1 = e.status
	case e.editing:
		line1 = "editing externally…"
	case e.value != "":
		line1 = editorFirstLine(e.value)
	case e.placeholder != "":
		line1 = e.placeholder
	default:
		line1 = "(empty)"
	}
	p.DrawText(x, y, line1, s, font)

	hint := "[ click to edit ]"
	switch {
	case e.readonly:
		hint = "[ read-only ]"
	case e.editing:
		hint = "[ click when done ]"
	}
	p.DrawText(x, y+m.CellHeight, hint, s, font)
}

func (e *Editor) SizeHint() core.UnitSize {
	m := e.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  m.TextWidth(40) + 2*m.CellWidth,
		Height: m.TextHeight(3) + 2*m.CellHeight,
	}
}

func editorFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// --- Input ---

func (e *Editor) HandleMousePress(ev core.MousePressEvent) bool {
	if ev.Button != core.LeftButton || !e.IsEnabled() {
		return false
	}
	e.SetFocus()
	e.pressed = true
	return true
}

func (e *Editor) HandleMouseRelease(ev core.MouseReleaseEvent) bool {
	if !e.pressed {
		return false
	}
	e.pressed = false
	e.activate()
	return true
}

func (e *Editor) HandleKeyPress(ev core.KeyPressEvent) bool {
	if !e.IsEnabled() {
		return false
	}
	switch ev.Key {
	case "Enter", " ", "Space":
		e.activate()
		return true
	}
	return false
}
