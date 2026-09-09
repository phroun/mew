package display

// The two trees a client's material sits in, what the window says they come to,
// and the one of them that can be thrown away.

import (
	"os"
	"path/filepath"
	"testing"
)

// tempConfig points the config directory at a directory of this test's own, so
// the two trees are built there rather than in the user's own config.
func tempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "kittytk")
}

// put writes a file of n bytes at a path under the config directory.
func put(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestASizeIsShownInAColumnWideEnoughForIt(t *testing.T) {
	for _, c := range []struct {
		bytes int64
		want  string
	}{
		{0, ""},
		{124, "124b"},
		{1023, "1023b"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{10 * 1024, "10K"},
		{24 * 1024, "24K"},
		{1024*1024 + 1024*200, "1.2M"},
		{124 * 1024 * 1024, "124M"},
		{3 * 1024 * 1024 * 1024, "3.0G"},
	} {
		got := storageSize(c.bytes)
		if got != c.want {
			t.Errorf("%d bytes reads as %q, want %q", c.bytes, got, c.want)
		}
		if len(got) > 8 {
			t.Errorf("%d bytes reads as %q, which is wider than the column", c.bytes, got)
		}
	}
}

// The figure is both trees added together: the window asks what a client is
// taking up, not what it is taking up in one of two places the user was never
// told about.
func TestTheFigureCoversDataAndCacheTogether(t *testing.T) {
	root := tempConfig(t)
	put(t, filepath.Join(root, "data", "laptop", "notes"), 600)
	put(t, filepath.Join(root, "cache", "laptop", "thumbs"), 400)

	if got := storageUsed("laptop", ""); got != 1000 {
		t.Errorf("the client is taking up %d bytes, want 1000", got)
	}
}

// A client's figure covers the apps inside it, since their folders sit in its
// folder; an app's figure covers only its own.
func TestAClientsFigureCoversItsApps(t *testing.T) {
	root := tempConfig(t)
	put(t, filepath.Join(root, "data", "laptop", "own"), 100)
	put(t, filepath.Join(root, "data", "laptop", "demo", "bundle"), 200)
	put(t, filepath.Join(root, "cache", "laptop", "demo", "scratch"), 300)

	if got := storageUsed("laptop", ""); got != 600 {
		t.Errorf("the client is taking up %d bytes, want 600", got)
	}
	if got := storageUsed("laptop", "demo"); got != 500 {
		t.Errorf("the app is taking up %d bytes, want 500", got)
	}
}

// A client nothing has ever been stored for is taking up nothing, rather than
// being an error to ask about.
func TestAClientWithNothingStoredTakesUpNothing(t *testing.T) {
	tempConfig(t)
	if got := storageUsed("never-here", ""); got != 0 {
		t.Errorf("a client with no folders is taking up %d bytes", got)
	}
}

// Clearing the cache clears the cache. What is under data/ is the material the
// user has not agreed to lose, and this button does not touch it.
func TestClearingTheCacheLeavesTheDataAlone(t *testing.T) {
	root := tempConfig(t)
	keep := filepath.Join(root, "data", "laptop", "demo", "bundle")
	put(t, keep, 200)
	put(t, filepath.Join(root, "cache", "laptop", "demo", "scratch"), 300)
	put(t, filepath.Join(root, "cache", "laptop", "own"), 50)

	if err := clearCache("laptop", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the app's data went with its cache: %v", err)
	}
	if got := storageUsed("laptop", "demo"); got != 200 {
		t.Errorf("the app is taking up %d bytes after its cache was cleared, want 200", got)
	}
	// One app's cache, not the client's: the button was pressed on the app.
	if got := storageUsed("laptop", ""); got != 250 {
		t.Errorf("the client is taking up %d bytes, want 250", got)
	}

	if err := clearCache("laptop", ""); err != nil {
		t.Fatal(err)
	}
	if got := storageUsed("laptop", ""); got != 200 {
		t.Errorf("clearing the client's cache left %d bytes, want the 200 of data", got)
	}
}

// Renaming a client moves its folder in both trees, so the material it had
// before the rename is the material it has after.
func TestRenamingAClientMovesItsMaterial(t *testing.T) {
	root := tempConfig(t)
	put(t, filepath.Join(root, "data", "old-name", "demo", "bundle"), 200)
	put(t, filepath.Join(root, "cache", "old-name", "scratch"), 300)

	if err := renameStorage("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	if got := storageUsed("new-name", ""); got != 500 {
		t.Errorf("the renamed client is taking up %d bytes, want 500", got)
	}
	if got := storageUsed("old-name", ""); got != 0 {
		t.Errorf("%d bytes were left behind under the old name", got)
	}
	if got := storageUsed("new-name", "demo"); got != 200 {
		t.Errorf("the app under the renamed client is taking up %d bytes, want 200", got)
	}
}

// A client with nothing in one of the trees does not gain an empty folder there
// by being renamed.
func TestRenamingDoesNotMakeFoldersThatWereNotThere(t *testing.T) {
	root := tempConfig(t)
	put(t, filepath.Join(root, "data", "old-name", "notes"), 100)

	if err := renameStorage("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cache", "new-name")); err == nil {
		t.Error("a cache folder was made for a client that had never cached anything")
	}
}
