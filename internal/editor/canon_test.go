package editor

import "testing"

// A canonical URL's path is always spelled with "/", and turning it back into
// a path for the OS is the one place that changes. On POSIX nothing changes:
// an absolute path already began with "/". On Windows the canonical form put
// a slash in FRONT of the drive letter to root it, and /C:/proj is not a path
// Windows accepts — it reads as a component named "C:" on the current drive.
func TestURLPathForOS(t *testing.T) {
	for _, tc := range []struct {
		in      string
		windows bool
		want    string
	}{
		// POSIX: the canonical path IS the OS path.
		{"/home/user/project/notes.txt", false, "/home/user/project/notes.txt"},
		{"/", false, "/"},

		// Windows: the rooting slash comes off, separators go back.
		{"/C:/Users/me/proj", true, `C:\Users\me\proj`},
		{"/c:/tmp/x.txt", true, `c:\tmp\x.txt`},
		// A UNC path was //server/share before the URL and keeps both slashes.
		{"//server/share/dir", true, `\\server\share\dir`},
		// No drive letter to unroot: separators only.
		{"/opt/thing", true, `\opt\thing`},
		{"/C", true, `\C`},
	} {
		if got := urlPathForOS(tc.in, tc.windows); got != tc.want {
			t.Errorf("urlPathForOS(%q, windows=%v) = %q, want %q",
				tc.in, tc.windows, got, tc.want)
		}
	}
}

// canonicalOSFileURL and urlPathToOS are inverses on this platform: whatever
// spelling goes in, the path that comes back out is the one the OS was given.
// This is the property the whole canonical-identity scheme rests on — two
// spellings of one file compare equal BECAUSE the URL is normalized, which is
// only safe if the normalization can be undone exactly.
func TestCanonicalURLRoundTripsToTheSamePath(t *testing.T) {
	for _, in := range []string{
		"/home/user/project/notes.txt",
		"/tmp/x",
	} {
		url := canonicalOSFileURL(in, "")
		back, ok := LocalPathFromURL(url)
		if !ok {
			t.Fatalf("LocalPathFromURL(%q) reported no local path", url)
		}
		if back != in {
			t.Errorf("%q -> %q -> %q, want the original path back", in, url, back)
		}
	}
}

// A mew:/// document may have no local path at all, so the scheme is checked
// rather than assumed.
func TestLocalPathFromURLRejectsOtherSchemes(t *testing.T) {
	for _, url := range []string{"mew:///help/start.txt", "help:/start.txt", "", "notes.txt"} {
		if p, ok := LocalPathFromURL(url); ok {
			t.Errorf("LocalPathFromURL(%q) = %q, true; want false", url, p)
		}
	}
}
