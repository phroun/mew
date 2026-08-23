package trinkets

// Submenus nested more than one level deep, over the wire.
//
// A submenu has no verb and no property: menuitem's Append makes the Menu on
// the first child a menuitem is given, so depth is spelled by nesting
// children={} and nothing else. One level was already covered; the question
// this answers is whether the same rule keeps applying underneath itself, and
// whether a trigger from the bottom of a chain reaches the registry the same
// way a top-level item's does.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

func TestWireSubMenusNestArbitrarilyDeep(t *testing.T) {
	commands := core.NewCommandRegistry()
	deep := 0
	commands.Register("deep.bottom", func() { deep++ })

	f, _ := buildUI(t, commands, `
bar=new menubar children={
	new menu caption="&Nested" children={
		new menuitem caption="&Deeper" children={
			new menuitem caption="Level &2" children={
				new menuitem caption="Level &3" children={
					new menuitem caption="Level &4" action=deep.bottom
					new menuitem separator
					new menuitem caption="Also at level 4"
				}
				new menuitem caption="Also at level 3"
			}
			new menuitem caption="Also at level 2"
		}
	}
}
`)

	bar := f.targets[0].(interface{ Menus() []*Menu })
	top := bar.Menus()[0]

	// Walk the chain down, checking at each step that the item carrying
	// children became a submenu and that its siblings did not.
	item := top.Items()[0]
	for level := 2; level <= 4; level++ {
		if item.SubMenu == nil {
			t.Fatalf("level %d: no submenu on %q", level, item.Text)
		}
		items := item.SubMenu.Items()
		if len(items) != 2 && level != 4 {
			t.Fatalf("level %d: %d items, want 2", level, len(items))
		}
		if items[len(items)-1].SubMenu != nil {
			t.Errorf("level %d: last sibling should be a leaf", level)
		}
		item = items[0]
	}

	// item is now the level-4 leaf. Its two siblings came through too. The
	// accelerator marker is consumed at every level, not only the top: Text
	// is the caption without it.
	if item.Text != "Level 4" {
		t.Fatalf("bottom item = %q", item.Text)
	}
	bottom := top.Items()[0].SubMenu.Items()[0].SubMenu.Items()[0].SubMenu
	if got := len(bottom.Items()); got != 3 {
		t.Fatalf("level 4 items = %d, want 3", got)
	}
	if !bottom.Items()[1].Separator {
		t.Error("separator inside a level-4 submenu did not survive")
	}

	// BindCommands recurses, so the deepest item dispatches like any other.
	top.BindCommands(commands)
	item.Trigger()
	if deep != 1 {
		t.Errorf("deep.bottom dispatched %d times, want 1", deep)
	}
}
