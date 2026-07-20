package editor

import (
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/phroun/mew/internal/window"
)

// DokuWiki-compatible reference resolution — the implementation of
// docs/dokuwiki-ids.md. Three layers, resolved outermost-first, kept as three
// functions exactly as the spec requires:
//
//	1. schemeRef / interwikiRef — mew's scheme/interwiki gate (Layer 1)
//	2. resolveWikiRef           — context-dependent resolution (Layer 2)
//	3. cleanWikiID              — context-free id canonicalization (Layer 3)
//
// plus the resolution-time file matching the spec's "fidelity" section calls
// for: the canonical id is matched against the files actually present, with
// case-folded, cleaned-form comparison, so small divergences from DokuWiki's
// curated character table degrade to a normal not-found rather than a silent
// mis-link.
//
// CLEAN-ROOM NOTE: this file is written from the behavioral spec only. It
// contains no DokuWiki code and no DokuWiki data tables; the id character set
// is derived from Unicode general categories (see cleanWikiID).

// wikiCfg is the per-wiki resolution configuration (docs/dokuwiki-ids.md
// "Per-wiki config"). Values are DokuWiki's documented defaults; discovery of
// a tree's own settings is a later concern.
type wikiCfg struct {
	useSlash  bool   // treat "/" as a namespace separator (≡ ":")
	deaccent  bool   // fold accented letters to ASCII
	sepchar   rune   // what non-id characters collapse to
	startPage string // namespace default page name
}

func defaultWikiCfg() wikiCfg {
	return wikiCfg{useSlash: false, deaccent: true, sepchar: '_', startPage: "start"}
}

// linkSchemes is Layer 1's explicit scheme registry: a reference invokes a
// scheme only when it begins with one of these names followed by a slash form
// (scheme:/… or scheme://…). Anything not registered is DokuWiki content — a
// bare "wiki:syntax" is a namespace path, never a scheme.
var linkSchemes = map[string]bool{
	"http":  true,
	"https": true,
	"ftp":   true,
	"file":  true,
	"mew":   true,
}

// schemeRef reports whether ref invokes a registered scheme (Layer 1): a
// registered scheme name immediately followed by ":/" (rooted within the
// scheme) or "://" (an authority is present).
func schemeRef(ref string) (scheme string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i+1 >= len(ref) || ref[i+1] != '/' {
		return "", false
	}
	name := strings.ToLower(ref[:i])
	if linkSchemes[name] {
		return name, true
	}
	return "", false
}

// wikiDef describes a REGISTERED wiki: the reference format its pages use,
// the subtree its pages live under, the page-file extension auto-assumed for
// its pages, and its start page. Registered wiki names act as schemes in
// Layer 1 — "help:/start" opens the page "start" within the help wiki — and
// a page inside a registered root highlights as the wiki's format (the
// mew:-space analogue of the path-conditional [formats] rules).
type wikiDef struct {
	Format string // reference/grammar format ("dokuwiki")
	Root   string // canonical URL of the wiki root
	Ext    string // page file extension
	Start  string // start page name
}

// wikiRegistry is hardcoded for now — the built-in help wiki lives in mew's
// support tree. A config-driven registry can replace this later.
var wikiRegistry = map[string]wikiDef{
	"help": {Format: "dokuwiki", Root: "mew:///help", Ext: ".txt", Start: "start"},
}

// wikiSchemeRef reports whether ref invokes a registered wiki scheme with a
// slash form (help:/id, help://id): the wiki plus the reference within it
// ("" = the wiki's start page). Checked BEFORE the generic scheme gate, so a
// registered wiki name opens pages instead of gating out as external; a bare
// "help:foo" (no slash) stays an ordinary namespace reference, per the
// slash-form rule.
func wikiSchemeRef(ref string) (wikiDef, string, bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i+1 >= len(ref) || ref[i+1] != '/' {
		return wikiDef{}, "", false
	}
	def, ok := wikiRegistry[strings.ToLower(ref[:i])]
	if !ok {
		return wikiDef{}, "", false
	}
	return def, strings.TrimLeft(ref[i+1:], "/"), true
}

// interwikiRef reports whether ref uses DokuWiki's interwiki form
// (shortcut>rest). The shortcut registry is not populated yet, but the form
// itself always leaves the current wiki, so recognizing the syntax is enough
// to gate it out of Layer 2.
func interwikiRef(ref string) (shortcut, rest string, ok bool) {
	i := strings.IndexByte(ref, '>')
	if i < 0 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// resolveWikiRef is Layer 2: a within-wiki reference plus the context
// namespace (":"-separated, "" = wiki root) becomes an ABSOLUTE id, ready for
// cleaning. The fragment ("#anchor") is split off and returned separately;
// nsTarget reports a reference that designates a namespace (trailing
// separator), which resolves to that namespace's start page at match time.
func resolveWikiRef(ref, ctxNS string, cfg wikiCfg) (id, anchor string, nsTarget bool) {
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		anchor = ref[i+1:]
		ref = ref[:i]
	}
	if cfg.useSlash {
		ref = strings.ReplaceAll(ref, "/", ":")
	}
	ref = splitGluedDots(ref)
	nsTarget = ref == "" || strings.HasSuffix(ref, ":")

	// Relative vs absolute: bare name ⇒ relative; leading dot ⇒ explicitly
	// relative; anything else containing a separator ⇒ absolute from the root
	// (including a leading colon, whose empty first segment drops in the walk).
	relative := strings.HasPrefix(ref, ".") || !strings.Contains(ref, ":")
	full := ref
	if relative && ctxNS != "" {
		full = ctxNS + ":" + ref
	}

	// Walk "." and "..": pop at ".." (a no-op at the root), skip "." and
	// empty segments (leading-colon absolutes, trailing separators).
	var stack []string
	for _, s := range strings.Split(full, ":") {
		switch s {
		case "", ".":
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, s)
		}
	}
	return strings.Join(stack, ":"), anchor, nsTarget
}

// splitGluedDots normalizes a leading dot-run written glued to its name
// ("..example") into the separated form ("..:example"): the run is dot
// markers, never part of a namespace name. Longer runs split into ".."
// markers (plus a final "." when odd). Already-separated references pass
// through unchanged.
func splitGluedDots(ref string) string {
	n := 0
	for n < len(ref) && ref[n] == '.' {
		n++
	}
	if n == 0 || (n < len(ref) && ref[n] == ':') {
		return ref // no leading run, or already separated
	}
	var marks []string
	for d := n; d > 0; d -= 2 {
		if d >= 2 {
			marks = append(marks, "..")
		} else {
			marks = append(marks, ".")
		}
	}
	rest := ref[n:]
	if rest == "" {
		return strings.Join(marks, ":")
	}
	return strings.Join(marks, ":") + ":" + rest
}

// cleanWikiID is Layer 3: context-free canonicalization of an id. The
// character set is derived from Unicode general categories, not from
// DokuWiki's (GPL) curated table: keep letters (L*), decimal digits (Nd), the
// id punctuation "_" "." "-", and ":" as the separator; everything else
// collapses to the sepchar. Resolution-time matching absorbs any divergence
// on exotic codepoints.
func cleanWikiID(id string, cfg wikiCfg) string {
	id = strings.TrimSpace(id)
	id = strings.ToLower(id)

	// Alternate separators: ";" is always a separator; "/" is one only under
	// useslash (otherwise it is an ordinary character and falls through to
	// the sepchar rule below).
	id = strings.ReplaceAll(id, ";", ":")
	if cfg.useSlash {
		id = strings.ReplaceAll(id, "/", ":")
	}

	if cfg.deaccent {
		id = foldAccents(id)
	}

	sep := cfg.sepchar
	var b strings.Builder
	for _, r := range id {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '.' || r == '-' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune(sep)
		}
	}
	out := []rune(b.String())

	// Collapse runs of the sepchar and of ":".
	out = collapseRuns(out, sep)
	out = collapseRuns(out, ':')

	// Tidy punctuation around separators: any run of boundary punctuation and
	// separators that contains a ":" collapses to a single clean ":"; a run
	// without one is ordinary in-name punctuation and stays.
	isBoundary := func(r rune) bool { return r == '.' || r == '-' || r == sep }
	var tidy []rune
	for i := 0; i < len(out); {
		if !isBoundary(out[i]) && out[i] != ':' {
			tidy = append(tidy, out[i])
			i++
			continue
		}
		j := i
		hasColon := false
		for j < len(out) && (isBoundary(out[j]) || out[j] == ':') {
			if out[j] == ':' {
				hasColon = true
			}
			j++
		}
		if hasColon {
			tidy = append(tidy, ':')
		} else {
			tidy = append(tidy, out[i:j]...)
		}
		i = j
	}

	// Trim edge punctuation (":", ".", "-", sepchar) from both ends.
	s := strings.TrimFunc(string(tidy), func(r rune) bool {
		return r == ':' || r == '.' || r == '-' || r == sep
	})
	return s
}

// collapseRuns collapses consecutive repeats of r to a single occurrence.
func collapseRuns(in []rune, r rune) []rune {
	out := in[:0:len(in)]
	for i, c := range in {
		if c == r && i > 0 && in[i-1] == r {
			continue
		}
		out = append(out, c)
	}
	return out
}

// foldAccents strips combining marks after canonical decomposition, folding
// accented Latin letters to their base ASCII (é→e, ü→u). Derived from
// Unicode normalization — no transliteration tables are imported.
func foldAccents(s string) string {
	d := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

// ---- Canonical-URL space -------------------------------------------------
//
// Resolution-time matching works entirely in canonical-URL space, so a wiki
// can live on the document filesystem (file:///...) or inside mew's own
// support tree (mew:///docs/...) with identical behavior. urlSplit/urlDir/
// urlJoin are the path algebra; docStat/docList dispatch to the right backing
// store per scheme.

// urlSplit splits a canonical document URL into its scheme prefix ("file://",
// "mew://") and rooted "/"-path. ok=false for anything else (unnamed buffers,
// foreign schemes).
func urlSplit(url string) (prefix, p string, ok bool) {
	for _, pre := range []string{"file://", "mew://"} {
		if strings.HasPrefix(url, pre) {
			p = url[len(pre):]
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			return pre, p, true
		}
	}
	return "", "", false
}

// urlDir is the parent of a canonical URL, clamped at the scheme root
// ("file:///", "mew:///").
func urlDir(url string) string {
	prefix, p, ok := urlSplit(url)
	if !ok {
		return url
	}
	return prefix + path.Dir(p)
}

// urlJoin appends a name to a canonical directory URL.
func urlJoin(url, name string) string {
	prefix, p, ok := urlSplit(url)
	if !ok {
		return url + "/" + name
	}
	return prefix + path.Join(p, name)
}

// urlWithin reports whether url lies at or under the subtree rooted at root.
func urlWithin(url, root string) bool {
	return url == root || strings.HasPrefix(url, strings.TrimSuffix(root, "/")+"/")
}

// docStat reports existence and directory-ness for a canonical URL, through
// the document FileSystem (file://) or the mew support tree (mew://).
func (e *Editor) docStat(url string) (isDir, exists bool) {
	prefix, p, ok := urlSplit(url)
	if !ok {
		return false, false
	}
	if prefix == "mew://" {
		info, ok := e.mew.Stat("mew:" + p)
		return info.IsDir, ok
	}
	name := filepath.FromSlash(p)
	if st, ok := e.FS.(Statter); ok {
		info, err := st.Stat(name)
		if err != nil {
			return false, false
		}
		return info.IsDir, true
	}
	// A plain FileSystem cannot see directories; a readable name is a file.
	if _, err := e.FS.ReadFile(name); err == nil {
		return false, true
	}
	return false, false
}

// docList returns the canonical URLs of the entries in a canonical directory
// URL (nil on error or foreign scheme).
func (e *Editor) docList(dirURL string) []string {
	prefix, p, ok := urlSplit(dirURL)
	if !ok {
		return nil
	}
	if prefix == "mew://" {
		matches, err := e.mew.Glob("mew:" + path.Join(p, "*"))
		if err != nil {
			return nil
		}
		out := make([]string, 0, len(matches))
		for _, m := range matches {
			out = append(out, e.canonicalDocURL(m))
		}
		return out
	}
	matches, err := e.FS.Glob(filepath.FromSlash(path.Join(p, "*")))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, "file://"+filepath.ToSlash(m))
	}
	return out
}

// ---- Resolution-time file matching ----------------------------------------

// followResolution is the outcome of resolving a link target for navigation.
type followResolution struct {
	url  string // canonical URL of an existing file; "" when not followable
	root string // WikiRoot for the destination window ("" = none)
	// newWindow: the destination must surface in a FRESH window rather than
	// swapping in place — a window's root never changes, so a full-scheme
	// reference (the one way a link leaves a rooted wiki) opens a new,
	// rootless window (possibly sharing an already-open buffer).
	newWindow bool
	message   string // human notification when url is ""
}

// resolveFollow resolves a link target against the window's current document
// into the canonical URL of an EXISTING file, per the three layers plus
// resolution-time matching. Document schemes (mew:///, file:///) resolve as
// new-window destinations; other schemes and interwiki are gated out; wiki
// ids resolve within the window's root when it has one — absolute ids from
// the root, relative climbs clamped at it, no escape — and by nearest-
// ancestor discovery when it does not.
func (e *Editor) resolveFollow(w *window.Window, target string) followResolution {
	ref := strings.TrimSpace(target)

	// Registered wiki schemes first: "help:/start" opens a page within that
	// wiki, rooted at its registered root. The destination surfaces in a new
	// window unless the current window already carries that exact root (a
	// window's root never changes).
	if def, rest, ok := wikiSchemeRef(ref); ok {
		if p := e.resolveInWiki(def, rest); p != "" {
			newWin := w == nil || w.WikiRoot != def.Root
			return followResolution{url: p, root: def.Root, newWindow: newWin}
		}
		return followResolution{message: "Page not found: " + ref}
	}

	if scheme, ok := schemeRef(ref); ok {
		if scheme == "mew" || scheme == "file" {
			url := e.canonicalDocURL(ref)
			if isDir, exists := e.docStat(url); exists && !isDir {
				return followResolution{url: url, newWindow: true}
			}
			return followResolution{message: "Not found: " + ref}
		}
		return followResolution{message: "External link: " + ref}
	}
	if _, _, ok := interwikiRef(ref); ok {
		return followResolution{message: "Interwiki link: " + ref}
	}

	src := ""
	if w != nil {
		src = e.bufferCanonicalURL(w.Buffer)
	}
	prefix, _, ok := urlSplit(src)
	if !ok {
		// An unnamed buffer has no namespace context to resolve against.
		return followResolution{message: "Link: " + target}
	}
	curDir := urlDir(src)
	srcExt := path.Ext(src)
	cfg := defaultWikiCfg()

	// The window's wiki root confines resolution. A root that does not cover
	// the current document would be a bug elsewhere; ignore it defensively.
	root := w.WikiRoot
	if root != "" && !urlWithin(src, root) {
		root = ""
	}

	// Layer 2 decides relative vs absolute; the walk happens in id space with
	// an empty context, and the URL-space base supplies the context instead:
	// a relative reference resolves under the current document's directory,
	// an absolute one from the wiki root.
	relative := isRelativeRef(ref, cfg)
	id, _, nsTarget := resolveWikiRef(ref, "", cfg)
	id = cleanWikiID(id, cfg)
	var segs []string
	if id != "" {
		segs = strings.Split(id, ":")
	}

	if relative {
		// Re-apply the dot-walk against the real directory: climbs ("..")
		// pop the actual path, clamped at the window's root when set (a
		// rooted window's links can never back out of it).
		floor := prefix + "/"
		if root != "" {
			floor = root
		}
		base, names := relativeBase(curDir, floor, ref, cfg)
		if p := e.matchWikiPath(base, names, srcExt, nsTarget, cfg); p != "" {
			return followResolution{url: p, root: root}
		}
		return followResolution{message: "Page not found: " + target}
	}

	if root != "" {
		// Absolute within a rooted window: from the root, nowhere else.
		if p := e.matchWikiPath(root, segs, srcExt, nsTarget, cfg); p != "" {
			return followResolution{url: p, root: root}
		}
		return followResolution{message: "Page not found: " + target}
	}

	// Unrooted absolute: nearest ancestor of the current directory under
	// which the id matches real files wins (the spec's resolution-time rule).
	schemeRoot := prefix + "/"
	for dir := curDir; ; dir = urlDir(dir) {
		if p := e.matchWikiPath(dir, segs, srcExt, nsTarget, cfg); p != "" {
			return followResolution{url: p}
		}
		if dir == schemeRoot || urlDir(dir) == dir {
			break
		}
	}
	return followResolution{message: "Page not found: " + target}
}

// resolveInWiki resolves a reference within a registered wiki, from its
// root, with the wiki's own extension and start page. The scheme form is
// URL-flavored, so "/" separates namespaces here (help:/sample/widget ≡
// help:/sample:widget); an empty reference is the wiki's start page. Returns
// the matched canonical URL ("" = no page).
func (e *Editor) resolveInWiki(def wikiDef, rest string) string {
	cfg := defaultWikiCfg()
	cfg.useSlash = true
	cfg.startPage = def.Start
	id, _, nsTarget := resolveWikiRef(rest, "", cfg)
	id = cleanWikiID(id, cfg)
	var segs []string
	if id != "" {
		segs = strings.Split(id, ":")
	}
	return e.matchWikiPath(def.Root, segs, def.Ext, nsTarget, cfg)
}

// isRelativeRef applies Layer 2's relative/absolute rule to a raw reference:
// leading dot ⇒ relative, bare name (no separator) ⇒ relative, anything else
// with a separator ⇒ absolute.
func isRelativeRef(ref string, cfg wikiCfg) bool {
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	if cfg.useSlash {
		ref = strings.ReplaceAll(ref, "/", ":")
	}
	return strings.HasPrefix(ref, ".") || !strings.Contains(ref, ":")
}

// relativeBase resolves a RELATIVE reference's dot-walk against the real
// directory URL of the current document: "." stays, ".." climbs — never
// above floor (the window's wiki root, or the scheme root) — and the
// remaining name segments come back cleaned for matching.
func relativeBase(curDir, floor, ref string, cfg wikiCfg) (base string, names []string) {
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	if cfg.useSlash {
		ref = strings.ReplaceAll(ref, "/", ":")
	}
	ref = splitGluedDots(ref)
	base = curDir
	for _, s := range strings.Split(ref, ":") {
		switch s {
		case "", ".":
		case "..":
			// Climb only while strictly below the floor; at (or somehow
			// outside) it, ".." is a no-op — a rooted window's links can
			// never back out of their root.
			if base != floor && urlWithin(base, floor) {
				base = urlDir(base)
			}
		default:
			if c := cleanWikiID(s, cfg); c != "" {
				names = append(names, c)
			}
		}
	}
	return base, names
}

// matchWikiPath matches cleaned id segments against the files actually under
// the base directory URL: namespace segments match subdirectories, the final
// segment matches a page file. Comparison folds case and cleans the on-disk
// name, so canonical lowercase ids find MyPage.txt. Candidate page names try
// the source file's extension first, then DokuWiki's ".txt", then the bare
// name; a namespace target (or a namespace that exists where a page does
// not) resolves to its start page. Returns the matched canonical URL
// ("" = no match).
func (e *Editor) matchWikiPath(base string, segs []string, srcExt string, nsTarget bool, cfg wikiCfg) string {
	dir := base
	for i, seg := range segs {
		last := i == len(segs)-1
		if !last {
			sub := e.matchEntry(dir, seg, "", true, cfg)
			if sub == "" {
				return ""
			}
			dir = sub
			continue
		}
		if !nsTarget {
			if p := e.matchPageFile(dir, seg, srcExt, cfg); p != "" {
				return p
			}
		}
		// Namespace target (explicit trailing separator, or a directory where
		// no page file matched): its start page.
		if sub := e.matchEntry(dir, seg, "", true, cfg); sub != "" {
			if p := e.matchPageFile(sub, cleanWikiID(cfg.startPage, cfg), srcExt, cfg); p != "" {
				return p
			}
		}
		return ""
	}
	// No segments at all (a bare namespace reference like ":"): the base
	// directory's start page.
	return e.matchPageFile(dir, cleanWikiID(cfg.startPage, cfg), srcExt, cfg)
}

// matchPageFile finds a page FILE for a cleaned id segment in a directory
// URL: the segment plus the source extension, then ".txt", then the bare
// name.
func (e *Editor) matchPageFile(dir, seg, srcExt string, cfg wikiCfg) string {
	exts := []string{}
	if srcExt != "" {
		exts = append(exts, srcExt)
	}
	if srcExt != ".txt" {
		exts = append(exts, ".txt")
	}
	exts = append(exts, "")
	for _, ext := range exts {
		if p := e.matchEntry(dir, seg, ext, false, cfg); p != "" {
			return p
		}
	}
	return ""
}

// matchEntry scans a directory URL for an entry whose cleaned name equals
// seg (+ext): exact spelling first (the overwhelmingly common case — one
// probe, no listing), then a listing pass comparing cleaned, case-folded
// names. wantDir selects directories; otherwise files. Returns the entry's
// canonical URL.
func (e *Editor) matchEntry(dir, seg, ext string, wantDir bool, cfg wikiCfg) string {
	direct := urlJoin(dir, seg+ext)
	if isDir, exists := e.docStat(direct); exists && isDir == wantDir {
		return direct
	}
	for _, entry := range e.docList(dir) {
		name := path.Base(entry)
		if ext != "" {
			if !strings.EqualFold(path.Ext(name), ext) {
				continue
			}
			name = name[:len(name)-len(path.Ext(name))]
		}
		if cleanWikiID(name, cfg) != seg {
			continue
		}
		if isDir, exists := e.docStat(entry); exists && isDir == wantDir {
			return entry
		}
	}
	return ""
}
