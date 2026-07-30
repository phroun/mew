package editor

import (
	"strconv"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/viewport"
)

// viewportClass / viewportType / viewportGrammarName expose a viewport's three overlay
// dimensions. A viewport's syntax is its buffer's syntax, so every option can be
// resolved for a viewport through the class/grammar/type cascade.
// The class of a viewport running a terminal is "pty", DERIVED rather than
// stored: a buffer becomes a terminal and stops being one again without the
// viewport ever changing, and the same viewport shows an ordinary document
// either side of that. An explicit Class still wins — a prompt that happens to
// display a terminal buffer is still that prompt.
func (e *Editor) viewportClass(w *viewport.Viewport) string {
	if w == nil {
		return ""
	}
	if w.Class == "" && e.ptySessionFor(w.Buffer) != nil {
		return ptyViewportClass
	}
	return w.Class
}

func (e *Editor) viewportType(w *viewport.Viewport) string {
	if w == nil {
		return ""
	}
	return w.Type.Name()
}

func (e *Editor) viewportGrammarName(w *viewport.Viewport) string {
	if w == nil {
		return ""
	}
	return e.bufferGrammarName(w.Buffer)
}

// optSig is the composite overlay signature (class, grammar, type) for a
// viewport — used to tell when a viewport's resolved options need re-deriving.
func (e *Editor) optSig(w *viewport.Viewport) string {
	return e.viewportClass(w) + "\x1f" + e.viewportGrammarName(w) + "\x1f" + e.viewportType(w)
}

// resolveOpt returns the raw overlaid value for key and whether the cascade
// supplied it (else the caller's base applies).
func (e *Editor) resolveOpt(w *viewport.Viewport, key string) (string, bool) {
	return e.LoadedConfig.ResolveOptionOverlay(e.viewportClass(w), e.viewportGrammarName(w), e.viewportType(w), key)
}

// optBool / optInt / optStr / optDir resolve a typed option for a viewport: the
// class/grammar/type overlay if present, else the given base (the editor-wide
// value).
func (e *Editor) optBool(w *viewport.Viewport, key string, base bool) bool {
	if raw, ok := e.resolveOpt(w, key); ok {
		if b, ok := parseBoolOption(raw); ok {
			return b
		}
	}
	return base
}

func (e *Editor) optInt(w *viewport.Viewport, key string, base, min int) int {
	if raw, ok := e.resolveOpt(w, key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n >= min {
			return n
		}
	}
	return base
}

func (e *Editor) optStr(w *viewport.Viewport, key, base string) string {
	if raw, ok := e.resolveOpt(w, key); ok {
		return raw
	}
	return base
}

// optMarks resolves the showMarks enum for a viewport: the class/grammar/type
// overlay if present and valid, else the given base, normalized to no/yes/all
// (config.ParseShowMarks, which also accepts boolean aliases).
func (e *Editor) optMarks(w *viewport.Viewport, base string) string {
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

func (e *Editor) optDir(w *viewport.Viewport, key, base string) string {
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

// reconcileGrammarOptions re-derives a main-buffer viewport's per-viewport options
// from the base [options] overlaid by the class/grammar/type cascade whenever
// the viewport's overlay signature changes. Options the user set explicitly
// (marked overridden) are left untouched. It runs each frame but only does work
// on an actual change, so a plain viewport (empty signature, no overlays) is never
// touched.
func (e *Editor) reconcileGrammarOptions(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil || w.Type == viewport.PromptViewport {
		return
	}
	// Per-viewport syntax first (grammar-agnostic: class + type, never grammar,
	// which would be circular). Set before anything reads the viewport's
	// grammar, so viewportGrammarName below reflects it.
	e.reconcileViewportSyntax(w)
	class, grammar, bufType := e.viewportClass(w), e.viewportGrammarName(w), e.viewportType(w)
	newSig := class + "\x1f" + grammar + "\x1f" + bufType
	oldSig := w.AppliedOptionSig()
	if newSig == oldSig {
		return
	}
	w.SetAppliedOptionSig(newSig)

	// Only rewrite ViewState when an overlay applies now, or applied before (so
	// a removed overlay reverts to base). A plain viewport — no overlay either way
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

	// Re-derive every per-viewport option the user has not pinned. The resolution
	// rule for each lives in applyResolvedOption, shared with clear_option.
	for _, key := range perViewportOptionKeys {
		if !w.IsOptionOverridden(key) {
			e.applyResolvedOption(w, key)
		}
	}
}

// reconcileViewportSyntax resolves the viewport's default grammar from a
// grammar-agnostic overlay ([options.<type>] / [<class>.options] syntax=...)
// and stores it in ViewState.Syntax. On a change it drops the buffer's
// highlight cache so the new grammar takes effect. "" inherits the global
// syntax option.
func (e *Editor) reconcileViewportSyntax(w *viewport.Viewport) {
	name := ""
	if v, ok := e.LoadedConfig.ResolveOptionOverlay(e.viewportClass(w), "", e.viewportType(w), "syntax"); ok {
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
// mapping set — resolved through the focused viewport's class/grammar/type
// overlay, whenever that viewport's signature changes. These are editor-wide in
// effect (one modebar, one key processor), so they follow the focused viewport.
func (e *Editor) reconcileFocusedOptions() {
	fw := e.ViewportManager.GetFocusedViewport()
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

// applyFocusedMappings loads the mapping set the focused viewport resolves to
// (its "mappings" option over the base, refined by the class/type cascade) into
// the key processor, skipping the rebuild when the set name is unchanged.
func (e *Editor) applyFocusedMappings(fw *viewport.Viewport) {
	setName := e.optStr(fw, "mappings", e.Config.MappingsName)
	class, grammar, bufType := e.viewportClass(fw), e.viewportGrammarName(fw), e.viewportType(fw)
	// The effective keymap depends on the set name and the class/grammar/type
	// refinements, so key the skip on all of them.
	mapSig := setName + "\x1f" + class + "\x1f" + grammar + "\x1f" + bufType
	if mapSig == e.appliedMappingSet {
		return
	}
	e.appliedMappingSet = mapSig
	km, origins := e.LoadedConfig.ResolveMappingSet(setName, class, grammar, bufType, e.Config.MappingsName, e.LoadedConfig.Mappings)
	e.KeyProcessor.SetMappings(km)
	e.mappingOrigins = origins
	e.remapPrec = maxPrecedence(origins)
}
