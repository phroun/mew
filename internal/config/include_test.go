package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Quoted includes resolve relative to the including file and nest, each file
// through its own directory; angle includes resolve in the mew config dir.
func TestIncludeFromDisk(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "editor.conf"),
		"#include \"sub/general.conf\"\n#include <shared.conf>\n")
	write(filepath.Join(sub, "general.conf"),
		"[options]\ntabSize=6\n#include \"deep.conf\"\n")
	// deep.conf is quoted from sub/general.conf: relative to sub/.
	write(filepath.Join(sub, "deep.conf"), "[options]\nshowLineNumbers=false\n")
	// shared.conf is angle-included: found in the standard (config) dir.
	write(filepath.Join(dir, "shared.conf"), "[options]\nwordWrap=true\n")

	m := &Manager{configDir: dir, configPath: filepath.Join(dir, "editor.conf")}
	cfg, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.TabSize != 6 {
		t.Fatalf("quoted include not applied: tabSize %d", cfg.General.TabSize)
	}
	if cfg.General.ShowLineNumbers {
		t.Fatal("nested quoted include should resolve relative to its includer")
	}
	if !cfg.General.WordWrap {
		t.Fatal("angle include should resolve in the config dir")
	}
}

// A sandboxed host's reader serves every include: quoted paths arrive as the
// literal relative path (the host's own namespace), angle paths under the
// standard directory.
func TestIncludeSandboxedReader(t *testing.T) {
	var requested []string
	m := &Manager{configDir: "/mewstd"}
	m.SetIncludeReader(func(path string) ([]byte, error) {
		requested = append(requested, path)
		switch path {
		case "extra.conf":
			return []byte("[options]\ntabSize=5\n"), nil
		case filepath.Join("/mewstd", "site.conf"):
			return []byte("[options]\nwordWrap=true\n"), nil
		}
		return nil, os.ErrNotExist
	})

	cfg := m.LoadFromString("#include \"extra.conf\"\n#include <site.conf>\n")
	if cfg.General.TabSize != 5 || !cfg.General.WordWrap {
		t.Fatalf("includes not applied: %+v (requested %v)", cfg.General, requested)
	}
	if len(requested) != 2 || requested[0] != "extra.conf" {
		t.Fatalf("quoted include must be requested by its relative path: %v", requested)
	}
}

// Cycles and repeats are include-once; a missing file doesn't stop parsing.
func TestIncludeCycleAndMissing(t *testing.T) {
	m := &Manager{configDir: "/std"}
	m.SetIncludeReader(func(path string) ([]byte, error) {
		switch path {
		case "a.conf":
			return []byte("#include \"b.conf\"\n[options]\ntabSize=7\n"), nil
		case "b.conf":
			return []byte("#include \"a.conf\"\n[options]\nwordWrap=true\n"), nil
		}
		return nil, os.ErrNotExist
	})
	cfg := m.LoadFromString("#include \"a.conf\"\n#include \"missing.conf\"\n[options]\nshowInvisibles=true\n")
	if cfg.General.TabSize != 7 || !cfg.General.WordWrap {
		t.Fatal("cyclic includes should each apply once")
	}
	if !cfg.General.ShowInvisibles {
		t.Fatal("a missing include must not stop the rest of the config")
	}
}

// Ordinary comments that merely start with #inc... are untouched, and a
// non-directive #include-ish line stays a comment.
func TestIncludeNonDirectiveLines(t *testing.T) {
	m := &Manager{configDir: "/std"}
	cfg := m.LoadFromString("# include notes about includes\n#includes are cool\n[options]\ntabSize=9\n")
	if cfg.General.TabSize != 9 {
		t.Fatal("comment lines must parse as before")
	}
}
