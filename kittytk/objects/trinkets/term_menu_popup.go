package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// openTermMenu opens a right-click menu of items as a popup, in the
// presentation PurfecTerm's terminal menu uses: the same rows, keys and
// greying, measured by the same function in font. owner is the trinket the menu
// belongs to, repainted as the pointer moves over it; the menu opens at local,
// a point in target's own coordinates, kept on the screen. Choosing an item
// closes the menu and runs the item's action; so does a press anywhere else,
// without running anything.
func openTermMenu(owner core.Trinket, font *core.Font, pc core.PopupController, id string, target core.Trinket, local core.UnitPoint, items []termMenuItem) {
	items = withShortcuts(owner, items)
	lay := termMenuLayoutFrom(core.FindGraphicalFrames(owner), font,
		termMenuScreenMetrics(pc), items)
	height := core.Unit(0)
	for _, it := range items {
		if it.separator {
			height += lay.sepH
		} else {
			height += lay.rowH
		}
	}
	height += 2 * lay.padTop
	at := pc.MapToScreen(target, local)
	screen := pc.ScreenBounds()
	if at.X+lay.width > screen.X+screen.Width {
		at.X = screen.X + screen.Width - lay.width
	}
	if at.Y+height > screen.Y+screen.Height {
		at.Y = screen.Y + screen.Height - height
	}
	menuBounds := gridPopupRect(owner, termMenuScreenMetrics(pc),
		core.UnitRect{X: at.X, Y: at.Y, Width: lay.width, Height: height})
	hovered := -1

	itemAt := func(y core.Unit) int {
		pos := lay.padTop
		for i, it := range items {
			h := lay.rowH
			if it.separator {
				h = lay.sepH
			}
			if y >= pos && y < pos+h {
				if it.separator {
					return -1
				}
				return i
			}
			pos += h
		}
		return -1
	}

	pc.RegisterPopup(&core.PopupRequest{
		ID:     id,
		Bounds: menuBounds,
		Paint: func(p *core.Painter) {
			bg := style.DefaultStyle().WithFg(style.RGB(32, 32, 32)).WithBg(style.RGB(238, 238, 238))
			hover := style.DefaultStyle().WithFg(style.RGB(255, 255, 255)).WithBg(style.RGB(56, 120, 220))
			p.FillRect(core.UnitRect{X: menuBounds.X, Y: menuBounds.Y, Width: menuBounds.Width, Height: menuBounds.Height}, ' ', bg)
			// The 1-pixel outer frame every popup gets, in the padded
			// margin just outside the bounds (graphical only).
			if p.Graphical() {
				lineStyle := style.DefaultStyle().WithBg(owner.GetScheme().GetMenuSeparator().Fg)
				paintPopupOuterStroke(p, menuBounds, p.DeviceScale(), lineStyle, 0, 0, false)
			}
			pos := menuBounds.Y + lay.padTop
			for i, it := range items {
				if it.separator {
					paintTermMenuSeparator(p, menuBounds, pos, lay)
					pos += lay.sepH
					continue
				}
				paintTermMenuItem(p, menuBounds, pos, lay, it, i == hovered, bg, hover)
				pos += lay.rowH
			}
		},
		HandleMouseMove: func(event core.MouseMoveEvent) bool {
			if !menuBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				return false
			}
			idx := itemAt(event.Y - menuBounds.Y)
			if idx >= 0 && items[idx].disabled {
				idx = -1
			}
			if idx != hovered {
				hovered = idx
				owner.Update()
			}
			return true
		},
		HandleMousePress: func(event core.MousePressEvent) bool {
			idx := itemAt(event.Y - menuBounds.Y)
			pc.UnregisterPopup(id)
			if idx >= 0 && !items[idx].disabled && items[idx].action != nil {
				items[idx].action()
			}
			owner.Update()
			return true
		},
	})
	owner.Update()
}
