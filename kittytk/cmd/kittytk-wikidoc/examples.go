package main

// Executing the wiki's examples.
//
// Parsing an example is not enough to know it is right. The protocol
// accepts any syntactically valid property name and only rejects one
// the registry has never heard of, so a made-up property parses
// cleanly and fails at execution. The same goes for a child under a
// parent that will not take it. The only way to know a wiki example
// works is to run it against the real registry, which is what this
// does.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phroun/kittytk/protocol"
)

// runExamples parses and executes every wire example in the wiki and
// returns the number that failed.
func runExamples(pages []string) (int, error) {
	failed := 0
	ran, skipped := 0, 0

	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			return 0, err
		}
		name := filepath.Base(page)

		// One session per page. A fence that continues the previous
		// one -- `set tv.b ...` after the build that made `tv` -- is
		// then executed against what the earlier fence built, which
		// is how a reader would run them.
		ctx := &protocol.BindContext{Dispatch: func(string) {}}
		f := protocol.NewRegistryFactory(ctx)
		session := protocol.NewSession()

		for _, ex := range fences(string(body)) {
			if ex.noexec || !isWireExample(ex.src) {
				skipped++
				continue
			}
			ran++
			if err := execExample(session, f, ex.src); err != nil {
				failed++
				fmt.Fprintf(os.Stderr, "%s:%d: %v\n", name, ex.line, err)
				fmt.Fprintf(os.Stderr, "    %s\n", strings.ReplaceAll(
					strings.TrimSpace(ex.src), "\n", "\n    "))
			}
		}
	}
	fmt.Printf("%d example(s) executed, %d failed, %d not wire scripts\n",
		ran, failed, skipped)
	return failed, nil
}

type fence struct {
	src    string
	line   int  // 1-based line of the fence's first content line
	noexec bool // preceded by <!-- ktkdoc:noexec -->
}

// noexecMarker exempts the next fenced block from execution. A handful
// of examples cannot run as written -- one that quotes a wire ID an
// application would have read out of a reply cannot have a real number
// in it. Those are exempted where they sit, so the exemption is visible
// to whoever edits the page and the tool's report stays at zero
// failures. Anything else that fails is a broken example.
const noexecMarker = "<!-- ktkdoc:noexec -->"

// fences returns the fenced code blocks. This is a state machine
// rather than a regular expression because a pattern that matches
// "```...```" pairs an example's CLOSING fence with the NEXT
// example's opening one, and then reports the prose between two
// examples as a third example.
func fences(body string) []fence {
	var out []fence
	var block []string
	var start int
	in, exempt := false, false
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !in && trimmed == noexecMarker {
			exempt = true
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			if !in {
				in, block, start = true, nil, i+2
				continue
			}
			in = false
			out = append(out, fence{src: strings.Join(block, "\n"), line: start, noexec: exempt})
			exempt = false
			continue
		}
		if in {
			block = append(block, line)
		}
	}
	return out
}

// isWireExample reports whether a fenced block is a wire script this
// tool should run. Pages also carry client code, shell commands, and
// fragments that stand for something rather than being it.
func isWireExample(src string) bool {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return false
	}
	for _, marker := range []string{
		":=", "func ", "ui.", "conn.", "kt_ui_", // client code
		"$ ", "git ", "go run", "sh ", "echo ", "#!/", // shell
		"->", // an example paired with the result it produces
		"…",  // an ellipsis stands for elided prose, not a statement
	} {
		if strings.Contains(src, marker) {
			return false
		}
	}
	first := strings.TrimSpace(strings.Split(trimmed, "\n")[0])
	for _, verb := range []string{
		"new ", "set ", "destroy ", "sub ", "unsub ",
		"alias ", "template ", "describe",
	} {
		if strings.HasPrefix(first, verb) {
			return true
		}
	}
	// A correlation key assignment: `tv=new treeview ...`, `aid=tv.a`.
	if i := strings.Index(first, "="); i > 0 && !strings.Contains(first[:i], " ") {
		return true
	}
	return false
}

func execExample(s *protocol.Session, f protocol.Factory, src string) error {
	script, err := protocol.Parse(src)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if _, err := s.Execute(script, f); err != nil {
		return fmt.Errorf("execute: %w", err)
	}
	return nil
}
