//go:build sdl

package sdl

// The Dock menu the desktop gives is written into the native menu as given:
// every item in order, its mark and indentation kept, a submenu under the
// item that opens it, and each item that does something tagged with the
// action it runs. Choosing an item posts that action -- and a tag left over
// from a menu since replaced runs nothing.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/platform"
)

// dockRecorder writes menus out as text: each menu is a numbered list of its
// lines, and an item opening a submenu names it.
type dockRecorder struct {
	menus [][]string
	items map[uintptr][2]int // item handle -> menu index, line index
}

func (r *dockRecorder) newMenu() uintptr {
	r.menus = append(r.menus, nil)
	return uintptr(len(r.menus))
}

func (r *dockRecorder) addItem(menu uintptr, title string, tag int, enabled bool, mark platform.DockMark, indent int) uintptr {
	m := int(menu) - 1
	line := fmt.Sprintf("%s%s tag=%d", strings.Repeat("  ", indent), title, tag)
	if !enabled {
		line += " off"
	}
	if mark != platform.DockMarkNone {
		line += fmt.Sprintf(" mark=%d", mark)
	}
	r.menus[m] = append(r.menus[m], line)
	if r.items == nil {
		r.items = map[uintptr][2]int{}
	}
	h := uintptr(1000 + len(r.items))
	r.items[h] = [2]int{m, len(r.menus[m]) - 1}
	return h
}

func (r *dockRecorder) addSeparator(menu uintptr) {
	m := int(menu) - 1
	r.menus[m] = append(r.menus[m], "-----")
}

func (r *dockRecorder) setSubmenu(item, submenu uintptr) {
	at := r.items[item]
	r.menus[at[0]][at[1]] += fmt.Sprintf(" > menu%d", submenu)
}

func (r *dockRecorder) menu(n int) string { return strings.Join(r.menus[n-1], "\n") }

func setDockForTest(t *testing.T, build func() []platform.DockMenuItem) *[]func() {
	t.Helper()
	posted := &[]func(){}
	dockMenu.mu.Lock()
	dockMenu.build = build
	dockMenu.post = func(fn func()) { *posted = append(*posted, fn) }
	dockMenu.actions = nil
	dockMenu.mu.Unlock()
	t.Cleanup(func() {
		dockMenu.mu.Lock()
		dockMenu.build, dockMenu.post, dockMenu.actions = nil, nil, nil
		dockMenu.mu.Unlock()
	})
	return posted
}

func TestTheDockMenuIsWrittenAsGiven(t *testing.T) {
	var ran []string
	do := func(what string) func() { return func() { ran = append(ran, what) } }
	posted := setDockForTest(t, func() []platform.DockMenuItem {
		return []platform.DockMenuItem{
			{Title: "Demo", Submenu: []platform.DockMenuItem{
				{Title: "Show Demo", Action: do("show")},
				{Separator: true},
				{Title: "Quit Demo", Action: do("quit")},
			}},
			{Title: "Main", Indent: 1, Mark: platform.DockMarkCheck, Action: do("main")},
			{Title: "◇ Tucked", Indent: 1, Action: do("tucked")},
			{Title: "Label"},
			{Separator: true},
			{Title: "Hide Desktop", Disabled: true, Action: do("hide")},
		}
	})

	r := &dockRecorder{}
	if got := buildDockMenu(r); got != 1 {
		t.Fatalf("the menu built is handle %d, want the first", got)
	}
	top := strings.Join([]string{
		"Demo tag=0 > menu2",
		"  Main tag=3 mark=1",
		"  ◇ Tucked tag=4",
		"Label tag=0 off",
		"-----",
		"Hide Desktop tag=0 off",
	}, "\n")
	if got := r.menu(1); got != top {
		t.Errorf("the menu reads\n%s\nwant\n%s", got, top)
	}
	sub := "Show Demo tag=1\n-----\nQuit Demo tag=2"
	if got := r.menu(2); got != sub {
		t.Errorf("Demo's submenu reads\n%s\nwant\n%s", got, sub)
	}

	for _, tag := range []int{3, 2, 0, 9, -1} {
		dockItemChosen(tag)
	}
	if len(*posted) != 2 {
		t.Fatalf("%d actions were posted, want 2 (tags 0, 9 and -1 name nothing)", len(*posted))
	}
	for _, fn := range *posted {
		fn()
	}
	if got := strings.Join(ran, " "); got != "main quit" {
		t.Errorf("the posted actions ran %q, want main then quit", got)
	}
}

// Each menu replaces the last one's actions: a tag from before names nothing
// it used to.
func TestADockMenuReplacesTheLastOnesActions(t *testing.T) {
	var ran []string
	title := "First"
	posted := setDockForTest(t, func() []platform.DockMenuItem {
		name := title
		return []platform.DockMenuItem{{Title: name, Action: func() { ran = append(ran, name) }}}
	})
	buildDockMenu(&dockRecorder{})
	title = "Second"
	buildDockMenu(&dockRecorder{})
	dockItemChosen(1)
	for _, fn := range *posted {
		fn()
	}
	if len(ran) != 1 || ran[0] != "Second" {
		t.Errorf("tag 1 ran %q, want the second menu's action", ran)
	}
}

// With nothing set there is no menu of ours, and choosing does nothing.
func TestNoDockMenuUntilOneIsSet(t *testing.T) {
	setDockForTest(t, nil)
	if got := buildDockMenu(&dockRecorder{}); got != 0 {
		t.Errorf("with no builder the menu is handle %d, want none", got)
	}
	dockItemChosen(1)
}
