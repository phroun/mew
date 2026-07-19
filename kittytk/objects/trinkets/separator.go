// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// captionFont75 returns the face used for divider/separator captions
// on pixel surfaces: 75% of the given font, so the trinketry stays
// visually subordinate to application text and the bands can be thin.
func captionFont75(f *core.Font) *core.Font {
	size := f.Size * 3 / 4
	if size < 6 {
		size = 6
	}
	return &core.Font{Name: f.Name, Size: size, Style: f.Style}
}

// LineSeparator is a visual divider trinket that draws a horizontal or vertical line.
// For horizontal separators, it draws: ────·· Title ··────
// For vertical separators, it draws a vertical line with optional title.
type LineSeparator struct {
	core.TrinketBase
	core.AccessibleTrinket

	title       string
	orientation core.Orientation
}

// NewLineSeparator creates a new horizontal line separator.
func NewLineSeparator() *LineSeparator {
	s := &LineSeparator{
		orientation: core.Horizontal,
	}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	s.SetFocusPolicy(core.NoFocus)
	s.SetAccessibleRole(core.RoleSeparator)
	s.applyOrientationPolicy()
	return s
}

// applyOrientationPolicy makes the separator span its container along
// the line axis while staying one cell thick across it.
func (s *LineSeparator) applyOrientationPolicy() {
	if s.orientation == core.Horizontal {
		s.SetSizePolicy(core.NewSizePolicy(core.SizeExpanding, core.SizeFixed))
	} else {
		s.SetSizePolicy(core.NewSizePolicy(core.SizeFixed, core.SizeExpanding))
	}
}

// NewHSeparator creates a new horizontal separator with optional title.
func NewHSeparator(title string) *LineSeparator {
	s := NewLineSeparator()
	s.title = title
	s.orientation = core.Horizontal
	// Horizontal separator expands horizontally, fixed height
	s.SetSizePolicy(core.NewSizePolicy(core.SizeExpanding, core.SizeFixed))
	return s
}

// NewVSeparator creates a new vertical separator with optional title.
func NewVSeparator(title string) *LineSeparator {
	s := NewLineSeparator()
	s.title = title
	s.orientation = core.Vertical
	// Vertical separator expands vertically, fixed width
	s.SetSizePolicy(core.NewSizePolicy(core.SizeFixed, core.SizeExpanding))
	return s
}

// Title returns the separator title.
func (s *LineSeparator) Title() string {
	return s.title
}

// SetTitle sets the separator title.
func (s *LineSeparator) SetTitle(title string) {
	s.title = title
	s.Update()
}

// Orientation returns the separator orientation.
func (s *LineSeparator) Orientation() core.Orientation {
	return s.orientation
}

// SetOrientation sets the separator orientation.
func (s *LineSeparator) SetOrientation(o core.Orientation) {
	s.orientation = o
	s.applyOrientationPolicy()
	s.Update()
}

// SizeHint returns the preferred size.
func (s *LineSeparator) SizeHint() core.UnitSize {
	metrics := s.EffectiveCellMetrics()
	font := s.EffectiveFont()
	if s.orientation == core.Horizontal {
		// Horizontal separator: 1 cell tall, width depends on title
		titleWidth := font.MeasureText(s.title)
		decorWidth := font.MeasureText("──  ──") // line stubs + title padding
		return core.UnitSize{
			Width:  titleWidth + decorWidth,
			Height: metrics.CellHeight,
		}
	}
	// Vertical separator: 1 cell wide, height depends on title
	return core.UnitSize{
		Width:  metrics.CellWidth,
		Height: metrics.TextHeight(5),
	}
}

// Paint renders the separator.
func (s *LineSeparator) Paint(p *core.Painter) {
	bounds := s.Bounds()
	metrics := s.EffectiveCellMetrics()

	// A separator is decoration, not a control: draw it like a group
	// box border - line on the inherited background - so it never
	// reads as an interactable splitter bar.
	scheme := s.GetScheme()
	inheritedBG := s.EffectiveBackgroundColor()
	lineStyle := scheme.GetGroupBoxBorder(true, inheritedBG)
	titleStyle := scheme.GetGroupBoxTitle(true, inheritedBG)

	// Use custom style if set
	if customStyle := s.Style(); customStyle != nil {
		lineStyle = customStyle.WithBg(inheritedBG)
		titleStyle = lineStyle
	}

	if p.Graphical() {
		// A separator is a line and (optionally) a title - it owns no
		// background. Opaque cell boxes here were clipping the
		// descenders of the text row above.
		lineStyle = lineStyle.WithBg(style.ColorTransparent)
		titleStyle = titleStyle.WithBg(style.ColorTransparent)
		if s.orientation == core.Horizontal {
			s.paintHorizontalGraphical(p, bounds, lineStyle, titleStyle)
		} else {
			s.paintVerticalGraphical(p, bounds, lineStyle, titleStyle)
		}
		return
	}

	if s.orientation == core.Horizontal {
		s.paintHorizontal(p, bounds, lineStyle, titleStyle, metrics)
	} else {
		s.paintVertical(p, bounds, lineStyle, titleStyle, metrics)
	}
}

// paintHorizontalGraphical draws the pixel-surface rule: a hairline
// spanning the full width, vertically centered, broken around an
// exactly-centered title in the 75% caption face.
func (s *LineSeparator) paintHorizontalGraphical(p *core.Painter, bounds core.UnitRect, lineStyle, titleStyle style.CellStyle) {
	line := lineStyle.WithBg(lineStyle.Fg)
	hairH := p.ScreenHeightToLocal(1)
	if hairH < 1 {
		hairH = 1
	}
	midY := (bounds.Height - hairH) / 2
	if s.title == "" {
		p.FillRect(core.UnitRect{Y: midY, Width: bounds.Width, Height: hairH}, ' ', line)
		return
	}
	font := captionFont75(s.EffectiveFont())
	// Font metrics are screen-space (see ScreenHeightToLocal).
	w := p.ScreenWidthToLocal(font.MeasureText(s.title))
	h := p.ScreenHeightToLocal(font.LineHeight())
	pad := core.Unit(6)
	boxW := w + pad*2
	if boxW > bounds.Width {
		boxW = bounds.Width
	}
	boxX := (bounds.Width - boxW) / 2
	// The line in two segments: the mid-section belongs to the title.
	p.FillRect(core.UnitRect{X: 0, Y: midY, Width: boxX, Height: hairH}, ' ', line)
	p.FillRect(core.UnitRect{X: boxX + boxW, Y: midY, Width: bounds.Width - boxX - boxW, Height: hairH}, ' ', line)
	p.DrawText(boxX+pad, midY+hairH/2-h/2, s.title, titleStyle, font)
}

// paintVerticalGraphical draws the vertical rule: a hairline spanning
// the full height, horizontally centered, broken around the stacked
// title runes in the 75% caption face.
func (s *LineSeparator) paintVerticalGraphical(p *core.Painter, bounds core.UnitRect, lineStyle, titleStyle style.CellStyle) {
	line := lineStyle.WithBg(lineStyle.Fg)
	hairW := p.ScreenWidthToLocal(1)
	if hairW < 1 {
		hairW = 1
	}
	midX := (bounds.Width - hairW) / 2
	if s.title == "" {
		p.FillRect(core.UnitRect{X: midX, Width: hairW, Height: bounds.Height}, ' ', line)
		return
	}
	font := captionFont75(s.EffectiveFont())
	h := p.ScreenHeightToLocal(font.LineHeight())
	runes := []rune(s.title)
	pad := core.Unit(4)
	boxH := core.Unit(len(runes))*h + pad*2
	if boxH > bounds.Height {
		boxH = bounds.Height
	}
	boxY := (bounds.Height - boxH) / 2
	p.FillRect(core.UnitRect{X: midX, Y: 0, Width: hairW, Height: boxY}, ' ', line)
	p.FillRect(core.UnitRect{X: midX, Y: boxY + boxH, Width: hairW, Height: bounds.Height - boxY - boxH}, ' ', line)
	y := boxY + pad
	for _, r := range runes {
		rw := p.ScreenWidthToLocal(font.MeasureText(string(r)))
		p.DrawText(midX-rw/2, y, string(r), titleStyle, font)
		y += h
	}
}

// paintHorizontal draws: ────·· Title ··────
func (s *LineSeparator) paintHorizontal(p *core.Painter, bounds core.UnitRect, lineStyle, titleStyle style.CellStyle, metrics core.CellMetrics) {
	width := int(bounds.Width / metrics.CellWidth)
	if width <= 0 {
		return
	}

	y := core.Unit(0)
	titleRunes := []rune(s.title)
	titleLen := len(titleRunes)

	// No grab-handle dots here: the ···· decoration means "draggable"
	// and belongs to Splitter alone. A separator is a plain rule,
	// optionally interrupted by a space-padded title.
	if titleLen == 0 {
		// ────────────────
		for x := 0; x < width; x++ {
			p.DrawCell(metrics.CellToUnitsX(x), y, '─', lineStyle)
		}
	} else {
		// ────── Title ──────
		middleRunes := []rune(" " + s.title + " ")
		middleLen := len(middleRunes)
		startMiddle := (width - middleLen) / 2

		for x := 0; x < width; x++ {
			ch := '─'
			cellStyle := lineStyle
			if rel := x - startMiddle; rel >= 0 && rel < middleLen {
				ch = middleRunes[rel]
				if rel >= 1 && rel < 1+titleLen {
					cellStyle = titleStyle
				}
			}
			p.DrawCell(metrics.CellToUnitsX(x), y, ch, cellStyle)
		}
	}
}

// paintVertical draws a vertical line with optional centered title.
func (s *LineSeparator) paintVertical(p *core.Painter, bounds core.UnitRect, lineStyle, titleStyle style.CellStyle, metrics core.CellMetrics) {
	height := int(bounds.Height / metrics.CellHeight)
	if height <= 0 {
		return
	}

	x := core.Unit(0)
	titleRunes := []rune(s.title)
	titleLen := len(titleRunes)

	// Same rule as horizontal: no grab-handle dots on a separator.
	if titleLen == 0 {
		for y := 0; y < height; y++ {
			p.DrawCell(x, metrics.CellToUnitsY(y), '│', lineStyle)
		}
	} else {
		// Title characters stacked vertically, one blank row above
		// and below, centered in the line.
		middleLen := titleLen + 2
		startMiddle := (height - middleLen) / 2
		if startMiddle < 0 {
			startMiddle = 0
		}

		for y := 0; y < height; y++ {
			ch := '│'
			cellStyle := lineStyle
			if rel := y - startMiddle; rel >= 0 && rel < middleLen {
				if rel >= 1 && rel < 1+titleLen {
					ch = titleRunes[rel-1]
					cellStyle = titleStyle
				} else {
					ch = ' '
				}
			}
			p.DrawCell(x, metrics.CellToUnitsY(y), ch, cellStyle)
		}
	}
}

// AccessibleInfo returns accessibility information.
func (s *LineSeparator) AccessibleInfo() core.AccessibleInfo {
	info := s.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleSeparator
	if s.title != "" {
		info.Name = s.title
	}
	return info
}
