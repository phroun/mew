package trinkets

// A button's caption marks the letter it would answer to with "&", as a menu
// title does. A message box's area, holding the focus when the dialog opens,
// offers its buttons by that letter (see core.Mnemonic) -- drawing each
// button's letter out and answering a single bare keypress of it, so Y or N
// answers a yes-or-no question without a Tab first. Once a button has the
// focus, the buttons paint as they always have and the letters do nothing.

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

func exitQuestion(t *testing.T) *MessageBox {
	t.Helper()
	core.SetTextMeasurer(nil)
	mb := NewMessageBox("Exit Desktop",
		"Exiting the desktop will quit 1 running application.\n\nAre you sure?",
		ButtonYes|ButtonNo|ButtonPopOut)
	mb.SetButtonText(ResultPopOut, "&Pop It Out")
	return mb
}

// wonBy is the letter btn answers to while the message area holds the focus,
// with Pos -1 when it won none. It leaves the area focused.
func wonBy(mb *MessageBox, btn *Button) core.MnemonicChoice {
	mb.content.SetFocus()
	pos := btn.liveMnemonic()
	if pos < 0 {
		return core.MnemonicChoice{Pos: -1}
	}
	return core.MnemonicChoice{Char: unicode.ToLower([]rune(btn.Text())[pos]), Pos: pos}
}

// paintContent draws a message box's area into a cell grid, so what each cell
// holds, and in what style, can be read back.
func paintContent(t *testing.T, mb *MessageBox) *closeGrid {
	t.Helper()
	px, err := raster.New(800, 400)
	if err != nil {
		t.Fatal(err)
	}
	m := mb.content.EffectiveCellMetrics()
	cols, rows := 80, 12
	g := &closeGrid{RenderBackend: px, cw: m.UnitsPerCellWidth, ch: m.UnitsPerCellHeight,
		focusFg: style.ColorRed, styles: map[[2]int]style.CellStyle{}}
	for i := 0; i < rows; i++ {
		g.rows = append(g.rows, []rune(strings.Repeat(" ", cols)))
		g.focus = append(g.focus, make([]bool, cols))
	}
	mb.content.Paint(core.NewPainter(g))
	return g
}

// underlined is the text of every cell drawn red and underlined -- the
// mnemonic style -- in reading order.
func underlined(g *closeGrid) string {
	var out []rune
	for y := range g.rows {
		for x := range g.rows[y] {
			s := g.styles[[2]int{x, y}]
			if s.Fg == style.ColorRed && s.Attrs&style.StyleUnderline != 0 {
				out = append(out, g.rows[y][x])
			}
		}
	}
	return string(out)
}

// The stock captions are shown without their markup, and each button answers
// to its own first letter.
func TestTheStockButtonsAnswerToTheirFirstLetters(t *testing.T) {
	mb := exitQuestion(t)
	want := map[string]rune{"Yes": 'y', "No": 'n', "Pop It Out": 'p'}
	for _, btn := range mb.content.buttonTrinkets {
		letter, ok := want[btn.Text()]
		if !ok {
			t.Errorf("a button shows %q, which is not one of the captions", btn.Text())
			continue
		}
		if got := wonBy(mb, btn); got.Char != letter || got.Pos != 0 {
			t.Errorf("%q answers to %q at %d, want %q at 0", btn.Text(), got.Char, got.Pos, letter)
		}
	}
}

// While the message area has the focus, each button's letter is drawn red and
// underlined over the face's own background; once a button has the focus, no
// letter is.
func TestTheLettersShowWhileTheMessageHasTheFocus(t *testing.T) {
	mb := exitQuestion(t)
	if !mb.content.HasFocus() {
		t.Fatal("the message area does not have the focus when the dialog opens")
	}
	g := paintContent(t, mb)
	if got := underlined(g); got != "YNP" {
		t.Errorf("drawn as mnemonics: %q, want YNP", got)
	}
	for y := range g.rows {
		for x := range g.rows[y] {
			s := g.styles[[2]int{x, y}]
			if g.rows[y][x] == 'Y' && s.Attrs&style.StyleUnderline != 0 {
				face := mb.GetScheme().GetButtonState(true, false, false, false)
				if s.Bg != face.Bg {
					t.Errorf("the Y is on %v, want the button's face %v", s.Bg, face.Bg)
				}
			}
		}
	}

	mb.FocusManager().SetFocusedTrinket(mb.content.buttonTrinkets[1])
	if got := underlined(paintContent(t, mb)); got != "" {
		t.Errorf("with a button focused, still drawn as mnemonics: %q", got)
	}
}

// awaitResult waits for the dialog's answer: a letter presses its button the
// way Space does, and the press lands a moment later.
func awaitResult(mb *MessageBox, got *DialogResult) {
	deadline := time.Now().Add(2 * time.Second)
	for *got == ResultNone && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// A single bare letter answers, in either case, while the message area has the
// focus.
func TestALetterAnswersTheQuestion(t *testing.T) {
	for key, want := range map[string]DialogResult{"y": ResultYes, "N": ResultNo, "p": ResultPopOut} {
		mb := exitQuestion(t)
		got := ResultNone
		mb.SetOnFinished(func(r DialogResult) { got = r })
		if !mb.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%q was not taken", key)
		}
		awaitResult(mb, &got)
		if got != want {
			t.Errorf("%q answered %v, want %v", key, got, want)
		}
	}
}

// A letter no button won, a chord, or any letter once a button has the focus,
// answers nothing.
func TestOtherKeysDoNotAnswer(t *testing.T) {
	for _, key := range []string{"q", "M-y", "^Y", "PageUp", "Next"} {
		mb := exitQuestion(t)
		got := ResultNone
		mb.SetOnFinished(func(r DialogResult) { got = r })
		mb.HandleKeyPress(core.KeyPressEvent{Key: key})
		time.Sleep(400 * time.Millisecond)
		if got != ResultNone {
			t.Errorf("%q answered %v", key, got)
		}
	}

	mb := exitQuestion(t)
	got := ResultNone
	mb.SetOnFinished(func(r DialogResult) { got = r })
	mb.FocusManager().SetFocusedTrinket(mb.content.buttonTrinkets[1])
	mb.HandleKeyPress(core.KeyPressEvent{Key: "y"})
	time.Sleep(400 * time.Millisecond)
	if got != ResultNone {
		t.Errorf("with a button focused, y answered %v", got)
	}
}

// A disabled button's letter does nothing.
func TestADisabledButtonsLetterDoesNothing(t *testing.T) {
	mb := exitQuestion(t)
	got := ResultNone
	mb.SetOnFinished(func(r DialogResult) { got = r })
	mb.content.buttonTrinkets[0].SetEnabled(false)
	mb.HandleKeyPress(core.KeyPressEvent{Key: "y"})
	time.Sleep(400 * time.Millisecond)
	if got != ResultNone {
		t.Errorf("a disabled Yes answered %v", got)
	}
}

// Two buttons offering the same letter: the one written first has it, and the
// other takes its next choice, or goes without.
func TestAClashGoesToTheFirstButton(t *testing.T) {
	mb := NewMessageBox("Apply", "Apply the changes?", ButtonAbort|ButtonApply)
	letters := map[string]rune{}
	for _, btn := range mb.content.buttonTrinkets {
		letters[btn.Text()] = wonBy(mb, btn).Char
	}
	if letters["Abort"] != 'a' || letters["Apply"] != 'p' {
		t.Errorf("Abort answers to %q and Apply to %q, want a and p", letters["Abort"], letters["Apply"])
	}

	mb = NewMessageBox("Save", "Save?", ButtonSave|ButtonDiscard)
	mb.SetButtonText(ResultDiscard, "&Scrap")
	for _, btn := range mb.content.buttonTrinkets {
		if btn.Text() == "Scrap" {
			if wonBy(mb, btn).Pos >= 0 {
				t.Error("Scrap has a letter after Save took its only one")
			}
		}
	}
}

// A button with nothing focused above it shows its caption without the markup
// and draws no letter; "&&" is an ampersand.
func TestAButtonElsewhereDrawsNoLetter(t *testing.T) {
	b := NewButton("&Save && Exit")
	if b.Text() != "Save & Exit" || b.RawText() != "&Save && Exit" {
		t.Errorf("shows %q from %q", b.Text(), b.RawText())
	}
	if b.liveMnemonic() >= 0 {
		t.Error("a button in no container offers a letter")
	}
	if got := b.AccessibleInfo(); got.Name != "Save & Exit" || got.KeyboardShortcut != "" {
		t.Errorf("tells a screen reader %q with shortcut %q", got.Name, got.KeyboardShortcut)
	}
}

// A screen reader is told the letter while it answers, and not after.
func TestAScreenReaderIsToldTheLetter(t *testing.T) {
	mb := exitQuestion(t)
	yes := mb.content.buttonTrinkets[0]
	if got := yes.AccessibleInfo().KeyboardShortcut; got != "Y" {
		t.Errorf("Yes announces shortcut %q, want Y", got)
	}
	mb.FocusManager().SetFocusedTrinket(yes)
	if got := yes.AccessibleInfo().KeyboardShortcut; got != "" {
		t.Errorf("with Yes focused it still announces %q", got)
	}
}

// The mnemonic style is the scheme's foreground and attributes over the
// background the caption is already on.
func TestTheMnemonicStyleKeepsTheFace(t *testing.T) {
	face := style.DefaultStyle().WithFg(style.ColorBlack).WithBg(style.ColorCyan).WithAttrs(style.StyleBold)
	got := style.DefaultScheme().GetButtonMnemonic(face, false)
	if got.Fg != style.ColorRed || got.Bg != style.ColorCyan ||
		got.Attrs&style.StyleUnderline == 0 || got.Attrs&style.StyleBold == 0 {
		t.Errorf("mnemonic over the face = %+v", got)
	}
	if got := (&style.Scheme{}).GetButtonMnemonic(face, false); got.Fg != style.ColorRed || got.Attrs&style.StyleUnderline == 0 {
		t.Errorf("a scheme that says nothing gives %+v, want red and underlined", got)
	}
}

// On a hovered or pressed face the letter keeps the face's own foreground --
// red on the press colour is a clash -- and is told apart by the underline
// alone.
func TestALitFaceKeepsItsOwnColourForTheLetter(t *testing.T) {
	face := style.DefaultStyle().WithFg(style.ColorWhite).WithBg(style.ColorMagenta).WithAttrs(style.StyleBold)
	got := style.DefaultScheme().GetButtonMnemonic(face, true)
	if got.Fg != style.ColorWhite || got.Bg != style.ColorMagenta ||
		got.Attrs&style.StyleUnderline == 0 || got.Attrs&style.StyleBold == 0 {
		t.Errorf("mnemonic over a lit face = %+v, want the face underlined", got)
	}
}

// Painted: a pressed button's letter is underlined in the pressed caption's
// colour, while its neighbours' letters stay red.
func TestAPressedButtonsLetterIsInThePressedColour(t *testing.T) {
	mb := exitQuestion(t)
	var yes *Button
	for _, btn := range mb.content.buttonTrinkets {
		if btn.Text() == "Yes" {
			yes = btn
		}
	}
	mb.content.SetFocus()
	yes.spacePressed = true
	g := paintContent(t, mb)
	if got := underlined(g); strings.ContainsRune(got, 'Y') || !strings.ContainsRune(got, 'N') {
		t.Errorf("red underlined letters are %q; the pressed Yes should not be among them, No should", got)
	}
	pressed := mb.content.GetScheme().GetButtonState(true, false, false, true)
	found := false
	for y := range g.rows {
		for x, r := range g.rows[y] {
			s := g.styles[[2]int{x, y}]
			if r == 'Y' && s.Attrs&style.StyleUnderline != 0 {
				found = true
				if s.Fg != pressed.Fg {
					t.Errorf("the pressed Y is drawn in %v, want the pressed caption's %v", s.Fg, pressed.Fg)
				}
			}
		}
	}
	if !found {
		t.Error("the pressed Yes has no underlined letter")
	}
}

// The whole way: a dialog put up on a desktop answers a letter typed at the
// desktop, with nothing pressed first.
func TestALetterTypedAtTheDesktopAnswers(t *testing.T) {
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	mb := exitQuestion(t)
	got := ResultNone
	mb.SetOnFinished(func(r DialogResult) { got = r })
	if !d.showModal(mb) {
		t.Fatal("the dialog could not be shown")
	}
	d.dispatchEvent(core.KeyPressEvent{Key: "n"})
	if !mb.content.buttonTrinkets[1].animatingPress.Load() {
		t.Fatal("n typed at the desktop did not press No")
	}
	// The press lands on the desktop's own timer, which its loop would run.
	deadline := time.Now().Add(2 * time.Second)
	for got == ResultNone && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		d.ProcessTimers()
	}
	if got != ResultNo {
		t.Errorf("n typed at the desktop answered %v, want No", got)
	}
}

// A caption offering two letters, both free, answers to the first; the second
// is left for a button after it.
func TestACaptionTakesItsFirstFreeLetter(t *testing.T) {
	mb := NewMessageBox("Q", "?", ButtonYes|ButtonNo)
	mb.SetButtonText(ResultYes, "&Y&es")
	mb.SetButtonText(ResultNo, "N&ever")
	for _, btn := range mb.content.buttonTrinkets {
		want := map[string]rune{"Yes": 'y', "Never": 'e'}[btn.Text()]
		if got := wonBy(mb, btn).Char; got != want {
			t.Errorf("%q answers to %q, want %q", btn.Text(), got, want)
		}
	}
}

// The message area answers letters only while it holds the focus, however it
// is asked.
func TestTheAreaAnswersOnlyWithTheFocus(t *testing.T) {
	mb := exitQuestion(t)
	mb.FocusManager().SetFocusedTrinket(mb.content.buttonTrinkets[1])
	if mb.content.HandleKeyPress(core.KeyPressEvent{Key: "y"}) {
		t.Error("the message area took y without the focus")
	}
}

// A disabled button draws no letter.
func TestADisabledButtonDrawsNoLetter(t *testing.T) {
	mb := exitQuestion(t)
	mb.content.buttonTrinkets[0].SetEnabled(false)
	if got := underlined(paintContent(t, mb)); got != "NP" {
		t.Errorf("drawn as mnemonics with Yes disabled: %q, want NP", got)
	}
}
