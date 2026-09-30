package trinkets

// A dialog embeds a window it copies out of window.NewWindow, and a copied
// window still names the one it was copied from as itself -- so its own
// Update marked that orphan, and a title-bar focus change reached the screen
// only when something else repainted the dialog. Each dialog's window names
// itself.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

func TestADialogsWindowRepaintsItself(t *testing.T) {
	core.SetTextMeasurer(nil)
	for _, tc := range []struct {
		name string
		win  *window.Window
	}{
		{"input dialog", &NewInputDialog("Rename", "New name:", "draft").Window},
		{"message box", &NewMessageBox("Exit", "Are you sure?", ButtonYes|ButtonNo).Window},
		{"file dialog", &NewFileDialog(FileDialogOpen).Window},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.win.Self(); got != core.Trinket(tc.win) {
				t.Errorf("the window names %p as itself, want %p", got, tc.win)
			}
			before := tc.win.SubtreeRepaintRevision()
			tc.win.SetTitleFocus(window.TitleFocusClose)
			if tc.win.SubtreeRepaintRevision() == before {
				t.Error("focusing the title bar did not mark the window for repaint")
			}
		})
	}
}
