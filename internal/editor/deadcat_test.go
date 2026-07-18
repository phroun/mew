package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A standalone dump writes the full modified-buffer content to the private
// config target and leaves a discoverable breadcrumb in the working directory.
func TestDeadcatConfigDumpAndBreadcrumb(t *testing.T) {
	e, _ := newTestEditor(t, "line one\n")
	typeText(t, e, "EDIT ") // make the buffer modified

	cfgDir := t.TempDir()
	workDir := t.TempDir()
	e.deadcat = deadcatPlan{
		configTarget: filepath.Join(cfgDir, "DEADCAT"),
		cwd:          workDir,
	}

	path, err := e.DumpDeadcat("boom")
	if err != nil {
		t.Fatalf("DumpDeadcat: %v", err)
	}
	if path != e.deadcat.configTarget {
		t.Fatalf("returned path %q, want config target %q", path, e.deadcat.configTarget)
	}

	full := readFile(t, e.deadcat.configTarget)
	for _, want := range []string{
		"*** These modified files were found in mew when it aborted on",
		"*** (boom)",
		"*** File '",
		"EDIT line one",
	} {
		if !strings.Contains(full, want) {
			t.Fatalf("config DEADCAT missing %q:\n%s", want, full)
		}
	}

	crumb := readFile(t, filepath.Join(workDir, "DEADCAT"))
	if !strings.Contains(crumb, "See DEADCAT in your mew configuration folder for details.") {
		t.Fatalf("cwd breadcrumb wrong:\n%s", crumb)
	}
	if strings.Contains(crumb, "EDIT line one") {
		t.Fatal("cwd breadcrumb must NOT contain the buffer content")
	}
}

// When the config target can't be written, the full dump falls back to the
// working directory (no breadcrumb — the fallback IS the full dump).
func TestDeadcatConfigFailFallsBackToCwd(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	typeText(t, e, "z")

	workDir := t.TempDir()
	e.deadcat = deadcatPlan{
		configTarget: filepath.Join(t.TempDir(), "no-such-dir", "DEADCAT"), // parent missing
		cwd:          workDir,
	}

	path, err := e.DumpDeadcat("fallback")
	if err != nil {
		t.Fatalf("DumpDeadcat: %v", err)
	}
	cwdPath := filepath.Join(workDir, "DEADCAT")
	if path != cwdPath {
		t.Fatalf("returned %q, want cwd fallback %q", path, cwdPath)
	}
	if !strings.Contains(readFile(t, cwdPath), "*** File '") {
		t.Fatal("cwd fallback should hold the full dump")
	}
	if _, err := os.Stat(e.deadcat.configTarget); !os.IsNotExist(err) {
		t.Fatal("config target should not have been created")
	}
}

// Repeated dumps append (a crash loop accumulates rather than clobbers).
func TestDeadcatAppends(t *testing.T) {
	e, _ := newTestEditor(t, "a\n")
	typeText(t, e, "m")
	cfgDir := t.TempDir()
	e.deadcat = deadcatPlan{configTarget: filepath.Join(cfgDir, "DEADCAT"), cwd: t.TempDir()}

	if _, err := e.DumpDeadcat("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DumpDeadcat("second"); err != nil {
		t.Fatal(err)
	}
	full := readFile(t, e.deadcat.configTarget)
	if strings.Count(full, "aborted on") != 2 {
		t.Fatalf("expected two appended dumps:\n%s", full)
	}
}

// No modified buffers → nothing to rescue, nothing written.
func TestDeadcatNoModifiedBuffers(t *testing.T) {
	e, _ := newTestEditor(t, "clean\n") // loaded, not modified
	cfgDir := t.TempDir()
	e.deadcat = deadcatPlan{configTarget: filepath.Join(cfgDir, "DEADCAT"), cwd: t.TempDir()}

	path, err := e.DumpDeadcat("nothing")
	if err != nil || path != "" {
		t.Fatalf("clean buffers should dump nothing: path=%q err=%v", path, err)
	}
	if _, err := os.Stat(e.deadcat.configTarget); !os.IsNotExist(err) {
		t.Fatal("no DEADCAT should be written for an unmodified session")
	}
}

// An unnamed modified buffer is labeled '(unnamed)'.
func TestDeadcatUnnamedBuffer(t *testing.T) {
	e, _ := newTestEditor(t, "")
	typeText(t, e, "scratch work")
	cfgDir := t.TempDir()
	e.deadcat = deadcatPlan{configTarget: filepath.Join(cfgDir, "DEADCAT"), cwd: t.TempDir()}

	if _, err := e.DumpDeadcat(""); err != nil {
		t.Fatal(err)
	}
	full := readFile(t, e.deadcat.configTarget)
	if !strings.Contains(full, "*** File '(unnamed)'") || !strings.Contains(full, "scratch work") {
		t.Fatalf("unnamed dump wrong:\n%s", full)
	}
}

// Module mode routes the dump through the host FileSystem, appending.
func TestDeadcatHostFS(t *testing.T) {
	e, _ := newTestEditor(t, "h\n")
	typeText(t, e, "H")
	host := &fakeHostFS{files: map[string][]byte{}}
	e.FS = host
	e.deadcat = deadcatPlan{useHost: true, hostName: "DEADCAT"}

	path, err := e.DumpDeadcat("host shutdown")
	if err != nil {
		t.Fatalf("DumpDeadcat: %v", err)
	}
	if path != "DEADCAT" {
		t.Fatalf("host path %q, want DEADCAT", path)
	}
	got := string(host.files["DEADCAT"])
	if !strings.Contains(got, "Hh") && !strings.Contains(got, "*** File '") {
		t.Fatalf("host dump wrong:\n%s", got)
	}
	// Append through the host FS too.
	if _, err := e.DumpDeadcat("again"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(host.files["DEADCAT"]), "aborted on") != 2 {
		t.Fatal("host dumps should append")
	}
}

// The launch notice surfaces a transient when a prior DEADCAT is present.
func TestDeadcatLaunchNotice(t *testing.T) {
	e, _ := newTestEditor(t, "")
	cfgDir := t.TempDir()
	target := filepath.Join(cfgDir, "DEADCAT")
	os.WriteFile(target, []byte("*** old crash\n"), 0o644)
	e.deadcat = deadcatPlan{configTarget: target, cwd: t.TempDir()}

	e.deadcatLaunchNotice()
	found := false
	for _, w := range e.WindowManager.AllWindows() {
		if strings.Contains(w.MessageTopInner, "DEADCAT") {
			found = true
		}
	}
	if !found {
		t.Fatal("a DEADCAT-present launch should raise a transient notice")
	}
}
