//go:build sdl

package sdl

// The desktop's menus are written into the OS menu bar as given: each menu in
// order, its items with their marks, submenus under the items that open them,
// and each item that does something tagged with its entry. A KittyTK key
// becomes the key equivalent the OS takes, when the OS can say it and it would
// not be taken from under someone typing. Choosing an item posts its action;
// the OS asking whether it may run gets the item's own answer; and a tag from
// items since rewritten names nothing.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/platform"
)

func TestAKittyTKKeyBecomesAMacKeyEquivalent(t *testing.T) {
	for _, tc := range []struct {
		key   string
		equiv string
		mods  uint
		ok    bool
	}{
		{"s-c", "c", macCommandFlag, true},
		{"s-C", "c", macCommandFlag | macShiftFlag, true},
		{"S-s-z", "z", macCommandFlag | macShiftFlag, true},
		{"^X", "x", macControlFlag, true},
		{"C-x", "x", macControlFlag, true},
		{"M-x", "x", macOptionFlag, true},
		{"s-,", ",", macCommandFlag, true},
		{"s-Left", "", macCommandFlag, true},
		{"s-Return", "\r", macCommandFlag, true},
		{"s-Backspace", "\b", macCommandFlag, true},
		{"C-FDel", "", macControlFlag, true},
		{"F4", "", 0, true},
		{"M-S-F4", "", macOptionFlag | macShiftFlag, true},
		{"s-F35", string(rune(0xF704 + 34)), macCommandFlag, true},
		// Nothing the OS can say, or nothing it should take.
		{"", "", 0, false},
		{"a", "", 0, false},
		{"Return", "", 0, false},
		{"S-Tab", "", 0, false},
		{"m-x", "", 0, false},
		{"H-s-x", "", 0, false},
		{"m-s-x", "", 0, false},
		{"G-s-x", "", 0, false},
		{"C-x C-s", "", 0, false},
		{"s-F36", "", 0, false},
		{"s-F0", "", 0, false},
		{"s-Frob", "", 0, false},
	} {
		equiv, mods, ok := macKeyEquivalent(tc.key)
		if equiv != tc.equiv || mods != tc.mods || ok != tc.ok {
			t.Errorf("%q became (%q, %#x, %v), want (%q, %#x, %v)",
				tc.key, equiv, mods, ok, tc.equiv, tc.mods, tc.ok)
		}
	}
}

// menuRecorder writes the bar out as text.
type menuRecorder struct {
	menus  map[uintptr][]string
	titles map[uintptr]string
	next   uintptr
	items  map[uintptr][2]uintptr // item -> menu, line
	bar    []string
	fills  int
	app    uintptr // the application menu, 0 for a host without one
}

func newMenuRecorder() *menuRecorder {
	return &menuRecorder{menus: map[uintptr][]string{}, titles: map[uintptr]string{}, items: map[uintptr][2]uintptr{}}
}

func (r *menuRecorder) newMenu(title string) uintptr {
	r.next++
	r.titles[r.next] = title
	return r.next
}

func (r *menuRecorder) clear(menu uintptr) {
	r.menus[menu] = nil
	r.fills++
}

func (r *menuRecorder) addItem(menu uintptr, title string, tag int, checked bool, equiv string, mods uint) uintptr {
	line := fmt.Sprintf("%s tag=%d", title, tag)
	if checked {
		line = "✓ " + line
	}
	if equiv != "" {
		line += fmt.Sprintf(" key=%q/%#x", equiv, mods)
	}
	r.menus[menu] = append(r.menus[menu], line)
	r.next++
	r.items[r.next] = [2]uintptr{menu, uintptr(len(r.menus[menu]) - 1)}
	return r.next
}

func (r *menuRecorder) addSeparator(menu uintptr) { r.menus[menu] = append(r.menus[menu], "-----") }

func (r *menuRecorder) setSubmenu(item, submenu uintptr) {
	at := r.items[item]
	r.menus[at[0]][at[1]] += " > " + r.titles[submenu]
}

func (r *menuRecorder) appMenu() uintptr { return r.app }

func (r *menuRecorder) addServices(menu uintptr) { r.menus[menu] = append(r.menus[menu], "Services >") }

func (r *menuRecorder) setBar(menus []uintptr, titles []string) {
	r.bar = nil
	for i, m := range menus {
		r.bar = append(r.bar, fmt.Sprintf("%s:%s", titles[i], r.titles[m]))
	}
}

func (r *menuRecorder) read(menu uintptr) string { return strings.Join(r.menus[menu], "\n") }

func postedInto(t *testing.T) *[]func() {
	t.Helper()
	posted := &[]func(){}
	nativeMenus.mu.Lock()
	nativeMenus.post = func(fn func()) { *posted = append(*posted, fn) }
	nativeMenus.nextTag = 0
	nativeMenus.mu.Unlock()
	t.Cleanup(func() {
		nativeMenus.mu.Lock()
		nativeMenus.post = nil
		nativeMenus.entries = nil
		nativeMenus.models = nil
		nativeMenus.mu.Unlock()
	})
	return posted
}

func TestTheNativeMenuBarIsWrittenAsGiven(t *testing.T) {
	posted := postedInto(t)
	var ran []string
	do := func(what string) func() { return func() { ran = append(ran, what) } }
	allowed := true
	file := platform.NativeMenu{Title: "File", Items: func() []platform.NativeMenuItem {
		return []platform.NativeMenuItem{
			{Title: "Open", Key: "s-o", Action: do("open")},
			{Title: "Wrap", Checked: true, Action: do("wrap")},
			{Separator: true},
			{Title: "Recent", Submenu: &platform.NativeMenu{Title: "Recent", Items: func() []platform.NativeMenuItem {
				return []platform.NativeMenuItem{{Title: "notes.txt", Action: do("notes")}}
			}}},
			{Title: "Save", Key: "s-s", Action: do("save"), Enabled: func() bool { return allowed }},
			{Title: "Label"},
		}
	}}
	edit := platform.NativeMenu{Title: "Edit", Items: func() []platform.NativeMenuItem { return nil }}

	r := newMenuRecorder()
	setNativeBar(r, platform.NativeMenu{}, []platform.NativeMenu{file, edit})
	if got := strings.Join(r.bar, " "); got != "File:File Edit:Edit" {
		t.Errorf("the bar holds %q", got)
	}
	want := strings.Join([]string{
		fmt.Sprintf("Open tag=1 key=%q/%#x", "o", macCommandFlag),
		"✓ Wrap tag=2",
		"-----",
		"Recent tag=0 > Recent",
		fmt.Sprintf("Save tag=4 key=%q/%#x", "s", macCommandFlag),
		"Label tag=5",
	}, "\n")
	if got := r.read(1); got != want {
		t.Errorf("File reads\n%s\nwant\n%s", got, want)
	}

	// The OS asking, and choosing.
	if !nativeItemEnabled(1) || nativeItemEnabled(5) || nativeItemEnabled(99) {
		t.Error("Open should be enabled, the label and an unknown tag not")
	}
	allowed = false
	if nativeItemEnabled(4) {
		t.Error("Save says it may run though its item says no")
	}
	nativeItemChosen(3) // notes.txt, in the submenu
	nativeItemChosen(1)
	nativeItemChosen(99)
	nativeItemChosen(5) // the label, which does nothing
	if len(*posted) != 2 {
		t.Fatalf("%d actions were posted, want 2", len(*posted))
	}
	for _, fn := range *posted {
		fn()
	}
	if got := strings.Join(ran, " "); got != "notes open" {
		t.Errorf("choosing ran %q, want notes then open", got)
	}

	// Written again when the OS asks: new tags, the old ones naming nothing,
	// the submenu's with them.
	fillNativeMenu(r, 1)
	if nativeItemEnabled(1) || nativeItemEnabled(3) {
		t.Error("a tag from before the menu was rewritten still names an entry")
	}
	if !strings.HasPrefix(r.read(1), "Open tag=6 ") {
		t.Errorf("rewritten, File reads\n%s", r.read(1))
	}
	nativeMenus.mu.Lock()
	models := len(nativeMenus.models)
	nativeMenus.mu.Unlock()
	if models != 3 {
		t.Errorf("%d menus are remembered after the rewrite, want 3 (File, Edit, the new Recent)", models)
	}

	// A handle this bar did not write is left alone.
	fills := r.fills
	fillNativeMenu(r, 12345)
	if r.fills != fills {
		t.Error("a menu nobody knows was cleared")
	}
}

// Setting the bar again starts from nothing: last time's tags name nothing.
func TestSettingTheNativeBarAgainForgetsTheLast(t *testing.T) {
	postedInto(t)
	one := platform.NativeMenu{Title: "One", Items: func() []platform.NativeMenuItem {
		return []platform.NativeMenuItem{{Title: "x", Action: func() {}}}
	}}
	r := newMenuRecorder()
	setNativeBar(r, platform.NativeMenu{}, []platform.NativeMenu{one})
	if !nativeItemEnabled(1) {
		t.Fatal("the first bar's item is not enabled")
	}
	setNativeBar(r, platform.NativeMenu{}, nil)
	if nativeItemEnabled(1) {
		t.Error("an item from the last bar still names an entry")
	}
	if len(r.bar) != 0 {
		t.Errorf("the empty bar holds %q", r.bar)
	}
}

// Menus set before the application is up are kept for Run to write.
func TestNativeMenusSetEarlyAreKept(t *testing.T) {
	t.Cleanup(func() {
		nativeMenus.mu.Lock()
		nativeMenus.set, nativeMenus.haveSet, nativeMenus.post = nil, false, nil
		nativeMenus.mu.Unlock()
	})
	p := &Platform{}
	p.SetNativeMenus(platform.NativeMenu{Title: "App"}, []platform.NativeMenu{{Title: "Early"}})
	nativeMenus.mu.Lock()
	have, n, app := nativeMenus.haveSet, len(nativeMenus.set), nativeMenus.app.Title
	nativeMenus.mu.Unlock()
	if !have || n != 1 || app != "App" {
		t.Errorf("set before Run, kept %v with %d menus and application menu %q", have, n, app)
	}
	nativeMenus.mu.Lock()
	post := nativeMenus.post
	nativeMenus.mu.Unlock()
	if post == nil {
		t.Error("the platform's Post was not kept for running what is chosen")
	}
}

// The OS's application menu is filled from the one given, with the OS's
// Services in a group of their own before the last group; and written again
// when the OS asks, like any menu of the bar's.
func TestTheApplicationMenuIsTheDesktopsOwn(t *testing.T) {
	postedInto(t)
	desk := platform.NativeMenu{Title: "Ψ", Items: func() []platform.NativeMenuItem {
		return []platform.NativeMenuItem{
			{Title: "About Desktop", Action: func() {}},
			{Separator: true},
			{Title: "Event Viewer", Action: func() {}},
			{Separator: true},
			{Title: "Exit Desktop", Key: "M-^X", Action: func() {}},
		}
	}}
	r := newMenuRecorder()
	r.app = r.newMenu("KittyTK")
	setNativeBar(r, desk, []platform.NativeMenu{{Title: "Demo"}})
	want := strings.Join([]string{
		"About Desktop tag=1",
		"-----",
		"Event Viewer tag=2",
		"-----",
		"Services >",
		"-----",
		fmt.Sprintf("Exit Desktop tag=3 key=%q/%#x", "x", macControlFlag|macOptionFlag),
	}, "\n")
	if got := r.read(r.app); got != want {
		t.Errorf("the application menu reads\n%s\nwant\n%s", got, want)
	}
	if got := strings.Join(r.bar, " "); got != "Demo:Demo" {
		t.Errorf("after the application menu the bar holds %q", got)
	}
	fillNativeMenu(r, r.app)
	if got := strings.Count(r.read(r.app), "Services"); got != 1 {
		t.Errorf("written again, the application menu holds Services %d times", got)
	}
	if nativeItemEnabled(1) || !nativeItemEnabled(4) {
		t.Error("written again, the application menu's old tags still name entries, or its new ones do not")
	}

	// A menu of one group takes Services at its end; an empty one, alone.
	for _, tc := range []struct {
		items []platform.NativeMenuItem
		want  string
	}{
		{[]platform.NativeMenuItem{{Title: "About", Action: func() {}}}, "About tag=7\n-----\nServices >"},
		{nil, "Services >"},
	} {
		items := tc.items
		r := newMenuRecorder()
		r.app = r.newMenu("KittyTK")
		setNativeBar(r, platform.NativeMenu{Items: func() []platform.NativeMenuItem { return items }}, nil)
		if got := r.read(r.app); got != tc.want {
			t.Errorf("the application menu reads\n%s\nwant\n%s", got, tc.want)
		}
	}

	// A menu in the bar is not the application menu, and gets no Services.
	for h, title := range r.titles {
		if title == "Demo" && strings.Contains(r.read(h), "Services") {
			t.Error("a menu in the bar took the Services")
		}
	}
}

// A bar set where there is no application menu forgets the last one: a menu
// the OS later hands out at the same address is an ordinary menu, not the
// application menu, and gets no Services.
func TestTheApplicationMenuIsForgottenWithItsBar(t *testing.T) {
	postedInto(t)
	grouped := func() []platform.NativeMenuItem {
		return []platform.NativeMenuItem{{Title: "a", Action: func() {}}, {Separator: true}, {Title: "b", Action: func() {}}}
	}
	r := newMenuRecorder()
	r.app = r.newMenu("KittyTK")
	reused := r.app
	setNativeBar(r, platform.NativeMenu{Items: grouped}, nil)

	r.app = 0
	r.next = reused - 1 // the next menu made lands where the application menu was
	setNativeBar(r, platform.NativeMenu{}, []platform.NativeMenu{{Title: "Demo", Items: grouped}})
	if got := r.read(reused); strings.Contains(got, "Services") {
		t.Errorf("a menu at the old application menu's address reads\n%s", got)
	}
}
