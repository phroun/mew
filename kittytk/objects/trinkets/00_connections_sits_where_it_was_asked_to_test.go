package trinkets

import (
	"strings"
	"testing"
)

// menuLayout renders a menu as one line per row, so a test can say where an
// item sits rather than only that it exists.
func menuLayout(m *Menu) []string {
	var out []string
	for _, it := range m.Items() {
		if it.Separator {
			out = append(out, "---")
			continue
		}
		out = append(out, it.Text)
	}
	return out
}

func indexOf(rows []string, want string) int {
	for i, r := range rows {
		if strings.Contains(r, want) {
			return i
		}
	}
	return -1
}

// The desktop's own menu carries Connections whatever any app says, directly
// under About Desktop -- the two things that are about this machine rather
// than about whatever is running on it.
func TestConnectionsSitsUnderAboutDesktop(t *testing.T) {
	d := NewDesktop()

	if rows := menuLayout(d.createSystemMenu()); indexOf(rows, "Connections") != -1 {
		t.Fatalf("the item was offered with nothing installed to answer it: %v", rows)
	}

	d.SetConnectionsOpener(func() {})
	rows := menuLayout(d.createSystemMenu())
	about := indexOf(rows, "About Desktop")
	conn := indexOf(rows, "Connections")
	if about < 0 || conn < 0 {
		t.Fatalf("menu is %v", rows)
	}
	if conn != about+1 {
		t.Errorf("Connections is at %d and About Desktop at %d; it should be the "+
			"next row: %v", conn, about, rows)
	}
}

// On an app's own menu it appears only where the app asked for it, and it sits
// directly above Quit with a rule between them.
func TestConnectionsSitsAboveQuitWhenAsked(t *testing.T) {
	d := NewDesktop()
	d.SetConnectionsOpener(func() {})

	quiet := NewMenu("≡")
	d.appendQuitSection(quiet, "Editor")
	if indexOf(menuLayout(quiet), "Connections") != -1 {
		t.Errorf("an app that did not ask for it got it: %v", menuLayout(quiet))
	}

	d.AddApplication(&mockApp{name: "Editor", showConnections: true})
	asked := NewMenu("≡")
	d.appendQuitSection(asked, "Editor")
	rows := menuLayout(asked)

	conn := indexOf(rows, "Connections")
	quit := indexOf(rows, "Quit")
	if conn < 0 || quit < 0 {
		t.Fatalf("menu is %v", rows)
	}
	if quit != conn+2 || rows[conn+1] != "---" {
		t.Errorf("Connections at %d and Quit at %d should be separated by exactly "+
			"one rule: %v", conn, quit, rows)
	}
}

// And an app that asks gets nothing if nothing can answer, so the item is
// never offered where pressing it would do nothing.
func TestAnAppCannotConjureTheItem(t *testing.T) {
	d := NewDesktop()
	d.AddApplication(&mockApp{name: "Editor", showConnections: true})

	menu := NewMenu("≡")
	d.appendQuitSection(menu, "Editor")
	if rows := menuLayout(menu); indexOf(rows, "Connections") != -1 {
		t.Errorf("an app asked and got an item nothing answers: %v", rows)
	}
}
