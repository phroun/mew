package config

import "testing"

func TestPathMatches(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// Component patterns (no slash).
		{"pages", "/srv/dokuwiki/data/pages/start.txt", true},
		{"pages", "/home/u/pages.txt", false}, // basename is pages.txt, not pages
		{"pages", "/home/u/notes/draft.txt", false},
		{"*wiki*", "/home/u/mywiki/notes.txt", true},
		{"*wiki*", "/home/u/DokuWiki/x.txt", true}, // case-insensitive
		{"*wiki*", "/home/u/teamwiki-notes.txt", true},
		{"*wiki*", "/home/u/docs/notes.txt", false},
		// Full-path patterns (with slash): '*' crosses separators.
		{"*/data/pages/*", "/srv/w/data/pages/ns/page.txt", true},
		{"*/data/pages/*", "/srv/w/data/media/x.txt", false},
		{"srv/*", "/also/srv/things", false}, // slash patterns anchor to the whole path
		{"*/srv/*", "/also/srv/things", true},
		// '?' single character.
		{"v?", "/proj/v2/file", true},
		{"v?", "/proj/v22/file", false},
		// Windows separators normalize.
		{"pages", `C:\wiki\data\pages\start.txt`, true},
		// Degenerate inputs.
		{"", "/x", false},
		{"x", "", false},
	}
	for _, c := range cases {
		if got := PathMatches(c.pattern, c.path); got != c.want {
			t.Errorf("PathMatches(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
