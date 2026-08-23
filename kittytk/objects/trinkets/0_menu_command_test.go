package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// An item that names a COMMAND holds no key. What prints in its column is
// whatever key means that command, asked when the menu is drawn.
func TestItemWithCommandAdvertisesTheRegistrysKey(t *testing.T) {
	item := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit)

	want := core.DefaultKeyRegistry().KeyForCommand(core.CmdAppQuit)
	if want == "" {
		t.Fatal("app_quit is unbound; the fixture assumes the toolkit's own keymap")
	}
	if got := item.ShortcutDisplay(); got != want {
		t.Errorf("column shows %q, want %q", got, want)
	}
}

// A composed menu asks its composer, which is what makes the answer follow
// the focus: the same item shows a different key in a different situation,
// and nothing at all where nothing means it.
func TestItemAsksTheMenusResolver(t *testing.T) {
	menu := NewMenu("File")
	item := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit)
	menu.AddItem(item)

	answer := "s-q"
	menu.SetKeyResolver(func(command string) string {
		if command == core.CmdAppQuit {
			return answer
		}
		return ""
	})

	if got := item.ShortcutDisplay(); got != "s-q" {
		t.Errorf("column shows %q, want the resolver's s-q", got)
	}

	// Nothing means it here -- a guest has the keyboard -- and that is a real
	// answer, so the column is blank rather than advertising a dead key.
	answer = ""
	if got := item.ShortcutDisplay(); got != "" {
		t.Errorf("column shows %q, want nothing", got)
	}
}

// The resolver reaches items added later, and submenus, since a bar is
// recomposed from scratch whenever its app's menus change.
func TestKeyResolverReachesLaterItemsAndSubmenus(t *testing.T) {
	bar := NewMenuBar()
	bar.SetKeyResolver(func(string) string { return "^Q" })

	menu := NewMenu("File")
	bar.AddMenu(menu)

	later := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit)
	menu.AddItem(later)
	if got := later.ShortcutDisplay(); got != "^Q" {
		t.Errorf("an item added later shows %q, want ^Q", got)
	}

	sub := NewMenu("More")
	deep := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit)
	sub.AddItem(deep)
	menu.AddMenu(sub)
	if got := deep.ShortcutDisplay(); got != "^Q" {
		t.Errorf("an item in a submenu shows %q, want ^Q", got)
	}
}

// A command overrides a legacy shortcut on the same item: it has stopped
// naming a key and started naming a meaning.
func TestCommandOverridesALegacyShortcut(t *testing.T) {
	item := NewMenuItem("&Quit")
	item.SetShortcut(core.NewShortcut("^Z"))
	item.SetCommand(core.CmdAppQuit)

	menu := NewMenu("File")
	menu.AddItem(item)
	menu.SetKeyResolver(func(string) string { return "^Q" })

	if got := item.ShortcutDisplay(); got != "^Q" {
		t.Errorf("column shows %q, want the command's ^Q rather than the stored ^Z", got)
	}
}

// ShortcutText is ink for a key the toolkit does not handle, and still prints
// beside a resolved one -- a command reachable either way advertises both.
func TestShortcutTextStillAppends(t *testing.T) {
	item := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit)
	item.SetShortcutText("^K Q")

	menu := NewMenu("File")
	menu.AddItem(item)
	menu.SetKeyResolver(func(string) string { return "^Q" })

	if got := item.ShortcutDisplay(); got != "^Q ^K Q" {
		t.Errorf("column shows %q, want both spellings", got)
	}

	// ...and with nothing meaning the command here, the literal text is all
	// there is to show.
	menu.SetKeyResolver(func(string) string { return "" })
	if got := item.ShortcutDisplay(); got != "^K Q" {
		t.Errorf("column shows %q, want the literal text alone", got)
	}
}
