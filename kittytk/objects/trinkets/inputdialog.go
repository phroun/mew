package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

// InputDialog asks for one line of text: a prompt saying what is wanted, a
// field to type it in, and OK and Cancel.
//
// **Everything in it is a real trinket in the window's tree**, so the focus
// ring, the pointer, the buttons' letters and a screen reader all reach them
// the way they reach any window's content. The dialog opens with the field
// focused and its text selected, so typing replaces what was offered.
//
// **Keys go where the focus is, and nowhere else.** Return in the field is the
// field completing, and that accepts; Return or Space on a button presses that
// button. Nothing is caught at the window: Esc does nothing here, and Return
// with Cancel focused cancels.
//
// It is answered once. OK, Return in the field, Cancel, and closing the window
// without choosing each finish it, and whichever comes first is the answer.
type InputDialog struct {
	window.Window

	body         *Panel
	prompt       *Label
	input        *TextInput
	okButton     *Button
	cancelButton *Button

	result   DialogResult
	text     string
	answered bool

	onFinished func(text string, accepted bool)
}

// NewInputDialog creates an input dialog titled title, asking prompt, with
// value already in the field.
func NewInputDialog(title, prompt, value string) *InputDialog {
	d := &InputDialog{result: ResultNone}
	d.Window = *window.NewWindow(title)
	d.SetType(window.WindowTypeModal)
	d.SetFlags(window.WindowFlagNoResize)

	metrics := d.EffectiveCellMetrics()

	d.prompt = NewLabel(prompt)
	d.input = NewTextInput()
	d.input.SetText(value)
	d.input.SelectAll()
	d.input.SetOnComplete(func() { d.finish(ResultOK) })

	d.okButton = NewButton("&OK")
	d.okButton.SetOnClick(func() { d.finish(ResultOK) })
	d.cancelButton = NewButton("&Cancel")
	d.cancelButton.SetOnClick(func() { d.finish(ResultCancel) })

	// The buttons sit centred under the field, as a message box's do.
	row := NewPanel()
	rowLayout := layout.NewHBoxLayout()
	rowLayout.SetSpacing(metrics.UnitsPerCellWidth)
	row.SetLayoutManager(rowLayout)
	rowLayout.AddStretch(1)
	row.AddChild(d.okButton)
	row.AddChild(d.cancelButton)
	rowLayout.AddStretch(1)

	d.body = NewPanel()
	bodyLayout := layout.NewVBoxLayout()
	bodyLayout.SetSpacing(0)
	bodyLayout.SetContentsMargins(core.UnitMargins{
		Left:   metrics.UnitsPerCellWidth * 2,
		Right:  metrics.UnitsPerCellWidth * 2,
		Top:    metrics.UnitsPerCellHeight,
		Bottom: 0,
	})
	d.body.SetLayoutManager(bodyLayout)
	d.body.AddChild(d.prompt)
	d.body.AddChild(d.input)
	bodyLayout.AddStretch(1)
	d.body.AddChild(row)

	d.SetContent(d.body)

	// Closing the window any other way -- its [x], or the application that owns
	// it going -- is an answer too: nothing was chosen.
	d.AddOnClosed(func() { d.finish(ResultNone) })

	d.calculateSize()
	return d
}

// finish settles the dialog, once. The first answer is the one given; any
// that follow -- a second button pressed before the first has finished
// animating, or the window closing behind an answer -- are ignored.
func (d *InputDialog) finish(result DialogResult) {
	if d.answered {
		return
	}
	d.answered = true
	d.result = result
	if result == ResultOK {
		d.text = d.input.Text()
	}
	if d.onFinished != nil {
		d.onFinished(d.text, result == ResultOK)
	}
	d.Close()
}

// dismiss closes the dialog without telling anyone it was answered: the one
// who asked is the one taking it away.
func (d *InputDialog) dismiss() {
	d.answered = true
	d.Close()
}

// calculateSize fits the window to its content: wide enough for the prompt
// and never narrower than forty columns, and tall enough for what the body
// asks, plus whatever the window's own chrome takes.
func (d *InputDialog) calculateSize() {
	metrics := d.EffectiveCellMetrics()
	font := d.prompt.EffectiveFont()

	contentW := font.MeasureTextIn(d.prompt.Text(), metrics) + metrics.UnitsPerCellWidth*4
	if floor := metrics.UnitsPerCellWidth * 40; contentW < floor {
		contentW = floor
	}
	if ceiling := metrics.UnitsPerCellWidth * 64; contentW > ceiling {
		contentW = ceiling
	}

	// A margin, the prompt, the field, a row between, and the buttons, whose
	// second row is their shadow and serves as the bottom margin -- the rows a
	// message box spends -- or more, if the body's own measure says so.
	contentH := metrics.UnitsPerCellHeight * 6
	if h := d.body.SizeHint().Height + metrics.UnitsPerCellHeight; h > contentH {
		contentH = h
	}

	d.SetBounds(core.UnitRect{Width: contentW, Height: contentH})
	cb := d.ContentBounds()
	chromeW, chromeH := contentW-cb.Width, contentH-cb.Height
	if chromeW < 0 {
		chromeW = 0
	}
	if chromeH < 0 {
		chromeH = 0
	}
	d.SetBounds(core.UnitRect{Width: contentW + chromeW, Height: contentH + chromeH})
}

// ResizeToFitContent recomputes the dialog's size. Call it after the dialog is
// parented, when the window's chrome is known.
func (d *InputDialog) ResizeToFitContent() { d.calculateSize() }

// SetPrompt replaces what the dialog says it wants.
func (d *InputDialog) SetPrompt(prompt string) {
	d.prompt.SetText(prompt)
	d.calculateSize()
	d.Update()
}

// Prompt returns what the dialog says it wants.
func (d *InputDialog) Prompt() string { return d.prompt.Text() }

// SetText replaces what is in the field, selected so typing replaces it.
func (d *InputDialog) SetText(text string) {
	d.input.SetText(text)
	d.input.SelectAll()
}

// Text returns what is in the field now. After the dialog is answered, Result
// is what was accepted.
func (d *InputDialog) Text() string { return d.input.Text() }

// SetPlaceholder sets the hint the field shows while it is empty.
func (d *InputDialog) SetPlaceholder(text string) { d.input.SetPlaceholder(text) }

// Input is the dialog's field, for settings the dialog does not repeat.
func (d *InputDialog) Input() *TextInput { return d.input }

// Result returns the text that was accepted, or "" if it was not.
func (d *InputDialog) Result() string { return d.text }

// Accepted reports whether the dialog was answered with OK.
func (d *InputDialog) Accepted() bool { return d.result == ResultOK }

// Answer is how the dialog was answered: ResultOK, ResultCancel, or ResultNone
// for a window closed without choosing, or not answered yet.
func (d *InputDialog) Answer() DialogResult { return d.result }

// SetOnFinished sets what hears the answer: the text, and whether it was
// accepted. The text is "" when it was not.
func (d *InputDialog) SetOnFinished(handler func(text string, accepted bool)) {
	d.onFinished = handler
}
