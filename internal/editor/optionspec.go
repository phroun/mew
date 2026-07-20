package editor

import (
	"strings"

	"github.com/phroun/mew/internal/window"
)

// This file is the single canonical description of mew's set_option surface:
// every option name, whether set_option routes it to a window or the editor,
// its value grammar, and — for boolean and enum options — the ordered list of
// canonical values a rotation walks. Everything else that needs to know the
// option set (the CLI known/per-window maps, set_option_next / set_option_prior)
// derives from optionSpecs here, so there is one place to keep in step.

// optionKind classifies an option's value grammar.
type optionKind int

const (
	optBoolKind  optionKind = iota // false | true (accepts on/off/1/0/yes/no as input)
	optEnumKind                    // one of a fixed, ordered set of string values
	optIntKind                     // a non-negative integer
	optCountKind                   // a count (24) or a percentage (50%)
	optStrKind                     // free-form text (templates, grammar/mapping names)
)

// optionSpec is the canonical description of one option. Values holds the
// allowed values in the sequence a rotation advances through — for boolean and
// enum options only; the other kinds have no finite value list (Values is nil).
// The values are exactly the strings getOption reports and setOption accepts, so
// a rotation can round-trip them through the cascade.
type optionSpec struct {
	Name   string     // canonical camelCase name, e.g. "showMarks"
	PerWin bool       // set_option routes it to the window's ViewState (else the editor Config)
	Kind   optionKind //
	Values []string   // ordered canonical values (bool/enum only), else nil
}

// boolValues is the canonical value sequence shared by every boolean option:
// no (off) first, then yes (on), so a rotation reads as a natural toggle. These
// are exactly what getOption reports (see boolText); setOption additionally
// accepts on/true/1 and off/false/0 as input aliases.
var boolValues = []string{"no", "yes"}

// optionSpecs is the ordered, canonical list of every option set_option accepts
// at runtime. Load-time-only [general] keys (layout, projectConfig, useLocks,
// ...) are deliberately not here — they are not part of the set_option surface.
// The order is the sequence other code should present these in.
var optionSpecs = []optionSpec{
	// Per-window view options (set_option targets a window's ViewState).
	{"tabSize", true, optIntKind, nil},
	{"showLineNumbers", true, optBoolKind, boolValues},
	{"showInvisibles", true, optBoolKind, boolValues},
	{"showBidi", true, optBoolKind, boolValues},
	{"showMarks", true, optEnumKind, []string{"no", "yes", "all"}},
	{"insertMode", true, optBoolKind, boolValues},
	{"readOnly", true, optBoolKind, boolValues},
	{"showColumnRuler", true, optBoolKind, boolValues},
	{"direction", true, optEnumKind, []string{"ltr", "rtl"}},

	// Global editor options.
	{"rulerShowsCursor", false, optBoolKind, boolValues},
	{"syntax", false, optStrKind, nil},
	{"syntaxDetect", false, optBoolKind, boolValues},
	{"macOptionKeys", false, optEnumKind, []string{"auto", "true", "false"}},
	{"matchIgnoresSingleQuote", false, optBoolKind, boolValues},
	{"matchIgnoresDoubleQuote", false, optBoolKind, boolValues},
	{"matchIgnoresSlashStar", false, optBoolKind, boolValues},
	{"matchIgnoresSlashSlash", false, optBoolKind, boolValues},
	{"matchIgnoresHash", false, optBoolKind, boolValues},
	{"matchIgnoresDoubleHyphen", false, optBoolKind, boolValues},
	{"matchIgnoresSemicolon", false, optBoolKind, boolValues},
	{"matchIgnoresPercent", false, optBoolKind, boolValues},
	{"wordWrap", false, optBoolKind, boolValues},
	{"searchIgnoreCase", false, optBoolKind, boolValues},
	{"searchWrap", false, optBoolKind, boolValues},
	{"searchRegex", false, optBoolKind, boolValues},
	{"modebarLocation", false, optEnumKind, []string{"top", "bottom"}},
	{"pageSizeOptimal", false, optCountKind, nil},
	{"pageOverlapMinimum", false, optCountKind, nil},
	{"pageSizeStep", false, optIntKind, nil},
	{"maxRepeat", false, optIntKind, nil},
	{"killRingEntries", false, optIntKind, nil},
	{"promptTimeout", false, optIntKind, nil},
	{"scriptTimeout", false, optIntKind, nil},
	{"debounceMs", false, optIntKind, nil},
	{"maxRenderDelayMs", false, optIntKind, nil},
	{"modebarInner", false, optStrKind, nil},
	{"modebarDefault", false, optStrKind, nil},
	{"modebarOuter", false, optStrKind, nil},
	{"mappings", false, optStrKind, nil},
	{"flipBidiForHost", false, optEnumKind, []string{"auto", "true", "false"}},
}

// optionSpecByLower maps a lowercased option name to its spec. cliKnownOptions /
// cliPerWindowOptions (the maps the launch walk and setOption consult) are
// derived from the same table, so the three can never drift.
var (
	optionSpecByLower   = map[string]optionSpec{}
	cliKnownOptions     = map[string]bool{}
	cliPerWindowOptions = map[string]bool{}
	// perWindowOptionKeys is the lowercased per-window option names in canonical
	// order — the set reconcileGrammarOptions and clear_option re-derive.
	perWindowOptionKeys []string
)

func init() {
	for _, s := range optionSpecs {
		key := strings.ToLower(s.Name)
		optionSpecByLower[key] = s
		cliKnownOptions[key] = true
		if s.PerWin {
			cliPerWindowOptions[key] = true
			perWindowOptionKeys = append(perWindowOptionKeys, key)
		}
	}
}

// applyResolvedOption writes a per-window option's ViewState value from the
// class/grammar/type overlay over the editor default — the value the window
// would have with no explicit override. reconcileGrammarOptions and clear_option
// share it, so the resolution rule for each option lives in exactly one place.
func (e *Editor) applyResolvedOption(w *window.Window, key string) {
	switch key {
	case "tabsize":
		w.ViewState.TabSize = e.optInt(w, "tabsize", e.Config.TabSize, 1)
	case "showlinenumbers":
		w.ViewState.ShowLineNumbers = e.optBool(w, "showlinenumbers", e.Config.ShowLineNumbers)
	case "showinvisibles":
		w.ViewState.ShowInvisibles = e.optBool(w, "showinvisibles", e.Config.ShowInvisibles)
	case "showbidi":
		w.ViewState.ShowBidi = e.optBool(w, "showbidi", e.Config.ShowBidi)
	case "showmarks":
		w.ViewState.ShowMarks = e.optMarks(w, e.Config.ShowMarks)
	case "insertmode":
		// Resolve the insert-mode sense through the overlay, then store inverted.
		w.ViewState.OverwriteMode = !e.optBool(w, "insertmode", !e.Config.OverwriteMode)
	case "readonly":
		w.ViewState.ReadOnly = e.optBool(w, "readonly", e.Config.ReadOnly)
	case "showcolumnruler":
		w.ViewState.ShowRuler = e.optBool(w, "showcolumnruler", e.Config.ShowColumnRuler)
	case "direction":
		w.ViewState.Direction = e.optDir(w, "direction", e.Config.Direction)
	}
}

// clearOption drops a per-window option's explicit override on the given window
// and reverts its ViewState to the resolved default (the class/grammar/type
// overlay over the editor default). It fails (with a transient warning) for
// unknown options and for global options, which have no per-window layer.
func (e *Editor) clearOption(w *window.Window, name string) bool {
	spec, ok := lookupOptionSpec(name)
	if !ok {
		e.ShowWarning("Unknown option: " + name)
		return false
	}
	if !spec.PerWin {
		e.ShowWarning(spec.Name + " is a global option; it has no per-window value to clear")
		return false
	}
	if w == nil {
		e.ShowWarning("clear_option needs a target window")
		return false
	}
	key := strings.ToLower(spec.Name)
	w.ClearOptionOverridden(key)
	// Re-derive unconditionally (not via the overlay-gated reconcile) so a plain
	// window with no overlay also reverts to the editor default.
	e.applyResolvedOption(w, key)
	e.ShowNotification("Option '" + spec.Name + "' cleared")
	e.RequestRender()
	return true
}

// lookupOptionSpec returns the spec for a name (case-insensitively) and whether
// it is a known option.
func lookupOptionSpec(name string) (optionSpec, bool) {
	s, ok := optionSpecByLower[strings.ToLower(strings.TrimSpace(name))]
	return s, ok
}

// rotateOption reads an option's current value through the cascade, advances it
// by dir (+1 = next, -1 = prior) within its canonical value sequence, and sets
// the result. It fails (with a transient warning) for unknown options and for
// options with no finite value list — integers, counts, and free-form strings.
func (e *Editor) rotateOption(w *window.Window, name string, dir int) bool {
	spec, ok := lookupOptionSpec(name)
	if !ok {
		e.ShowWarning("Unknown option: " + name)
		return false
	}
	if len(spec.Values) == 0 {
		e.ShowWarning(spec.Name + " has no fixed set of values to rotate through")
		return false
	}
	cur, ok := e.getOption(w, name)
	if !ok {
		e.ShowWarning("Unknown option: " + name)
		return false
	}
	// Locate the current value in the canonical sequence. If it is not one of
	// them (an input alias, or an unset value), step onto the sequence end the
	// requested direction points at, so a rotation still moves.
	idx := -1
	for i, v := range spec.Values {
		if strings.EqualFold(v, cur) {
			idx = i
			break
		}
	}
	n := len(spec.Values)
	var next int
	switch {
	case idx < 0 && dir > 0:
		next = 0
	case idx < 0:
		next = n - 1
	default:
		next = ((idx+dir)%n + n) % n
	}
	return e.setOption(w, name, spec.Values[next])
}

// promptSetOption opens an input prompt for an option's value — used when
// set_option is called with just a name. The label lists the canonical choices
// ("Set direction (ltr/rtl): "), and the prompt history is seeded with every
// value in registry order followed by the current value as the last filled
// line, so pressing Up walks the choices and Enter on the blank line keeps the
// current value (the assumed default). Options with no fixed value list just
// offer the current value as an editable default. Returns true once the prompt
// is open (the set happens in the callback); false for an unknown option.
func (e *Editor) promptSetOption(w *window.Window, name string) bool {
	spec, ok := lookupOptionSpec(name)
	if !ok {
		e.ShowWarning("Unknown option: " + name)
		return false
	}
	cur, _ := e.getOption(w, spec.Name)

	label := "Set " + spec.Name
	if len(spec.Values) > 0 {
		label += " (" + strings.Join(spec.Values, "/") + ")"
	}
	label += ": "

	// One value per history line; the current value is repeated as the last
	// filled line, and the trailing "\n" leaves the cursor on a blank input line
	// whose Enter falls back to that default.
	lines := append(append([]string{}, spec.Values...), cur)
	initial := strings.Join(lines, "\n") + "\n"

	e.PromptMgr.PromptForInput(label, initial, func(accepted bool, _, text string) {
		defer e.RequestRender()
		if !accepted {
			return
		}
		v := strings.TrimSpace(text)
		if v == "" {
			v = cur // empty input keeps the current value
		}
		e.setOption(w, spec.Name, v)
	}, "")
	return true
}
