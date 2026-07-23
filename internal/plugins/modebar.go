// Package plugins provides editor plugins like Modebar and ColumnRuler.
package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/window"
)

// ModebarPlugin renders the main modebar.
type ModebarPlugin struct {
	windowManager     *window.Manager
	windowID          string
	location          string // "top" (default) or "bottom" screen line
	maxSequenceLength int
	activeSequence    string
	completions       string // Key-sequence autocompletion for the current sequence
	colors            config.ColorScheme

	// Templates (text with %CODE% substitutions): the filename region, the
	// middle default text (shown when no live context applies), and the
	// right-hand readout (ellipsized when too long). See RenderContent.
	tmplInner   string
	tmplDefault string
	tmplOuter   string

	// logoRTL flips the logo from M_ to _M when the focused caret sits in a
	// right-to-left segment, signalling which way the next keypress moves.
	logoRTL bool

	// bindingValues holds dynamic %CODE% values sourced from the editor's live
	// keymap (e.g. %SPU% -> the key bound to stat_peek_up). Merged into the
	// substitution set so templates and the peek labels resolve them.
	bindingValues map[string]string

	// filenameFn, when set, supplies a display name for %FN% given the main
	// buffer window — the editor uses it to show a wiki page's scheme form
	// ("help:/start") instead of the underlying file base ("start.txt").
	// Returning "" defers to the ordinary filename base.
	filenameFn func(*window.Window) string

	// Nav-history buttons ([<] / [>]) shown just before the filename when the
	// context window has back/forward history. navStateFn supplies the live
	// mouse state the editor tracks (the captured button, whether the pointer
	// is over it, and the hovered button); the button column ranges are
	// recorded each render (content-relative, 0-based, [start,end)) so the
	// editor can hit-test a click against them. end<=start means absent.
	navStateFn               func() (pressed int, pressedOn bool, hover int)
	navBackStart, navBackEnd int
	navFwdStart, navFwdEnd   int
}

// Modebar nav-button identities (also the editor's).
const (
	ModebarNavNone = 0
	ModebarNavBack = 1
	ModebarNavFwd  = 2
)

// SetFilenameFunc installs a display-name provider for %FN% (see filenameFn).
func (s *ModebarPlugin) SetFilenameFunc(fn func(*window.Window) string) {
	s.filenameFn = fn
}

// SetNavStateFunc installs the provider for the nav-history buttons' live
// mouse state (captured button, pointer-over, hovered button).
func (s *ModebarPlugin) SetNavStateFunc(fn func() (pressed int, pressedOn bool, hover int)) {
	s.navStateFn = fn
}

// WindowID returns the modebar window's id ("" before CreateWindow), so the
// editor can locate the bar's screen row for nav-button hit-testing.
func (s *ModebarPlugin) WindowID() string { return s.windowID }

// NavButtonAtColumn reports which nav button ([<]=Back, [>]=Fwd) occupies the
// content-relative column, from the ranges recorded by the last render, or
// ModebarNavNone. The 3-space placeholder that holds the back button's width
// when only forward history exists is inert (no button there).
func (s *ModebarPlugin) NavButtonAtColumn(col int) int {
	if col >= s.navBackStart && col < s.navBackEnd {
		return ModebarNavBack
	}
	if col >= s.navFwdStart && col < s.navFwdEnd {
		return ModebarNavFwd
	}
	return ModebarNavNone
}

// renderNav builds the nav-history button section shown just before the
// filename and records the buttons' content-relative column ranges. It
// collapses to "" when the context window has no history. Each button paints
// its brackets in the button-SHADOW color and the "<"/">" glyph in the button
// color, in one of three states — normal, pressed (mouse held over the
// captured button), or hover (pointer over, graphical build only). When only
// forward history exists, a 3-space placeholder holds the missing back
// button's width so [>] stays put. startCol is the section's content column.
func (s *ModebarPlugin) renderNav(col func(string) string, fillColor string, ctxWindow *window.Window, startCol int) string {
	s.navBackStart, s.navBackEnd, s.navFwdStart, s.navFwdEnd = 0, 0, 0, 0
	prior, next := 0, 0
	if ctxWindow != nil {
		prior, next = ctxWindow.NavHistoryDepths()
	}
	back, fwd := prior > 0, next > 0
	if !back && !fwd {
		return ""
	}
	pressed, pressedOn, hover := 0, false, 0
	if s.navStateFn != nil {
		pressed, pressedOn, hover = s.navStateFn()
	}
	// btn resolves the face + shadow colors for one button given its state.
	btn := func(id int, glyph string) string {
		faceName, shadowName := "button", "buttonShadow"
		switch {
		case pressed == id && pressedOn:
			faceName, shadowName = "buttonPressed", "buttonShadowPressed"
		case pressed == ModebarNavNone && hover == id:
			faceName, shadowName = "buttonHover", "buttonShadowHover"
		}
		return col(shadowName) + "[" + col(faceName) + glyph + col(shadowName) + "]"
	}

	var b strings.Builder
	if back {
		b.WriteString(btn(ModebarNavBack, "<"))
		s.navBackStart, s.navBackEnd = startCol, startCol+3
	} else {
		// Placeholder that keeps [>] in the same place a [<][>] pair would.
		b.WriteString(fillColor + "   ")
	}
	if fwd {
		b.WriteString(btn(ModebarNavFwd, ">"))
		s.navFwdStart, s.navFwdEnd = startCol+3, startCol+6
	}
	return b.String()
}

// modebarBottomPriority keeps a bottom-located modebar on the very last
// screen line: the bottom dock renders its highest-priority window at the
// screen bottom, and prompt priorities (which exclude the modebar from
// their scan) never climb anywhere near this.
const modebarBottomPriority = 1 << 20

// NewModebar creates a new modebar plugin.
func NewModebar(wm *window.Manager) *ModebarPlugin {
	return &ModebarPlugin{
		windowManager:     wm,
		maxSequenceLength: 4,
		colors:            config.NewColorScheme(),
		tmplInner:         "%FN%",
		tmplDefault:       "%FORTUNE%",
		tmplOuter:         "Frag:%FRAG% Heap:%HEAP% Line:%LINE% Rune:%RUNE%",
	}
}

// SetTemplates sets the modebar's inner (filename), default (middle fallback),
// and outer (right readout) templates. Blank leaves the current value.
func (s *ModebarPlugin) SetTemplates(inner, def, outer string) {
	if inner != "" {
		s.tmplInner = inner
	}
	if def != "" {
		s.tmplDefault = def
	}
	if outer != "" {
		s.tmplOuter = outer
	}
}

// Templates returns the current inner, default, and outer templates.
func (s *ModebarPlugin) Templates() (inner, def, outer string) {
	return s.tmplInner, s.tmplDefault, s.tmplOuter
}

// SetBindingValues sets the dynamic keymap-sourced %CODE% values (e.g. %SPU%)
// merged into the modebar's substitution set.
func (s *ModebarPlugin) SetBindingValues(vals map[string]string) {
	s.bindingValues = vals
}

// ExpandModebar substitutes %CODE% tokens in tmpl from vals (see the internal
// engine): %% is a literal %, an unrecognized %CODE% is left verbatim. Exported
// so other UI strings — the peek-indicator labels — run through the same engine
// as the modebar templates.
func ExpandModebar(tmpl string, vals map[string]string) string {
	return expandModebar(tmpl, vals)
}

// SetColorScheme sets the layered color scheme used to resolve modebar colors.
func (s *ModebarPlugin) SetColorScheme(cs config.ColorScheme) {
	s.colors = cs
}

// SetLocation places the modebar on the "top" (default) or "bottom" screen
// line. When the window already exists it is relocated in place, so the
// option can be flipped at runtime via set_option.
func (s *ModebarPlugin) SetLocation(location string) {
	if location != "bottom" {
		location = "top"
	}
	s.location = location
	if s.windowID == "" {
		return
	}
	dock, priority := s.dockAndPriority()
	s.windowManager.UpdateWindow(s.windowID, func(w *window.Window) {
		w.Dock = dock
		w.Priority = priority
	})
}

// dockAndPriority returns the dock placement for the configured location.
// At the top the modebar outranks the other top windows; at the bottom it
// takes the fixed always-last-line priority.
func (s *ModebarPlugin) dockAndPriority() (window.DockPosition, int) {
	if s.location == "bottom" {
		return window.DockBottom, modebarBottomPriority
	}
	highestPriority := 0
	for _, w := range s.windowManager.GetWindowsByDock(window.DockTop) {
		if w.Priority > highestPriority {
			highestPriority = w.Priority
		}
	}
	return window.DockTop, highestPriority + 100
}

// CreateWindow creates the modebar window.
func (s *ModebarPlugin) CreateWindow() string {
	if s.windowID != "" {
		return s.windowID
	}

	dock, priority := s.dockAndPriority()

	// Create the modebar window with custom rendering; colors resolve
	// dynamically through the "modebar" class.
	s.windowID = s.windowManager.CreateWindow(window.WindowOptions{
		Type:           window.ToolWindow,
		WindowSet:      window.WindowSetModebar,
		Class:          "modebar",
		Dock:           dock,
		Priority:       priority,
		MinHeight:      1,
		MaxHeight:      1,
		CustomRenderer: "modebar",
	})

	return s.windowID
}

// SetActiveSequence sets the active key sequence for display.
func (s *ModebarPlugin) SetActiveSequence(seq string) {
	s.activeSequence = seq
	if len(seq) > s.maxSequenceLength {
		s.maxSequenceLength = len(seq)
	}
}

// SetCompletions sets the key-sequence autocompletion text for display. When
// non-empty it takes precedence over the focused window's context in the
// modebar's middle section.
func (s *ModebarPlugin) SetCompletions(completions string) {
	s.completions = completions
}

// SetLogoRTL flips the modebar logo to its RTL form (_M) or back (M_).
func (s *ModebarPlugin) SetLogoRTL(rtl bool) {
	s.logoRTL = rtl
}

// RenderContent renders the modebar content.
func (s *ModebarPlugin) RenderContent(w *window.Window, screenWidth int) string {
	mainBufferWindow := s.windowManager.GetLastMainWindow()
	focusedWindow := s.windowManager.GetFocusedWindow()

	// Resolve colors dynamically through the modebar's class/type:
	// - text:       modebar fill
	// - modifiers:  active modifiers (key sequence) & surrounding space
	// - buffer:     buffer name (filename)
	// - completion: autocompletion (& surrounding space) when showing
	// - context:    window context when autocompletion isn't showing
	// - messages:   Frag/Heap/Line/Rune stats readout
	// - logo:       M_ logo
	col := func(name string) string {
		return s.colors.Resolve(w.Class, w.Type.Name(), name)
	}
	textColor := col("text")
	modifiersColor := col("modifiers")
	bufferColor := col("buffer")
	numbersColor := col("messages")
	logoColor := col("logo")
	resetColor := col("reset")

	if mainBufferWindow == nil {
		return s.padToWidth(textColor+" Mew Editor "+resetColor, screenWidth)
	}

	// The context/position window is the focused window, unless a prompt is
	// focused, in which case it is the last main buffer (the document).
	ctxWindow := focusedWindow
	if ctxWindow == nil || ctxWindow.Type == window.PromptWindow {
		ctxWindow = mainBufferWindow
	}
	vals := modebarValues(mainBufferWindow, ctxWindow)
	if s.filenameFn != nil {
		if name := s.filenameFn(mainBufferWindow); name != "" {
			vals["FN"] = name
		}
	}
	for code, v := range s.bindingValues {
		vals[code] = v
	}

	// Left: active key sequence.
	sequenceReserved := s.maxSequenceLength
	if len(s.activeSequence) > sequenceReserved {
		sequenceReserved = len(s.activeSequence)
	}
	leftText := " " + s.padRight(s.activeSequence, sequenceReserved) + " "

	logo := "M_"
	if s.logoRTL {
		logo = "_M"
	}
	logoStr := " " + logo + " "

	// Nav-history buttons, inserted just BEFORE the filename and only as wide
	// as needed (collapsing to nothing when the context window has no history).
	// The section sits in the gap that already exists between the key-sequence
	// area and the filename (each is space-padded), so when it collapses the
	// layout is unchanged.
	navStr := s.renderNav(col, modifiersColor, ctxWindow, calculateVisibleLength(leftText))
	navWidth := calculateVisibleLength(navStr)

	// Inner (filename) and outer (readout) come from templates.
	innerStr := " " + expandModebar(s.tmplInner, vals) + " "
	outerStr := " " + expandModebar(s.tmplOuter, vals) + " "

	// Middle: key-sequence completion, else a live outline breadcrumb, else the
	// default template. Context is a live breadcrumb only when it differs from
	// the window's spawn placeholder (the fortune).
	middleColor := col("completion")
	middle := s.completions
	if middle == "" {
		middleColor = col("context")
		if ctx := ctxWindow.Context; ctx != "" && ctx != ctxWindow.SpawnContext {
			middle = ctx
		} else {
			middle = expandModebar(s.tmplDefault, vals)
		}
	}

	// Lay out the space between inner and the logo: the outer readout is
	// right-aligned and ellipsized when it does not fit the remaining space;
	// the middle fills the gap (truncated as needed). Inner and logo are kept.
	remaining := screenWidth - calculateVisibleLength(leftText) - navWidth - calculateVisibleLength(innerStr) - calculateVisibleLength(logoStr)
	if remaining < 0 {
		remaining = 0
	}
	if calculateVisibleLength(outerStr) > remaining {
		outerStr = ellipsizeRight(outerStr, remaining)
	}
	middleStr := fitPad(" "+middle, remaining-calculateVisibleLength(outerStr))

	var modebar strings.Builder
	modebar.WriteString(modifiersColor)
	modebar.WriteString(leftText)
	modebar.WriteString(navStr)
	modebar.WriteString(bufferColor)
	modebar.WriteString(innerStr)
	modebar.WriteString(middleColor)
	modebar.WriteString(middleStr)
	modebar.WriteString(numbersColor)
	modebar.WriteString(outerStr)
	modebar.WriteString(textColor)
	modebar.WriteString(logoColor)
	modebar.WriteString(logoStr)
	modebar.WriteString(resetColor)
	return modebar.String()
}

// modebarValues computes the %CODE% substitution values from the current
// editor state: the document window drives file/fragment metrics; the context
// window (focused, or the document when a prompt is focused) drives caret
// position.
func modebarValues(mainWin, ctxWin *window.Window) map[string]string {
	filename := "[New File]"
	fragStr := "0"
	if mainWin != nil && mainWin.Buffer != nil {
		if fn := mainWin.Buffer.GetFilename(); fn != "" {
			filename = filepath.Base(fn)
		}
		fragStr = formatNumber(mainWin.Buffer.NodeManipulations())
	}
	fortune := ""
	if ctxWin != nil {
		fortune = ctxWin.SpawnContext
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	line, rune_, colv, lineByte := 1, 1, 1, 0
	var absByte int64
	if ctxWin != nil && ctxWin.Buffer != nil {
		pos := ctxWin.CursorPos()
		line = pos.Line + 1
		rune_ = pos.Rune + 1
		runes := []rune(strings.TrimRight(ctxWin.Buffer.GetLine(pos.Line), "\n\r"))
		upto := pos.Rune
		if upto > len(runes) {
			upto = len(runes)
		}
		lineByte = len(string(runes[:upto]))
		colv = visualColumn(runes, upto, ctxWin.ViewState.TabSize) + 1
		absByte = ctxWin.CaretByte()
	}

	return map[string]string{
		"FN":       filename,
		"FORTUNE":  fortune,
		"FRAG":     fragStr,
		"HEAP":     formatBytes(memStats.HeapAlloc),
		"LINE":     strconv.Itoa(line),
		"RUNE":     strconv.Itoa(rune_),
		"COL":      strconv.Itoa(colv),
		"LINEBYTE": strconv.Itoa(lineByte),
		"ABSBYTE":  strconv.FormatInt(absByte, 10),
	}
}

// expandModebar substitutes %CODE% tokens from vals (case-insensitive keys),
// turns %% into a literal %, and leaves an unrecognized %CODE% verbatim so a
// not-yet-implemented code is visible rather than silently dropped.
func expandModebar(tmpl string, vals map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '%' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		if i+1 < len(tmpl) && tmpl[i+1] == '%' {
			b.WriteByte('%')
			i += 2
			continue
		}
		j := strings.IndexByte(tmpl[i+1:], '%')
		if j < 0 {
			b.WriteString(tmpl[i:]) // dangling %: emit the rest literally
			break
		}
		code := tmpl[i+1 : i+1+j]
		if v, ok := vals[strings.ToUpper(code)]; ok {
			b.WriteString(v)
		} else {
			b.WriteString("%" + code + "%")
		}
		i += 1 + j + 1
	}
	return b.String()
}

// visualColumn returns the visual column (0-based) at rune index upto, tabs
// expanding to the next tab stop.
func visualColumn(runes []rune, upto, tabSize int) int {
	if tabSize <= 0 {
		tabSize = 4
	}
	c := 0
	for i := 0; i < upto && i < len(runes); i++ {
		if runes[i] == '\t' {
			c += tabSize - (c % tabSize)
		} else {
			c += textwidth.Rune(runes[i])
		}
	}
	return c
}

// formatBytes renders a byte count with a k/m/g suffix (mirrors the old modebar
// heap readout).
func formatBytes(b uint64) string {
	unit := "k"
	v := b / 1024
	if v > 1024 {
		unit = "m"
		v /= 1024
		if v > 1024 {
			unit = "g"
			v /= 1024
		}
	}
	return fmt.Sprintf("%d%s", v, unit)
}

// truncateToWidth returns the longest prefix of s that fits in max columns.
func truncateToWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := textwidth.Rune(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

// ellipsizeRight truncates s to max columns with a trailing … when it is too
// long.
func ellipsizeRight(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if calculateVisibleLength(s) <= max {
		return s
	}
	return truncateToWidth(s, max-1) + "…"
}

// fitPad ellipsizes s to width columns, then right-pads with spaces to exactly
// width.
func fitPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ellipsizeRight(s, width)
	if vis := calculateVisibleLength(s); vis < width {
		s += strings.Repeat(" ", width-vis)
	}
	return s
}

// padRight pads a string to the right.
func (s *ModebarPlugin) padRight(str string, length int) string {
	if len(str) >= length {
		return str
	}
	return str + strings.Repeat(" ", length-len(str))
}

// padToWidth pads the modebar to screen width.
func (s *ModebarPlugin) padToWidth(line string, width int) string {
	// Calculate visible length (excluding ANSI codes)
	visibleLen := calculateVisibleLength(line)
	if visibleLen >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visibleLen)
}

// calculateVisibleLength calculates the visible column width excluding ANSI
// codes (combining/zero-width runes count 0, wide runes 2).
func calculateVisibleLength(s string) int {
	length := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length += textwidth.Rune(r)
	}
	return length
}

// formatNumber formats a number with k/m/g suffix for compact display.
func formatNumber(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d", n)
	}
	val := n / 1024
	if val < 1024 {
		return fmt.Sprintf("%dk", val)
	}
	val = val / 1024
	if val < 1024 {
		return fmt.Sprintf("%dm", val)
	}
	val = val / 1024
	return fmt.Sprintf("%dg", val)
}
