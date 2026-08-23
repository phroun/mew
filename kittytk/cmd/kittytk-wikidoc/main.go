// Command kittytk-wikidoc writes the wire vocabulary into the wiki.
//
// The property and event tables in the wiki are generated; the prose
// around them is not. A page marks the spans this tool owns, and it
// replaces those and nothing else:
//
//	<!-- ktkdoc:props textinput -->
//	| Property | Type | Default | Meaning |
//	...
//	<!-- /ktkdoc -->
//
// Everything outside a marked span survives untouched, byte for byte, so
// a page can be written and edited normally and re-generated at will.
// The markers are HTML comments, which render as nothing.
//
// Block kinds:
//
//	props <type>     the type's own properties
//	events <type>    the events the type emits, and their fields
//	common           the properties every non-virtual type accepts
//	types            the index of registered types
//
// Where the vocabulary comes from:
//
//	(default)        this binary's own registry -- no host need be running
//	-endpoint ADDR   a running display service, describing that build
//
// Usage:
//
//	kittytk-wikidoc -wiki ../kittytk.wiki            # rewrite in place
//	kittytk-wikidoc -wiki ../kittytk.wiki -check     # fail if stale
//	kittytk-wikidoc -wiki ../kittytk.wiki -list      # coverage both ways
//	kittytk-wikidoc -wiki ../kittytk.wiki -examples  # run the examples
//
// It edits files and never commits: the wiki is a separate repository,
// and a human reading the diff before it lands is the point.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"

	// Registration only: importing these runs the init functions that
	// populate the registry this tool reads.
	_ "github.com/phroun/kittytk/objects/trinkets"
	_ "github.com/phroun/kittytk/objects/window"
)

func main() {
	wiki := flag.String("wiki", "", "path to a wiki checkout (required)")
	endpoint := flag.String("endpoint", "", "describe a running display service instead of this binary's registry")
	check := flag.Bool("check", false, "report staleness and exit non-zero; write nothing")
	list := flag.Bool("list", false, "report coverage: types with no page, pages with no markers")
	examples := flag.Bool("examples", false, "execute the wiki's wire examples; exit non-zero on a failure. Writes nothing")
	flag.Parse()

	if *wiki == "" {
		fmt.Fprintln(os.Stderr, "kittytk-wikidoc: -wiki is required")
		flag.Usage()
		os.Exit(2)
	}

	vocab, err := loadVocabulary(*endpoint)
	if err != nil {
		fatal(err)
	}

	pages, err := filepath.Glob(filepath.Join(*wiki, "*.md"))
	if err != nil {
		fatal(err)
	}
	sort.Strings(pages)
	if len(pages) == 0 {
		fatal(fmt.Errorf("no .md files under %s -- is that a wiki checkout?", *wiki))
	}

	if *list {
		if err := report(vocab, pages); err != nil {
			fatal(err)
		}
		return
	}

	if *examples {
		failed, err := runExamples(pages)
		if err != nil {
			fatal(err)
		}
		if failed > 0 {
			os.Exit(1)
		}
		return
	}

	stale, err := apply(vocab, pages, *check)
	if err != nil {
		fatal(err)
	}
	if *check && stale > 0 {
		fmt.Fprintf(os.Stderr, "%d page(s) out of date; run without -check to update\n", stale)
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "kittytk-wikidoc:", err)
	os.Exit(1)
}

// loadVocabulary reads the vocabulary from a running service, or from
// this binary's own registry when no endpoint is given.
//
// Both paths land on the same type: a described stream decodes back into
// the structure DescribeVocabulary builds, so the renderer below cannot
// tell them apart and cannot drift between them.
func loadVocabulary(endpoint string) (*protocol.Vocabulary, error) {
	if endpoint == "" {
		return protocol.DescribeVocabulary(), nil
	}
	conn, err := client.Dial(endpoint, "kittytk-wikidoc", nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	defer conn.Close()
	v, err := conn.Describe()
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", endpoint, err)
	}
	return v, nil
}

// ---------------------------------------------------------------
// Markers
// ---------------------------------------------------------------

// openRe matches a block's opening marker and captures its kind and
// argument. The closing marker is fixed.
var openRe = regexp.MustCompile(`^[ \t]*<!--[ \t]*ktkdoc:([a-z]+)(?:[ \t]+([A-Za-z0-9_]+))?[ \t]*-->[ \t]*$`)

const closeMarker = "<!-- /ktkdoc -->"

func isClose(line string) bool {
	return strings.TrimSpace(line) == closeMarker
}

// Not every ktkdoc marker opens a span. A standalone directive stands
// on its own line and has no closing marker, so the scanner has to know
// them by name -- read as a span, one reports itself as never closed,
// which is a confusing way to say "that is not a span".
var standalone = map[string]bool{
	"noexec": true, // examples.go: exempt the next fenced block
}

// block is one marked span in one page.
type block struct {
	kind    string // props, events, common, types
	arg     string // the type name, where the kind takes one
	open    int    // line index of the opening marker
	close   int    // line index of the closing marker
	openRaw string // the opening marker verbatim, so indentation survives
}

// scan finds every marked block in a page, or reports the first
// malformed one. An unbalanced marker is an error rather than something
// to work around: guessing where a generated span ends is how a tool
// eats prose.
func scan(path string, lines []string) ([]block, error) {
	var blocks []block
	for i := 0; i < len(lines); i++ {
		m := openRe.FindStringSubmatch(lines[i])
		if m == nil {
			if isClose(lines[i]) {
				return nil, fmt.Errorf("%s:%d: closing marker with nothing open", path, i+1)
			}
			continue
		}
		if standalone[m[1]] {
			continue
		}
		b := block{kind: m[1], arg: m[2], open: i, close: -1, openRaw: lines[i]}
		for j := i + 1; j < len(lines); j++ {
			if sub := openRe.FindStringSubmatch(lines[j]); sub != nil && !standalone[sub[1]] {
				return nil, fmt.Errorf("%s:%d: block opened inside the one at line %d", path, j+1, i+1)
			}
			if isClose(lines[j]) {
				b.close = j
				break
			}
		}
		if b.close < 0 {
			return nil, fmt.Errorf("%s:%d: %s block is never closed (want %q)", path, i+1, b.kind, closeMarker)
		}
		blocks = append(blocks, b)
		i = b.close
	}
	return blocks, nil
}

// ---------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------

// render produces a block's body from the vocabulary. It returns an
// error for a block naming something the vocabulary does not have --
// never an empty table, which would quietly blank a page when a type is
// renamed.
func render(v *protocol.Vocabulary, b block) ([]string, error) {
	switch b.kind {
	case "common":
		if b.arg != "" {
			return nil, fmt.Errorf("ktkdoc:common takes no argument, got %q", b.arg)
		}
		return propTable(v.Common), nil

	case "types":
		if b.arg != "" {
			return nil, fmt.Errorf("ktkdoc:types takes no argument, got %q", b.arg)
		}
		return typeIndex(v), nil

	case "props", "events":
		if b.arg == "" {
			return nil, fmt.Errorf("ktkdoc:%s needs a type name", b.kind)
		}
		t := findType(v, b.arg)
		if t == nil {
			return nil, fmt.Errorf("ktkdoc:%s names %q, which is not a registered type", b.kind, b.arg)
		}
		if b.kind == "props" {
			if len(t.Props) == 0 {
				return []string{fmt.Sprintf("`%s` takes no properties of its own.", t.Name)}, nil
			}
			return propTable(t.Props), nil
		}
		if len(t.Events) == 0 {
			return []string{fmt.Sprintf("`%s` emits no events.", t.Name)}, nil
		}
		return eventTables(t.Events), nil
	}
	return nil, fmt.Errorf("unknown block kind %q", b.kind)
}

func findType(v *protocol.Vocabulary, name string) *protocol.TypeInfo {
	for i := range v.Types {
		if v.Types[i].Name == name {
			return &v.Types[i]
		}
	}
	return nil
}

func propTable(props []protocol.PropInfo) []string {
	out := []string{
		"| Property | Type | Default | Meaning |",
		"|---|---|---|---|",
	}
	for _, p := range props {
		out = append(out, fmt.Sprintf("| `%s` | %s | %s | %s |",
			p.Name, kindCell(p.Kind, p.Enum), defaultCell(p.Default), cell(p.Doc)))
	}
	return out
}

// eventTables renders one heading and field table per event. An event
// with no fields still gets its description, since "carries nothing" is
// an answer.
func eventTables(events []protocol.EventInfo) []string {
	var out []string
	for i, e := range events {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, fmt.Sprintf("**`%s`** — %s", e.Name, cell(e.Doc)))
		if len(e.Fields) == 0 {
			continue
		}
		out = append(out, "", "| Field | Type | Meaning |", "|---|---|---|")
		for _, f := range e.Fields {
			out = append(out, fmt.Sprintf("| `%s` | %s | %s |", f.Name, f.Kind, cell(f.Doc)))
		}
	}
	return out
}

// typeIndex lists the registered types, virtual ones separately: a
// virtual type is a piece of another object's structure (a column, a
// row, an item) rather than something an application places on its own.
func typeIndex(v *protocol.Vocabulary) []string {
	var real, virtual []string
	for _, t := range v.Types {
		if t.Virtual {
			virtual = append(virtual, "`"+t.Name+"`")
		} else {
			real = append(real, "`"+t.Name+"`")
		}
	}
	out := []string{"**Types** — " + strings.Join(real, " ")}
	if len(virtual) > 0 {
		out = append(out, "", "**Virtual types**, which are part of another object rather than placed on their own — "+
			strings.Join(virtual, " "))
	}
	return out
}

func kindCell(kind string, enum []string) string {
	if len(enum) == 0 {
		return kind
	}
	quoted := make([]string, len(enum))
	for i, e := range enum {
		quoted[i] = "`" + e + "`"
	}
	// Escaped, because an unescaped pipe would end the table cell.
	return strings.Join(quoted, " \\| ")
}

func defaultCell(def string) string {
	if def == "" {
		return "—"
	}
	return "`" + def + "`"
}

// cell makes a documentation string safe inside a table row: a pipe
// would end the cell, and a newline would end the row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

// ---------------------------------------------------------------
// Applying
// ---------------------------------------------------------------

// apply rewrites every page's marked blocks, and reports how many pages
// differ from what the vocabulary says they should be.
//
// Blocks are replaced back to front so an earlier replacement cannot
// shift the line numbers a later one was found at.
func apply(v *protocol.Vocabulary, pages []string, checkOnly bool) (int, error) {
	stale := 0
	for _, path := range pages {
		raw, err := os.ReadFile(path)
		if err != nil {
			return stale, err
		}
		text := string(raw)
		lines := strings.Split(text, "\n")

		blocks, err := scan(path, lines)
		if err != nil {
			return stale, err
		}
		if len(blocks) == 0 {
			continue
		}

		out := lines
		for i := len(blocks) - 1; i >= 0; i-- {
			b := blocks[i]
			body, err := render(v, b)
			if err != nil {
				return stale, fmt.Errorf("%s:%d: %w", path, b.open+1, err)
			}
			replacement := append([]string{b.openRaw}, body...)
			replacement = append(replacement, closeMarker)
			out = append(out[:b.open:b.open], append(replacement, out[b.close+1:]...)...)
		}

		updated := strings.Join(out, "\n")
		if updated == text {
			continue
		}
		stale++
		name := filepath.Base(path)
		if checkOnly {
			fmt.Printf("stale: %s (%d block(s))\n", name, len(blocks))
			continue
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return stale, err
		}
		fmt.Printf("updated: %s (%d block(s))\n", name, len(blocks))
	}
	if !checkOnly && stale == 0 {
		fmt.Println("up to date")
	}
	return stale, nil
}

// report says what is documented and what is not, in both directions: a
// type nothing documents, and a page documenting nothing.
func report(v *protocol.Vocabulary, pages []string) error {
	documented := map[string]bool{}
	var bare []string

	for _, path := range pages {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		blocks, err := scan(path, strings.Split(string(raw), "\n"))
		if err != nil {
			return err
		}
		if len(blocks) == 0 {
			name := filepath.Base(path)
			if !strings.HasPrefix(name, "_") {
				bare = append(bare, name)
			}
			continue
		}
		for _, b := range blocks {
			if b.arg != "" {
				documented[b.arg] = true
			}
		}
	}

	var undocumented []string
	for _, t := range v.Types {
		if !documented[t.Name] {
			label := t.Name
			if t.Virtual {
				label += " (virtual)"
			}
			undocumented = append(undocumented, label)
		}
	}

	fmt.Printf("%d type(s) registered, %d documented\n", len(v.Types), len(documented))
	if len(undocumented) > 0 {
		fmt.Printf("\nno page carries a block for these types:\n  %s\n", strings.Join(undocumented, "\n  "))
	}
	if len(bare) > 0 {
		fmt.Printf("\npages with no generated blocks:\n  %s\n", strings.Join(bare, "\n  "))
	}
	return nil
}
