package editor

import (
	"strconv"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/window"
)

// windowClass / windowType / windowGrammarName expose a window's three overlay
// dimensions. A window's syntax is its buffer's syntax, so every option can be
// resolved for a window through the class/grammar/type cascade.
func (e *Editor) windowClass(w *window.Window) string {
	if w == nil {
		return ""
	}
	return w.Class
}

func (e *Editor) windowType(w *window.Window) string {
	if w == nil {
		return ""
	}
	return w.Type.Name()
}

func (e *Editor) windowGrammarName(w *window.Window) string {
	if w == nil {
		return ""
	}
	return e.bufferGrammarName(w.Buffer)
}

// optSig is the composite overlay signature (class, grammar, type) for a
// window — used to tell when a window's resolved options need re-deriving.
func (e *Editor) optSig(w *window.Window) string {
	return e.windowClass(w) + "\x1f" + e.windowGrammarName(w) + "\x1f" + e.windowType(w)
}

// resolveOpt returns the raw overlaid value for key and whether the cascade
// supplied it (else the caller's base applies).
func (e *Editor) resolveOpt(w *window.Window, key string) (string, bool) {
	return e.LoadedConfig.ResolveOptionOverlay(e.windowClass(w), e.windowGrammarName(w), e.windowType(w), key)
}

// optBool / optInt / optStr / optDir resolve a typed option for a window: the
// class/grammar/type overlay if present, else the given base (the editor-wide
// value).
func (e *Editor) optBool(w *window.Window, key string, base bool) bool {
	if raw, ok := e.resolveOpt(w, key); ok {
		if b, ok := parseBoolOption(raw); ok {
			return b
		}
	}
	return base
}

func (e *Editor) optInt(w *window.Window, key string, base, min int) int {
	if raw, ok := e.resolveOpt(w, key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n >= min {
			return n
		}
	}
	return base
}

func (e *Editor) optStr(w *window.Window, key, base string) string {
	if raw, ok := e.resolveOpt(w, key); ok {
		return raw
	}
	return base
}

// optMarks resolves the showMarks enum for a window: the class/grammar/type
// overlay if present and valid, else the given base, normalized to no/yes/all
// (config.ParseShowMarks, which also accepts boolean aliases).
func (e *Editor) optMarks(w *window.Window, base string) string {
	if raw, ok := e.resolveOpt(w, "showmarks"); ok {
		if v, ok := config.ParseShowMarks(raw); ok {
			return v
		}
	}
	if base == "" {
		return "no"
	}
	return base
}

func (e *Editor) optDir(w *window.Window, key, base string) string {
	if raw, ok := e.resolveOpt(w, key); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "ltr":
			return "ltr"
		case "rtl":
			return "rtl"
		case "":
			return ""
		}
	}
	return base
}

// bufferGrammarName returns the name of the syntax grammar in effect for a
// buffer ("" when none), preferring the grammar the highlighter has already
// resolved and cached so repeated calls are cheap.
func (e *Editor) bufferGrammarName(b *buffer.Buffer) string {
	if b == nil {
		return ""
	}
	if c := e.synCaches[b]; c != nil && c.grammar != nil {
		return c.grammar.Name
	}
	if g, _ := e.bufferGrammar(b); g != nil {
		return g.Name
	}
	return ""
}

// reconcileGrammarOptions re-derives a main-buffer window's per-window options
// from the base [options] overlaid by the class/grammar/type cascade whenever
// the window's overlay signature changes. Options the user set explicitly
// (marked overridden) are left untouched. It runs each frame but only does work
// on an actual change, so a plain window (empty signature, no overlays) is never
// touched.
func (e *Editor) reconcileGrammarOptions(w *window.Window) {
	if w == nil || w.Buffer == nil || w.Type == window.PromptWindow {
		return
	}
	// Per-window syntax first (grammar-agnostic: class + type, never grammar,
	// which would be circular). Set before anything reads the window's
	// grammar, so windowGrammarName below reflects it.
	e.reconcileWindowSyntax(w)
	class, grammar, bufType := e.windowClass(w), e.windowGrammarName(w), e.windowType(w)
	newSig := class + "\x1f" + grammar + "\x1f" + bufType
	oldSig := w.AppliedOptionSig()
	if newSig == oldSig {
		return
	}
	w.SetAppliedOptionSig(newSig)

	// Only rewrite ViewState when an overlay applies now, or applied before (so
	// a removed overlay reverts to base). A plain window — no overlay either way
	// — is left exactly as created.
	affected := e.LoadedConfig.HasOptionOverlay(class, grammar, bufType)
	if !affected {
		if p := strings.SplitN(oldSig, "\x1f", 3); len(p) == 3 {
			affected = e.LoadedConfig.HasOptionOverlay(p[0], p[1], p[2])
		}
	}
	if !affected {
		return
	}

	// Re-derive every per-window option the user has not pinned. The resolution
	// rule for each lives in applyResolvedOption, shared with clear_option.
	for _, key := range perWindowOptionKeys {
		if !w.IsOptionOverridden(key) {
			e.applyResolvedOption(w, key)
		}
	}
}

// reconcileWindowSyntax resolves the window's default grammar from a
// grammar-agnostic overlay ([options.<type>] / [<class>.options] syntax=...)
// and stores it in ViewState.Syntax. On a change it drops the buffer's
// highlight cache so the new grammar takes effect. "" inherits the global
// syntax option.
func (e *Editor) reconcileWindowSyntax(w *window.Window) {
	name := ""
	if v, ok := e.LoadedConfig.ResolveOptionOverlay(e.windowClass(w), "", e.windowType(w), "syntax"); ok {
		name = strings.TrimSpace(v)
		if strings.EqualFold(name, "none") {
			name = ""
		}
	}
	if name != w.ViewState.Syntax {
		w.ViewState.Syntax = name
		if e.synCaches != nil {
			delete(e.synCaches, w.Buffer)
		}
	}
}

// invalidateFocusedOptions forces the next reconcileFocusedOptions to re-apply
// (after a set_option changed a focused-scoped base value).
func (e *Editor) invalidateFocusedOptions() { e.appliedFocusedSig = "\x00" }

// reconcileFocusedOptions applies the focused-scoped options — the modebar
// templates and location, the macOS-Option key layer, and the active key
// mapping set — resolved through the focused window's class/grammar/type
// overlay, whenever that window's signature changes. These are editor-wide in
// effect (one modebar, one key processor), so they follow the focused window.
func (e *Editor) reconcileFocusedOptions() {
	fw := e.WindowManager.GetFocusedWindow()
	sig := e.optSig(fw)
	if sig == e.appliedFocusedSig {
		return
	}
	e.appliedFocusedSig = sig

	e.Modebar.SetTemplates(
		e.optStr(fw, "modebarinner", e.Config.ModebarInner),
		e.optStr(fw, "modebardefault", e.Config.ModebarDefault),
		e.optStr(fw, "modebarouter", e.Config.ModebarOuter),
	)
	e.Modebar.SetLocation(e.optStr(fw, "modebarlocation", e.Config.ModebarLocation))
	e.applyMacOptionKeys()
	e.applyFocusedMappings(fw)
}

// applyFocusedMappings loads the mapping set the focused window resolves to
// (its "mappings" option over the base, refined by the class/type cascade) into
// the key processor, skipping the rebuild when the set name is unchanged.
func (e *Editor) applyFocusedMappings(fw *window.Window) {
	setName := e.optStr(fw, "mappings", e.Config.MappingsName)
	class, grammar, bufType := e.windowClass(fw), e.windowGrammarName(fw), e.windowType(fw)
	// The effective keymap depends on the set name and the class/grammar/type
	// refinements, so key the skip on all of them.
	mapSig := setName + "\x1f" + class + "\x1f" + grammar + "\x1f" + bufType
	if mapSig == e.appliedMappingSet {
		return
	}
	e.appliedMappingSet = mapSig
	km := e.LoadedConfig.ResolveMappingSet(setName, class, grammar, bufType, e.Config.MappingsName, e.LoadedConfig.Mappings)
	e.KeyProcessor.SetMappings(km)
}
