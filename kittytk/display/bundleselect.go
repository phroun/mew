package display

// What an include names, and which includes name the same thing.
//
// An include is bound either to a literal hash or to a version expression, and
// the two are different kinds of binding rather than two spellings of one:
//
//   - A PIN names a resolved bundle. It never moves.
//   - A RANGE names the newest bundle satisfying it. It moves when the store's
//     newest does.
//
// Selection is NEWEST-WINS: an include takes the highest version the store
// holds under its key, and fails if that is below its floor. The floor is a
// check on the answer rather than part of the question, which is what lets
// `>= 0.1.0` and `>= 0.1.2` be one binding -- they select the same thing and
// differ only in what they will accept.
//
// So a SELECTOR is the key and the upper bound, and nothing else. Two includes
// share a source when their selectors match, whatever their floors, which makes
// the common case -- everybody wanting the newest -- shared by construction.
// Divergence is opt-in: it takes an upper bound or a pin to get two of
// something.
//
// That identity is what a later live binding will be told about. When a new
// version lands, what changed is the SELECTOR's answer, not any bundle, so one
// notice reaches every include that shares it and each re-checks its own floor.

import (
	"fmt"
	"strconv"
	"strings"
)

// A selector is what an include selects by, and what two includes must agree on
// to share a source. The floor is deliberately absent.
type selector struct {
	key   string // the bundle's name, or a registered source's
	upper string // the version it must stay below, or "" for the newest there is
	pin   string // an exact version or a hash, when it names one bundle for good
	live  bool   // a source registered by name rather than a bundle in the store
}

func (s selector) String() string {
	if s.live {
		// Its own namespace: a bundle called `inbox` and a source called
		// `inbox` are two things, and an include says which it meant.
		return "source:" + s.key
	}
	switch {
	case s.pin != "":
		return s.key + "@" + s.pin
	case s.upper != "":
		return s.key + "@<" + s.upper
	}
	return s.key + "@latest"
}

// A want is one include's binding: what it selects, and what it will accept.
type want struct {
	selector
	floor    string // the version it will not go below, or ""
	optional bool
}

// hashWanted reports whether this names a bundle by its content.
func (w want) hashWanted() bool { return len(w.pin) == 64 && isHex(w.pin) }

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return s != ""
}

// parseWant reads what an include is bound to: comma-separated terms, in any
// order.
//
//	"deadbeef…"                 a hash: one bundle, for good
//	"0.1.0"                     a version: one bundle, for good
//	">= 0.1.0"                  the newest, and not below 0.1.0
//	">= 0.1.0, < 0.2.0"         the newest below 0.2.0, and not below 0.1.0
//	"1.4.2, optional"           and an absence this author can live with
//
// A bare version is a PIN rather than a floor, because an author who writes one
// and means "or newer" has `>=` to say it with, and one who writes one and means
// it exactly has nothing else.
func parseWant(key, text string) (want, error) {
	w := want{selector: selector{key: key}}
	if strings.TrimSpace(text) == "" {
		return w, fmt.Errorf("this include says nothing about which %s it wants", key)
	}
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
			continue
		case part == optionalTerm:
			w.optional = true
		case strings.HasPrefix(part, ">="):
			w.floor = strings.TrimSpace(part[2:])
		case strings.HasPrefix(part, "<"):
			w.upper = strings.TrimSpace(part[1:])
		default:
			if w.pin != "" {
				return want{}, fmt.Errorf("%q names two versions and can have one", text)
			}
			w.pin = part
		}
	}
	switch {
	case w.pin != "" && (w.floor != "" || w.upper != ""):
		return want{}, fmt.Errorf(
			"%q pins a version and bounds one; a pin has nothing to range over", text)
	case w.pin == "" && w.floor == "" && w.upper == "":
		return want{}, fmt.Errorf("%q names no version at all", text)
	}
	return w, nil
}

// optionalTerm is how an expression says an absence is survivable. It reads the
// same wherever an expression is written, and the list form has a member of its
// own for where there is no expression to put it in.
const optionalTerm = "optional"

// admits reports whether a version is one this want will take. The upper bound
// took part in choosing it; the floor is checked here, on the answer.
func (w want) admits(version string) bool {
	if w.upper != "" && compareVersions(version, w.upper) >= 0 {
		return false
	}
	return w.floor == "" || compareVersions(version, w.floor) >= 0
}

// compareVersions orders two version strings: -1, 0 or 1.
//
// Dotted parts, compared as NUMBERS where both are numbers so that 0.1.10 is
// above 0.1.9, and as text otherwise so that anything else still has an order.
// A version with fewer parts is the lower where the rest agree: 0.1 is below
// 0.1.1, because a release with something after it came later.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if c := comparePart(x, y); c != 0 {
			return c
		}
	}
	return 0
}

func comparePart(x, y string) int {
	xn, xok := strconv.Atoi(x)
	yn, yok := strconv.Atoi(y)
	if xok == nil && yok == nil {
		switch {
		case xn < yn:
			return -1
		case xn > yn:
			return 1
		}
		return 0
	}
	// An absent part is below anything present, which is what makes 0.1 sort
	// below 0.1.0 rather than beside it.
	switch {
	case x == "" && y != "":
		return -1
	case x != "" && y == "":
		return 1
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
