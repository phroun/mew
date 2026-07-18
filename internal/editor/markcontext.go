package editor

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/runenames"

	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/window"
)

// caretMarkContext describes the zero-width character backspace would delete
// at the caret — a combining diacritic, a ZWJ, a direction mark — for the
// modebar's context slot ("" when backspace would remove an ordinary rune).
// Combining marks are shown attached to a dotted-circle placeholder, followed
// by the codepoint and its Unicode name, so cursoring through a combined glyph
// always tells which stroke a backspace would remove:
//
//	◌́ U+0301 combining acute accent
//	U+200F right-to-left mark
func (e *Editor) caretMarkContext(w *window.Window) string {
	if w == nil || w.Buffer == nil {
		return ""
	}
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	runes := []rune(line)
	pos := w.CursorPos().Rune
	if pos <= 0 || pos > len(runes) {
		return ""
	}
	r := runes[pos-1]
	if r < 0x20 || r == 0x7F {
		return "" // controls/tabs render as visible ^X / tab cells, not marks
	}
	if textwidth.Rune(r) != 0 {
		return "" // an ordinary visible rune: nothing to explain
	}
	name := strings.ToLower(runenames.Name(r))
	if name == "" {
		name = "unnamed"
	}
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) {
		// A combining mark renders onto the dotted-circle placeholder.
		return fmt.Sprintf("◌%c U+%04X %s", r, r, name)
	}
	// Other zero-width characters (ZWJ, direction marks, ...) have no visual
	// form to attach; the codepoint and name identify them.
	return fmt.Sprintf("U+%04X %s", r, name)
}
