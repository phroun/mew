//go:build mew

package trinkets

import (
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kittytk/ reaches upstream as a subtree split, and the split drops mew's own
// files by NAME: a Go file whose name spells `mew` as an underscore-delimited
// word is mew-owned and stays on this side. The `//go:build mew` tag is what
// actually decides which files the mew build compiles, so the two have to
// agree on every Go file in the tree.
//
// A mew-owned file the name rule misses travels upstream and refers to symbols
// that do not exist there. An upstream-owned file the name rule catches is
// dropped from a sync it belongs in. Both have happened.
//
// The `0_`/`00_` prefix a test file carries under TEST-NAMING.md leaves the
// word intact: `0_editor_mew_blink_test.go` still spells `mew`.

// upstreamOwnedMewTagged names the files that carry the tag and are upstream's
// anyway, so the name rule must NOT catch them.
var upstreamOwnedMewTagged = map[string]bool{
	// Upstream's proof that a tree with no mew editor fails to build under
	// -tags mew rather than producing a host with no editor registered.
	"objects/trinkets/editor_tag_assert.go": true,
}

// mewNamed reports whether a file name spells `mew` as its own word, which is
// what the upstream split matches on.
func mewNamed(path string) bool {
	base := strings.TrimSuffix(filepath.Base(path), ".go")
	for _, word := range strings.Split(base, "_") {
		if word == "mew" {
			return true
		}
	}
	return false
}

// constraintTags collects the tag names an expression names.
func constraintTags(expr constraint.Expr, into map[string]bool) {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		into[e.Tag] = true
	case *constraint.NotExpr:
		constraintTags(e.X, into)
	case *constraint.AndExpr:
		constraintTags(e.X, into)
		constraintTags(e.Y, into)
	case *constraint.OrExpr:
		constraintTags(e.X, into)
		constraintTags(e.Y, into)
	}
}

// satisfiable reports whether any setting of the other tags makes expr true
// while mew holds the given value. Constraints name a handful of tags, so
// every combination is cheap to try, and trying them all is what keeps a
// negated tag (`mew && !windows`) from reading as unsatisfiable.
func satisfiable(expr constraint.Expr, mew bool) bool {
	named := map[string]bool{}
	constraintTags(expr, named)
	var others []string
	for tag := range named {
		if tag != "mew" {
			others = append(others, tag)
		}
	}
	for combo := 0; combo < 1<<len(others); combo++ {
		set := map[string]bool{"mew": mew}
		for i, tag := range others {
			set[tag] = combo&(1<<i) != 0
		}
		if expr.Eval(func(tag string) bool { return set[tag] }) {
			return true
		}
	}
	return false
}

// mewTagged reports whether a file's build constraint can only be satisfied
// with the mew tag set.
func mewTagged(t *testing.T, path string) bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return satisfiable(expr, true) && !satisfiable(expr, false)
	}
	return false
}

func TestTheForkBoundaryIsSpelledInEveryFileName(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "wiki" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		named, tagged := mewNamed(rel), mewTagged(t, path)
		switch {
		case upstreamOwnedMewTagged[rel]:
			if named {
				t.Errorf("%s is upstream's and its name matches the split's rule, "+
					"so a sync would drop it", rel)
			}
		case tagged && !named:
			t.Errorf("%s only builds with -tags mew but its name does not spell "+
				"mew, so the split would send it upstream", rel)
		case named && !tagged:
			t.Errorf("%s is named as mew's but has no mew build constraint, so "+
				"the split drops a file upstream owns", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
