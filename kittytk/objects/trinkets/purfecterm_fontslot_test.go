package trinkets

import "testing"

// The gfx trinket resolves a cell's font family from its slot: slot 0 (and any
// slot this terminal has not configured) paints in the primary "ui-term" face;
// a slot configured via OSC 7004 and selected with SGR 11-20 paints in its
// family.
func TestGfxCellFamilyFromSlot(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term") // the primary (slot 0)
	buf := term.Terminal().Buffer()

	// Configure slot 2 -> "Comic Mono" (OSC 7004), then paint 'A' in the
	// primary and 'B' in slot 2 (SGR 12).
	term.Feed([]byte("\x1b]7004;f;2;Comic Mono\x07A\x1b[12mB\x1b[10mC"))

	a := buf.GetVisibleCell(0, 0) // primary
	b := buf.GetVisibleCell(1, 0) // slot 2
	c := buf.GetVisibleCell(2, 0) // back to primary (SGR 10)

	if fam := term.cellFamily(buf, &a); fam != "ui-term" {
		t.Errorf("slot-0 cell A: family = %q, want ui-term", fam)
	}
	if fam := term.cellFamily(buf, &b); fam != "Comic Mono" {
		t.Errorf("slot-2 cell B: family = %q, want Comic Mono", fam)
	}
	if fam := term.cellFamily(buf, &c); fam != "ui-term" {
		t.Errorf("reset-to-slot-0 cell C: family = %q, want ui-term", fam)
	}
}

// A slot with no configured family inherits slot 0's family — the primary,
// never an empty string (which would confuse the rasterizer's default).
func TestGfxCellFamilyUnsetSlotInheritsPrimary(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	buf := term.Terminal().Buffer()

	// SGR 15 selects slot 5, which is never configured for this terminal.
	term.Feed([]byte("\x1b[15mZ"))
	z := buf.GetVisibleCell(0, 0)
	if got := z.Font; got != 5 {
		t.Fatalf("SGR 15 should select slot 5, got %d", got)
	}
	if fam := term.cellFamily(buf, &z); fam != "ui-term" {
		t.Errorf("unconfigured slot 5 should fall back to primary, got %q", fam)
	}
}

// An app-configured script-class font (OSC 7005) overrides the engine's
// ui-term-<script> default for a glyph of that script on the SDL/gfx path — the
// same map the standalone gtk/qt renderers honor. An explicit font slot still
// wins over it, and non-script (Latin) cells are untouched.
func TestGfxCellFamilyScriptFontOSC(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	buf := term.Terminal().Buffer()

	// OSC 7005: hebrew -> "My Hebrew"; then paint a Hebrew alef and a Latin 'A'.
	term.Feed([]byte("\x1b]7005;s;hebrew;My Hebrew\x07אA"))
	alef := buf.GetVisibleCell(0, 0)
	latin := buf.GetVisibleCell(1, 0)

	if fam := term.cellFamily(buf, &alef); fam != "My Hebrew" {
		t.Errorf("Hebrew cell should use the OSC-7005 font, got %q", fam)
	}
	if fam := term.cellFamily(buf, &latin); fam != "ui-term" {
		t.Errorf("Latin cell should stay on the primary, got %q", fam)
	}

	// A hebrew cell painted in an explicit slot uses the slot, not the script map.
	buf.SetFontSlot(3, "Slot Three")
	term.Feed([]byte("\x1b[13mב")) // SGR 13 = slot 3, Hebrew bet
	bet := buf.GetVisibleCell(2, 0)
	if fam := term.cellFamily(buf, &bet); fam != "Slot Three" {
		t.Errorf("explicit slot should win over the script map, got %q", fam)
	}
}
