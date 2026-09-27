package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/hostterm"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/viewport"
)

// parseBoolOption parses a boolean option value (true/false/1/0/yes/no/on/off).
func parseBoolOption(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// boolText is the canonical display form of a boolean option: yes / no. Most
// boolean options carry a verb in their name (show…, …Ignores…, wrap), so
// "showMarks: yes" reads naturally. Input still accepts on/true/1/yes and
// off/false/0/no (parseBoolOption); this is only how a value is reported.
func boolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// getOption returns the current effective value of a named editor option (as a
// string) for the given main-buffer viewport. Per-viewport options are read from the
// viewport's view state (what actually renders); globals from the runtime Config.
func (e *Editor) getOption(w *viewport.Viewport, name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tabsize":
		v := e.Config.TabSize
		if w != nil && w.ViewState.TabSize > 0 {
			v = w.ViewState.TabSize
		}
		return strconv.Itoa(v), true
	case "showlinenumbers":
		v := e.Config.ShowLineNumbers
		if w != nil {
			v = w.ViewState.ShowLineNumbers
		}
		return boolText(v), true
	case "deletenewlineaschar":
		// Stored inverted (ProtectNewlines); report the on/off sense.
		v := e.Config.DeleteNewlineAsChar
		if w != nil {
			v = !w.ViewState.ProtectNewlines
		}
		return boolText(v), true
	case "showinvisibles":
		v := e.Config.ShowInvisibles
		if w != nil {
			v = w.ViewState.ShowInvisibles
		}
		return boolText(v), true
	case "showbidi":
		v := e.Config.ShowBidi
		if w != nil {
			v = w.ViewState.ShowBidi
		}
		return boolText(v), true
	case "rtlcombining":
		// Stored inverted (SuppressRTLCombining); report the on/off sense.
		v := e.Config.RtlCombining
		if w != nil {
			v = !w.ViewState.SuppressRTLCombining
		}
		return boolText(v), true
	case "showmarks":
		v := e.Config.ShowMarks
		if w != nil {
			v = w.ViewState.ShowMarks
		}
		if v == "" {
			v = "no"
		}
		return v, true
	case "insertmode":
		// Stored inverted (OverwriteMode); report the insert-mode sense.
		over := e.Config.OverwriteMode
		if w != nil {
			over = w.ViewState.OverwriteMode
		}
		return boolText(!over), true
	case "readonly":
		v := e.Config.ReadOnly
		if w != nil {
			v = w.ViewState.ReadOnly
		}
		return boolText(v), true
	case "insertcursor":
		return strconv.Itoa(e.Config.InsertCursor), true
	case "overwritecursor":
		return strconv.Itoa(e.Config.OverwriteCursor), true
	case "navigationcursor":
		return strconv.Itoa(e.Config.NavigationCursor), true
	case "autoindent":
		v := e.Config.AutoIndent
		if w != nil {
			v = w.ViewState.AutoIndent
		}
		return boolText(v), true
	case "linkbrowsing":
		v := e.Config.LinkBrowsing
		if w != nil {
			v = w.ViewState.LinkBrowsing
		}
		return boolText(v), true
	case "navigationmode":
		// Navigation mode is the viewport's live browse state — per-viewport
		// only, no global config default.
		v := false
		if w != nil {
			v = w.BrowseActive
		}
		return boolText(v), true
	case "showcolumnruler":
		v := e.Config.ShowColumnRuler
		if w != nil {
			v = w.ViewState.ShowRuler
		}
		return boolText(v), true
	case "scrollbar":
		v := e.Config.Scrollbar
		if w != nil {
			v = w.ViewState.Scrollbar
		}
		return boolText(v), true
	case "rulershowscursor":
		return boolText(e.Config.RulerShowsCursor), true
	case "syntax":
		return e.Config.Syntax, true
	case "syntaxdetect":
		return boolText(e.Config.SyntaxDetect), true
	case "syntaxoverrides":
		v := e.Config.SyntaxOverrides
		if w != nil {
			v = w.ViewState.SyntaxOverrides
		}
		return v, true
	case "macoptionkeys":
		if e.Config.MacOptionKeys == "" {
			return "auto", true
		}
		return e.Config.MacOptionKeys, true
	case "matchignoressinglequote", "matchignoresdoublequote", "matchignoresslashstar",
		"matchignoresslashslash", "matchignoreshash", "matchignoresdoublehyphen",
		"matchignoressemicolon", "matchignorespercent":
		return boolText(*e.matchIgnoreFlag(name)), true
	case "wordwrap":
		return boolText(e.Config.WordWrap), true
	case "searchignorecase":
		return boolText(e.Config.SearchIgnoreCase), true
	case "searchwrap":
		return boolText(e.Config.SearchWrap), true
	case "searchregex":
		return boolText(e.Config.SearchRegex), true
	case "searchbackwards":
		return boolText(e.Config.SearchBackwards), true
	case "searchallbuffers":
		return boolText(e.Config.SearchAllBuffers), true
	case "modebarlocation":
		return e.Config.ModebarLocation, true
	case "modebarinner":
		return e.optStr(w, "modebarinner", e.Config.ModebarInner), true
	case "modebardefault":
		return e.optStr(w, "modebardefault", e.Config.ModebarDefault), true
	case "modebarouter":
		return e.optStr(w, "modebarouter", e.Config.ModebarOuter), true
	case "mappings":
		return e.optStr(w, "mappings", e.Config.MappingsName), true
	case "pagesizeoptimal":
		return e.Config.PageSizeOptimal, true
	case "pageoverlapminimum":
		return e.Config.PageOverlapMinimum, true
	case "pagesizestep":
		return strconv.Itoa(e.Config.PageSizeStep), true
	case "maxrepeat":
		return strconv.Itoa(e.Config.MaxRepeat), true
	case "killringentries":
		return strconv.Itoa(e.Config.KillRingEntries), true
	case "direction":
		// Per-viewport override when set, else the global base direction.
		if w != nil && w.ViewState.Direction != "" {
			return w.ViewState.Direction, true
		}
		if e.Config.Direction == "rtl" {
			return "rtl", true
		}
		return "ltr", true
	case "flipbidiforhost":
		return e.Config.FlipBidiForHost, true
	case "rtlmarkmode":
		return e.Config.RtlMarkMode, true
	case "prompttimeout":
		return strconv.Itoa(e.Config.PromptTimeout), true
	case "scripttimeout":
		return strconv.Itoa(e.Config.ScriptTimeout), true
	}
	return "", false
}

// resolveRtlMarkMode turns the "auto" sentinel into a concrete rtlMarkMode from
// the detected host terminal; any explicit mode passes through unchanged. The
// stored option keeps reading "auto"; only the value pushed to the renderer and
// host is resolved. Terminals whose quirks are not yet handled resolve to
// "normal".
func resolveRtlMarkMode(mode string) string {
	if mode != "auto" {
		return mode
	}
	return rtlMarkModeForTerminal(hostterm.Detect())
}

// rtlMarkModeForTerminal maps a detected host terminal to the rtlMarkMode "auto"
// selects for it. Terminals whose quirks are not yet handled map to "normal".
func rtlMarkModeForTerminal(k hostterm.Kind) string {
	switch k {
	case hostterm.TerminalITerm2:
		return "iterm2"
	case hostterm.TerminalAlacritty, hostterm.TerminalGhostty:
		return "drift"
	case hostterm.TerminalAppleTerminal:
		return "compose"
	case hostterm.TerminalSDL:
		// The graphical host shapes and positions marks natively; no terminal
		// mark-drift workaround applies, so emit the plain cluster.
		return "normal"
	default:
		return "normal"
	}
}

// resolveFlipBidiForHost turns the flipBidiForHost setting into the boolean
// pushed to the renderer at startup (or on a set_option). "true"/"false" pass
// through; "auto" turns the flip ON for a host known by sniffing to apply its
// own bidi reordering (see flipBidiForTerminal) and otherwise leaves it off for
// the runtime probe to decide once RTL content appears.
func resolveFlipBidiForHost(mode string) bool {
	flip, _, _, _ := flipSettings(mode)
	return flip
}

// flipSettings resolves the flipBidiForHost setting into the three renderer
// axes: whether to flip, the run segmentation (wordwise), and whether the host
// needs the ride-safe selection bar. Also reports whether sniffing recognised
// the host (known), so "auto" can decide between trusting the sniff and arming
// the probe. "true"/"false" are explicit; an explicit flip takes the
// conservative Terminal.app profile (whole-run + ride-safe).
func flipSettings(mode string) (flip, wordwise, rideSafe, known bool) {
	switch mode {
	case "true":
		return true, false, true, true
	case "false":
		return false, false, false, true
	case "auto":
		return hostterm.BidiProfile(hostterm.Detect())
	}
	return false, false, false, false
}

// flippingForKitty reports whether mew is turning right-to-left runs back for
// kitty -- the one path where kitty may drop niqqud, and so the condition the
// force_ltr nudge watches for. hostterm.BidiProfile already reads the config to
// decide whether kitty is reordering at all; this only asks which host it is.
func flippingForKitty(flip bool) bool {
	return flip && hostterm.Detect() == hostterm.TerminalKitty
}

// setOption sets a named editor option. Per-viewport options (tabSize,
// showLineNumbers, showInvisibles, showColumnRuler) are written to the given
// viewport's own ViewState, so the change applies to that viewport and does not
// leak into the editor defaults or future viewports. Global options are written to the runtime
// Config. Nothing is written to config.GeneralConfig / the on-disk config.
func (e *Editor) setOption(w *viewport.Viewport, name, value string) bool {
	parseInt := func(minVal int) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < minVal {
			e.ShowWarning(name + " must be an integer >= " + strconv.Itoa(minVal))
			return 0, false
		}
		return n, true
	}
	parseBool := func() (bool, bool) {
		b, ok := parseBoolOption(value)
		if !ok {
			e.ShowWarning(name + " must be true or false")
		}
		return b, ok
	}

	lname := strings.ToLower(strings.TrimSpace(name))
	// Setting a per-viewport option on a specific viewport is a deliberate choice:
	// pin it so the grammar options overlay does not overwrite it later.
	if w != nil && cliPerViewportOptions[lname] {
		w.MarkOptionOverridden(lname)
	}

	switch lname {
	// Per-viewport options: write the viewport's ViewState (fall back to the
	// editor default only when there is no viewport).
	case "tabsize":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.TabSize = n
		} else {
			e.Config.TabSize = n
		}
	case "showlinenumbers":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowLineNumbers = b
		} else {
			e.Config.ShowLineNumbers = b
		}
	case "deletenewlineaschar":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ProtectNewlines = !b // stored inverted
		} else {
			e.Config.DeleteNewlineAsChar = b
		}
	case "showinvisibles":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowInvisibles = b
		} else {
			e.Config.ShowInvisibles = b
		}
	case "showbidi":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowBidi = b
		} else {
			e.Config.ShowBidi = b
		}
	case "rtlcombining":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.SuppressRTLCombining = !b // stored inverted
		} else {
			e.Config.RtlCombining = b
		}
	case "showmarks":
		v, ok := config.ParseShowMarks(value)
		if !ok {
			e.ShowWarning("showMarks must be no, yes, or all")
			return false
		}
		if w != nil {
			w.ViewState.ShowMarks = v
		} else {
			e.Config.ShowMarks = v
		}
	case "insertmode":
		b, ok := parseBool()
		if !ok {
			return false
		}
		// Stored inverted: insertMode on -> not overwrite.
		if w != nil {
			w.ViewState.OverwriteMode = !b
		} else {
			e.Config.OverwriteMode = !b
		}
	case "readonly":
		b, ok := parseBool()
		if !ok {
			return false
		}
		// Read-only is a per-view flag that gates edits; it does not drive the
		// lock. The mew lock is lazy — claimed on the first actual edit
		// (trackEdit) — so toggling read-only never acquires or releases it.
		if w != nil {
			w.ViewState.ReadOnly = b
		} else {
			e.Config.ReadOnly = b
		}
	case "insertcursor", "overwritecursor", "navigationcursor":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		if n > 6 {
			e.ShowWarning(name + " must be a DECSCUSR shape 0-6")
			return false
		}
		switch lname {
		case "insertcursor":
			e.Config.InsertCursor = n
		case "overwritecursor":
			e.Config.OverwriteCursor = n
		default:
			e.Config.NavigationCursor = n
		}
		e.RequestRender()
	case "autoindent":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.AutoIndent = b
		} else {
			e.Config.AutoIndent = b
		}
	case "linkbrowsing":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.LinkBrowsing = b
			if !b {
				w.BrowseActive = false // turning the layer off retires the buttons
			}
		} else {
			e.Config.LinkBrowsing = b
		}
		e.RequestRender()
	case "navigationmode":
		// Enter/leave link browse (navigation) mode for the viewport — the
		// explicit replacement for the old auto-arm. Per-viewport only; a
		// nil target (global) has no navigation state to toggle. Rendering
		// still gates buttons on the link layer being on and the page being
		// linkable, so toggling this on a non-wiki viewport is a harmless no-op.
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			// Entering nav mode needs the link layer on (buttons render only
			// then); with linkBrowsing off, ^O N is a no-op. Leaving always
			// disarms.
			w.BrowseActive = b && w.ViewState.LinkBrowsing
			w.NavIdealSet = false // drop any vertical-nav ideal on a mode flip
			e.RequestRender()
		}
	case "showcolumnruler":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowRuler = b
		} else {
			e.Config.ShowColumnRuler = b
		}
	case "scrollbar":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.Scrollbar = b
		} else {
			e.Config.Scrollbar = b
		}
	case "rulershowscursor":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.RulerShowsCursor = b
	case "syntax":
		return e.setSyntax(strings.TrimSpace(value))
	case "syntaxdetect":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SyntaxDetect = b
		e.resetSyntaxCaches()
	case "syntaxoverrides":
		v := strings.TrimSpace(value)
		if w != nil {
			w.ViewState.SyntaxOverrides = v
		} else {
			e.Config.SyntaxOverrides = v
		}
		// Grammar file resolution changes, so drop cached grammars and reload
		// the global grammar under the new override set.
		e.resetSyntaxCaches()
		e.reloadGlobalGrammar()
	case "macoptionkeys":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "auto" && v != "true" && v != "false" {
			e.ShowWarning("macOptionKeys: auto, true, or false")
			return false
		}
		e.Config.MacOptionKeys = v
		e.invalidateFocusedOptions()
		e.applyMacOptionKeys()
	case "matchignoressinglequote", "matchignoresdoublequote", "matchignoresslashstar",
		"matchignoresslashslash", "matchignoreshash", "matchignoresdoublehyphen",
		"matchignoressemicolon", "matchignorespercent":
		b, ok := parseBool()
		if !ok {
			return false
		}
		*e.matchIgnoreFlag(name) = b
	// Global options: write the runtime editor Config.
	case "wordwrap":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.WordWrap = b
	case "searchignorecase":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchIgnoreCase = b
	case "searchwrap":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchWrap = b
	case "searchregex":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchRegex = b
	case "searchbackwards":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchBackwards = b
	case "searchallbuffers":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchAllBuffers = b
	case "modebarlocation":
		loc := strings.ToLower(strings.TrimSpace(value))
		if loc != "top" && loc != "bottom" {
			e.ShowWarning(name + " must be top or bottom")
			return false
		}
		e.Config.ModebarLocation = loc
		e.Modebar.SetLocation(e.optStr(e.ViewportManager.GetFocusedViewport(), "modebarlocation", loc))
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebarinner":
		e.Config.ModebarInner = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebardefault":
		e.Config.ModebarDefault = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebarouter":
		e.Config.ModebarOuter = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "mappings":
		e.Config.MappingsName = strings.TrimSpace(value)
		e.appliedMappingSet = "" // force the key processor to reload
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "pagesizeoptimal":
		if _, _, _, ok := parseCountOrPercent(value); !ok {
			e.ShowWarning("pageSizeOptimal must be a count (24) or a percentage (50%)")
			return false
		}
		e.Config.PageSizeOptimal = strings.TrimSpace(value)
		e.rebuildPageSizeSpec()
	case "pageoverlapminimum":
		if _, _, _, ok := parseCountOrPercent(value); !ok {
			e.ShowWarning("pageOverlapMinimum must be a count (2) or a percentage (10%)")
			return false
		}
		e.Config.PageOverlapMinimum = strings.TrimSpace(value)
		e.rebuildPageSizeSpec()
	case "pagesizestep":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.PageSizeStep = n
		e.rebuildPageSizeSpec()
	case "maxrepeat":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		e.Config.MaxRepeat = n
	case "killringentries":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		e.Config.KillRingEntries = n
		e.trimKillRing()
	case "direction":
		dir := strings.ToLower(strings.TrimSpace(value))
		if dir != "ltr" && dir != "rtl" {
			e.ShowWarning("direction must be ltr or rtl")
			return false
		}
		if w != nil {
			// Per-viewport base direction (rendering reads ViewState.Direction
			// first — see winRTL). The global renderer base is untouched.
			w.ViewState.Direction = dir
		} else {
			e.Config.Direction = dir
			e.Renderer.SetBaseRTL(dir == "rtl")
			e.ColumnRuler.SetRTL(dir == "rtl")
		}
		e.RequestRender()
	case "flipbidiforhost":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "auto" && v != "true" && v != "false" {
			e.ShowWarning("flipBidiForHost: auto, true, or false")
			return false
		}
		e.Config.FlipBidiForHost = v
		flip, wordwise, rideSafe, known := flipSettings(v)
		e.kittyFlipActive = flippingForKitty(flip)
		e.Renderer.SetFlipBidiForHost(flip)
		e.Renderer.SetFlipWordwise(wordwise)
		e.Renderer.SetFlipRideSafeSelection(rideSafe)
		if v == "auto" && !known {
			e.bidiProbeState = bidiProbeIdle // re-arm detection for an unknown host
		} else {
			e.bidiProbeState = bidiProbeDone // explicit choice or a recognised host
		}
		e.RequestRender()
	case "rtlmarkmode":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "normal" && v != "iterm2" && v != "compose" && v != "drift" && v != "auto" {
			e.ShowWarning("rtlMarkMode: auto, normal, iterm2, compose, or drift")
			return false
		}
		e.Config.RtlMarkMode = v
		resolved := resolveRtlMarkMode(v)
		e.Renderer.SetRtlMarkMode(resolved)
		if e.Config.RtlMarkModeSink != nil {
			e.Config.RtlMarkModeSink(resolved)
		}
		e.RequestRender()
	case "prompttimeout":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.PromptTimeout = n
	case "scripttimeout":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.ScriptTimeout = n
		if e.pawConfig != nil {
			e.pawConfig.DefaultTokenTimeout = tokenTimeout(n)
		}
	default:
		e.ShowWarning("Unknown option: " + name)
		return false
	}

	e.ShowNotificationTagged("Option '"+name+"' set to "+value, "optionset")
	e.RequestRender()
	return true
}

// matchIgnoreFlag maps a matchIgnores* option name to its field.
func (e *Editor) matchIgnoreFlag(name string) *bool {
	switch strings.ToLower(name) {
	case "matchignoressinglequote":
		return &e.Config.MatchIgnoresSingleQuote
	case "matchignoresdoublequote":
		return &e.Config.MatchIgnoresDoubleQuote
	case "matchignoresslashstar":
		return &e.Config.MatchIgnoresSlashStar
	case "matchignoresslashslash":
		return &e.Config.MatchIgnoresSlashSlash
	case "matchignoreshash":
		return &e.Config.MatchIgnoresHash
	case "matchignoresdoublehyphen":
		return &e.Config.MatchIgnoresDoubleHyphen
	case "matchignoressemicolon":
		return &e.Config.MatchIgnoresSemicolon
	case "matchignorespercent":
		return &e.Config.MatchIgnoresPercent
	}
	panic("unknown match flag " + name)
}

// toggleOptions shows the editor-options display, or dismisses it if a second
// invocation arrives while it is already open (mirroring toggleHelp). The
// viewport carries Class "options" so it can be found and removed.
func (e *Editor) toggleOptions() bool {
	// Dismiss an existing options viewport (top dock, Class "options").
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.Class == "options" {
			e.ViewportManager.RemoveViewport(w.ID)
			e.RequestRender()
			return true
		}
	}

	// Build options display, showing the EFFECTIVE values for the last
	// active editor viewport (per-viewport settings govern the viewport).
	optWin := e.ViewportManager.GetLastMainViewport()
	opt := func(name string) string {
		v, _ := e.getOption(optWin, name)
		return v
	}
	var content strings.Builder
	content.WriteString("Editor Options:\n\n")
	content.WriteString(fmt.Sprintf("  Show Line Numbers: %s\n", opt("showLineNumbers")))
	content.WriteString(fmt.Sprintf("  Show Column Ruler: %s\n", opt("showColumnRuler")))
	content.WriteString(fmt.Sprintf("  Ruler Shows Cursor: %s\n", opt("rulerShowsCursor")))
	syntaxName := e.Config.Syntax
	if syntaxName == "" {
		syntaxName = "(none)"
	}
	content.WriteString(fmt.Sprintf("  Syntax: %s\n", syntaxName))
	content.WriteString(fmt.Sprintf("  Syntax Detect: %s\n", opt("syntaxDetect")))
	if ov := opt("syntaxOverrides"); strings.TrimSpace(ov) != "" {
		content.WriteString(fmt.Sprintf("  Syntax Overrides: %s\n", ov))
	}
	var ignores []string
	for _, f := range []struct {
		on    bool
		label string
	}{
		{e.Config.MatchIgnoresSingleQuote, "'..'"},
		{e.Config.MatchIgnoresDoubleQuote, "\"..\""},
		{e.Config.MatchIgnoresSlashStar, "/*..*/"},
		{e.Config.MatchIgnoresSlashSlash, "//.."},
		{e.Config.MatchIgnoresHash, "#.."},
		{e.Config.MatchIgnoresDoubleHyphen, "--.."},
		{e.Config.MatchIgnoresSemicolon, ";.."},
		{e.Config.MatchIgnoresPercent, "%.."},
	} {
		if f.on {
			ignores = append(ignores, f.label)
		}
	}
	if len(ignores) == 0 {
		ignores = append(ignores, "(none)")
	}
	content.WriteString(fmt.Sprintf("  Match Ignores (no grammar): %s\n", strings.Join(ignores, " ")))
	content.WriteString(fmt.Sprintf("  Tab Size: %s\n", opt("tabSize")))
	content.WriteString(fmt.Sprintf("  Show Invisibles: %s\n", opt("showInvisibles")))
	content.WriteString(fmt.Sprintf("  Show Bidi Markers: %s\n", opt("showBidi")))
	content.WriteString(fmt.Sprintf("  Show Marks: %s\n", opt("showMarks")))
	content.WriteString(fmt.Sprintf("  Insert Mode: %s\n", opt("insertMode")))
	content.WriteString(fmt.Sprintf("  Read Only: %s\n", opt("readOnly")))
	content.WriteString(fmt.Sprintf("  Word Wrap: %s\n", opt("wordWrap")))
	content.WriteString(fmt.Sprintf("  Search Ignore Case: %s\n", opt("searchIgnoreCase")))
	content.WriteString(fmt.Sprintf("  Search Wrap: %s\n", opt("searchWrap")))
	content.WriteString(fmt.Sprintf("  Search Regex (standard syntax): %s\n", opt("searchRegex")))
	content.WriteString(fmt.Sprintf("  Search Backwards: %s\n", opt("searchBackwards")))
	content.WriteString(fmt.Sprintf("  Search All Buffers: %s\n", opt("searchAllBuffers")))
	content.WriteString(fmt.Sprintf("  Modebar Location: %s\n", opt("modebarLocation")))
	content.WriteString(fmt.Sprintf("  Page Size Optimal: %s\n", opt("pageSizeOptimal")))
	content.WriteString(fmt.Sprintf("  Page Overlap Minimum: %s\n", opt("pageOverlapMinimum")))
	content.WriteString(fmt.Sprintf("  Page Size Step: %s\n", opt("pageSizeStep")))
	content.WriteString(fmt.Sprintf("  Max Repeat: %s\n", opt("maxRepeat")))
	content.WriteString(fmt.Sprintf("  Kill Ring Entries: %s\n", opt("killRingEntries")))
	content.WriteString(fmt.Sprintf("  Direction: %s\n", opt("direction")))
	content.WriteString(fmt.Sprintf("  Prompt Timeout (s, 0=never): %s\n", opt("promptTimeout")))
	content.WriteString(fmt.Sprintf("  Script Timeout (s, 0=never): %s\n", opt("scriptTimeout")))
	content.WriteString(fmt.Sprintf("\n  Mappings: %s\n", e.LoadedConfig.General.MappingsName))
	content.WriteString(fmt.Sprintf("  Layout: %s\n", e.LoadedConfig.General.Layout))
	content.WriteString("\nInvoke editor_options again to close...")

	buf := e.lib.NewFromString(content.String())
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:             viewport.ToolViewport,
		ViewportSet:      "help",
		Class:            "options",
		Dock:             viewport.DockTop,
		Priority:         100,
		MinHeight:        8,
		MaxHeight:        15,
		MessageTopCenter: "Editor Options",
		Buffer:           buf,
		ShowLineNumbers:  false,
	})
	e.RequestRender()
	return true
}
