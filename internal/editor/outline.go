package editor

import (
	"regexp"
	"sort"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// outlineWalkLimit bounds how far back the breadcrumb builder scans.
const outlineWalkLimit = 5000

// outlineStoplist rejects control-flow keywords that loose heuristic
// patterns (the C/Java method forms) can capture as a "name".
var outlineStoplist = map[string]bool{
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "catch": true, "return": true, "foreach": true,
	"using": true, "lock": true, "new": true, "defer": true, "select": true,
}

// outlineDef is one definition found on a line: its name and nesting depth.
type outlineDef struct {
	name  string
	depth int
}

// outlineMemo caches the last computed breadcrumb (per render/caret line).
type outlineMemo struct {
	buf  *buffer.Buffer
	seq  int64
	line int
	out  string
}

// outlinePatterns returns the compiled [outline.<grammar>] patterns in a
// deterministic order, memoized per pattern string (bad patterns drop out).
func (e *Editor) outlinePatterns(grammar string) []*regexp.Regexp {
	specs := e.LoadedConfig.Outline[grammar]
	if len(specs) == 0 {
		return nil
	}
	if e.outlineREs == nil {
		e.outlineREs = make(map[string]*regexp.Regexp)
	}
	kinds := make([]string, 0, len(specs))
	for k := range specs {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	out := make([]*regexp.Regexp, 0, len(specs))
	for _, k := range kinds {
		pat := specs[k]
		re, seen := e.outlineREs[pat]
		if !seen {
			re, _ = regexp.Compile(pat)
			e.outlineREs[pat] = re
		}
		if re != nil {
			out = append(out, re)
		}
	}
	return out
}

// indentWidth measures leading whitespace (tabs at the configured width).
func (e *Editor) indentWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += e.Config.TabSize
		default:
			return w
		}
	}
	return w
}

// depthPrefix reports whether a first capture group is a depth prefix, and
// its depth if so. Whitespace and '#' runs count their width (markdown-style:
// more '#' is deeper); a pure '=' run counts INVERTED (dokuwiki-style:
// ====== is the top level, == the deepest).
func (e *Editor) depthPrefix(s string) (int, bool) {
	w, eq, other := 0, 0, 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += e.Config.TabSize
		case '#':
			w++
		case '=':
			eq++
		default:
			other++
		}
	}
	if other > 0 {
		return 0, false
	}
	if eq > 0 {
		if w > 0 {
			return 0, false // mixed prefix: not a depth marker
		}
		d := 32 - eq
		if d < 1 {
			d = 1
		}
		return d, true
	}
	return w, true
}

// outlineDefOn matches one line's text against the grammar's patterns and
// returns the definition it declares, if any. Matches whose name lies in
// comment/string context are ignored (a commented-out def is no def). The
// caller passes the (terminator-trimmed) line text so a whole backward scan
// can be read sequentially rather than one O(line) GetLine at a time.
func (e *Editor) outlineDefOn(b *buffer.Buffer, patterns []*regexp.Regexp, docLine int, text string) (outlineDef, bool) {
	for _, re := range patterns {
		idx := re.FindStringSubmatchIndex(text)
		if idx == nil {
			continue
		}
		// The name is the last non-empty capture group.
		nameStart, nameEnd := -1, -1
		for g := len(idx)/2 - 1; g >= 1; g-- {
			if idx[2*g] >= 0 && idx[2*g+1] > idx[2*g] {
				nameStart, nameEnd = idx[2*g], idx[2*g+1]
				break
			}
		}
		if nameStart < 0 {
			continue
		}
		name := text[nameStart:nameEnd]
		if outlineStoplist[name] {
			continue
		}
		// Skip matches inside comments/strings (byte -> rune index first).
		if ctx := e.syntaxCtxLine(b, docLine); ctx != nil {
			r := len([]rune(text[:nameStart]))
			if r < len(ctx) && ctx[r] != 0 {
				continue
			}
		}
		// Depth: a whitespace/'#' first group, else the line's indentation.
		depth := e.indentWidth(text)
		if len(idx) >= 6 && idx[2] >= 0 {
			if w, ok := e.depthPrefix(text[idx[2]:idx[3]]); ok {
				depth = w
			}
		}
		return outlineDef{name: name, depth: depth}, true
	}
	return outlineDef{}, false
}

// outlineContext builds the enclosing-definition breadcrumb for the window's
// caret. The nearest definition at or above the caret is always the chain's
// innermost entry (sectioning formats — markdown headings, conf [sections] —
// have flat bodies, so no indentation test can apply; for indented languages
// this matches the classic which-function fallback). The chain then extends
// outward through definitions at strictly shallower depths, outermost first:
// "Manager·Load", "Intro·Setup·Deep".
func (e *Editor) outlineContext(w *window.Window) string {
	if w == nil || w.Buffer == nil || w.Type == window.PromptWindow {
		return ""
	}
	b := w.Buffer
	caret := w.CursorPos().Line
	if m := e.outlineMemoVal; m != nil && m.buf == b && m.seq == b.ChangeSeq() && m.line == caret {
		return m.out
	}

	out := ""
	if c := e.ensureSynCache(b, caret); c != nil {
		if patterns := e.outlinePatterns(c.grammar.Name); patterns != nil {
			out = e.buildOutline(b, patterns, caret)
		}
	}
	e.outlineMemoVal = &outlineMemo{buf: b, seq: b.ChangeSeq(), line: caret, out: out}
	return out
}

func (e *Editor) buildOutline(b *buffer.Buffer, patterns []*regexp.Regexp, caret int) string {
	var chain []string
	refDepth := 0
	low := caret - outlineWalkLimit
	if low < 0 {
		low = 0
	}
	// Scan backward in sequential chunks. The enclosing def is usually just
	// above the caret, so this reads one block (O(block)) and stops — rather
	// than an O(line) GetLine per line, which is O(caret²) deep in a big file.
	const chunk = 256
	hi := caret + 1 // exclusive top of the loaded block
	chunkLo := hi - chunk
	if chunkLo < low {
		chunkLo = low
	}
	lines := b.GetLineRange(chunkLo, hi)
	for ln := caret; ln >= low; ln-- {
		if ln < chunkLo {
			hi = chunkLo
			chunkLo = hi - chunk
			if chunkLo < low {
				chunkLo = low
			}
			lines = b.GetLineRange(chunkLo, hi)
		}
		text := strings.TrimRight(lines[ln-chunkLo], "\n\r")
		def, ok := e.outlineDefOn(b, patterns, ln, text)
		if !ok {
			continue
		}
		if len(chain) == 0 || def.depth < refDepth {
			chain = append(chain, def.name)
			refDepth = def.depth
			if def.depth == 0 || len(chain) >= 6 {
				break
			}
		}
	}
	// Outermost first.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return strings.Join(chain, "·")
}
