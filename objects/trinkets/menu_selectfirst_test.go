package trinkets

import "testing"

// SelectFirstItem lands on the first enabled item, skipping a leading
// separator and a disabled item - the behavior used when a menu is opened
// from the keyboard (Down/Space/Enter on a focused menu bar).
func TestMenuSelectFirstItemSkipsSeparatorAndDisabled(t *testing.T) {
	m := NewMenu("Test")
	m.AddSeparator()
	disabled := NewMenuItem("Disabled")
	disabled.SetEnabled(false)
	m.AddItem(disabled)
	m.AddItem(NewMenuItem("First Real"))
	m.AddItem(NewMenuItem("Second"))

	m.SelectFirstItem()

	it := m.CurrentItem()
	if it == nil || it.Text != "First Real" {
		t.Fatalf("SelectFirstItem should land on the first enabled item, got %v", it)
	}
}
