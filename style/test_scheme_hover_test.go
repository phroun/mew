package style

import "testing"

// The default hover palette is bright yellow on purple; HoverTextFG is the
// text-only variant.
func TestHoverPaletteDefaults(t *testing.T) {
	s := DefaultScheme()
	if got := s.GetHoverBG(); got != ColorMagenta {
		t.Errorf("HoverBG = %v, want magenta", got)
	}
	if got := s.GetHoverFG(); got != ColorBrightYellow {
		t.Errorf("HoverFG = %v, want bright yellow", got)
	}
	if got := s.GetHoverTextFG(); got != ColorBrightYellow {
		t.Errorf("HoverTextFG = %v, want bright yellow", got)
	}
}

// Hovered values fall back to the HoverBG/HoverFG pair when left nil.
func TestHoveredDefaultsUseHoverPair(t *testing.T) {
	s := DefaultScheme()
	for name, got := range map[string]CellStyle{
		"button":     s.GetHoveredButton(),
		"dockItem":   s.GetHoveredDockItem(),
		"titleBtn":   s.GetHoveredTitleBarButton(),
		"menuBar":    s.GetHoveredMenuBar(),
		"splitterH":  s.GetHoveredSplitterHandle(),
		"splitterT":  s.GetHoveredSplitterTitle(),
		"menuBarBtn": s.GetHoveredMenuBarButton(),
	} {
		if got.Fg != ColorBrightYellow || got.Bg != ColorMagenta {
			t.Errorf("%s hover = %v/%v, want bright yellow/magenta", name, got.Fg, got.Bg)
		}
	}
}

// The hovered scrollbar thumb fills dark magenta (its FG is the fill
// colour), distinct from the yellow-on-purple hover pair.
func TestHoveredScrollbarThumbIsMagenta(t *testing.T) {
	if got := DefaultScheme().GetHoveredScrollbarThumb(); got.Fg != ColorMagenta {
		t.Errorf("hovered scrollbar thumb Fg = %v, want magenta", got.Fg)
	}
}

// Focus takes priority over hover when an item is both.
func TestFocusBeatsHover(t *testing.T) {
	s := DefaultScheme()

	if got := s.GetButtonState(true, true, true, false); got != s.GetFocusedButton() {
		t.Error("focused+hovered button did not resolve to the focused style")
	}
	if got := s.GetButtonState(true, false, true, false); got != s.GetHoveredButton() {
		t.Error("hovered-only button did not resolve to the hovered style")
	}
	// Pressed beats focus.
	if got := s.GetButtonState(true, true, true, true); got != s.GetPressedButton(true) {
		t.Error("pressed button did not win over focus/hover")
	}

	if got := s.GetDockItemState(true, true); got != s.GetFocusedDockItem() {
		t.Error("focused+hovered dock item did not resolve to the focused style")
	}
	if got := s.GetTitleBarButtonState(true, true, true, false); got != or(s.FocusedTitleBarButton) {
		t.Error("focused+hovered titlebar button did not resolve to the focused style")
	}
}

// The hovered accelerator on the menu bar is bright green on the hover
// background.
func TestHoveredMenuBarMetaAccent(t *testing.T) {
	s := DefaultScheme()
	got := s.GetHoveredMenuBarMeta()
	if got.Fg != ColorBrightGreen {
		t.Errorf("hovered menu-bar meta Fg = %v, want bright green", got.Fg)
	}
	if got.Bg != ColorMagenta {
		t.Errorf("hovered menu-bar meta Bg = %v, want magenta", got.Bg)
	}
}

// Splitting MenuBarButton into FG/BG still composes the same style as
// before through the getter.
func TestMenuBarButtonSplitComposes(t *testing.T) {
	s := DefaultScheme()
	btn := s.GetMenuBarButton()
	if btn.Fg != ColorBlack || btn.Bg != ColorWhite {
		t.Errorf("menu-bar button = %v/%v, want black/white", btn.Fg, btn.Bg)
	}
	dis := s.GetDisabledMenuBarButton()
	if dis.Fg != ColorBrightBlack || dis.Bg != ColorWhite {
		t.Errorf("disabled menu-bar button = %v/%v, want bright black/white", dis.Fg, dis.Bg)
	}
}

// Active/inactive title-bar buttons inherit the matching title-bar
// background when the scheme leaves their background unspecified.
func TestTitleBarButtonBgInheritsTitle(t *testing.T) {
	s := DefaultScheme()
	if got, want := s.GetTitleBarButton(true, false, false).Bg, s.GetWindowTitle(true).Bg; got != want {
		t.Errorf("active title button bg = %v, want active title bg %v", got, want)
	}
	if got, want := s.GetTitleBarButton(false, false, false).Bg, s.GetWindowTitle(false).Bg; got != want {
		t.Errorf("inactive title button bg = %v, want inactive title bg %v", got, want)
	}
	// State resolver takes the same default.
	if got, want := s.GetTitleBarButtonState(false, false, false, false).Bg, s.GetWindowTitle(false).Bg; got != want {
		t.Errorf("inactive title button (state) bg = %v, want %v", got, want)
	}
}
