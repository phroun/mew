package config

import (
	"fmt"
	"strings"
)

// The startup log is what a config file says back.
//
// Almost everything mew reads from a config file fails softly by design — an
// option it does not know is skipped, a value it cannot parse keeps its
// default — and that is right for options, where the cost of a typo is one
// setting. It is wrong for KEY MAPPINGS, where the cost of a typo is a key
// that goes somewhere else: the binding is not missing, it is bound to a key
// nobody presses, and nothing about using mew afterwards points at the line
// that did it.
//
// So mappings report. A line mew read but could not honor is collected here
// with the file and line it came from, and the editor shows the collection in
// a buffer at startup, beside whatever was opened. Nothing is fatal and
// nothing blocks: the log is a note, and closing it costs nothing.

// StartupLogTitle is the log buffer's first line. A plain buffer carries no
// grammar, so this prints as exactly these characters.
const StartupLogTitle = "== Startup Log =="

// A ConfigWarning is one thing a config file said that mew could not honor,
// with where it was written. Source and Line survive @include expansion, so the
// file named is the one to open — not necessarily the one mew was pointed at.
type ConfigWarning struct {
	Source string // config file the line came from ("" when not a real file)
	Line   int    // 1-based line within Source (0 when not from a file)
	Text   string // what was wrong, in one sentence
}

// String renders one warning the way a compiler would, which is also the way
// JOE's startup log does: file, line, then what happened.
func (w ConfigWarning) String() string {
	switch {
	case w.Source != "" && w.Line > 0:
		return fmt.Sprintf("%s %d: %s", w.Source, w.Line, w.Text)
	case w.Source != "":
		return w.Source + ": " + w.Text
	default:
		return w.Text
	}
}

// StartupLog renders the collected warnings as the log buffer's text, or "" to
// say there is nothing to show. A blank log is dropped rather than opened, so
// the ordinary startup — every config file understood — shows nothing at all.
func StartupLog(warnings []ConfigWarning) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(StartupLogTitle)
	b.WriteString("\n\n")
	for _, w := range warnings {
		b.WriteString(w.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// warn records one thing this config file could not be honored on. The nil
// check is so a Manager parsing built-ins into a throwaway config need not
// carry a sink.
func (m *Manager) warn(sl SourcedLine, format string, args ...any) {
	if m == nil {
		return
	}
	m.warnings = append(m.warnings, ConfigWarning{
		Source: sl.Source,
		Line:   sl.Line,
		Text:   fmt.Sprintf(format, args...),
	})
}

// staleLevelWords are the words that USED to raise a binding's precedence when
// written bare, before the key sequence processor moved them into parentheses.
// A keymap still written the old way does not fail: "capture ^C" is now a
// two-key chord starting with a key named "capture", which binds cleanly and
// never fires. That is the worst kind of breakage — the binding is neither
// missing nor working — so it is worth saying out loud.
var staleLevelWords = map[string]string{
	"capture":  "(capture)",
	"override": "(override)",
}

// warnStaleLevelWords reports a mapping key whose first token is a level word
// written the old way. Only the FIRST token, because that is the only place the
// old notation allowed one — a later "capture" really could be someone binding
// a key by that name, and guessing at it would cry wolf.
func (m *Manager) warnStaleLevelWords(sl SourcedLine, key string) {
	fields := strings.Fields(key)
	if len(fields) < 2 {
		return
	}
	paren, stale := staleLevelWords[strings.ToLower(fields[0])]
	if !stale {
		return
	}
	m.warn(sl, "%q reads as a chord starting with a key named %q: "+
		"level words are written %s now", key, fields[0], paren)
}

// scanHostType finds [window] host_type in a config stream, which decides what
// the keymap's desktop hints ((kde), (gnome), ...) are tested against. It is
// read ahead of everything because hints are evaluated per mapping LINE, while
// this is one value from the same file that may be written after them.
//
// Deliberately simple: section header, key, value, nothing else. The real parse
// runs immediately afterwards and owns every other question about the file.
func scanHostType(lines []SourcedLine) string {
	section, found := "", ""
	for _, sl := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(sl.Text, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "window" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 || strings.ToLower(strings.TrimSpace(line[:eq])) != "host_type" {
			continue
		}
		// Last one wins, matching how every other config value layers.
		if v := strings.ToLower(stripQuotes(strings.TrimSpace(line[eq+1:]))); v != "" {
			found = v
		}
	}
	return found
}
