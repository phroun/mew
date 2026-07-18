package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// confine must reduce any "mew:" path to a clean, root-relative name that can
// never point above the tree root — a "../" is dropped, not honored.
func TestMewConfineNeverEscapes(t *testing.T) {
	cases := map[string]string{
		"mew:/editor.conf":            "editor.conf",
		"mew:/syntax/go.jsf":          "syntax/go.jsf",
		"mew:editor.conf":             "editor.conf", // scheme without a slash
		"mew:/":                       "",
		"mew:":                        "",
		"mew:/./x":                    "x",
		"mew:/a/../b":                 "b",
		"mew:/../etc/passwd":          "etc/passwd",
		"mew:/../../../../etc/passwd": "etc/passwd",
		"mew:/a/../../../b":           "b",
		"mew:/..":                     "",
		"mew:/../..":                  "",
	}
	for in, want := range cases {
		if got := confine(in); got != want {
			t.Errorf("confine(%q) = %q, want %q", in, got, want)
		}
	}
}

// In local mode the concrete name is always under localRoot, however many
// "../" the caller stuffs into the path.
func TestMewVFSLocalNameConfined(t *testing.T) {
	root := t.TempDir()
	v := &mewVFS{fs: osFileSystem{}, localRoot: filepath.Join(root, ".mew")}
	mewRoot := filepath.Join(root, ".mew")

	for _, p := range []string{
		"mew:/editor.conf",
		"mew:/../../../etc/passwd",
		"mew:/a/../../../../b",
	} {
		got, ok := v.name(p)
		if !ok {
			t.Fatalf("name(%q) not ok", p)
		}
		rel, err := filepath.Rel(mewRoot, got)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("name(%q) = %q escaped root %q (rel %q)", p, got, mewRoot, rel)
		}
	}
}

// Local mode reads and writes real files under <home>/.mew, and a confined
// escape lands inside that tree rather than on the real target.
func TestMewVFSLocalRoundTrip(t *testing.T) {
	home := t.TempDir()
	v := newMewVFS(&Config{HomeDir: home})
	if v.virtual {
		t.Fatal("no MewFS supplied: expected local mode")
	}

	if err := v.WriteFile("mew:/syntax/x.jsf", []byte("body")); err != nil {
		t.Fatal(err)
	}
	// Landed under <home>/.mew, and WriteFile created the parent directory.
	onDisk := filepath.Join(home, ".mew", "syntax", "x.jsf")
	if b, err := os.ReadFile(onDisk); err != nil || string(b) != "body" {
		t.Fatalf("write did not land at %q: %v / %q", onDisk, err, b)
	}
	if b, err := v.ReadFile("mew:/syntax/x.jsf"); err != nil || string(b) != "body" {
		t.Fatalf("round-trip read failed: %v / %q", err, b)
	}

	// A climbing path is confined: it must not create /etc-style files, only a
	// file inside <home>/.mew.
	if err := v.WriteFile("mew:/../../escapee", []byte("z")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".mew", "escapee")); err != nil {
		t.Fatalf("confined write should land in the tree: %v", err)
	}
}

// recFS records the concrete names a virtualizing host is asked for.
type recFS struct {
	files map[string][]byte
	names []string
}

func (r *recFS) ReadFile(name string) ([]byte, error) {
	r.names = append(r.names, name)
	if b, ok := r.files[name]; ok {
		return b, nil
	}
	return nil, os.ErrNotExist
}
func (r *recFS) WriteFile(name string, data []byte) error {
	r.names = append(r.names, name)
	if r.files == nil {
		r.files = map[string][]byte{}
	}
	r.files[name] = data
	return nil
}
func (r *recFS) Glob(string) ([]string, error) { return nil, nil }

// In virtual mode the host receives "mew:/rel" verbatim (scheme intact) and
// always confined — never a real filesystem path and never with a surviving
// "..".
func TestMewVFSVirtualRouting(t *testing.T) {
	fs := &recFS{}
	v := newMewVFS(&Config{MewFS: fs})
	if !v.virtual {
		t.Fatal("MewFS supplied: expected virtual mode")
	}

	_ = v.WriteFile("mew:/editor.conf", []byte("x"))
	_, _ = v.ReadFile("mew:/../../../etc/passwd")

	for _, n := range fs.names {
		if !strings.HasPrefix(n, "mew:/") {
			t.Errorf("host was handed a non-scheme name: %q", n)
		}
		if strings.Contains(n, "..") {
			t.Errorf("host was handed an unconfined name: %q", n)
		}
	}
	if got := fs.names[len(fs.names)-1]; got != "mew:/etc/passwd" {
		t.Errorf("climbing read = %q, want mew:/etc/passwd", got)
	}
}

// newMewVFS selects virtual mode iff a MewFS is supplied, and resolves the
// local root against the host home override.
func TestNewMewVFSSelection(t *testing.T) {
	if v := newMewVFS(&Config{HomeDir: "/opt/u"}); v.virtual || v.localRoot != filepath.Join("/opt/u", ".mew") {
		t.Fatalf("home override: virtual=%v root=%q", v.virtual, v.localRoot)
	}
	if v := newMewVFS(&Config{MewFS: &recFS{}}); !v.virtual {
		t.Fatal("MewFS supplied: expected virtual")
	}
}

// The host may override the identity mew stamps into locks; empty fields fall
// back to the OS values.
func TestIdentityOverride(t *testing.T) {
	e := &Editor{Config: Config{
		IdentityUser: "jeffd",
		IdentityHost: "Desdemona.local",
		IdentityPID:  4242,
	}}
	if got, want := e.lockOwnerString(), "jeffd@Desdemona.local.4242"; got != want {
		t.Fatalf("lockOwnerString = %q, want %q", got, want)
	}
	if got := e.identityHost(); got != "Desdemona.local" {
		t.Fatalf("identityHost = %q, want Desdemona.local", got)
	}

	// A partial override keeps the overridden host but fills user/pid from the OS.
	partial := &Editor{Config: Config{IdentityHost: "vhost"}}
	owner := partial.lockOwnerString()
	if !strings.Contains(owner, "@vhost.") {
		t.Fatalf("partial override should keep host: %q", owner)
	}
}
