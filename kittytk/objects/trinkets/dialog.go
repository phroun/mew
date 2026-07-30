// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// snapCellX floors an X origin to a whole column on cell surfaces, where
// drawing is column-quantized, so a trinket's stored bounds (used for
// hit-testing) match the column its content actually paints in. Smooth
// (pixel) surfaces keep sub-column precision.
func snapCellX(self core.Trinket, m core.CellMetrics, x core.Unit) core.Unit {
	if m.CellWidth > 0 && !core.FindSmoothPositioning(self) {
		x = (x / m.CellWidth) * m.CellWidth
	}
	return x
}

// DialogButton represents standard dialog buttons.
type DialogButton int

const (
	ButtonOK DialogButton = 1 << iota
	ButtonCancel
	ButtonYes
	ButtonNo
	ButtonRetry
	ButtonIgnore
	ButtonAbort
	ButtonSave
	ButtonDiscard
	ButtonApply
	ButtonHelp
)

// DialogResult represents the result of a dialog.
type DialogResult int

const (
	ResultNone DialogResult = iota
	ResultOK
	ResultCancel
	ResultYes
	ResultNo
	ResultRetry
	ResultIgnore
	ResultAbort
	ResultSave
	ResultDiscard
	ResultApply
	ResultHelp
)

// MessageBoxIcon represents message box icons.
type MessageBoxIcon int

const (
	IconNone MessageBoxIcon = iota
	IconInformation
	IconWarning
	IconError
	IconQuestion
)

// MessageBox displays a message with buttons.
type MessageBox struct {
	window.Window

	content *messageBoxContent
	buttons DialogButton
	result  DialogResult

	// Callbacks
	onFinished func(result DialogResult)
}

// messageBoxContent is the content trinket for a MessageBox.
type messageBoxContent struct {
	core.TrinketBase
	icon           MessageBoxIcon
	text           string
	buttonTrinkets []*Button
	onDone         func(result DialogResult)
}

// Children returns the button trinkets as children.
func (c *messageBoxContent) Children() []core.Trinket {
	children := make([]core.Trinket, len(c.buttonTrinkets))
	for i, btn := range c.buttonTrinkets {
		children[i] = btn
	}
	return children
}

// AddChild is not used for messageBoxContent.
func (c *messageBoxContent) AddChild(child core.Trinket) {}

// RemoveChild is not used for messageBoxContent.
func (c *messageBoxContent) RemoveChild(child core.Trinket) {}

// ChildAt returns the child trinket at the given position.
func (c *messageBoxContent) ChildAt(pos core.UnitPoint) core.Trinket {
	for _, btn := range c.buttonTrinkets {
		bounds := btn.Bounds()
		if pos.X >= bounds.X && pos.X < bounds.X+bounds.Width &&
			pos.Y >= bounds.Y && pos.Y < bounds.Y+bounds.Height {
			return btn
		}
	}
	return nil
}

// Layout arranges children (buttons are laid out in Paint).
func (c *messageBoxContent) Layout() {}

// LayoutManager returns nil (custom layout).
func (c *messageBoxContent) LayoutManager() core.LayoutManager { return nil }

// SetLayoutManager does nothing (custom layout).
func (c *messageBoxContent) SetLayoutManager(lm core.LayoutManager) {}

// NewMessageBox creates a new message box.
func NewMessageBox(title, text string, buttons DialogButton) *MessageBox {
	m := &MessageBox{
		buttons: buttons,
		result:  ResultNone,
	}
	m.Window = *window.NewWindow(title)
	m.SetType(window.WindowTypeModal)
	m.SetFlags(window.WindowFlagNoResize)

	// Create content trinket
	m.content = &messageBoxContent{
		text:   text,
		onDone: m.done,
	}
	m.content.TrinketBase = *core.NewTrinketBase()
	m.content.Init(m.content)
	m.content.SetFocusPolicy(core.StrongFocus)
	m.content.createButtons(buttons)

	// Set as window content
	m.SetContent(m.content)
	m.calculateSize()
	return m
}

// createButtons creates the dialog buttons for the content.
func (c *messageBoxContent) createButtons(buttons DialogButton) {
	buttonDefs := []struct {
		flag   DialogButton
		text   string
		result DialogResult
	}{
		{ButtonOK, "OK", ResultOK},
		{ButtonCancel, "Cancel", ResultCancel},
		{ButtonYes, "Yes", ResultYes},
		{ButtonNo, "No", ResultNo},
		{ButtonRetry, "Retry", ResultRetry},
		{ButtonIgnore, "Ignore", ResultIgnore},
		{ButtonAbort, "Abort", ResultAbort},
		{ButtonSave, "Save", ResultSave},
		{ButtonDiscard, "Discard", ResultDiscard},
		{ButtonApply, "Apply", ResultApply},
		{ButtonHelp, "Help", ResultHelp},
	}

	for _, def := range buttonDefs {
		if buttons&def.flag != 0 {
			btn := NewButton(def.text)
			btn.SetParent(c)     // Set parent so button can inherit background color
			result := def.result // Capture for closure
			btn.SetOnClick(func() {
				if c.onDone != nil {
					c.onDone(result)
				}
			})
			c.buttonTrinkets = append(c.buttonTrinkets, btn)
		}
	}
}

// ResizeToFitContent recomputes the dialog's size from its text and buttons
// using the window's current chrome. Call it after the dialog is parented (so
// the graphical vs cell frame insets are known) to guarantee the content area
// holds every text line plus the button row.
func (m *MessageBox) ResizeToFitContent() { m.calculateSize() }

// calculateSize sets the dialog size based on content.
func (m *MessageBox) calculateSize() {
	metrics := m.EffectiveCellMetrics()
	font := m.content.EffectiveFont()

	lines := strings.Split(m.content.text, "\n")

	// Width: measure each line in the ACTUAL (proportional) font, not one cell
	// per character - assuming a full cell per glyph makes the dialog far too
	// wide on graphical surfaces. Reserve the icon gutter on the left (matching
	// Paint's textX) and a margin on the right.
	leftGutter := metrics.CellWidth * 6
	rightMargin := metrics.CellWidth * 4
	var maxLineW core.Unit
	for _, line := range lines {
		if w := font.MeasureText(line); w > maxLineW {
			maxLineW = w
		}
	}
	contentW := leftGutter + maxLineW + rightMargin

	// Absolute minimum: the dialog must be wide enough for its button row (as
	// Paint measures it) plus a little slack on each side, so even a blank
	// dialog comfortably holds its OK button.
	var rowWidth core.Unit
	for _, btn := range m.content.buttonTrinkets {
		rowWidth += core.Unit(len(btn.Text())+4) * metrics.CellWidth
	}
	if n := len(m.content.buttonTrinkets); n > 1 {
		rowWidth += core.Unit(n-1) * metrics.CellWidth // inter-button gaps
	}
	minW := rowWidth + metrics.CellWidth*4
	if floor := metrics.CellWidth * 16; minW < floor {
		minW = floor
	}
	if maxW := metrics.CellWidth * 64; contentW > maxW {
		contentW = maxW
	}
	if contentW < minW {
		contentW = minW
	}

	// Height: 1 top margin + text lines + 1 gap + 1 button row + 1 bottom margin
	textHeight := len(lines) + 4

	// These are CONTENT dimensions (what messageBoxContent.Paint lays text and
	// the button into). The window also spends rows on its title bar and frame,
	// so measure that chrome and add it - otherwise the content area is short
	// and the OK button rides up over the last line of text.
	contentH := core.Unit(textHeight) * metrics.CellHeight

	m.SetBounds(core.UnitRect{Width: contentW, Height: contentH})
	cb := m.ContentBounds()
	chromeW := contentW - cb.Width
	chromeH := contentH - cb.Height
	if chromeW < 0 {
		chromeW = 0
	}
	if chromeH < 0 {
		chromeH = 0
	}
	m.SetBounds(core.UnitRect{Width: contentW + chromeW, Height: contentH + chromeH})
}

// getIconText returns the text representation of the icon.
func (c *messageBoxContent) getIconText() string {
	switch c.icon {
	case IconInformation:
		return "ℹ"
	case IconWarning:
		return "⚠"
	case IconError:
		return "✖"
	case IconQuestion:
		return "?"
	default:
		return ""
	}
}

// SetIcon sets the message box icon.
func (m *MessageBox) SetIcon(icon MessageBoxIcon) {
	m.content.icon = icon
}

// SetText replaces the message text.
func (m *MessageBox) SetText(text string) {
	m.content.text = text
	m.calculateSize()
	m.Update()
}

// SetButtons replaces the button set.
func (m *MessageBox) SetButtons(buttons DialogButton) {
	m.buttons = buttons
	m.content.createButtons(buttons)
	m.calculateSize()
	m.Update()
}

// Buttons returns the current button set.
func (m *MessageBox) Buttons() DialogButton {
	return m.buttons
}

// SetOnFinished sets the finished callback.
func (m *MessageBox) SetOnFinished(handler func(result DialogResult)) {
	m.onFinished = handler
}

// Result returns the dialog result.
func (m *MessageBox) Result() DialogResult {
	return m.result
}

// done completes the dialog with the given result.
func (m *MessageBox) done(result DialogResult) {
	m.result = result
	if m.onFinished != nil {
		m.onFinished(result)
	}
	m.Close()
}

// Paint renders the message box content.
func (c *messageBoxContent) Paint(p *core.Painter) {
	bounds := c.Bounds()
	scheme := c.GetScheme()
	metrics := c.EffectiveCellMetrics()
	inheritedBG := c.EffectiveBackgroundColor()

	// Build style from scheme with inherited background
	contentStyle := scheme.GetNormal(true).WithBg(inheritedBG)

	// Draw background
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', contentStyle)

	// Draw icon (indented two columns from the left edge so it isn't crowding
	// the frame).
	iconText := c.getIconText()
	textY := metrics.CellHeight
	if iconText != "" {
		p.DrawCell(metrics.CellWidth*3, textY, []rune(iconText)[0], contentStyle)
	}

	// Draw message text in the proportional font, one DrawText per line. textX
	// matches the icon indent plus the icon gutter (kept in sync with
	// calculateSize's leftGutter).
	font := c.EffectiveFont()
	textX := metrics.CellWidth * 6
	lineY := textY
	for _, line := range strings.Split(c.text, "\n") {
		p.DrawText(textX, lineY, line, contentStyle, font)
		lineY += metrics.CellHeight
	}

	// Lay the buttons out at the bottom, centered as a row.
	buttonY := bounds.Height - metrics.CellHeight*2

	btnWidths := make([]core.Unit, len(c.buttonTrinkets))
	rowWidth := core.Unit(0)
	for i, btn := range c.buttonTrinkets {
		btnWidths[i] = core.Unit(len(btn.Text())+4) * metrics.CellWidth
		rowWidth += btnWidths[i]
	}
	if n := len(c.buttonTrinkets); n > 1 {
		rowWidth += core.Unit(n-1) * metrics.CellWidth // inter-button gaps
	}

	buttonX := (bounds.Width - rowWidth) / 2
	if buttonX < metrics.CellWidth {
		buttonX = metrics.CellWidth
	}
	// Centering can land the row origin on a half column when the slack is an
	// odd number of columns; the cell backend draws on whole columns, so snap
	// it or the buttons' stored bounds (used for hit-testing) drift half a
	// column from where they paint.
	buttonX = snapCellX(c.Self(), metrics, buttonX)

	for i, btn := range c.buttonTrinkets {
		btnWidth := btnWidths[i]
		btn.SetBounds(core.UnitRect{
			X:      buttonX,
			Y:      buttonY,
			Width:  btnWidth,
			Height: metrics.CellHeight * 2, // buttons are two rows: face + shadow
		})
		// Use a translated painter for the button at its position
		btnPainter := p.WithOffset(buttonX, buttonY)
		btn.Paint(btnPainter)
		buttonX += btnWidth + metrics.CellWidth
	}
}

// HandleMousePress handles mouse clicks on buttons.
func (c *messageBoxContent) HandleMousePress(event core.MousePressEvent) bool {
	// Check if click is on any button
	for _, btn := range c.buttonTrinkets {
		btnBounds := btn.Bounds()
		if event.X >= btnBounds.X && event.X < btnBounds.X+btnBounds.Width &&
			event.Y >= btnBounds.Y && event.Y < btnBounds.Y+btnBounds.Height {
			// Translate event to button's local coordinates
			localEvent := event
			localEvent.X -= btnBounds.X
			localEvent.Y -= btnBounds.Y
			return btn.HandleMousePress(localEvent)
		}
	}
	return false
}

// HandleMouseMove forwards pointer motion to the buttons. Without it the
// buttons never see movement inside the dialog: they can't light up on hover,
// and - worse - a pressed button never learns the pointer left, so it sticks
// depressed until release and a click can't be cancelled by dragging off it.
func (c *messageBoxContent) HandleMouseMove(event core.MouseMoveEvent) bool {
	// A pressed button captures motion: keep feeding it moves even after the
	// pointer wanders off its bounds, so it can drop its own pressed look.
	for _, btn := range c.buttonTrinkets {
		if btn.pressed {
			b := btn.Bounds()
			local := event
			local.X -= b.X
			local.Y -= b.Y
			btn.HandleMouseMove(local)
			return true
		}
	}
	// No press in flight: give every button the translated move so the one
	// under the pointer hovers and the others clear.
	for _, btn := range c.buttonTrinkets {
		b := btn.Bounds()
		local := event
		local.X -= b.X
		local.Y -= b.Y
		btn.HandleMouseMove(local)
	}
	return false
}

// HandleMouseRelease handles mouse release on buttons.
func (c *messageBoxContent) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	// Forward to all buttons (the pressed one will handle it)
	for _, btn := range c.buttonTrinkets {
		btnBounds := btn.Bounds()
		localEvent := event
		localEvent.X -= btnBounds.X
		localEvent.Y -= btnBounds.Y
		if btn.HandleMouseRelease(localEvent) {
			return true
		}
	}
	return false
}

// HandleKeyPress handles keyboard input.
func (m *MessageBox) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case "Escape":
		if m.buttons&ButtonCancel != 0 {
			m.done(ResultCancel)
		} else if m.buttons&ButtonNo != 0 {
			m.done(ResultNo)
		}
		return true

	case "Enter":
		if m.buttons&ButtonOK != 0 {
			m.done(ResultOK)
		} else if m.buttons&ButtonYes != 0 {
			m.done(ResultYes)
		}
		return true
	}

	return m.Window.HandleKeyPress(event)
}

// Information shows an information message box.
func Information(title, text string) DialogResult {
	mb := NewMessageBox(title, text, ButtonOK)
	mb.SetIcon(IconInformation)
	return ResultOK
}

// Warning shows a warning message box.
func Warning(title, text string) DialogResult {
	mb := NewMessageBox(title, text, ButtonOK)
	mb.SetIcon(IconWarning)
	return ResultOK
}

// ErrorDialog shows an error message box.
func ErrorDialog(title, text string) DialogResult {
	mb := NewMessageBox(title, text, ButtonOK)
	mb.SetIcon(IconError)
	return ResultOK
}

// Question shows a question message box.
func Question(title, text string) DialogResult {
	mb := NewMessageBox(title, text, ButtonYes|ButtonNo)
	mb.SetIcon(IconQuestion)
	return ResultYes
}

// FileDialogMode represents the file dialog mode.
type FileDialogMode int

const (
	FileDialogOpen FileDialogMode = iota
	FileDialogSave
	FileDialogSelectDirectory
)

// FileFilter represents a file filter.
type FileFilter struct {
	Name     string   // e.g., "Text Files"
	Patterns []string // e.g., ["*.txt", "*.text"]
}

// FileDialog provides file selection functionality.
type FileDialog struct {
	window.Window

	mode          FileDialogMode
	directory     string
	fileName      string
	filters       []FileFilter
	currentFilter int

	// State
	entries      []os.DirEntry
	currentIndex int
	scrollOffset int

	// Trinkets
	pathInput     *TextInput
	fileList      *ListView
	fileNameInput *TextInput
	filterCombo   *ComboBox
	okButton      *Button
	cancelButton  *Button

	// Callbacks
	onFileSelected func(path string)
	onCancelled    func()
}

// NewFileDialog creates a new file dialog.
func NewFileDialog(mode FileDialogMode) *FileDialog {
	f := &FileDialog{
		mode:      mode,
		directory: ".",
	}

	title := "Open"
	switch mode {
	case FileDialogSave:
		title = "Save As"
	case FileDialogSelectDirectory:
		title = "Select Directory"
	}

	f.Window = *window.NewWindow(title)
	f.SetType(window.WindowTypeModal)
	f.setupUI()
	return f
}

// setupUI creates the file dialog UI.
func (f *FileDialog) setupUI() {
	metrics := f.EffectiveCellMetrics()

	// Path input
	f.pathInput = NewTextInput()
	f.pathInput.SetText(f.directory)
	f.pathInput.SetOnReturnPressed(func() {
		f.navigateTo(f.pathInput.Text())
	})

	// File list
	f.fileList = NewListView()
	f.fileList.SetOnItemActivated(func(index int) {
		f.itemActivated(index)
	})
	f.fileList.SetOnCurrentChanged(func(index int) {
		if index >= 0 && index < len(f.entries) {
			entry := f.entries[index]
			if entry != nil && !entry.IsDir() && f.fileNameInput != nil {
				f.fileNameInput.SetText(entry.Name())
			}
		}
	})

	// File name input (for save dialog)
	if f.mode == FileDialogSave {
		f.fileNameInput = NewTextInput()
		f.fileNameInput.SetPlaceholder("Enter file name")
	}

	// Filter combo
	f.filterCombo = NewComboBox()

	// Buttons
	buttonText := "Open"
	if f.mode == FileDialogSave {
		buttonText = "Save"
	} else if f.mode == FileDialogSelectDirectory {
		buttonText = "Select"
	}

	f.okButton = NewButton(buttonText)
	f.okButton.SetOnClick(func() { f.accept() })

	f.cancelButton = NewButton("Cancel")
	f.cancelButton.SetOnClick(func() { f.reject() })

	// Set size
	f.SetBounds(core.UnitRect{
		Width:  metrics.CellWidth * 60,
		Height: metrics.CellHeight * 20,
	})

	// Initial load
	f.refreshFileList()
}

// SetDirectory sets the initial directory.
func (f *FileDialog) SetDirectory(dir string) {
	f.directory = dir
	if f.pathInput != nil {
		f.pathInput.SetText(dir)
	}
	f.refreshFileList()
}

// SetFileName sets the initial file name.
func (f *FileDialog) SetFileName(name string) {
	f.fileName = name
	if f.fileNameInput != nil {
		f.fileNameInput.SetText(name)
	}
}

// AddFilter adds a file filter.
func (f *FileDialog) AddFilter(name string, patterns ...string) {
	f.filters = append(f.filters, FileFilter{
		Name:     name,
		Patterns: patterns,
	})
	if f.filterCombo != nil {
		f.filterCombo.AddItem(name)
	}
}

// SetOnFileSelected sets the file selected callback.
func (f *FileDialog) SetOnFileSelected(handler func(path string)) {
	f.onFileSelected = handler
}

// SetOnCancelled sets the cancelled callback.
func (f *FileDialog) SetOnCancelled(handler func()) {
	f.onCancelled = handler
}

// SelectedFile returns the selected file path.
func (f *FileDialog) SelectedFile() string {
	return filepath.Join(f.directory, f.fileName)
}

// refreshFileList refreshes the file list.
func (f *FileDialog) refreshFileList() {
	f.fileList.Clear()
	f.entries = nil

	// Read directory
	entries, err := os.ReadDir(f.directory)
	if err != nil {
		f.fileList.AddTextItem("Error: " + err.Error())
		return
	}

	// Add parent directory
	if f.directory != "/" && f.directory != "." {
		item := NewListItem("..")
		f.fileList.AddItem(item)
		f.entries = append(f.entries, nil) // Placeholder for parent
	}

	// Sort: directories first, then files
	var dirs, files []os.DirEntry
	for _, entry := range entries {
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			// Apply filter
			if f.matchesFilter(entry.Name()) {
				files = append(files, entry)
			}
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name())
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name())
	})

	// Add directories
	for _, entry := range dirs {
		item := NewListItem("[DIR] " + entry.Name())
		f.fileList.AddItem(item)
		f.entries = append(f.entries, entry)
	}

	// Add files (skip for directory selection)
	if f.mode != FileDialogSelectDirectory {
		for _, entry := range files {
			item := NewListItem("     " + entry.Name())
			f.fileList.AddItem(item)
			f.entries = append(f.entries, entry)
		}
	}
}

// matchesFilter checks if a file matches the current filter.
func (f *FileDialog) matchesFilter(name string) bool {
	if len(f.filters) == 0 || f.currentFilter < 0 || f.currentFilter >= len(f.filters) {
		return true
	}

	filter := f.filters[f.currentFilter]
	for _, pattern := range filter.Patterns {
		if pattern == "*.*" || pattern == "*" {
			return true
		}
		matched, _ := filepath.Match(pattern, name)
		if matched {
			return true
		}
	}
	return false
}

// navigateTo navigates to a directory.
func (f *FileDialog) navigateTo(path string) {
	// Handle relative paths
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.directory, path)
	}

	// Clean the path
	path = filepath.Clean(path)

	// Check if it exists and is a directory
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}

	f.directory = path
	f.pathInput.SetText(path)
	f.refreshFileList()
}

// itemActivated handles item activation.
func (f *FileDialog) itemActivated(index int) {
	if index < 0 || index >= len(f.entries) {
		return
	}

	entry := f.entries[index]

	// Parent directory
	if entry == nil {
		f.navigateTo(filepath.Dir(f.directory))
		return
	}

	if entry.IsDir() {
		f.navigateTo(filepath.Join(f.directory, entry.Name()))
	} else {
		f.fileName = entry.Name()
		if f.fileNameInput != nil {
			f.fileNameInput.SetText(entry.Name())
		}
		f.accept()
	}
}

// accept accepts the dialog.
func (f *FileDialog) accept() {
	if f.mode == FileDialogSave && f.fileNameInput != nil {
		f.fileName = f.fileNameInput.Text()
	}

	if f.mode == FileDialogSelectDirectory {
		f.fileName = ""
	}

	if f.onFileSelected != nil {
		f.onFileSelected(f.SelectedFile())
	}
	f.Close()
}

// reject rejects the dialog.
func (f *FileDialog) reject() {
	if f.onCancelled != nil {
		f.onCancelled()
	}
	f.Close()
}

// Paint renders the file dialog.
func (f *FileDialog) Paint(p *core.Painter) {
	// First paint the window frame
	f.Window.Paint(p)

	bounds := f.Bounds()
	metrics := f.EffectiveCellMetrics()

	// Layout trinkets manually
	y := metrics.CellHeight * 2

	// Path label and input
	p.DrawText(metrics.CellWidth*2, y, "Location:", f.Theme().Normal, nil)
	f.pathInput.SetBounds(core.UnitRect{
		X:      metrics.CellWidth * 12,
		Y:      y,
		Width:  bounds.Width - metrics.CellWidth*14,
		Height: metrics.CellHeight,
	})
	f.pathInput.Paint(p)
	y += metrics.CellHeight + metrics.CellHeight/2

	// File list
	listHeight := bounds.Height - metrics.CellHeight*8
	f.fileList.SetBounds(core.UnitRect{
		X:      metrics.CellWidth * 2,
		Y:      y,
		Width:  bounds.Width - metrics.CellWidth*4,
		Height: listHeight,
	})
	f.fileList.Paint(p)
	y += listHeight + metrics.CellHeight/2

	// File name input (for save dialog)
	if f.fileNameInput != nil {
		p.DrawText(metrics.CellWidth*2, y, "File name:", f.Theme().Normal, nil)
		f.fileNameInput.SetBounds(core.UnitRect{
			X:      metrics.CellWidth * 14,
			Y:      y,
			Width:  bounds.Width - metrics.CellWidth*16,
			Height: metrics.CellHeight,
		})
		f.fileNameInput.Paint(p)
		y += metrics.CellHeight + metrics.CellHeight/2
	}

	// Buttons at bottom
	buttonY := bounds.Height - metrics.CellHeight*2
	buttonWidth := metrics.CellWidth * 10

	f.okButton.SetBounds(core.UnitRect{
		X:      snapCellX(f.Self(), metrics, bounds.Width-buttonWidth*2-metrics.CellWidth*4),
		Y:      buttonY,
		Width:  buttonWidth,
		Height: metrics.CellHeight * 2, // buttons are two rows: face + shadow
	})
	f.okButton.Paint(p)

	f.cancelButton.SetBounds(core.UnitRect{
		X:      snapCellX(f.Self(), metrics, bounds.Width-buttonWidth-metrics.CellWidth*2),
		Y:      buttonY,
		Width:  buttonWidth,
		Height: metrics.CellHeight * 2, // buttons are two rows: face + shadow
	})
	f.cancelButton.Paint(p)
}

// HandleKeyPress handles keyboard input.
func (f *FileDialog) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case "Escape":
		f.reject()
		return true

	case "Enter":
		if f.fileList.HasFocus() && f.fileList.CurrentIndex() >= 0 {
			f.itemActivated(f.fileList.CurrentIndex())
			return true
		}
		f.accept()
		return true

	case "Backspace":
		if !f.pathInput.HasFocus() && (f.fileNameInput == nil || !f.fileNameInput.HasFocus()) {
			f.navigateTo(filepath.Dir(f.directory))
			return true
		}
	}

	return f.Window.HandleKeyPress(event)
}

// OpenFile shows an open file dialog.
func OpenFile(filters ...FileFilter) string {
	dialog := NewFileDialog(FileDialogOpen)
	for _, filter := range filters {
		dialog.AddFilter(filter.Name, filter.Patterns...)
	}
	return ""
}

// SaveFile shows a save file dialog.
func SaveFile(defaultName string, filters ...FileFilter) string {
	dialog := NewFileDialog(FileDialogSave)
	dialog.SetFileName(defaultName)
	for _, filter := range filters {
		dialog.AddFilter(filter.Name, filter.Patterns...)
	}
	return ""
}

// SelectDirectory shows a directory selection dialog.
func SelectDirectory(startDir string) string {
	dialog := NewFileDialog(FileDialogSelectDirectory)
	dialog.SetDirectory(startDir)
	return ""
}

// InputDialog shows a simple input dialog.
type InputDialog struct {
	window.Window

	labelText    string
	input        *TextInput
	okButton     *Button
	cancelButton *Button

	result   string
	accepted bool

	onFinished func(text string, accepted bool)
}

// NewInputDialog creates a new input dialog.
func NewInputDialog(title, label, defaultValue string) *InputDialog {
	d := &InputDialog{
		labelText: label,
	}
	d.Window = *window.NewWindow(title)
	d.SetType(window.WindowTypeModal)
	d.SetFlags(window.WindowFlagNoResize)

	metrics := d.EffectiveCellMetrics()

	d.input = NewTextInput()
	d.input.SetText(defaultValue)
	d.input.SelectAll()

	d.okButton = NewButton("OK")
	d.okButton.SetOnClick(func() {
		d.result = d.input.Text()
		d.accepted = true
		if d.onFinished != nil {
			d.onFinished(d.result, true)
		}
		d.Close()
	})

	d.cancelButton = NewButton("Cancel")
	d.cancelButton.SetOnClick(func() {
		d.accepted = false
		if d.onFinished != nil {
			d.onFinished("", false)
		}
		d.Close()
	})

	d.SetBounds(core.UnitRect{
		Width:  metrics.CellWidth * 40,
		Height: metrics.CellHeight * 6,
	})

	return d
}

// Result returns the input result.
func (d *InputDialog) Result() string {
	return d.result
}

// Accepted returns whether OK was pressed.
func (d *InputDialog) Accepted() bool {
	return d.accepted
}

// SetOnFinished sets the finished callback.
func (d *InputDialog) SetOnFinished(handler func(text string, accepted bool)) {
	d.onFinished = handler
}

// Paint renders the input dialog.
func (d *InputDialog) Paint(p *core.Painter) {
	d.Window.Paint(p)

	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	theme := d.Theme()

	// Label
	y := metrics.CellHeight * 2
	p.DrawText(metrics.CellWidth*2, y, d.labelText, theme.Normal, nil)

	// Input
	y += metrics.CellHeight
	d.input.SetBounds(core.UnitRect{
		X:      metrics.CellWidth * 2,
		Y:      y,
		Width:  bounds.Width - metrics.CellWidth*4,
		Height: metrics.CellHeight,
	})
	d.input.Paint(p)

	// Buttons
	buttonY := bounds.Height - metrics.CellHeight*2
	buttonWidth := metrics.CellWidth * 10

	d.okButton.SetBounds(core.UnitRect{
		X:      snapCellX(d.Self(), metrics, bounds.Width/2-buttonWidth-metrics.CellWidth),
		Y:      buttonY,
		Width:  buttonWidth,
		Height: metrics.CellHeight * 2, // buttons are two rows: face + shadow
	})
	d.okButton.Paint(p)

	d.cancelButton.SetBounds(core.UnitRect{
		X:      snapCellX(d.Self(), metrics, bounds.Width/2+metrics.CellWidth),
		Y:      buttonY,
		Width:  buttonWidth,
		Height: metrics.CellHeight * 2, // buttons are two rows: face + shadow
	})
	d.cancelButton.Paint(p)
}

// HandleKeyPress handles keyboard input.
func (d *InputDialog) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case "Escape":
		d.accepted = false
		if d.onFinished != nil {
			d.onFinished("", false)
		}
		d.Close()
		return true

	case "Enter":
		d.result = d.input.Text()
		d.accepted = true
		if d.onFinished != nil {
			d.onFinished(d.result, true)
		}
		d.Close()
		return true
	}

	return d.Window.HandleKeyPress(event)
}

// GetText shows an input dialog and returns the text.
func GetText(title, label, defaultValue string) (string, bool) {
	_ = NewInputDialog(title, label, defaultValue)
	return "", false
}
