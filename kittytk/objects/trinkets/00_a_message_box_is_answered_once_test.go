package trinkets

// A message box takes the first answer it is given. A button pressed from the
// keyboard animates for a moment before it answers, so a click on another
// button in that moment used to answer too -- on the exit dialog, quitting
// and then not quitting. The click answers, and the press that finishes after
// it changes nothing.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

func TestAMessageBoxIsAnsweredOnce(t *testing.T) {
	mb := exitQuestion(t)
	var answers []DialogResult
	mb.SetOnFinished(func(r DialogResult) { answers = append(answers, r) })

	yes, no := mb.content.buttonTrinkets[0], mb.content.buttonTrinkets[1]
	mb.FocusManager().SetFocusedTrinket(yes)
	mb.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	if len(answers) != 0 {
		t.Fatalf("Return on Yes answered before its press finished: %v", answers)
	}
	no.Click()
	settle(t, func() bool { return !yes.MnemonicPressing() })

	if len(answers) != 1 || answers[0] != ResultNo {
		t.Errorf("heard %v, want [No] alone", answers)
	}
	if mb.Result() != ResultNo {
		t.Errorf("the dialog's result is %v, want No", mb.Result())
	}
}
