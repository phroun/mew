// Package bidi binds khatool — the ordering and shaping engine — to mew's own
// cell rule, so every caller in mew asks one question in one place.
//
// The rule is textwidth's: what mew PAINTS as zero-width is what may ride the
// cell in front of it. It has to be that one, because the renderer measures
// the line with the same rule and a reversal made against a different picture
// puts marks on the wrong cells — an ill-formed mark drawn as a spacing
// substitute has a cell of its own, so it reverses on its own.
//
// What the answers ARE is khatool's business and is tested there; that they
// have not MOVED is this package's, and is written down in testdata.
package bidi

import (
	"github.com/phroun/khatool"

	"github.com/phroun/mew/internal/textwidth"
)

// Layout is khatool's own, by alias rather than by conversion, so a consumer's
// field access and marker switches are the same code they always were.
type Layout = khatool.Layout

// Marker slot values that may appear in Layout.Perm when the layout was
// computed with direction markers (showBidi), and the ligature slot that takes
// no cell. See khatool.
const (
	MarkerLTR        = khatool.MarkerLTR
	MarkerRTL        = khatool.MarkerRTL
	MarkerEnd        = khatool.MarkerEnd
	LigatureAbsorbed = khatool.LigatureAbsorbed
)

// Compute returns the visual layout of a line under the base direction, or nil
// when visual order equals logical order.
func Compute(runes []rune, baseRTL bool) *Layout {
	return khatool.Order(runes, baseRTL, textwidth.RidesPreviousCell)
}

// ComputeMarked is Compute with the showBidi direction markers.
func ComputeMarked(runes []rune, baseRTL bool) *Layout {
	return khatool.OrderMarked(runes, baseRTL, textwidth.RidesPreviousCell)
}

// Shape returns the line with each Arabic letter in its contextual
// presentation form, or nil when there is no Arabic in it.
func Shape(runes []rune) []rune { return khatool.Shape(runes) }

// Mirror returns the paired counterpart of a bracket rune, or the rune
// unchanged.
func Mirror(r rune) rune { return khatool.Mirror(r) }

// IsStrongRTL reports whether a rune is strongly right-to-left (bidi classes R
// and AL). The renderer's host-bidi flip asks it of a finished row.
func IsStrongRTL(r rune) bool { return khatool.IsStrongRTL(r) }

// IsDirectionControl reports whether r is an explicit Unicode direction
// control. Under showBidi these render as a visible one-column marker.
func IsDirectionControl(r rune) bool { return khatool.IsDirectionControl(r) }

// RTLAt reports whether the rune at logical index idx sits in an RTL run (the
// registered rtl command).
func RTLAt(runes []rune, idx int, baseRTL bool) bool {
	return khatool.RTLAt(runes, idx, baseRTL)
}
