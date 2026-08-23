package text

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/text/fonts"
)

// loadFallbackFiles registers existing files at the TAIL of the
// fallback order (embedded faces keep priority), skips missing and
// unparseable files, and bumps the epoch so shape caches flush.
func TestLoadFallbackFilesAppendsToChainTail(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "Extra.ttf")
	if err := os.WriteFile(good, fonts.SansRegular, 0o644); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, "NotAFont.ttf")
	if err := os.WriteFile(junk, []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEngine()
	before := e.Epoch()
	embedded := len(e.db.order)

	n := e.loadFallbackFiles([]string{
		good,
		junk,
		filepath.Join(dir, "missing.ttf"),
	})
	if n != 1 {
		t.Fatalf("loaded %d faces, want 1 (junk and missing skipped)", n)
	}
	if e.Epoch() == before {
		t.Error("epoch did not change after registering a fallback face")
	}
	order := e.db.order
	if len(order) != embedded+1 {
		t.Fatalf("fallback order has %d families, want %d", len(order), embedded+1)
	}
	if got := order[len(order)-1]; got != "sys:extra.ttf" {
		t.Errorf("system face registered at %q, want tail entry sys:extra.ttf", got)
	}
}

// LoadSystemFallbacks never fails - on a machine with none of the
// known fonts it simply registers nothing.
func TestLoadSystemFallbacksIsSafe(t *testing.T) {
	e := NewEngine()
	_ = e.LoadSystemFallbacks()
}
