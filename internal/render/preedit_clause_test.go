package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// converting builds a viewport composing "羅なに" at the start of an empty
// line, with the clause the input method is converting marked.
func converting(clauseStart, clauseLen int) *viewport.Viewport {
	buf := buffer.NewFromString("\n\n")
	w := &viewport.Viewport{Type: viewport.DocViewport, Buffer: buf, Caret: buf.NewCaret()}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.SetPreedit(viewport.Preedit{
		Text: []rune("羅なに"), Caret: clauseStart + clauseLen,
		ClauseStart: clauseStart, ClauseLen: clauseLen,
	})
	return w
}

// A Japanese composition is several clauses and the candidate list converts one
// of them at a time: "らなに" becomes "羅なに" with the tail still in kana. The
// clause being converted is painted in the composition's own colour and the
// rest is dimmed, because otherwise the untouched kana reads as characters the
// composition failed to replace — which is exactly how it was first reported.
func TestTheConvertingClausePaintsApartFromTheRest(t *testing.T) {
	sr, _ := testRenderer()
	w := converting(0, 1)

	out := sr.prepareLineForDisplay("", "\n", 40, 0, w, 0, selectionRange{}, nil, nil)

	ime, rest := sr.col(w, "ime"), sr.col(w, "imeInactive")
	if ime == rest {
		t.Fatal("this scheme paints a clause and the rest alike; nothing to tell apart")
	}
	if !strings.Contains(out, ime+"羅") {
		t.Errorf("the converting clause is not in the composition colour: %q", out)
	}
	if !strings.Contains(out, rest+"な") {
		t.Errorf("the unconverted clauses are not dimmed: %q", out)
	}
}

// With no clause reported the whole composition is the active material, which
// is every composition that is BUILT rather than converted — an accent palette,
// a romaji run before conversion starts.
func TestAClauselessCompositionIsAllOnePiece(t *testing.T) {
	sr, _ := testRenderer()
	w := converting(0, 0)

	out := sr.prepareLineForDisplay("", "\n", 40, 0, w, 0, selectionRange{}, nil, nil)

	if strings.Contains(out, sr.col(w, "imeInactive")) {
		t.Errorf("part of a clauseless composition was dimmed: %q", out)
	}
}
