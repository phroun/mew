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

// builtinSyntax carries mew's own MIT-licensed grammar files.
//
//go:embed syntax/*.jsf
var builtinSyntax embed.FS

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
type synCache struct {
	seq     int64
	grammar *jsf.Instance
	colors  [][]string
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
// installed JOE directories.
func (e *Editor) resolveSyntaxFile(name string) ([]byte, error) {
	// A grammar name is a bare identifier, never a path.
	if strings.ContainsAny(name, "/\\") || name == "" {
		return nil, fmt.Errorf("invalid syntax name %q", name)
	}
	pd := e.LoadedConfig.ProjectDirs
	for i := len(pd) - 1; i >= 0; i-- {
		if src, err := e.FS.ReadFile(filepath.Join(pd[i], "syntax", name+".jsf")); err == nil {
			return src, nil
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
	if _, err := e.resolveSyntaxFile(name); err == nil {
		return name
	}
	return alias
}

// initSyntax prepares the highlighter and loads the configured grammar, if
// any. Load problems surface as a transient error and leave highlighting off.
func (e *Editor) initSyntax() {
	e.synCaches = make(map[*buffer.Buffer]*synCache)
	e.synSGR = make(map[*jsf.ColorRef]string)
	e.syntaxLoader = jsf.NewLoader(e.resolveSyntaxFile)
	if e.Config.Syntax == "" {
		return
	}
	name := e.canonicalSyntaxName(e.Config.Syntax)
	in, err := e.syntaxLoader.Load(name)
	if err != nil {
		e.ShowError("Syntax: " + err.Error())
		e.Config.Syntax = ""
		return
	}
	e.Config.Syntax = name
	e.syntaxGrammar = in
}

// setSyntax switches the active grammar at runtime ("" or "none" disables).
func (e *Editor) setSyntax(name string) bool {
	if strings.EqualFold(name, "none") {
		name = ""
	}
	if name == "" {
		e.Config.Syntax = ""
		e.syntaxGrammar = nil
		e.resetSyntaxCaches()
		return true
	}
	name = e.canonicalSyntaxName(name)
	in, err := e.syntaxLoader.Load(name)
	if err != nil {
		e.ShowError("Syntax: " + err.Error())
		return false
	}
	e.Config.Syntax = name
	e.syntaxGrammar = in
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
// loaded grammar or nil.
func (e *Editor) detectName(name string) *jsf.Instance {
	if name == "" {
		return nil
	}
	in, err := e.syntaxLoader.Load(e.canonicalSyntaxName(name))
	if err != nil {
		return nil
	}
	return in
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

// bufferGrammar picks the grammar for one buffer. With syntaxDetect on, a
// first-line shebang wins, then a vim/emacs modeline, then the filename's
// extension (or extensionless basename, e.g. Makefile) — all resolved
// through [formats]; a buffer that detects nothing falls back to the global
// syntax option's grammar.
func (e *Editor) bufferGrammar(b *buffer.Buffer) *jsf.Instance {
	if !e.Config.SyntaxDetect {
		return e.syntaxGrammar
	}
	if b.GetLineCount() > 0 {
		first := strings.TrimRight(b.GetLine(0), "\n\r")
		if in := e.detectName(shebangName(first)); in != nil {
			return in
		}
	}
	if in := e.detectName(e.modelineScan(b)); in != nil {
		return in
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
					if in := e.detectName(rules[p]); in != nil {
						return in
					}
				}
			}
		}
		if in := e.detectName(name); in != nil {
			return in
		}
	}
	return e.syntaxGrammar
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
		c = &synCache{seq: b.ChangeSeq(), grammar: e.bufferGrammar(b)}
		e.synCaches[b] = c
	case c.seq != b.ChangeSeq():
		// Content changed: the dirty watermark says where the damage starts.
		// Lines below it still hold exactly what they held, so truncate the
		// cache there instead of rebuilding — per-keystroke cost becomes
		// O(viewport), not O(prefix). Damage reaching line 0 can change
		// shebang detection, so that (and a grammarless cache) rebuilds.
		low := b.TakeDirtyLow()
		if low <= 0 || c.grammar == nil {
			c = &synCache{seq: b.ChangeSeq(), grammar: e.bufferGrammar(b)}
			e.synCaches[b] = c
		} else {
			if low < len(c.colors) {
				c.next = c.entries[low]
				c.colors = c.colors[:low]
				c.ctx = c.ctx[:low]
				c.entries = c.entries[:low]
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
			c.entries = append(c.entries, c.next)
			attrs, ctx, next := e.syntaxLoader.HighlightLineFull(c.grammar, c.next, []rune(line))
			colors := make([]string, len(attrs))
			for j, ref := range attrs {
				colors[j] = e.syntaxColorFor(ref)
			}
			c.colors = append(c.colors, colors)
			c.ctx = append(c.ctx, ctx)
			c.next = next
		}
	}
	return c
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
	return e.syntaxLoader.ContextAt(c.grammar, c.entries[docLine], []rune(line), runePos), true
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
