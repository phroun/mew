package trinkets

// The row of buttons along the bottom of a message box, and the one thing that can
// go wrong with it that nothing was watching for.
//
// A dialog's buttons arrive over the wire ONE FLAG AT A TIME -- `yes no` is two
// properties, not one -- and each of them sets the whole button set again. So the row
// is built as many times as there are buttons, and a build that added to the row
// instead of replacing it left the previous row on the screen: `yes` built [Yes], and
// `no` built [Yes No] after it, for [Yes Yes No].
//
// It stood because the test that covered this asked the dialog what buttons it HAD --
// the flags, which were right all along -- and never counted what a person would see.
// So these count the trinkets.

import "testing"

// shown is the buttons a person would see, in the order they are laid out.
func shown(m *MessageBox) []DialogResult {
	return append([]DialogResult(nil), m.content.buttonResults...)
}

func same(got, want []DialogResult) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The way an application actually asks for a two-button dialog: one statement, two
// flags, two calls to set the buttons.
func TestAWireBuiltDialogShowsEachButtonOnce(t *testing.T) {
	f, _ := buildWithEvents(t, nil, `
dlg=new messagebox title="Close Document 1" text="Close Document 1?" yes no icon=question
`)
	m := f.targets[0].(*MessageBox)
	want := []DialogResult{ResultYes, ResultNo}
	if got := shown(m); !same(got, want) {
		t.Errorf("the dialog shows %v, want %v -- a person sees a button for every answer, once", got, want)
	}
	// And the flags agree with the row, which is the part that was always right.
	if m.Buttons() != ButtonYes|ButtonNo {
		t.Errorf("buttons = %v, want yes|no", m.Buttons())
	}
}

// Three of them, and a fourth statement setting one that is already there. Setting a
// button set is saying what the row IS, so saying it twice says the same thing.
func TestSettingTheButtonsAgainReplacesTheRow(t *testing.T) {
	m := NewMessageBox("Confirm", "Really?", ButtonYes)
	if got := shown(m); !same(got, []DialogResult{ResultYes}) {
		t.Fatalf("the dialog starts as %v, want one Yes", got)
	}

	m.SetButtons(ButtonYes | ButtonNo)
	if got := shown(m); !same(got, []DialogResult{ResultYes, ResultNo}) {
		t.Errorf("after adding No the dialog shows %v, want Yes and No", got)
	}

	m.SetButtons(ButtonYes | ButtonNo | ButtonPopOut)
	want := []DialogResult{ResultYes, ResultNo, ResultPopOut}
	if got := shown(m); !same(got, want) {
		t.Errorf("after adding the offer the dialog shows %v, want %v", got, want)
	}

	// Taking one away takes it off the screen, not just out of the flags.
	m.SetButtons(ButtonYes)
	if got := shown(m); !same(got, []DialogResult{ResultYes}) {
		t.Errorf("after taking the others away the dialog shows %v, want one Yes", got)
	}
	// Saying the same thing twice changes nothing.
	m.SetButtons(ButtonYes)
	if got := shown(m); !same(got, []DialogResult{ResultYes}) {
		t.Errorf("setting the same buttons again shows %v, want one Yes", got)
	}
}

// Every button is clickable and answers for itself. A row built twice over left the
// ghosts parented and hit-testable, so the answer depended on which copy was struck.
func TestEveryButtonShownAnswersForItself(t *testing.T) {
	f, _ := buildWithEvents(t, nil, `
dlg=new messagebox title="Close" text="Close it?" yes no icon=question
`)
	m := f.targets[0].(*MessageBox)

	var answered []DialogResult
	m.SetOnFinished(func(r DialogResult) { answered = append(answered, r) })

	buttons := m.content.buttonTrinkets
	if len(buttons) != 2 {
		t.Fatalf("the dialog has %d buttons, want two", len(buttons))
	}
	for i, btn := range buttons {
		if btn.Text() == "" {
			t.Errorf("button %d has no caption", i)
		}
	}
	buttons[0].Click()
	if len(answered) != 1 || answered[0] != ResultYes {
		t.Errorf("clicking the first button answered %v, want one yes", answered)
	}
}

// The wording a dialog gives a button survives, because it is set on the row that is
// actually shown. Renaming one of a row that was about to be replaced renames a ghost.
func TestARenamedButtonKeepsItsWording(t *testing.T) {
	m := NewMessageBox("Exit Desktop", "Quit the application inside it?",
		ButtonYes|ButtonNo|ButtonPopOut)
	m.SetButtonText(ResultPopOut, "Pop It Out")

	for i, r := range m.content.buttonResults {
		if r != ResultPopOut {
			continue
		}
		if got := m.content.buttonTrinkets[i].Text(); got != "Pop It Out" {
			t.Errorf("the offer reads %q, want %q", got, "Pop It Out")
		}
		return
	}
	t.Error("the dialog shows no offer to pop it out")
}
