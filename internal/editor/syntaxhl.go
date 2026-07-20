package editor

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/jsf"
	"github.com/phroun/mew/internal/window"
)

// builtinSyntax carries mew's own MIT-licensed grammar files (and their
// LICENSE). They always resolve from here as the last-resort layer; on a real
// OS they are also dropped into ~/.mew/syntax/ for discoverability and editing.
//
//go:embed syntax
var builtinSyntax embed.FS

// installDefaultGrammars drops the embedded grammar pack into ~/.mew/syntax/ the
// first time that directory does not exist, so the shipped highlighters are
// visible and editable next to the user's own. It never clobbers an existing
// directory (the user owns it from then on), and it is a no-op when the mew tree
// is virtualized (a host owns its own layout).
func (e *Editor) installDefaultGrammars() {
	if !e.usingOSFS || e.mew == nil || e.mew.IsDir("mew:/syntax") {
		return
	}
	entries, err := builtinSyntax.ReadDir("syntax")
	if err != nil {
		return
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		data, err := builtinSyntax.ReadFile("syntax/" + ent.Name())
		if err != nil {
			continue
		}
		_ = e.mew.WriteFile("mew:/syntax/"+ent.Name(), data)
	}
}

// defaultSyntaxMap is the built-in mapping from conventional jsf color-class
// names to mew's systematic syntax* color names. [colors.syntax] and
// [colors.syntax.<grammar>] override it; classes that resolve nowhere fall
// back to the attributes in the grammar file itself. Keys are lowercase.
var defaultSyntaxMap = map[string]string{
	"comment":      "syntaxComment",
	"precomment":   "syntaxComment",
	"commentlabel": "syntaxComment",
	"string":       "syntaxString",
	"char":         "syntaxString",
	"regexp":       "syntaxString",
	"stringescape": "syntaxEscape",
	"regexpescape": "syntaxEscape",
	"escape":       "syntaxEscape",
	"number":       "syntaxConstant",
	"constant":     "syntaxConstant",
	"keyword":      "syntaxKeyword",
	"statement":    "syntaxKeyword",
	"builtin":      "syntaxKeyword",
	"operator":     "syntaxKeyword",
	"type":         "syntaxType",
	"customtype":   "syntaxType",
	"preproc":      "syntaxPreproc",
	"define":       "syntaxPreproc",
	"bad":          "syntaxBad",

	// Classes used by mew's builtin grammar pack.
	"command":   "syntaxKeyword",  // tex \commands
	"tag":       "syntaxKeyword",  // html tag names
	"attr":      "syntaxType",     // html attributes
	"var":       "syntaxEscape",   // shell/make $variables
	"decorator": "syntaxPreproc",  // python @decorators
	"macro":     "syntaxPreproc",  // rust ident! macros
	"selector":  "syntaxKeyword",  // css selectors
	"prop":      "syntaxType",     // css properties
	"atrule":    "syntaxPreproc",  // css @rules
	"key":       "syntaxType",     // conf/toml/yaml keys
	"section":   "syntaxKeyword",  // conf/toml [sections]
	"target":    "syntaxKeyword",  // make targets
	"entity":    "syntaxEscape",   // html &entities;
	"anchor":    "syntaxEscape",   // yaml &anchors/*refs
	"quote":     "syntaxEscape",   // lisp quote characters
	"code":      "syntaxString",   // markdown code spans/fences
	"bullet":    "syntaxConstant", // markdown list bullets
	"link":      "syntaxType",     // markdown [links]
	"heading":   "syntaxKeyword",  // markdown headings
	"document":  "syntaxKeyword",  // yaml --- markers
}

// synCache holds one buffer's highlight results: the grammar that applies to
// the buffer (detected or global), per-line resolved colors, and the machine
// state entering the next uncomputed line. Content edits (seen via ChangeSeq)
// drop the cache; lines re-highlight lazily as rendered.
// linkSpan is one hyperlink recognized by the buffer's grammar on a single
// line: the doc-rune range of its full source text ([[target|Title]]), and
// the parsed destination and display title. Link spans live in the syntax
// cache alongside the colors — same lifecycle, same ChangeSeq invalidation —
// so they are derived data, never stale across edits.
type linkSpan struct {
	Start, End    int // [Start, End) doc runes on the line
	Target, Title string
}

// markupKind classifies a dokuwiki inline/heading run that browse mode renders
// with its markers hidden and its text restyled.
type markupKind uint8

const (
	markupBold markupKind = iota
	markupItalic
	markupUnderline
	markupHeading
)

// markupSpan is one such run on a line: its full doc-rune source range
// (markers included), the marker length on each side, its kind, and — for a
// heading — its level 1..5 (====== is 1, == is 5). Lives in the syntax cache
// beside links and colors, same ChangeSeq lifecycle.
type markupSpan struct {
	Start, End int
	MarkLeft   int // marker runes hidden at the start
	MarkRight  int // marker runes hidden at the end
	Kind       markupKind
	Level      int
}

type synCache struct {
	seq     int64
	grammar *jsf.Instance
	// loader is the loader that produced grammar; highlighting runs through it so
	// its interned frame/state pointers stay consistent across the buffer.
	loader *jsf.Loader
	// linkable notes that the grammar recognizes hyperlinks mew can navigate
	// (currently the dokuwiki grammar); links/markup then hold per-line spans,
	// truncated/extended in lockstep with colors.
	linkable bool
	links    [][]linkSpan
	markup   [][]markupSpan
	colors   [][]string
	ctx     [][]uint8       // per-line, per-rune CtxComment/CtxString flags
	entries []jsf.LineState // entry state per computed line (for point queries)
	next    jsf.LineState
}

// joeSyntaxDirs are the installed JOE grammar collections, consulted only when
// running on a real OS (a virtualized host supplies grammars through mew:/ or
// its document FS instead).
var joeSyntaxDirs = []string{
	"/usr/share/joe/syntax",
	"/usr/local/share/joe/syntax",
}

// resolveSyntaxFile finds a grammar file by name, in order: project .mew/syntax
// directories (nearest project first, through the document FS), the user's
// mew:/syntax tree (virtualizable), mew's built-in set, then — on a real OS —
// installed JOE directories. When skipProject is set — for a flavor named in the
// syntaxOverrides option — the project layer is skipped, so the user's own copy
// (or a fallback) wins over whatever the document's project tree ships.
func (e *Editor) resolveSyntaxFile(name string, skipProject bool) ([]byte, error) {
	// A grammar name is a bare identifier, never a path.
	if strings.ContainsAny(name, "/\\") || name == "" {
		return nil, fmt.Errorf("invalid syntax name %q", name)
	}
	if !skipProject {
		pd := e.LoadedConfig.ProjectDirs
		for i := len(pd) - 1; i >= 0; i-- {
			if src, err := e.FS.ReadFile(filepath.Join(pd[i], "syntax", name+".jsf")); err == nil {
				return src, nil
			}
		}
	}
	if src, err := e.mew.ReadFile("mew:/syntax/" + name + ".jsf"); err == nil {
		return src, nil
	}
	if src, err := builtinSyntax.ReadFile("syntax/" + name + ".jsf"); err == nil {
		return src, nil
	}
	if e.usingOSFS {
		for _, dir := range joeSyntaxDirs {
			if src, err := os.ReadFile(filepath.Join(dir, name+".jsf")); err == nil {
				return src, nil
			}
		}
	}
	return nil, fmt.Errorf("no %s.jsf found", name)
}

// canonicalSyntaxName maps [formats] aliases to their grammar BEFORE loading,
// so the instance (and its color-class references, which key the
// [colors.syntax.*] maps) carries the canonical name. An actual file with the
// alias's own name — a user's or JOE's js.jsf, say — wins over the alias.
func (e *Editor) canonicalSyntaxName(name string) string {
	alias, ok := e.LoadedConfig.Formats[strings.ToLower(name)]
	if !ok || alias == "" {
		return name
	}
	if _, err := e.resolveSyntaxFile(name, false); err == nil {
		return name
	}
	return alias
}

// parseSyntaxOverrides splits a syntaxOverrides value ("go conf") into a set of
// lowercased grammar flavors. Whitespace- and comma-separated names are both
// accepted, so "go,conf" and "go conf" mean the same.
func parseSyntaxOverrides(s string) map[string]bool {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == ';'
	})
	if len(fields) == 0 {
		return nil
	}
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[strings.ToLower(f)] = true
	}
	return set
}

// loaderFor returns the loader to resolve grammar name through: the project-
// skipping loader when name is in the given syntaxOverrides set, else the normal
// one.
func (e *Editor) loaderFor(name string, overrides map[string]bool) *jsf.Loader {
	if overrides[strings.ToLower(name)] {
		return e.syntaxLoaderOverride
	}
	return e.syntaxLoader
}

// bufferSyntaxOverrides returns the effective syntaxOverrides set for buffer b:
// the per-window value from a main-buffer window showing b (already resolved
// through the option overlay into its ViewState), else the editor default. It
// reads the stored ViewState value directly (not through the overlay) so it can
// be called while the overlay is being resolved without recursing.
func (e *Editor) bufferSyntaxOverrides(b *buffer.Buffer) map[string]bool {
	raw := e.Config.SyntaxOverrides
	if b != nil && e.WindowManager != nil {
		for _, w := range e.WindowManager.AllWindows() {
			if w.Type == window.MainBuffer && w.Buffer == b {
				raw = w.ViewState.SyntaxOverrides
				break
			}
		}
	}
	return parseSyntaxOverrides(raw)
}

// initSyntax prepares the highlighter and loads the configured grammar, if
// any. Load problems surface as a transient error and leave highlighting off.
func (e *Editor) initSyntax() {
	e.synCaches = make(map[*buffer.Buffer]*synCache)
	e.synSGR = make(map[*jsf.ColorRef]string)
	e.syntaxLoader = jsf.NewLoader(func(name string) ([]byte, error) {
		return e.resolveSyntaxFile(name, false)
	})
	e.syntaxLoaderOverride = jsf.NewLoader(func(name string) ([]byte, error) {
		return e.resolveSyntaxFile(name, true)
	})
	e.reloadGlobalGrammar()
}

// reloadGlobalGrammar (re)loads the global fallback grammar from Config.Syntax
// through the editor-wide syntaxOverrides, recording the loader that produced it.
// Load problems surface as a transient error and leave highlighting off.
func (e *Editor) reloadGlobalGrammar() {
	e.syntaxGrammar = nil
	e.syntaxGrammarLoader = nil
	if e.Config.Syntax == "" {
		return
	}
	name := e.canonicalSyntaxName(e.Config.Syntax)
	ld := e.loaderFor(name, parseSyntaxOverrides(e.Config.SyntaxOverrides))
	in, err := ld.Load(name)
	if err != nil {
		e.ShowError("Syntax: " + err.Error())
		e.Config.Syntax = ""
		return
	}
	e.Config.Syntax = name
	e.syntaxGrammar = in
	e.syntaxGrammarLoader = ld
}

// setSyntax switches the active grammar at runtime ("" or "none" disables).
func (e *Editor) setSyntax(name string) bool {
	if strings.EqualFold(name, "none") {
		name = ""
	}
	if name == "" {
		e.Config.Syntax = ""
		e.syntaxGrammar = nil
		e.syntaxGrammarLoader = nil
		e.resetSyntaxCaches()
		return true
	}
	name = e.canonicalSyntaxName(name)
	ld := e.loaderFor(name, parseSyntaxOverrides(e.Config.SyntaxOverrides))
	in, err := ld.Load(name)
	if err != nil {
		e.ShowError("Syntax: " + err.Error())
		return false
	}
	e.Config.Syntax = name
	e.syntaxGrammar = in
	e.syntaxGrammarLoader = ld
	e.resetSyntaxCaches()
	return true
}

func (e *Editor) resetSyntaxCaches() {
	e.synCaches = make(map[*buffer.Buffer]*synCache)
	e.synSGR = make(map[*jsf.ColorRef]string)
}

// syntaxColorFor resolves a grammar color class to the SGR string painted for
// it, through the mapping chain: the per-grammar [colors.syntax.<name>] map,
// the global [colors.syntax] map, then the built-in conventions — each naming
// a mew color resolved through the color scheme. When no mapping resolves,
// the grammar file's own "=Class attrs" colors apply.
func (e *Editor) syntaxColorFor(ref *jsf.ColorRef) string {
	if ref == nil {
		return ""
	}
	if sgr, ok := e.synSGR[ref]; ok {
		return sgr
	}
	sgr := e.resolveSyntaxColor(ref)
	e.synSGR[ref] = sgr
	return sgr
}

func (e *Editor) resolveSyntaxColor(ref *jsf.ColorRef) string {
	class := strings.ToLower(ref.Class)
	lookup := func(name string) (string, bool) {
		if m := e.LoadedConfig.SyntaxMaps[strings.ToLower(ref.Syntax)]; m != nil {
			if v, ok := m[name]; ok {
				return v, true
			}
		}
		if m := e.LoadedConfig.SyntaxMaps[""]; m != nil {
			if v, ok := m[name]; ok {
				return v, true
			}
		}
		if v, ok := defaultSyntaxMap[name]; ok {
			return v, true
		}
		return "", false
	}
	if mewName, ok := lookup(class); ok && mewName != "" {
		if sgr := e.LoadedConfig.Colors.Resolve("", "main", mewName); sgr != "" {
			return sgr
		}
	}
	return ref.SGR
}

// shebangName extracts the interpreter name from a "#!" first line: the
// command's basename, following /usr/bin/env (skipping its flags and
// VAR=value assignments), with any trailing version stripped so python3 and
// lua5.4 detect as python and lua. Returns "" when the line is no shebang.
func shebangName(line string) string {
	if !strings.HasPrefix(line, "#!") {
		return ""
	}
	fields := strings.Fields(line[2:])
	if len(fields) == 0 {
		return ""
	}
	name := filepath.Base(fields[0])
	if name == "env" {
		name = ""
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") || strings.Contains(f, "=") {
				continue
			}
			name = filepath.Base(f)
			break
		}
		if name == "" {
			return ""
		}
	}
	return strings.ToLower(strings.TrimRight(name, "0123456789."))
}

// detectName resolves a detected short name (interpreter or extension)
// through the [formats] table and the grammar search path, returning the
// loaded grammar and the loader that produced it (or nil, nil). overrides is
// the effective syntaxOverrides set: a flavor listed there resolves through the
// project-skipping loader instead.
func (e *Editor) detectName(name string, overrides map[string]bool) (*jsf.Instance, *jsf.Loader) {
	if name == "" {
		return nil, nil
	}
	canon := e.canonicalSyntaxName(name)
	ld := e.loaderFor(canon, overrides)
	in, err := ld.Load(canon)
	if err != nil {
		return nil, nil
	}
	return in, ld
}

// vimModelineRe matches vim/vi/ex modelines: "vim: set ft=python:" or
// "vim:ft=python" (vim honors them in the first and last five lines).
var vimModelineRe = regexp.MustCompile(`(?:^|\s)(?:vim?|ex):[^\n]*?(?:ft|filetype)[ \t]*=[ \t]*([A-Za-z0-9_+-]+)`)

// emacsModelineRe matches emacs file-variable lines: "-*- mode: python -*-"
// or the short "-*- python -*-" (emacs honors line 1, or line 2 after a
// shebang).
var emacsModelineRe = regexp.MustCompile(`-\*-\s*(?:[Mm]ode:\s*)?([A-Za-z0-9_+-]+).*?-\*-`)

// modelineName extracts a format name from a vim or emacs modeline, or "".
func modelineName(line string) string {
	if m := vimModelineRe.FindStringSubmatch(line); m != nil {
		return strings.ToLower(m[1])
	}
	if m := emacsModelineRe.FindStringSubmatch(line); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// modelineScan checks the lines vim and emacs honor: the first five and the
// last five of the buffer.
func (e *Editor) modelineScan(b *buffer.Buffer) string {
	n := b.GetLineCount()
	seen := map[int]bool{}
	scan := func(i int) string {
		if i < 0 || i >= n || seen[i] {
			return ""
		}
		seen[i] = true
		return modelineName(strings.TrimRight(b.GetLine(i), "\n\r"))
	}
	for i := 0; i < 5; i++ {
		if name := scan(i); name != "" {
			return name
		}
	}
	for i := n - 5; i < n; i++ {
		if name := scan(i); name != "" {
			return name
		}
	}
	return ""
}

// bufferGrammar picks the grammar for one buffer, returning it alongside the
// loader that produced it (nil, nil when none applies). With syntaxDetect on, a
// first-line shebang wins, then a vim/emacs modeline, then the filename's
// extension (or extensionless basename, e.g. Makefile) — all resolved
// through [formats]; a buffer that detects nothing falls back to the global
// syntax option's grammar. Flavors named in the buffer's effective
// syntaxOverrides resolve with the project .mew/syntax layer skipped.
func (e *Editor) bufferGrammar(b *buffer.Buffer) (*jsf.Instance, *jsf.Loader) {
	if !e.Config.SyntaxDetect {
		return e.syntaxGrammar, e.syntaxGrammarLoader
	}
	overrides := e.bufferSyntaxOverrides(b)
	if b.GetLineCount() > 0 {
		first := strings.TrimRight(b.GetLine(0), "\n\r")
		if in, ld := e.detectName(shebangName(first), overrides); in != nil {
			return in, ld
		}
	}
	if in, ld := e.detectName(e.modelineScan(b), overrides); in != nil {
		return in, ld
	}
	if fn := b.GetFilename(); fn != "" {
		base := strings.ToLower(filepath.Base(fn))
		name := strings.TrimPrefix(filepath.Ext(base), ".")
		if name == "" {
			name = strings.TrimPrefix(base, ".") // Makefile, .emacs, ...
		}
		// Path-conditional rules first ([formats.<ext>]): the same
		// extension can mean different formats in different trees — a
		// .txt under a wiki highlights as dokuwiki. The path is resolved
		// absolute, so a relative filename tests the working directory too.
		if rules := e.LoadedConfig.FormatPaths[name]; len(rules) != 0 {
			abs := fn
			if a, err := filepath.Abs(fn); err == nil {
				abs = a
			}
			pats := make([]string, 0, len(rules))
			for p := range rules {
				pats = append(pats, p)
			}
			sort.Strings(pats)
			for _, p := range pats {
				if rules[p] != "" && config.PathMatches(p, abs) {
					if in, ld := e.detectName(rules[p], overrides); in != nil {
						return in, ld
					}
				}
			}
		}
		if in, ld := e.detectName(name, overrides); in != nil {
			return in, ld
		}
	}
	return e.syntaxGrammar, e.syntaxGrammarLoader
}

// ensureSynCache returns the buffer's highlight cache extended through
// docLine, or nil when highlighting does not apply to the buffer. The cache
// holds per-line resolved colors, context flags, and entry states; each
// line's entry state is the exit state of the line before.
func (e *Editor) ensureSynCache(b *buffer.Buffer, docLine int) *synCache {
	if b == nil || (e.syntaxGrammar == nil && !e.Config.SyntaxDetect) {
		return nil
	}
	if docLine < 0 || docLine >= b.GetLineCount() {
		return nil
	}
	c := e.synCaches[b]
	switch {
	case c == nil:
		e.pruneSyntaxCaches()
		b.TakeDirtyLow() // consume any pre-cache history
		g, ld := e.bufferGrammar(b)
		c = &synCache{seq: b.ChangeSeq(), grammar: g, loader: ld, linkable: grammarLinkable(g)}
		e.synCaches[b] = c
	case c.seq != b.ChangeSeq():
		// Content changed: the dirty watermark says where the damage starts.
		// Lines below it still hold exactly what they held, so truncate the
		// cache there instead of rebuilding — per-keystroke cost becomes
		// O(viewport), not O(prefix). Damage reaching line 0 can change
		// shebang detection, so that (and a grammarless cache) rebuilds.
		low := b.TakeDirtyLow()
		if low <= 0 || c.grammar == nil {
			g, ld := e.bufferGrammar(b)
			c = &synCache{seq: b.ChangeSeq(), grammar: g, loader: ld, linkable: grammarLinkable(g)}
			e.synCaches[b] = c
		} else {
			if low < len(c.colors) {
				c.next = c.entries[low]
				c.colors = c.colors[:low]
				c.ctx = c.ctx[:low]
				c.entries = c.entries[:low]
				if c.linkable {
					if low < len(c.links) {
						c.links = c.links[:low]
					}
					if low < len(c.markup) {
						c.markup = c.markup[:low]
					}
				}
			}
			c.seq = b.ChangeSeq()
		}
	}
	if c.grammar == nil {
		return nil
	}
	// Read the whole uncomputed span [start, docLine] in one sequential pass.
	// Per-line GetLine re-seeks to an absolute line (O(line)), so tokenizing a
	// large prefix that way is O(n²) — the dominant cost on big files. A single
	// GetLineRange is O(span).
	if start := len(c.colors); start <= docLine {
		lines := b.GetLineRange(start, docLine+1)
		for _, raw := range lines {
			line := strings.TrimRight(raw, "\n\r")
			lineRunes := []rune(line)
			c.entries = append(c.entries, c.next)
			attrs, ctx, next := c.loader.HighlightLineFull(c.grammar, c.next, lineRunes)
			colors := make([]string, len(attrs))
			for j, ref := range attrs {
				colors[j] = e.syntaxColorFor(ref)
			}
			c.colors = append(c.colors, colors)
			c.ctx = append(c.ctx, ctx)
			if c.linkable {
				c.links = append(c.links, extractLinkSpans(lineRunes, attrs))
				c.markup = append(c.markup, extractMarkupSpans(lineRunes, attrs))
			}
			c.next = next
		}
	}
	return c
}

// grammarLinkable reports whether a grammar's Link-class runs are hyperlinks
// mew understands. Currently the dokuwiki grammar (which also covers plain
// .txt files that the path-conditional [formats] rules route to it).
func grammarLinkable(g *jsf.Instance) bool {
	return g != nil && strings.EqualFold(g.Name, "dokuwiki")
}

// extractLinkSpans finds the runs the grammar colored with the "Link" class
// and splits each into individual [[target|Title]] links. The dokuwiki grammar
// colors the whole source — brackets included — as Link, and two adjacent
// links (...]][[...) form one continuous Link run with no gap between them, so
// a run is parsed into separate "[[" ... "]]" segments rather than taken whole.
// A link never crosses a line (an unclosed [[ resets at the newline).
func extractLinkSpans(runes []rune, attrs []*jsf.ColorRef) []linkSpan {
	var spans []linkSpan
	n := len(attrs)
	if n > len(runes) {
		n = len(runes)
	}
	isLink := func(i int) bool {
		return i < n && attrs[i] != nil && strings.EqualFold(attrs[i].Class, "Link")
	}
	for i := 0; i < n; {
		if !isLink(i) {
			i++
			continue
		}
		runStart := i
		for isLink(i) {
			i++
		}
		runEnd := i
		// Split the Link run into individual links on the "[[ ... ]]" pattern.
		for j := runStart; j < runEnd; {
			// Advance to the next "[[".
			for j < runEnd && !(runes[j] == '[' && j+1 < runEnd && runes[j+1] == '[') {
				j++
			}
			if j >= runEnd {
				break
			}
			ls := j
			// Find the closing "]]" (dokuwiki titles never contain "]]").
			le := runEnd
			for k := ls + 2; k+1 < runEnd; k++ {
				if runes[k] == ']' && runes[k+1] == ']' {
					le = k + 2
					break
				}
			}
			target, title := parseDokuLink(string(runes[ls:le]))
			spans = append(spans, linkSpan{Start: ls, End: le, Target: target, Title: title})
			j = le
		}
	}
	return spans
}

// extractMarkupSpans finds the Bold/Italic/Underline/Heading runs the grammar
// colored on a line and records each with its marker widths so browse mode can
// hide the markers and restyle the text. Inline markers are the doubled
// **, //, __ (2 runes each side). A Heading run is ...====== text ======...:
// the leading/trailing "=" groups (with one adjacent space) are the markers,
// and the leading "=" count gives the level (6→1 ... 2→5).
func extractMarkupSpans(runes []rune, attrs []*jsf.ColorRef) []markupSpan {
	class := func(i int) string {
		if i < len(attrs) && i < len(runes) && attrs[i] != nil {
			return attrs[i].Class
		}
		return ""
	}
	n := len(runes)
	if len(attrs) < n {
		n = len(attrs)
	}
	var spans []markupSpan
	for i := 0; i < n; {
		cl := class(i)
		var kind markupKind
		switch {
		case strings.EqualFold(cl, "Bold"):
			kind = markupBold
		case strings.EqualFold(cl, "Italic"):
			kind = markupItalic
		case strings.EqualFold(cl, "Underline"):
			kind = markupUnderline
		case strings.EqualFold(cl, "Heading"):
			kind = markupHeading
		default:
			i++
			continue
		}
		start := i
		for i < n && strings.EqualFold(class(i), cl) {
			i++
		}
		end := i
		s := markupSpan{Start: start, End: end, Kind: kind}
		if kind == markupHeading {
			// Count leading '=' (and one following space), trailing '=' (and one
			// preceding space).
			l := 0
			for start+l < end && runes[start+l] == '=' {
				l++
			}
			r := 0
			for end-1-r >= start && runes[end-1-r] == '=' {
				r++
			}
			s.Level = 7 - l
			if s.Level < 1 {
				s.Level = 1
			}
			if s.Level > 5 {
				s.Level = 5
			}
			if start+l < end && runes[start+l] == ' ' {
				l++
			}
			if end-1-r >= start && runes[end-1-r] == ' ' {
				r++
			}
			s.MarkLeft, s.MarkRight = l, r
		} else {
			// Doubled inline markers, but only when both sides are present and
			// there is content between them.
			if end-start >= 5 {
				s.MarkLeft, s.MarkRight = 2, 2
			}
		}
		// Guard against a malformed run with no content left after the markers.
		if s.MarkLeft+s.MarkRight >= end-start {
			s.MarkLeft, s.MarkRight = 0, 0
		}
		spans = append(spans, s)
	}
	return spans
}

// parseDokuLink splits a raw [[target|Title]] source into its destination and
// display title: the part before the first "|" is the target, the part after
// it the title, defaulting to the target when absent ([[target]]). Lenient —
// missing brackets leave the text as both.
func parseDokuLink(text string) (target, title string) {
	inner := strings.TrimSuffix(strings.TrimPrefix(text, "[["), "]]")
	if t, rest, ok := strings.Cut(inner, "|"); ok {
		target, title = strings.TrimSpace(t), strings.TrimSpace(rest)
	} else {
		target = strings.TrimSpace(inner)
	}
	if title == "" {
		title = target
	}
	if title == "" {
		title = text
	}
	return target, title
}

// syntaxLineColors returns per-rune SGR colors for one document line of w
// ("" entries paint in the normal text color), or nil when highlighting does
// not apply. This is the renderer's syntax colorizer callback.
func (e *Editor) syntaxLineColors(w *window.Window, docLine int) []string {
	if w == nil || w.Buffer == nil || w.Type != window.MainBuffer {
		return nil
	}
	c := e.ensureSynCache(w.Buffer, docLine)
	if c == nil {
		return nil
	}
	// Caret mode (browse off): links paint in the "link" color over their
	// syntax colors, marking them as followable. Copy-on-write — the cached
	// slice is shared. Browse mode replaces link text with buttons instead,
	// so the overlay is skipped there — and linkBrowsing=no disables the
	// whole layer (links render exactly as the grammar colors them).
	if c.linkable && w.ViewState.LinkBrowsing && !w.BrowseActive && docLine < len(c.links) && len(c.links[docLine]) > 0 {
		linkSGR := e.LoadedConfig.Colors.Resolve(w.Class, w.Type.Name(), "link")
		recentSGR := e.LoadedConfig.Colors.Resolve(w.Class, w.Type.Name(), "linkRecent")
		if linkSGR != "" || recentSGR != "" {
			colors := append([]string(nil), c.colors[docLine]...)
			for _, s := range c.links[docLine] {
				sgr := linkSGR
				if recentSGR != "" && w.Buffer.LinkVisited(s.Target) {
					sgr = recentSGR // a visited link paints in the recent color
				}
				if sgr == "" {
					continue
				}
				for i := s.Start; i < s.End && i < len(colors); i++ {
					colors[i] = sgr
				}
			}
			return colors
		}
	}
	return c.colors[docLine]
}

// syntaxCtxLine returns the context flags (CtxComment/CtxString per rune) for
// one document line, or nil when no grammar applies to the buffer.
func (e *Editor) syntaxCtxLine(b *buffer.Buffer, docLine int) []uint8 {
	c := e.ensureSynCache(b, docLine)
	if c == nil {
		return nil
	}
	return c.ctx[docLine]
}

// syntaxContextAt reports the highlighter's full context at a position, and
// whether a grammar applies to the buffer at all.
func (e *Editor) syntaxContextAt(b *buffer.Buffer, docLine, runePos int) (jsf.Context, bool) {
	c := e.ensureSynCache(b, docLine)
	if c == nil {
		return jsf.Context{}, false
	}
	line := strings.TrimRight(b.GetLine(docLine), "\n\r")
	return c.loader.ContextAt(c.grammar, c.entries[docLine], []rune(line), runePos), true
}

// pruneSyntaxCaches drops cache entries for buffers no longer shown in any
// window, so closed buffers do not pin their highlight state.
func (e *Editor) pruneSyntaxCaches() {
	if len(e.synCaches) < 8 {
		return
	}
	live := make(map[*buffer.Buffer]bool)
	for _, w := range e.WindowManager.AllWindows() {
		if w.Buffer != nil {
			live[w.Buffer] = true
		}
	}
	for b := range e.synCaches {
		if !live[b] {
			delete(e.synCaches, b)
		}
	}
}
