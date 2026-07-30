package cli

import "testing"

// RenderedCell carries the per-cell font slot through GetCells so a CLI
// consumer can map it to a family. SGR 12 selects slot 2 (SGR 10 is slot 0).
func TestCLIRenderedCellFont(t *testing.T) {
	term, err := New(Options{Cols: 5, Rows: 1, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	term.FeedString("\x1b[12mZ")

	if f := term.GetCells()[0][0].Font; f != 2 {
		t.Fatalf("RenderedCell.Font = %d, want 2", f)
	}
}
