// Package style provides theming and visual styling for KittyTK.
package style

// IconSize represents standard icon dimensions.
type IconSize int

const (
	// IconSmall is a small icon (3x1 characters in text mode, ~16x16 in graphics).
	IconSmall IconSize = iota

	// IconLarge is a large icon (5x2 characters in text mode, ~32x32 in graphics).
	IconLarge

	// IconCustom is a custom-sized icon.
	IconCustom
)

// IconCell represents a single cell of a text-mode icon with full ANSI support.
// Each cell can have its own character, foreground color, background color,
// and text attributes (bold, underline, etc.).
type IconCell struct {
	Char  rune
	Style CellStyle
}

// TextIcon represents an icon rendered as styled text characters.
// Each cell can have independent styling (colors, attributes).
// Small icons are typically 3x1, large icons are 5x2, but any size is supported.
type TextIcon struct {
	// Cells contains the icon characters with per-cell styling, row by row.
	// Index: row * Width + col
	Cells []IconCell

	// Width in characters.
	Width int

	// Height in characters.
	Height int
}

// NewTextIcon creates a text icon with the given dimensions.
// All cells are initialized to spaces with default style.
func NewTextIcon(width, height int) TextIcon {
	cells := make([]IconCell, width*height)
	defaultStyle := DefaultStyle()
	for i := range cells {
		cells[i] = IconCell{Char: ' ', Style: defaultStyle}
	}
	return TextIcon{Cells: cells, Width: width, Height: height}
}

// NewSmallTextIcon creates a 3x1 text icon from a string with uniform style.
func NewSmallTextIcon(text string, style CellStyle) TextIcon {
	icon := NewTextIcon(3, 1)
	icon.SetRow(0, text, style)
	return icon
}

// NewLargeTextIcon creates a 5x2 text icon from two strings with uniform style.
func NewLargeTextIcon(line1, line2 string, style CellStyle) TextIcon {
	icon := NewTextIcon(5, 2)
	icon.SetRow(0, line1, style)
	icon.SetRow(1, line2, style)
	return icon
}

// NewTextIconFromCells creates a text icon from a 2D slice of cells.
// This allows full per-cell styling control.
func NewTextIconFromCells(cells [][]IconCell) TextIcon {
	if len(cells) == 0 {
		return TextIcon{}
	}

	height := len(cells)
	width := 0
	for _, row := range cells {
		if len(row) > width {
			width = len(row)
		}
	}

	icon := NewTextIcon(width, height)
	for row, rowCells := range cells {
		for col, cell := range rowCells {
			icon.Set(col, row, cell.Char, cell.Style)
		}
	}
	return icon
}

// Set sets a single cell with character and style.
func (t *TextIcon) Set(col, row int, ch rune, style CellStyle) {
	if col < 0 || col >= t.Width || row < 0 || row >= t.Height {
		return
	}
	t.Cells[row*t.Width+col] = IconCell{Char: ch, Style: style}
}

// SetRow sets an entire row with text using uniform style.
func (t *TextIcon) SetRow(row int, text string, style CellStyle) {
	if row < 0 || row >= t.Height {
		return
	}
	runes := []rune(text)
	for col := 0; col < t.Width; col++ {
		if col < len(runes) {
			t.Cells[row*t.Width+col] = IconCell{Char: runes[col], Style: style}
		} else {
			t.Cells[row*t.Width+col] = IconCell{Char: ' ', Style: style}
		}
	}
}

// SetRowCells sets an entire row with per-cell styling.
func (t *TextIcon) SetRowCells(row int, cells []IconCell) {
	if row < 0 || row >= t.Height {
		return
	}
	for col := 0; col < t.Width && col < len(cells); col++ {
		t.Cells[row*t.Width+col] = cells[col]
	}
}

// CellAt returns the cell at the given position.
func (t TextIcon) CellAt(col, row int) IconCell {
	if col < 0 || col >= t.Width || row < 0 || row >= t.Height {
		return IconCell{Char: ' '}
	}
	idx := row*t.Width + col
	if idx >= len(t.Cells) {
		return IconCell{Char: ' '}
	}
	return t.Cells[idx]
}

// Icon represents an icon that can be rendered in both text and graphics modes.
type Icon struct {
	// ID is a unique identifier for the icon (e.g., "file.new", "edit.copy").
	ID string

	// TextSmall is the small text representation (3x1).
	TextSmall TextIcon

	// TextLarge is the large text representation (5x2).
	TextLarge TextIcon

	// GraphicsPath is the path to the graphics asset (SVG, PNG, etc.).
	// Used when running in graphics mode.
	GraphicsPath string

	// GraphicsData contains embedded image data (for self-contained icons).
	// Could be SVG string, base64 PNG, etc.
	GraphicsData []byte

	// GraphicsFormat indicates the format of GraphicsData.
	GraphicsFormat string // "svg", "png", "embedded"
}

// NewIcon creates a new icon with the given ID.
func NewIcon(id string) *Icon {
	return &Icon{ID: id}
}

// WithTextSmall sets the small text representation.
func (i *Icon) WithTextSmall(text string, style CellStyle) *Icon {
	i.TextSmall = NewSmallTextIcon(text, style)
	return i
}

// WithTextLarge sets the large text representation.
func (i *Icon) WithTextLarge(line1, line2 string, style CellStyle) *Icon {
	i.TextLarge = NewLargeTextIcon(line1, line2, style)
	return i
}

// WithGraphics sets the graphics path.
func (i *Icon) WithGraphics(path string) *Icon {
	i.GraphicsPath = path
	return i
}

// WithEmbeddedSVG sets embedded SVG data.
func (i *Icon) WithEmbeddedSVG(svg string) *Icon {
	i.GraphicsData = []byte(svg)
	i.GraphicsFormat = "svg"
	return i
}

// HasText returns true if the icon has a text representation.
func (i *Icon) HasText(size IconSize) bool {
	switch size {
	case IconSmall:
		return len(i.TextSmall.Cells) > 0
	case IconLarge:
		return len(i.TextLarge.Cells) > 0
	}
	return false
}

// HasGraphics returns true if the icon has a graphics representation.
func (i *Icon) HasGraphics() bool {
	return i.GraphicsPath != "" || len(i.GraphicsData) > 0
}

// IconRegistry manages a collection of icons.
type IconRegistry struct {
	icons map[string]*Icon
}

// NewIconRegistry creates a new icon registry.
func NewIconRegistry() *IconRegistry {
	return &IconRegistry{
		icons: make(map[string]*Icon),
	}
}

// Register adds an icon to the registry.
func (r *IconRegistry) Register(icon *Icon) {
	r.icons[icon.ID] = icon
}

// Get retrieves an icon by ID.
func (r *IconRegistry) Get(id string) *Icon {
	return r.icons[id]
}

// StandardIcons provides icon IDs for common actions.
var StandardIcons = struct {
	// File operations
	FileNew    string
	FileOpen   string
	FileSave   string
	FileSaveAs string
	FileClose  string
	FilePrint  string

	// Edit operations
	EditUndo   string
	EditRedo   string
	EditCut    string
	EditCopy   string
	EditPaste  string
	EditDelete string
	EditFind   string

	// Navigation
	NavBack    string
	NavForward string
	NavUp      string
	NavHome    string
	NavRefresh string

	// View
	ViewZoomIn  string
	ViewZoomOut string
	ViewFull    string

	// Common items
	Folder      string
	FolderOpen  string
	File        string
	FileText    string
	FileCode    string
	FileImage   string
	Application string

	// Status
	Info    string
	Warning string
	Error   string
	Success string
	Question string

	// Actions
	Add    string
	Remove string
	Check  string
	Close  string
	Menu   string
	Settings string
	Search string
	Help   string

	// Media
	Play  string
	Pause string
	Stop  string
	Next  string
	Prev  string

	// Misc
	User    string
	Lock    string
	Unlock  string
	Star    string
	Heart   string
	Flag    string
}{
	FileNew:    "file.new",
	FileOpen:   "file.open",
	FileSave:   "file.save",
	FileSaveAs: "file.save_as",
	FileClose:  "file.close",
	FilePrint:  "file.print",

	EditUndo:   "edit.undo",
	EditRedo:   "edit.redo",
	EditCut:    "edit.cut",
	EditCopy:   "edit.copy",
	EditPaste:  "edit.paste",
	EditDelete: "edit.delete",
	EditFind:   "edit.find",

	NavBack:    "nav.back",
	NavForward: "nav.forward",
	NavUp:      "nav.up",
	NavHome:    "nav.home",
	NavRefresh: "nav.refresh",

	ViewZoomIn:  "view.zoom_in",
	ViewZoomOut: "view.zoom_out",
	ViewFull:    "view.fullscreen",

	Folder:      "item.folder",
	FolderOpen:  "item.folder_open",
	File:        "item.file",
	FileText:    "item.file_text",
	FileCode:    "item.file_code",
	FileImage:   "item.file_image",
	Application: "item.application",

	Info:     "status.info",
	Warning:  "status.warning",
	Error:    "status.error",
	Success:  "status.success",
	Question: "status.question",

	Add:      "action.add",
	Remove:   "action.remove",
	Check:    "action.check",
	Close:    "action.close",
	Menu:     "action.menu",
	Settings: "action.settings",
	Search:   "action.search",
	Help:     "action.help",

	Play:  "media.play",
	Pause: "media.pause",
	Stop:  "media.stop",
	Next:  "media.next",
	Prev:  "media.prev",

	User:   "misc.user",
	Lock:   "misc.lock",
	Unlock: "misc.unlock",
	Star:   "misc.star",
	Heart:  "misc.heart",
	Flag:   "misc.flag",
}
