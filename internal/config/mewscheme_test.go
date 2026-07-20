package config

import (
	"os"
	"strings"
	"testing"
)

// recFileIO is a FileIO that serves canned files by exact path and records
// every read, so a test can assert which paths the config layer asked for.
type recFileIO struct {
	files map[string][]byte
	reads []string
}

func (r *recFileIO) fio() FileIO {
	return FileIO{
		Read: func(p string) ([]byte, error) {
			r.reads = append(r.reads, p)
			if b, ok := r.files[p]; ok {
				return b, nil
			}
			return nil, os.ErrNotExist
		},
		Write: func(p string, data []byte) error {
			if r.files == nil {
				r.files = map[string][]byte{}
			}
			r.files[p] = data
			return nil
		},
		IsDir: func(string) bool { return false },
	}
}

// The user config and its includes address the "mew:/" scheme, and every path
// the config layer asks the host for stays inside that scheme — a "../" in an
// include can never climb out of the mew tree.
func TestConfigMewRoutingAndConfinement(t *testing.T) {
	rec := &recFileIO{files: map[string][]byte{
		"mew:/editor.conf": []byte(
			"[options]\ntabSize=7\n" +
				"@include <../../secret.conf>\n" + // angle: resolves under the tree root
				"@include \"../../../q.conf\"\n"), // quoted: relative to the includer (mew:)
		"mew:/secret.conf": []byte("[options]\nshowLineNumbers=false\n"),
		"mew:/q.conf":      []byte("[options]\nwordWrap=true\n"),
	}}
	m := NewManager()
	m.SetFileIO(rec.fio())

	cfg, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.TabSize != 7 {
		t.Fatalf("tabSize = %d, want 7 (mew:/editor.conf not applied)", cfg.General.TabSize)
	}
	// The confined includes still resolved and applied.
	if cfg.General.ShowLineNumbers {
		t.Error("angle include (../../secret.conf) should confine to mew:/secret.conf and apply")
	}
	if !cfg.General.WordWrap {
		t.Error("quoted include (../../../q.conf) should confine to mew:/q.conf and apply")
	}

	// Every path handed to the host stayed inside the scheme, with no ".." left.
	for _, p := range rec.reads {
		if !strings.HasPrefix(p, "mew:") {
			t.Fatalf("config read escaped the mew: scheme: %q (all reads: %v)", p, rec.reads)
		}
		if strings.Contains(p, "..") {
			t.Fatalf("config read retained a '..': %q (all reads: %v)", p, rec.reads)
		}
	}
	if !containsStr(rec.reads, "mew:/secret.conf") || !containsStr(rec.reads, "mew:/q.conf") {
		t.Fatalf("includes should confine under the tree root, reads = %v", rec.reads)
	}
}

// A missing user config is created at "mew:/editor.conf" (not a real home path)
// through the injected FileIO.
func TestConfigWritesDefaultToMewScheme(t *testing.T) {
	rec := &recFileIO{files: map[string][]byte{}}
	m := NewManager()
	m.SetFileIO(rec.fio())

	if _, err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.files["mew:/editor.conf"]; !ok {
		t.Fatalf("default config should be written to mew:/editor.conf, wrote %v", keysOf(rec.files))
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
