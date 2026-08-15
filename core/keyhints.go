package core

import (
	"os"
	"runtime"
	"strings"
	"sync"
)

// Environment hints let one keymap describe every platform it runs on.
//
// A hint is a word in parentheses anywhere in a mapping's KEY text, before it,
// after it, or between the keys of a sequence — all three of these say the same
// thing:
//
//	(mac) s-w      = window_close
//	s-w (mac)      = window_close
//	^B (only_mac) O = whatever
//
// What a hint does depends on which family it is from:
//
//   - A plain hint (mac, wnt, linux, unix, gfx, con, and the Linux desktops:
//     kde, gnome, xfce, cinnamon, mate, lxqt, budgie, pantheon, deepin, sway,
//     hyprland, cosmic, i3, unity, enlightenment) is a PREFERENCE. The
//     binding exists everywhere; where the hint matches it is promoted above
//     the unhinted bindings of the same command, and where it does not it is
//     demoted below them. Both keys work on both platforms — the hint only
//     decides which one a menu ADVERTISES, so a Mac shows the Command-key
//     spelling and everything else shows the Control one, from one table.
//
//   - only_ and never_ are a REQUIREMENT. Where they do not match, the binding
//     is not bound at all: it enters no context, resolves nothing, and cannot
//     be advertised, exactly as if the line had not been written.
//
// non_ negates a plain hint (non_mac is "anywhere but a Mac"), and never_ is
// the requiring form of the same negation (never_mac). So there are four forms
// of every environment: mac, non_mac, only_mac, never_mac.
//
// Several hints on one line all apply: each must be satisfied for a requiring
// hint to keep the binding, and their preferences add up, so a line hinted
// (mac) (gfx) outranks one hinted (mac) alone on a Mac running a graphical
// host.
const (
	hintOnly  = "only_"
	hintNever = "never_"
	hintNon   = "non_"
)

// A KeymapEnvironment is what environment hints are tested against: which
// operating system this is, and whether the host paints pixels or characters.
// Its zero value is not useful — read CurrentKeymapEnvironment instead, which
// starts from the OS the binary was built for.
type KeymapEnvironment struct {
	// OS is a runtime.GOOS value ("darwin", "windows", "linux", ...).
	OS string
	// Graphical is true for a host drawing to a pixel surface (gfx) and false
	// for one drawing to a terminal (con). A toolkit cannot detect this for
	// itself — the same binary can be either — so the host declares it.
	Graphical bool
	// Desktop is the desktop environment, as XDG_CURRENT_DESKTOP spells it:
	// lowercased, and possibly several colon-separated names ("ubuntu:gnome").
	// It is what the kde / gnome / xfce family of hints is tested against, and
	// is empty where there is no such thing to ask (a Mac, a bare tty).
	Desktop string
}

var (
	keyEnvMu sync.RWMutex
	keyEnv   = KeymapEnvironment{OS: runtime.GOOS, Desktop: detectDesktop()}
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

// SetKeymapEnvironment declares the environment keymap hints are tested
// against. A graphical host calls it at startup, before it builds anything:
// hints are evaluated when a binding is ADDED to a registry, so a binding
// already rejected (or already ranked) does not reconsider itself later.
//
// An empty OS or Desktop keeps the detected one, so a host that only wants to
// declare itself graphical need not repeat what the process already knows.
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
// completeness: an unrecognized word in parentheses is left in the key text
// rather than silently matching nothing, so a missing one is a compile-time
// surprise in a keymap rather than a binding that quietly never applies.
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

// evalHint reads one hint word. ok is false for a word that is not a hint at
// all, which leaves the parentheses in the key text where they were — a key
// really can be "(" and a keymap really can bind it.
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
		return 1, true, true // this environment's own spelling: show it
	case requiring:
		return 0, false, true // some other environment's: not bound here at all
	default:
		return -1, true, true // some other environment's: bound, but not shown
	}
}

// keyHints strips the environment hints from a mapping's key text and reports
// what they said: the key as it is actually pressed, how far the binding is
// promoted (positive) or demoted (negative) among the keys for its command,
// and whether it belongs in this environment at all.
func keyHints(raw string, env KeymapEnvironment) (key string, weight int, keep bool) {
	keep = true
	var out strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '(' {
			out.WriteByte(raw[i])
			i++
			continue
		}
		end := strings.IndexByte(raw[i:], ')')
		if end < 0 {
			out.WriteString(raw[i:]) // unclosed: it is just text
			break
		}
		word := raw[i+1 : i+end]
		w, k, ok := evalHint(word, env)
		if !ok {
			out.WriteString(raw[i : i+end+1]) // not a hint: leave it be
			i += end + 1
			continue
		}
		weight += w
		keep = keep && k
		i += end + 1
	}
	return strings.Join(strings.Fields(out.String()), " "), weight, keep
}
