package trinkets

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

func TestListViewWidthTracksFontSize(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	dir := os.Getenv("KITTYTK_PROOF_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	widthCells := map[int]int{}
	for _, size := range []int{6, 12} {
		b, err := raster.New(600, 260)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(size)
		m := b.Metrics() // stays 8x16 under font_size

		d := NewDesktop()
		d.SetBackend(b)
		d.SetFont(&core.Font{Name: "ui-text", Size: 12})

		lv := NewListView()
		lv.SetParent(d)
		for i := 1; i <= 12; i++ {
			lv.AddItem(NewListItem("Item " + strconv.Itoa(i)))
		}
		hint := lv.SizeHint()
		widthCells[size] = int(hint.Width / m.UnitsPerCellWidth)

		lv.SetBounds(core.UnitRect{Width: hint.Width, Height: 10 * m.UnitsPerCellHeight})
		b.Clear(style.DefaultStyle())
		lv.Paint(core.NewPainter(b))
		out := filepath.Join(dir, "listview_"+strconv.Itoa(size)+".png")
		if err := b.WritePNG(out); err != nil {
			t.Fatal(err)
		}
		t.Logf("font_size=%d cell=%+v hint.Width=%d (%d cells) -> %s",
			size, m, hint.Width, widthCells[size], out)
	}
	if widthCells[6] != widthCells[12] {
		t.Errorf("ListView width in cells changed with font_size: 6pt=%d 12pt=%d cells",
			widthCells[6], widthCells[12])
	}
	if widthCells[12] != 3 {
		t.Errorf("ListView width = %d cells, want 3", widthCells[12])
	}
}
