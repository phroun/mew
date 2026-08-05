//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The mew Editor is a paste target: it embeds *PurfecTerm, so a bracketed paste
// the host received reaches inner mew through HandlePaste -> sendPaste, which
// re-brackets per mew's own paste mode. This is the mew editor trinket the user
// pastes into. The assertion is compile-time; the reference keeps vet quiet.
var _ core.PasteHandler = (*Editor)(nil)

func TestMewEditorIsPasteHandler(t *testing.T) {
	var _ core.PasteHandler = (*Editor)(nil)
}
