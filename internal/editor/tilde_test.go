package editor

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// expandTilde resolves ~, ~/rest, and ~user/rest cross-platform.
func TestExpandTilde(t *testing.T) {
	cases := []struct {
		in, home, want string
		ok             bool
	}{
		{"~/foo", "/home/me", "/home/me/foo", true},
		{"~", "/home/me", "/home/me", true},
		{"~/", "/home/me", "/home/me", true},
		{"~/a/b/c", "/home/me", "/home/me/a/b/c", true},
		{"/abs/path", "/home/me", "/abs/path", false},
		{"relative", "/home/me", "relative", false},
		{"", "/home/me", "", false},
		{"~ghost_user_zzz/x", "/home/me", "~ghost_user_zzz/x", false}, // unknown user: unchanged
	}
	for _, c := range cases {
		got, ok := expandTilde(c.in, c.home)
		if got != c.want || ok != c.ok {
			t.Errorf("expandTilde(%q, %q) = (%q,%v), want (%q,%v)", c.in, c.home, got, ok, c.want, c.ok)
		}
	}

	// A named user resolves through os/user; the current process user always
	// exists. Its home comes from the OS, not the currentHome argument.
	if u, err := user.Current(); err == nil && u.Username != "" && u.HomeDir != "" {
		in := "~" + u.Username + "/sub"
		want := filepath.Join(u.HomeDir, "sub")
		if got, ok := expandTilde(in, "/ignored"); !ok || got != want {
			t.Errorf("expandTilde(%q) = (%q,%v), want (%q,true)", in, got, ok, want)
		}
	}
}

// The OS-backed FileSystem expands a leading ~ in Glob (and friends).
func TestOSFileSystemGlobExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	fs := osFileSystem{}
	got, err := fs.Glob("~")
	if err != nil {
		t.Fatalf("Glob(~): %v", err)
	}
	if len(got) != 1 || got[0] != home {
		t.Fatalf("Glob(~) = %v, want [%q]", got, home)
	}
}

// "~/" completion expands to the home directory and lists/fills its contents.
func TestFilenameCompletionTildeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, n := range []string{"hello.txt", "help.txt"} {
		os.WriteFile(filepath.Join(home, n), nil, 0o644)
	}
	e, doc := newTestEditor(t, "")
	if e.home != home {
		t.Fatalf("editor home = %q, want %q", e.home, home)
	}
	// Anchor the prompt somewhere else entirely, so only ~ resolution reaches home.
	anchorPrompt(t, e, doc, t.TempDir())

	typeText(t, e, "~/hel")
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("completion should own the tab")
	}
	if got := promptLine(t, e); got != "~/help.txt" && got != "~/hel" {
		// two matches (hello, help) share "hel"; the fill keeps "~/hel"
		if got != "~/hel" {
			t.Fatalf("~/hel completion = %q, want the shared prefix kept", got)
		}
	}
	if !hasNotification(e, "hello.txt") || !hasNotification(e, "help.txt") {
		t.Fatalf("~/ completion should list the home directory's files")
	}

	// Disambiguate to a single match.
	typeText(t, e, "lo") // "~/hello"
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("second completion should own the tab")
	}
	if got := promptLine(t, e); got != "~/hello.txt" {
		t.Fatalf("~/hello completion = %q, want ~/hello.txt", got)
	}
}

// "~partial" completes user names — sibling home directories — as "~name/".
func TestFilenameCompletionUserName(t *testing.T) {
	root := t.TempDir()
	me := filepath.Join(root, "me")
	os.Mkdir(me, 0o755)
	os.Mkdir(filepath.Join(root, "alice"), 0o755)
	os.Mkdir(filepath.Join(root, "albert"), 0o755)
	os.WriteFile(filepath.Join(root, "alfile"), nil, 0o644) // a file, not a user home
	t.Setenv("HOME", me)

	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, t.TempDir())

	// Insert via the caret rather than the PawScript `insert` helper: `insert
	// "~al"` mis-parses a tilde-word literal (a harness quirk; real char-by-char
	// typing is unaffected).
	fp := focusedPrompt(e)

	// Ambiguous: alice and albert share "~al".
	fp.Caret.Insert("~al")
	if !e.completeFilename(fp) {
		t.Fatal("username completion should own the tab")
	}
	if !hasNotification(e, "~albert/") || !hasNotification(e, "~alice/") {
		t.Fatalf("~al should list the matching user homes")
	}
	if hasNotification(e, "alfile") {
		t.Fatalf("a plain file must not be offered as a user home")
	}

	// Unique: "~ali" completes to "~alice/".
	fp.Caret.Insert("i") // "~ali"
	if !e.completeFilename(fp) {
		t.Fatal("username completion should own the tab")
	}
	if got := promptLine(t, e); got != "~alice/" {
		t.Fatalf("~ali completion = %q, want ~alice/", got)
	}
}
