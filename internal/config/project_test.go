package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectMewDirs walks to the root collecting .mew dirs, outermost first,
// skipping the user config dir (localMewDir).
func TestProjectMewDirs(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "work")
	inner := filepath.Join(outer, "repo", "sub")
	for _, d := range []string{
		filepath.Join(outer, ".mew"),
		filepath.Join(inner, ".mew"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := &Manager{}
	got := m.projectMewDirs(inner)
	if len(got) < 2 {
		t.Fatalf("expected at least the two project dirs, got %v", got)
	}
	// Ours are the last two (an unrelated /tmp ancestor could contribute).
	if got[len(got)-2] != filepath.Join(outer, ".mew") || got[len(got)-1] != filepath.Join(inner, ".mew") {
		t.Fatalf("wrong order (want outermost first): %v", got)
	}

	// The user's own config dir (localMewDir) is not a project.
	m.localMewDir = filepath.Join(inner, ".mew")
	got = m.projectMewDirs(inner)
	if len(got) == 0 || got[len(got)-1] != filepath.Join(outer, ".mew") {
		t.Fatalf("exclude should drop the user dir: %v", got)
	}
}

// The full cascade: defaults, then the user layer, then each project layer
// outermost to nearest — later layers overriding only what they state.
func TestLoadLayeredProjects(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "home", ".mew")
	outer := filepath.Join(root, "proj")
	inner := filepath.Join(outer, "svc")

	writeFile(t, filepath.Join(userDir, "editor.conf"),
		"[options]\ntabSize=3\nsyntax=go\n\n[colors]\ntext=\"USER\"\n\n[mappings:mew]\n^Q\t=user_cmd\n")
	writeFile(t, filepath.Join(outer, ".mew", "editor.conf"),
		"[options]\ntabSize=5\n\n[colors]\nmessages=\"OUTER\"\n\n[formats]\nfoo = go\n")
	writeFile(t, filepath.Join(inner, ".mew", "editor.conf"),
		"[options]\nsyntax=cpp\n\n[storage]\nscratch=backups\n\n[mappings:mew]\n^W\t=proj_cmd\n")

	t.Chdir(inner)
	m := &Manager{configDir: userDir, configPath: filepath.Join(userDir, "editor.conf")}
	cfg, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}

	// General fields cascade: outer overrode tabSize, inner overrode syntax.
	if cfg.General.TabSize != 5 {
		t.Fatalf("tabSize = %d, want 5 (outer project)", cfg.General.TabSize)
	}
	if cfg.General.Syntax != "cpp" {
		t.Fatalf("syntax = %q, want cpp (inner project)", cfg.General.Syntax)
	}

	// Colors merge per key: the user's text color survives the outer layer's
	// messages color.
	if cfg.Colors.Global["text"] != "USER" || cfg.Colors.Global["messages"] != "OUTER" {
		t.Fatalf("colors should merge across layers: %v", cfg.Colors.Global)
	}

	// Mappings: the user layer replaced the builtin map (existing
	// semantics); project layers merge on top.
	if cfg.Mappings["^Q"] != "user_cmd" || cfg.Mappings["^W"] != "proj_cmd" {
		t.Fatalf("mappings should merge project over user: %v", cfg.Mappings)
	}
	if _, hasBuiltin := cfg.Mappings["^T"]; hasBuiltin {
		t.Fatal("user layer defines the keymap; builtins must not resurrect")
	}

	// Formats merged from the outer project over the defaults.
	if cfg.Formats["foo"] != "go" || cfg.Formats["c"] != "cpp" {
		t.Fatalf("formats should merge over defaults: foo=%q c=%q", cfg.Formats["foo"], cfg.Formats["c"])
	}

	// Relative scratch resolves against ITS project's .mew directory.
	if want := filepath.Join(inner, ".mew", "backups"); cfg.Storage.Scratch != want {
		t.Fatalf("scratch = %q, want %q", cfg.Storage.Scratch, want)
	}

	// ProjectDirs records the applied layers, outermost first.
	n := len(cfg.ProjectDirs)
	if n < 2 || cfg.ProjectDirs[n-2] != filepath.Join(outer, ".mew") || cfg.ProjectDirs[n-1] != filepath.Join(inner, ".mew") {
		t.Fatalf("ProjectDirs: %v", cfg.ProjectDirs)
	}
}

// projectConfig=false in the user layer disables the cascade entirely.
func TestProjectConfigDisabled(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "home", ".mew")
	proj := filepath.Join(root, "proj")

	writeFile(t, filepath.Join(userDir, "editor.conf"),
		"[general]\nprojectConfig=false\n\n[options]\ntabSize=3\n")
	writeFile(t, filepath.Join(proj, ".mew", "editor.conf"),
		"[options]\ntabSize=9\n")

	t.Chdir(proj)
	m := &Manager{configDir: userDir, configPath: filepath.Join(userDir, "editor.conf")}
	cfg, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.TabSize != 3 {
		t.Fatalf("tabSize = %d, want 3 (project layer disabled)", cfg.General.TabSize)
	}
	if len(cfg.ProjectDirs) != 0 {
		t.Fatalf("no project dirs should be recorded when disabled: %v", cfg.ProjectDirs)
	}
}

// A project directory without an editor.conf still joins ProjectDirs (its
// syntax/ folder is a resource even without config).
func TestProjectDirWithoutConf(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "home", ".mew")
	proj := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(userDir, "editor.conf"), "[general]\n")
	if err := os.MkdirAll(filepath.Join(proj, ".mew", "syntax"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(proj)
	m := &Manager{configDir: userDir, configPath: filepath.Join(userDir, "editor.conf")}
	cfg, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cfg.ProjectDirs); n == 0 || cfg.ProjectDirs[n-1] != filepath.Join(proj, ".mew") {
		t.Fatalf("conf-less project dir should still register: %v", cfg.ProjectDirs)
	}
}
