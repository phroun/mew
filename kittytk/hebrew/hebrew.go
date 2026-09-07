// Package hebrew folds Hebrew combining points into the single
// Alphabetic-Presentation-Form glyph that carries them, so a terminal that
// mispositions a free-standing point (drawing a dagesh, dot or rafe a cell off
// its base) renders the letter correctly.
//
// The folding itself is khatool's, beside the Arabic shaping that does the same
// job for the other script; this package is the name KittyTK already had for
// it. New code can call khatool directly.
package hebrew

import "github.com/phroun/khatool"

// Folds reports whether r is a Hebrew point that folds into its base's
// presentation form: the dagesh/mapiq, shin dot, sin dot, rafe, or the
// holam-haser-for-vav. Vowels and accents do not fold.
func Folds(r rune) bool { return khatool.Folds(r) }

// PrecomposeCluster folds a Hebrew cluster — a base rune followed by its
// combining marks — for a terminal that mishandles free-standing points. It
// returns the runes to emit: the base with its folding points folded into one
// presentation-form glyph, followed by the vowels/accents that ride normally;
// and ok=true when a fold happened.
func PrecomposeCluster(runes []rune) ([]rune, bool) { return khatool.PrecomposeCluster(runes) }

// ComposedBase folds base + its folding points into the single presentation-form
// glyph, ignoring any vowels. It is PrecomposeCluster's base rune alone — for
// callers that fold the base but handle the vowels themselves (e.g. drift, which
// moves the vowels to another cell). Returns the base unchanged, ok=false, when
// nothing folds.
func ComposedBase(runes []rune) (rune, bool) { return khatool.ComposedBase(runes) }

// dottedCircle is the base an isolated combining mark is anchored on.
const dottedCircle = khatool.MarkAnchor
