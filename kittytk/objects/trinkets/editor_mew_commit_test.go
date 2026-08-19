//go:build mew

package trinkets

import "testing"

// A commit tells mew to put the text where the COMPOSITION stood, not where
// the caret happens to be.
//
// Dismissing a palette by typing lands the keystroke before the input method's
// commit catches up, so counting back from the caret replaced that keystroke
// rather than the letter — "oò" with the character eaten. mew tracks the
// region with a cursor of its own, so the command names no count at all.
//
// It ends the composition itself, which matters on the route where the palette
// is confirmed by number: no empty update ever arrives there to do it.
func TestACommitIsAnchoredAndNeedsNoCount(t *testing.T) {
	cmds := commitCommands("ò", 1)

	if len(cmds) != 1 {
		t.Fatalf("commit issued %v, want one anchored command", cmds)
	}
	if cmds[0] != "preedit_commit 'ò'" {
		t.Errorf("command %q, want the text placed where the composition stood", cmds[0])
	}
}

// An empty commit still ends the composition — that is the whole of what it has
// to say.
func TestAnEmptyCommitStillEndsTheComposition(t *testing.T) {
	cmds := commitCommands("", 0)

	if len(cmds) != 1 || cmds[0] != "preedit_commit ''" {
		t.Errorf("commit issued %v, want only the composition ended", cmds)
	}
}

// A composed quote cannot break out of the command being built for it.
func TestACommitEscapesWhatItCommits(t *testing.T) {
	cmds := commitCommands("it's", 1)

	if len(cmds) != 1 || cmds[0] != `preedit_commit 'it\'s'` {
		t.Errorf("commit issued %v, want the quote escaped", cmds)
	}
}
