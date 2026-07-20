package editor

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/phroun/mew/internal/window"
)

// completeFilename is the completion handler attached to filename prompts. It
// globs candidates for the partial name on the prompt's caret line — always
// through the file-system abstraction (e.FS.Glob), so it works the same over a
// host's virtual file system — auto-fills as much of the shared name as
// possible, and shows the remaining choices as a transient. It returns true
// once it owns the key (even with no candidates): a filename prompt must never
// fall through to inserting a literal tab. Only the absence of a handler (a
// plain buffer) lets the tab fallback run.
func (e *Editor) completeFilename(w *window.Window) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	raw := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")

	// A bare "~partial" (no separator yet) completes user names — sibling home
	// directories — presented as "~name/".
	if e.usingOSFS && strings.HasPrefix(raw, "~") && !strings.ContainsAny(raw, `/\`) {
		return e.completeUserName(w, raw)
	}

	globDir, prefix := e.completionSplit(w, raw)
	pattern := prefix + "*"
	if globDir != "" {
		pattern = filepath.Join(globDir, prefix+"*")
	}
	infos, err := e.globStat(pattern)
	if err != nil || len(infos) == 0 {
		// No candidates, but the handler still owns the key: a filename prompt
		// must never insert a literal tab, so report success and do nothing.
		return true
	}

	// Reduce to display names — the final path component, a directory marked
	// with a trailing "/" so the shared-prefix fill descends into it — and
	// their common prefix. Every match starts with prefix (the glob
	// guarantees it), so the common prefix always extends it.
	names := completionNames(infos)
	common := longestCommonPrefix(names)
	if len(common) > len(prefix) {
		e.insertText(common[len(prefix):]) // auto-fill the shared part
	}
	if len(names) > 1 {
		// Tagged so a re-completion replaces this list rather than stacking.
		e.showTaggedTransient("Complete: "+completionList(names), "notification", "completion")
	}
	return true
}

// completeUserName completes a "~partial" into "~name/" over the user home
// directories that neighbor the current user's home (its parent — /Users on
// macOS, /home on most Linux). It auto-fills the shared part and lists the rest,
// like ordinary filename completion. Always owns the tab (returns true).
func (e *Editor) completeUserName(w *window.Window, raw string) bool {
	root := filepath.Dir(e.home)
	if e.home == "" || root == "" || root == "." || root == e.home {
		return true
	}
	partial := strings.TrimPrefix(raw, "~")
	infos, err := e.globStat(filepath.Join(root, partial+"*"))
	if err != nil || len(infos) == 0 {
		return true
	}
	seen := map[string]bool{}
	var names []string
	for _, info := range infos {
		if !info.IsDir {
			continue // a user's home is a directory
		}
		n := filepath.Base(info.Path)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, "~"+n+"/")
	}
	if len(names) == 0 {
		return true
	}
	sort.Strings(names)
	common := longestCommonPrefix(names)
	if len(common) > len(raw) {
		e.insertText(common[len(raw):]) // auto-fill the shared "~name" part
	}
	if len(names) > 1 {
		e.showTaggedTransient("Complete: "+completionList(names), "notification", "completion")
	}
	return true
}

// globStat globs candidates with directory information, preferring the host's
// single-call DirGlobber, then Glob+Stat, then plain Glob (no dir info) — so
// completion works over any FileSystem and pays only the round-trips it must.
func (e *Editor) globStat(pattern string) ([]FileInfo, error) {
	if dg, ok := e.FS.(DirGlobber); ok {
		return dg.GlobStat(pattern)
	}
	matches, err := e.FS.Glob(pattern)
	if err != nil {
		return nil, err
	}
	st, hasStat := e.FS.(Statter)
	out := make([]FileInfo, 0, len(matches))
	for _, m := range matches {
		info := FileInfo{Path: m}
		if hasStat {
			if s, err := st.Stat(m); err == nil {
				info.IsDir = s.IsDir
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// completionSplit resolves the directory to glob and the filename prefix to
// match from the partial text on the prompt line. Path text in the partial
// (foo/ba) descends from the base directory; an absolute path is used as-is; a
// trailing slash lists the directory's contents.
func (e *Editor) completionSplit(w *window.Window, raw string) (globDir, prefix string) {
	base := e.completionBaseDir(w)
	text := raw
	if e.usingOSFS {
		// ~/… and ~user/… (and bare ~ / ~user) resolve to a home directory.
		if expanded, ok := expandTilde(text, e.home); ok {
			text = expanded
		}
	}
	switch {
	case text == "":
		return base, ""
	case strings.HasSuffix(raw, "/"):
		if filepath.IsAbs(text) {
			return filepath.Clean(text), ""
		}
		return filepath.Join(base, text), ""
	default:
		full := text
		if !filepath.IsAbs(text) {
			full = filepath.Join(base, text)
		}
		return filepath.Dir(full), filepath.Base(full)
	}
}

// resolvePromptPath anchors a filename typed at a prompt to the same directory
// completion searches (baseDir), so a plain name opens or saves next to the
// anchoring file rather than in the process's working directory. Relative text
// joins the base — including ../, which walks up FROM the anchor and stays in
// the file system's namespace. It is left untouched only when the user marked
// it truly non-relative: an absolute path (/…) or a scheme (proto:/…). A
// leading ~ expands to the home directory (standalone only).
func (e *Editor) resolvePromptPath(text, baseDir string) string {
	if text == "" {
		return text
	}
	// A leading ~ / ~user resolves to a home directory regardless of the base.
	if e.usingOSFS {
		if expanded, ok := expandTilde(text, e.home); ok {
			return expanded
		}
	}
	if baseDir == "" {
		return text
	}
	if strings.HasPrefix(text, "/") || hasScheme(text) {
		return text // absolute or scheme: not relative to the base
	}
	return filepath.Join(baseDir, text) // relative (incl. ../) resolves from base
}

// hasScheme reports whether s begins with an RFC-3986-style "scheme:/" marker
// (http://, file:/, s3://, …): a letter followed by letters/digits/+.- up to
// the first ":/".
func hasScheme(s string) bool {
	i := strings.Index(s, ":/")
	if i <= 0 {
		return false
	}
	if c := s[0]; !isSchemeAlpha(c) {
		return false
	}
	for j := 1; j < i; j++ {
		c := s[j]
		if !isSchemeAlpha(c) && !(c >= '0' && c <= '9') && c != '+' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

func isSchemeAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// completionBaseDir resolves the directory a fresh completion anchors to: the
// parent buffer's own directory (where the file was opened from — the classic
// case), else the launch directory (standalone), else [storage] documents=,
// else empty so module mode globs the host's default location.
func (e *Editor) completionBaseDir(w *window.Window) string {
	main := w.ParentMain
	if main == nil && w.Type == window.MainBuffer {
		main = w
	}
	if main != nil && main.Buffer != nil {
		if fn := main.Buffer.GetFilename(); fn != "" {
			return filepath.Dir(fn)
		}
	}
	if e.usingOSFS && e.launchDir != "" {
		return e.launchDir
	}
	return e.LoadedConfig.Storage.Documents
}

// completionNames reduces glob results to their final path component, with a
// trailing "/" on directories, deduped and sorted.
func completionNames(infos []FileInfo) []string {
	seen := map[string]bool{}
	var out []string
	for _, info := range infos {
		n := filepath.Base(info.Path)
		if n == "" {
			continue
		}
		if info.IsDir {
			n += "/"
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// longestCommonPrefix returns the longest byte prefix shared by every input
// (filenames are matched case-sensitively).
func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		i := 0
		for i < len(p) && i < len(s) && p[i] == s[i] {
			i++
		}
		p = p[:i]
		if p == "" {
			break
		}
	}
	return p
}

// completionList renders the choices for the transient, bounded so a huge
// directory does not overflow the message.
func completionList(names []string) string {
	const max = 20
	if len(names) > max {
		return strings.Join(names[:max], "  ") + "  … (" + strconv.Itoa(len(names)-max) + " more)"
	}
	return strings.Join(names, "  ")
}
