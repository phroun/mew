package config

import "strings"

// PathMatches reports whether a file path satisfies a path pattern — the
// generic test behind path-conditional configuration ([formats.<ext>]
// sections today; per-tree rules later). Matching is case-insensitive and
// separator-agnostic (backslashes normalize to slashes).
//
// Two pattern shapes:
//   - Without a slash, the pattern is tested against EACH path component:
//     "pages" matches any file inside a folder named pages, "*wiki*" any
//     path with wiki somewhere in a component (including the basename).
//   - With a slash, the pattern is tested against the whole path, with '*'
//     crossing separators: "*/data/pages/*".
//
// Globs support '*' (any run of characters) and '?' (one character).
func PathMatches(pattern, path string) bool {
	pattern = strings.ToLower(strings.ReplaceAll(pattern, "\\", "/"))
	path = strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	if pattern == "" || path == "" {
		return false
	}
	if strings.Contains(pattern, "/") {
		return globMatch(pattern, path)
	}
	for _, comp := range strings.Split(path, "/") {
		if comp != "" && globMatch(pattern, comp) {
			return true
		}
	}
	return false
}

// globMatch is a simple glob: '*' matches any run (including separators),
// '?' any single rune, everything else literally.
func globMatch(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	// Iterative wildcard matching with backtracking to the last '*'.
	pi, ti := 0, 0
	star, starTi := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star, starTi = pi, ti
			pi++
		case star >= 0:
			starTi++
			pi, ti = star+1, starTi
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
