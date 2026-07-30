package text

import (
	"strings"
	"testing"
)

// The runtime diagnostic reports join=OK for the embedded faces, and names the
// resolved family.
func TestArabicJoinDiagEmbedded(t *testing.T) {
	e := NewEngine()
	got := e.ArabicJoinDiag("ui-term")
	t.Log(got)
	if !strings.Contains(got, "join=OK") {
		t.Errorf("embedded faces should join: %s", got)
	}
	if !strings.Contains(got, "Noto Naskh Arabic") {
		t.Errorf("diag should name the resolved face: %s", got)
	}
}
