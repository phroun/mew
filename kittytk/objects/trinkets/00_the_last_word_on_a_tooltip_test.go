package trinkets

// What the desktop does with a tooltip nothing else wanted.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// bareDesktop is a desktop on a cell surface with a status bar to lend.
func bareDesktop(t *testing.T) (*Desktop, *StatusBar) {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	bar := NewStatusBar()
	bar.SetText("Ready")
	d.SetStatusBar(bar)
	return d, bar
}

// On a surface with no popups the status bar says it -- and says what it was
// saying again afterwards, since it was only lent.
func TestTheStatusBarLendsItsRowAndGetsItBack(t *testing.T) {
	d, bar := bareDesktop(t)
	label := NewLabel("x")

	if !d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label}) {
		t.Fatal("the desktop refused a tooltip nothing else took")
	}
	if got := bar.Text(); got != "the whole fingerprint" {
		t.Errorf("the status bar says %q", got)
	}

	d.HideTooltip(label)
	if got := bar.Text(); got != "Ready" {
		t.Errorf("after the tooltip went the status bar says %q, not what it said before", got)
	}
}

// A tooltip is an answer to a pointer resting somewhere. A reader who has
// started typing is not resting, so the first keystroke takes it down.
func TestAKeystrokeTakesTheTooltipDown(t *testing.T) {
	d, bar := bareDesktop(t)
	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: NewLabel("x")})

	d.HandleKeyPress(core.KeyPressEvent{Key: "j", Text: "j"})

	if got := bar.Text(); got != "Ready" {
		t.Errorf("a key was pressed and the status bar still says %q", got)
	}
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("a key was pressed and the desktop is still showing %q", showing)
	}
}

// The status bar is one row, so the breaks in a tooltip become separators
// rather than disappearing, and a blank line between paragraphs reads as the
// one break it looks like.
func TestABrokenTooltipReadsAsOneRow(t *testing.T) {
	d, bar := bareDesktop(t)
	d.ShowTooltip(core.TooltipRequest{
		Text: "first line\nsecond line\n\nafter a gap",
		From: NewLabel("x"),
	})

	want := "first line · second line · after a gap"
	if got := bar.Text(); got != want {
		t.Errorf("the status bar says %q, want %q", got, want)
	}
	if strings.Contains(bar.Text(), "\n") {
		t.Error("a break survived into the one row there is")
	}
}
