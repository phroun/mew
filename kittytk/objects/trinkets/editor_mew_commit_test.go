//go:build mew

package trinkets

import (
	"strings"
	"testing"
)

// A commit ENDS the composition, and mew is told so rather than left to infer
// it from a later event.
//
// On the route where the palette is confirmed by number, the toolkit's own
// composition is the only one there is: macOS synthesizes a Backspace and the
// replacement, and reports no composition before or after. Nothing follows the
// commit to close it, so the letter underneath kept painting as provisional
// text for good — armed behind everything typed afterwards.
func TestACommitEndsMewsCompositionFirst(t *testing.T) {
	cmds := commitCommands("ò", 1)

	if len(cmds) != 2 {
		t.Fatalf("commit issued %v, want the ending and the replacement", cmds)
	}
	if !strings.HasPrefix(cmds[0], "preedit ") || !strings.Contains(cmds[0], "''") {
		t.Errorf("first command %q, want the composition ended", cmds[0])
	}
	if cmds[1] != "replace_prior 1, 'ò'" {
		t.Errorf("second command %q, want the accent replacing the letter", cmds[1])
	}
}

// A commit with nothing to put in and nothing to take out still ends the
// composition — that is the whole of what it has to say.
func TestAnEmptyCommitStillEndsTheComposition(t *testing.T) {
	cmds := commitCommands("", 0)

	if len(cmds) != 1 || !strings.Contains(cmds[0], "''") {
		t.Errorf("commit issued %v, want only the composition ended", cmds)
	}
}

// A composition that stood over nothing commits as an ordinary insert, which is
// what a CJK candidate confirming does.
func TestACommitOverNothingJustInserts(t *testing.T) {
	cmds := commitCommands("きょう", 0)

	if len(cmds) != 2 || cmds[1] != "replace_prior 0, 'きょう'" {
		t.Errorf("commit issued %v, want the text inserted over nothing", cmds)
	}
}

// A composed quote cannot break out of the command being built for it.
func TestACommitEscapesWhatItCommits(t *testing.T) {
	cmds := commitCommands("it's", 1)

	if len(cmds) != 2 || cmds[1] != `replace_prior 1, 'it\'s'` {
		t.Errorf("commit issued %v, want the quote escaped", cmds)
	}
}
