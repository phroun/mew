package config

import (
	"os"
	"runtime"
	"strings"
	"sync"
)

// Environment hints let one keymap describe every platform it runs on.
//
// A hint is a word in parentheses anywhere in a mapping's KEY text — before the
// keys, after them, or between the keys of a chord. All three of these say the
// same thing:
//
//	(mac) s-w        = window_close
//	s-w (mac)        = window_close
//	^B (mac) O       = window_close
//
// What a hint does depends on which family it is from:
//
//   - A plain hint (mac, wnt, linux, unix, gfx, con, and the Linux desktops:
//     kde, gnome, xfce, cinnamon, mate, lxqt, budgie, pantheon, deepin, sway,
//     hyprland, cosmic, i3, unity, enlightenment) is a PREFERENCE, and only a
//     preference. The binding is bound everywhere; where the hint matches it is
//     promoted among the keys for its command, and where it does not it is
//     demoted. Both keys work on both platforms — the hint decides which one a
//     key badge ADVERTISES, so a Mac shows the Command spelling and everything
//     else shows the Control one, out of one keymap.
//
//   - only_ and never_ are a REQUIREMENT. Where they do not match, the line is
//     dropped as it is read: nothing is bound, and no layer inherits or
//     overrides it, exactly as if it had not been written.
//
// non_ negates a plain hint (non_mac is "anywhere but a Mac"), and never_ is
// the requiring form of the same negation (never_mac). So there are four forms
// of every environment: mac, non_mac, only_mac, never_mac.
//
// Several hints on one line all apply: each must be satisfied for a requiring
// hint to keep the binding, and their preferences add up, so a line hinted
// (mac) (gfx) outranks one hinted (mac) alone on a Mac running a graphical
// host.
//
// A hint is a WHOLE token, so a parenthesis is still a key: `(`, `)` and `^(`
// bind as they always did. This is the same rule the key sequence processor
// applies to its own (capture)/(override) words, which mew leaves in the key
// text untouched — each reader takes the words it owns.
const (
	hintOnly  = "only_"
	hintNever = "never_"
	hintNon   = "non_"
)

// A KeymapEnvironment is what environment hints are tested against.
type KeymapEnvironment struct {
	// OS is a runtime.GOOS value ("darwin", "windows", "linux", ...).
	OS string
	// Graphical is true for a host drawing to a pixel surface (gfx) and false
	// for one drawing to a terminal (con). No binary is both, so it comes from
	// the build rather than from anything mew can ask at runtime.
	Graphical bool
	// Desktop is the desktop environment, as XDG_CURRENT_DESKTOP spells it:
	// lowercased, and possibly several colon-separated names ("ubuntu:gnome").
	// [window] host_type in the config overrides it; it is empty where there is
	// no such thing to ask (a Mac, a bare tty).
	Desktop string
}

var (
	keyEnvMu sync.RWMutex
	keyEnv   = KeymapEnvironment{
		OS:        runtime.GOOS,
		Graphical: hostIsGraphical,
		Desktop:   detectDesktop(),
	}
)

// detectDesktop reads the desktop environment the session belongs to, the way
// every Linux desktop advertises it. DESKTOP_SESSION is the older spelling and
// is consulted only when the newer one says nothing.
func detectDesktop() string {
	for _, name := range []string{"XDG_CURRENT_DESKTOP", "DESKTOP_SESSION"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

// SetKeymapEnvironment declares the environment hints are tested against.
// Hints are evaluated as a mapping line is READ, so a binding already dropped
// (or already ranked) does not reconsider itself later: anything that wants to
// change the answer has to say so before the config is parsed.
//
// A blank OS or Desktop keeps the detected one, so a caller that only wants to
// set one field need not repeat what the process already knows.
func SetKeymapEnvironment(env KeymapEnvironment) {
	keyEnvMu.Lock()
	defer keyEnvMu.Unlock()
	if env.OS == "" {
		env.OS = runtime.GOOS
	}
	if env.Desktop == "" {
		env.Desktop = detectDesktop()
	}
	keyEnv = env
}

// CurrentKeymapEnvironment reports the environment hints are tested against.
func CurrentKeymapEnvironment() KeymapEnvironment {
	keyEnvMu.RLock()
	defer keyEnvMu.RUnlock()
	return keyEnv
}

// unixLike are the GOOS values that are a Unix of some description. macOS is
// one of them: a hint of (unix) covers a Mac, and (mac) is how a keymap says
// the Mac specifically.
var unixLike = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true,
	"freebsd": true, "illumos": true, "ios": true, "linux": true,
	"netbsd": true, "openbsd": true, "solaris": true,
}

// hintTests is the environment vocabulary. Each answers "is this that kind of
// environment?"; the only_/never_/non_ forms are built from these.
//
// wnt is Windows NT, spelled the way the family is actually named.
var hintTests = map[string]func(KeymapEnvironment) bool{
	"mac":   func(e KeymapEnvironment) bool { return e.OS == "darwin" },
	"wnt":   func(e KeymapEnvironment) bool { return e.OS == "windows" },
	"linux": func(e KeymapEnvironment) bool { return e.OS == "linux" },
	"unix":  func(e KeymapEnvironment) bool { return unixLike[e.OS] },
	"gfx":   func(e KeymapEnvironment) bool { return e.Graphical },
	"con":   func(e KeymapEnvironment) bool { return !e.Graphical },
}

// desktopAliases maps the names a session actually advertises onto the hint
// vocabulary, because they do not all call themselves what people call them:
// KDE announces Plasma, Cinnamon announces X-Cinnamon, and a distribution may
// prepend its own name ("ubuntu:GNOME").
var desktopAliases = map[string]string{
	"plasma":          "kde",
	"kde-plasma":      "kde",
	"x-cinnamon":      "cinnamon",
	"gnome-flashback": "gnome",
	"gnome-classic":   "gnome",
	"pop":             "cosmic",
	"elementary":      "pantheon",
}

// desktopNames are the desktop environments a keymap can name. Popularity, not
// completeness: an unrecognized word in parentheses is reported to the startup
// log rather than quietly matching nothing, so a missing one is a message in a
// buffer rather than a binding that silently never applies.
var desktopNames = []string{
	"kde", "gnome", "xfce", "cinnamon", "mate", "lxqt", "budgie",
	"pantheon", "deepin", "sway", "hyprland", "cosmic", "i3", "unity",
	"enlightenment",
}

func init() {
	for _, name := range desktopNames {
		want := name // one binding per closure
		hintTests[want] = func(e KeymapEnvironment) bool { return desktopIs(e, want) }
	}
}

// desktopIs reports whether the session belongs to the named desktop.
// XDG_CURRENT_DESKTOP is a colon-separated LIST, most specific first, so every
// entry is considered and each is resolved through the alias table.
func desktopIs(e KeymapEnvironment, want string) bool {
	for _, tok := range strings.Split(e.Desktop, ":") {
		tok = strings.TrimSpace(tok)
		if alias, ok := desktopAliases[tok]; ok {
			tok = alias
		}
		if tok == want {
			return true
		}
	}
	return false
}

// foreignMetaWords are parenthesized words that belong to a LOWER layer and
// pass through mew untouched. The key sequence processor owns its precedence
// words; mew must neither strip them (the level would be lost) nor complain
// about them (they are not typos).
var foreignMetaWords = map[string]bool{
	"capture":  true,
	"override": true,
}

// metaWord returns the word inside a whole parenthesized token, or "" for a
// token that is a key. "()" is a key: a metadata token has something between
// its parentheses. This is deliberately the same rule keyseq applies, because
// a token that is metadata to one reader and a keystroke to the other is the
// bug the notation exists to prevent.
func metaWord(tok string) string {
	if len(tok) > 2 && tok[0] == '(' && tok[len(tok)-1] == ')' {
		return tok[1 : len(tok)-1]
	}
	return ""
}

// evalHint reads one hint word. ok is false for a word that is not a hint at
// all, which leaves the token in the key text where it was.
func evalHint(word string, env KeymapEnvironment) (weight int, keep, ok bool) {
	word = strings.ToLower(strings.TrimSpace(word))
	name, requiring, negated := word, false, false
	switch {
	case strings.HasPrefix(word, hintOnly):
		name, requiring = strings.TrimPrefix(word, hintOnly), true
	case strings.HasPrefix(word, hintNever):
		name, requiring, negated = strings.TrimPrefix(word, hintNever), true, true
	case strings.HasPrefix(word, hintNon):
		name, negated = strings.TrimPrefix(word, hintNon), true
	}
	test, ok := hintTests[name]
	if !ok {
		return 0, true, false
	}
	matches := test(env)
	if negated {
		matches = !matches
	}
	switch {
	case matches:
		return 1, true, true // this environment's own spelling: prefer it
	case requiring:
		return 0, false, true // some other environment's: not bound here at all
	default:
		return -1, true, true // some other environment's: bound, but not shown
	}
}

// KeyHints strips mew's environment hints from a mapping's key text and reports
// what they said: the key as it is written for everything BELOW mew (level
// words and all), how far the binding is promoted (positive) or demoted
// (negative) among the keys for its command, whether it belongs in this
// environment at all, and any parenthesized word nobody claimed.
//
// An unclaimed word is left in the key text rather than dropped, so a typo
// produces a binding under a key text nothing will press — visibly wrong —
// instead of a hint that silently matched nothing. It is also reported, which
// is what the startup log says out loud.
func KeyHints(raw string, env KeymapEnvironment) (key string, weight int, keep bool, unknown []string) {
	keep = true
	var out []string
	for _, tok := range strings.Fields(raw) {
		word := metaWord(tok)
		if word == "" || foreignMetaWords[strings.ToLower(word)] {
			out = append(out, tok)
			continue
		}
		w, k, ok := evalHint(word, env)
		if !ok {
			unknown = append(unknown, word)
			out = append(out, tok)
			continue
		}
		weight += w
		keep = keep && k
	}
	return strings.Join(out, " "), weight, keep, unknown
}
