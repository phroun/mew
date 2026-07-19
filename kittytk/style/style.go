// Package style provides theming and visual styling for KittyTK.
package style

import (
	"fmt"
	"strings"
)

// Color represents a terminal color.
type Color int

const (
	ColorDefault Color = -1

	// ColorTransparent (background only, graphical targets only):
	// draw no background - glyphs blend over whatever is already
	// painted. Cell targets cannot express it, so trinkets only pass
	// it when the painter reports Graphical(); label-type text uses
	// it so opaque line boxes never clip neighboring glyphs.
	ColorTransparent Color = -2

	// Standard colors (0-7)
	ColorBlack Color = iota - 2
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorWhite

	// Bright colors (8-15)
	ColorBrightBlack
	ColorBrightRed
	ColorBrightGreen
	ColorBrightYellow
	ColorBrightBlue
	ColorBrightMagenta
	ColorBrightCyan
	ColorBrightWhite
)

// RGB creates a 24-bit true color.
func RGB(r, g, b int) Color {
	return Color(256 + (r<<16 | g<<8 | b))
}

// Color256 creates a 256-color palette color.
func Color256(n int) Color {
	return Color(256 + 0x1000000 + n)
}

// RGBComponents resolves this color to 24-bit RGB through the active
// terminal palette, mirroring the graphical backend's background
// resolution: indexed colors (0-15) and ColorDefault (and anything else)
// map through the active theme, true-color values decode directly.
// Graphical overlays that must match a painted background - e.g. the
// scroll-area edge fades - sample this.
func (c Color) RGBComponents() (r, g, b uint8) {
	switch {
	case c >= 0 && c < 16:
		t := ActiveTermANSIColor(int(c))
		return t.R, t.G, t.B
	case c >= 256 && c < 256+0x1000000:
		v := uint32(c - 256)
		return uint8(v >> 16), uint8(v >> 8), uint8(v)
	default:
		bg := ActiveTermPalette.Background
		return bg.R, bg.G, bg.B
	}
}

// FgCode returns the ANSI escape code for foreground color.
func (c Color) FgCode() string {
	if c == ColorDefault {
		return "\033[39m"
	}
	if c >= 0 && c < 8 {
		return fmt.Sprintf("\033[%dm", 30+int(c))
	}
	if c >= 8 && c < 16 {
		return fmt.Sprintf("\033[%dm", 90+int(c)-8)
	}
	// True color or 256-color
	if c >= 256 {
		val := int(c) - 256
		if val >= 0x1000000 {
			// 256-color
			return fmt.Sprintf("\033[38;5;%dm", val-0x1000000)
		}
		// True color
		r := (val >> 16) & 0xFF
		g := (val >> 8) & 0xFF
		b := val & 0xFF
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
	}
	return ""
}

// BgCode returns the ANSI escape code for background color.
func (c Color) BgCode() string {
	if c == ColorDefault {
		return "\033[49m"
	}
	if c >= 0 && c < 8 {
		return fmt.Sprintf("\033[%dm", 40+int(c))
	}
	if c >= 8 && c < 16 {
		return fmt.Sprintf("\033[%dm", 100+int(c)-8)
	}
	// True color or 256-color
	if c >= 256 {
		val := int(c) - 256
		if val >= 0x1000000 {
			// 256-color
			return fmt.Sprintf("\033[48;5;%dm", val-0x1000000)
		}
		// True color
		r := (val >> 16) & 0xFF
		g := (val >> 8) & 0xFF
		b := val & 0xFF
		return fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
	}
	return ""
}

// TextStyle represents text formatting attributes.
type TextStyle int

const (
	StyleNormal TextStyle = 0
	StyleBold   TextStyle = 1 << iota
	StyleDim
	StyleItalic
	StyleUnderline
	StyleBlink
	StyleReverse
	StyleStrikethrough
	StyleOverline // Virtual attribute: overline below becomes underline above during render
)

// Code returns the ANSI codes for text style.
func (s TextStyle) Code() string {
	if s == StyleNormal {
		return "\033[0m"
	}
	var codes []string
	if s&StyleBold != 0 {
		codes = append(codes, "1")
	}
	if s&StyleDim != 0 {
		codes = append(codes, "2")
	}
	if s&StyleItalic != 0 {
		codes = append(codes, "3")
	}
	if s&StyleUnderline != 0 {
		codes = append(codes, "4")
	}
	if s&StyleBlink != 0 {
		codes = append(codes, "5")
	}
	if s&StyleReverse != 0 {
		codes = append(codes, "7")
	}
	if s&StyleStrikethrough != 0 {
		codes = append(codes, "9")
	}
	if len(codes) == 0 {
		return ""
	}
	return fmt.Sprintf("\033[%sm", strings.Join(codes, ";"))
}

// CellStyle combines all styling for a single cell.
type CellStyle struct {
	Fg    Color
	Bg    Color
	Attrs TextStyle
}

// DefaultStyle returns a default cell style.
func DefaultStyle() CellStyle {
	return CellStyle{Fg: ColorDefault, Bg: ColorDefault, Attrs: StyleNormal}
}

// WithFg returns a copy with the foreground color set.
func (s CellStyle) WithFg(c Color) CellStyle {
	s.Fg = c
	return s
}

// WithBg returns a copy with the background color set.
func (s CellStyle) WithBg(c Color) CellStyle {
	s.Bg = c
	return s
}

// WithAttrs returns a copy with the text attributes set.
func (s CellStyle) WithAttrs(attrs TextStyle) CellStyle {
	s.Attrs = attrs
	return s
}

// Bold returns a copy with bold attribute added.
func (s CellStyle) Bold() CellStyle {
	s.Attrs |= StyleBold
	return s
}

// Underline returns a copy with underline attribute added.
func (s CellStyle) Underline() CellStyle {
	s.Attrs |= StyleUnderline
	return s
}

// Reverse returns a copy with reverse attribute added.
func (s CellStyle) Reverse() CellStyle {
	s.Attrs |= StyleReverse
	return s
}

// Overline returns a copy with overline attribute added.
// Overline is a virtual attribute: cells with overlines cause the cell
// directly above them to be rendered with an underline.
func (s CellStyle) Overline() CellStyle {
	s.Attrs |= StyleOverline
	return s
}

// Code returns the complete ANSI escape sequence for this style.
func (s CellStyle) Code() string {
	var sb strings.Builder
	sb.WriteString("\033[0m") // Reset first
	if s.Attrs != StyleNormal {
		sb.WriteString(s.Attrs.Code())
	}
	if s.Fg != ColorDefault {
		sb.WriteString(s.Fg.FgCode())
	}
	if s.Bg != ColorDefault {
		sb.WriteString(s.Bg.BgCode())
	}
	return sb.String()
}

// BorderStyle defines the characters used for drawing borders.
type BorderStyle struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horizontal  rune
	Vertical    rune
	// T-junctions for menus and dividers
	TopT    rune
	BottomT rune
	LeftT   rune
	RightT  rune
	Cross   rune
}

var (
	// BorderNone represents no border.
	BorderNone = BorderStyle{}

	// BorderSingle uses single-line box drawing characters.
	BorderSingle = BorderStyle{
		TopLeft:     '┌',
		TopRight:    '┐',
		BottomLeft:  '└',
		BottomRight: '┘',
		Horizontal:  '─',
		Vertical:    '│',
		TopT:        '┬',
		BottomT:     '┴',
		LeftT:       '├',
		RightT:      '┤',
		Cross:       '┼',
	}

	// BorderDouble uses double-line box drawing characters.
	BorderDouble = BorderStyle{
		TopLeft:     '╔',
		TopRight:    '╗',
		BottomLeft:  '╚',
		BottomRight: '╝',
		Horizontal:  '═',
		Vertical:    '║',
		TopT:        '╦',
		BottomT:     '╩',
		LeftT:       '╠',
		RightT:      '╣',
		Cross:       '╬',
	}

	// BorderRounded uses rounded corners.
	BorderRounded = BorderStyle{
		TopLeft:     '╭',
		TopRight:    '╮',
		BottomLeft:  '╰',
		BottomRight: '╯',
		Horizontal:  '─',
		Vertical:    '│',
		TopT:        '┬',
		BottomT:     '┴',
		LeftT:       '├',
		RightT:      '┤',
		Cross:       '┼',
	}

	// BorderHeavy uses heavy box drawing characters.
	BorderHeavy = BorderStyle{
		TopLeft:     '┏',
		TopRight:    '┓',
		BottomLeft:  '┗',
		BottomRight: '┛',
		Horizontal:  '━',
		Vertical:    '┃',
		TopT:        '┳',
		BottomT:     '┻',
		LeftT:       '┣',
		RightT:      '┫',
		Cross:       '╋',
	}

	// BorderASCII uses basic ASCII characters.
	BorderASCII = BorderStyle{
		TopLeft:     '+',
		TopRight:    '+',
		BottomLeft:  '+',
		BottomRight: '+',
		Horizontal:  '-',
		Vertical:    '|',
		TopT:        '+',
		BottomT:     '+',
		LeftT:       '+',
		RightT:      '+',
		Cross:       '+',
	}
)

// Theme defines colors for all UI elements.
type Theme struct {
	// Application background
	Background CellStyle

	// Normal trinket appearance
	Normal   CellStyle
	Focused  CellStyle
	Disabled CellStyle
	Selected CellStyle
	Hover    CellStyle

	// Window appearance
	WindowFrame        CellStyle
	WindowTitle        CellStyle
	WindowTitleFocused CellStyle
	WindowBackground   CellStyle

	// Button appearance
	Button        CellStyle
	ButtonFocused CellStyle
	ButtonPressed CellStyle

	// Input field appearance
	Input          CellStyle
	InputFocused   CellStyle
	InputSelection CellStyle

	// Menu appearance
	MenuBar          CellStyle
	MenuBarSelected  CellStyle
	MenuItem         CellStyle
	MenuItemSelected CellStyle
	MenuItemDisabled CellStyle
	MenuSeparator    CellStyle

	// List appearance
	ListItem         CellStyle
	ListItemSelected CellStyle
	ListItemFocused  CellStyle

	// Scrollbar appearance
	ScrollTrack CellStyle
	ScrollThumb CellStyle

	// Progress bar
	ProgressFilled CellStyle
	ProgressEmpty  CellStyle

	// Desktop/Status bar
	Desktop   CellStyle
	StatusBar CellStyle

	// Border styles
	DefaultBorder BorderStyle
	WindowBorder  BorderStyle
	MenuBorder    BorderStyle
}

// DefaultTheme returns a sensible default theme.
func DefaultTheme() *Theme {
	return &Theme{
		Background: DefaultStyle().WithBg(ColorBlue),

		Normal:   DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		Focused:  DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		Disabled: DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorBlack),
		Selected: DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),
		Hover:    DefaultStyle().WithFg(ColorWhite).WithBg(ColorBrightBlack),

		WindowFrame:        DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		WindowTitle:        DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		WindowTitleFocused: DefaultStyle().WithFg(ColorYellow).WithBg(ColorBlue).Bold(),
		WindowBackground:   DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),

		Button:        DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		ButtonFocused: DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),
		ButtonPressed: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),

		Input:          DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		InputFocused:   DefaultStyle().WithFg(ColorBrightWhite).WithBg(ColorCyan),
		InputSelection: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),

		MenuBar:          DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		MenuBarSelected:  DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		MenuItem:         DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		MenuItemSelected: DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),
		MenuItemDisabled: DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite),
		MenuSeparator:    DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite),

		ListItem:         DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		ListItemSelected: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		ListItemFocused:  DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),

		ScrollTrack: DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorBlack),
		ScrollThumb: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBrightBlack),

		ProgressFilled: DefaultStyle().WithFg(ColorWhite).WithBg(ColorGreen),
		ProgressEmpty:  DefaultStyle().WithFg(ColorWhite).WithBg(ColorBrightBlack),

		Desktop:   DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorGreen).WithAttrs(StyleDim),
		StatusBar: DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),

		DefaultBorder: BorderSingle,
		WindowBorder:  BorderDouble,
		MenuBorder:    BorderSingle,
	}
}

// DarkTheme returns a modern dark theme.
func DarkTheme() *Theme {
	bg := RGB(30, 30, 46)
	surface := RGB(49, 50, 68)
	overlay := RGB(69, 71, 90)
	text := RGB(205, 214, 244)
	subtext := RGB(166, 173, 200)
	blue := RGB(137, 180, 250)
	lavender := RGB(180, 190, 254)
	green := RGB(166, 227, 161)
	_ = RGB(243, 139, 168) // red - reserved for future use

	return &Theme{
		Background: DefaultStyle().WithBg(bg),

		Normal:   DefaultStyle().WithFg(text).WithBg(surface),
		Focused:  DefaultStyle().WithFg(text).WithBg(overlay),
		Disabled: DefaultStyle().WithFg(subtext).WithBg(surface),
		Selected: DefaultStyle().WithFg(bg).WithBg(blue),
		Hover:    DefaultStyle().WithFg(text).WithBg(overlay),

		WindowFrame:        DefaultStyle().WithFg(lavender).WithBg(surface),
		WindowTitle:        DefaultStyle().WithFg(text).WithBg(surface),
		WindowTitleFocused: DefaultStyle().WithFg(blue).WithBg(surface).Bold(),
		WindowBackground:   DefaultStyle().WithFg(text).WithBg(surface),

		Button:        DefaultStyle().WithFg(text).WithBg(overlay),
		ButtonFocused: DefaultStyle().WithFg(bg).WithBg(blue),
		ButtonPressed: DefaultStyle().WithFg(bg).WithBg(lavender),

		Input:          DefaultStyle().WithFg(text).WithBg(bg),
		InputFocused:   DefaultStyle().WithFg(text).WithBg(overlay),
		InputSelection: DefaultStyle().WithFg(bg).WithBg(blue),

		MenuBar:          DefaultStyle().WithFg(text).WithBg(surface),
		MenuBarSelected:  DefaultStyle().WithFg(bg).WithBg(blue),
		MenuItem:         DefaultStyle().WithFg(text).WithBg(surface),
		MenuItemSelected: DefaultStyle().WithFg(bg).WithBg(blue),
		MenuItemDisabled: DefaultStyle().WithFg(subtext).WithBg(surface),
		MenuSeparator:    DefaultStyle().WithFg(overlay).WithBg(surface),

		ListItem:         DefaultStyle().WithFg(text).WithBg(surface),
		ListItemSelected: DefaultStyle().WithFg(bg).WithBg(blue),
		ListItemFocused:  DefaultStyle().WithFg(text).WithBg(overlay),

		ScrollTrack: DefaultStyle().WithFg(overlay).WithBg(bg),
		ScrollThumb: DefaultStyle().WithFg(lavender).WithBg(overlay),

		ProgressFilled: DefaultStyle().WithBg(green),
		ProgressEmpty:  DefaultStyle().WithBg(overlay),

		Desktop:   DefaultStyle().WithFg(overlay).WithBg(bg),
		StatusBar: DefaultStyle().WithFg(text).WithBg(surface),

		DefaultBorder: BorderRounded,
		WindowBorder:  BorderRounded,
		MenuBorder:    BorderRounded,
	}
}

// ClassicTheme returns a classic blue/cyan CGA-style theme.
func ClassicTheme() *Theme {
	return &Theme{
		Background: DefaultStyle().WithBg(ColorBlue),

		Normal:   DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),
		Focused:  DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		Disabled: DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorCyan),
		Selected: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		Hover:    DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightCyan),

		WindowFrame:        DefaultStyle().WithFg(ColorWhite).WithBg(ColorCyan),
		WindowTitle:        DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),
		WindowTitleFocused: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue).Bold(),
		WindowBackground:   DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),

		Button:        DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		ButtonFocused: DefaultStyle().WithFg(ColorWhite).WithBg(ColorGreen),
		ButtonPressed: DefaultStyle().WithFg(ColorBlack).WithBg(ColorGreen),

		Input:          DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		InputFocused:   DefaultStyle().WithFg(ColorBrightWhite).WithBg(ColorBlue),
		InputSelection: DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),

		MenuBar:          DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		MenuBarSelected:  DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		MenuItem:         DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		MenuItemSelected: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		MenuItemDisabled: DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite),
		MenuSeparator:    DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),

		ListItem:         DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),
		ListItemSelected: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue),
		ListItemFocused:  DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan),

		ScrollTrack: DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack),
		ScrollThumb: DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),

		ProgressFilled: DefaultStyle().WithBg(ColorGreen),
		ProgressEmpty:  DefaultStyle().WithBg(ColorBlack),

		Desktop:   DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorGreen).WithAttrs(StyleDim),
		StatusBar: DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite),

		DefaultBorder: BorderDouble,
		WindowBorder:  BorderDouble,
		MenuBorder:    BorderSingle,
	}
}
