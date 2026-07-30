package viewport

// The vertical scroll range, and the scrollbar drawn over it.
//
// Two rules live here, and they answer to each other:
//
//   - How far down a viewport may scroll. Far enough to put the document's LAST
//     line on the bottom row, and no further — a document does not get to drift
//     off the top leaving an empty window behind it. More than one blank line is
//     visible only when the document is shorter than the viewport, where there
//     is nothing to scroll at all.
//
//   - Whether there is a thumb. Only when the document is taller than the
//     viewport, which is exactly when the first rule permits any scrolling.
//
// Both are single functions because several places consult them — the wheel, the
// drag-autoscroll, every scroll_* command, the renderer painting the bar, the
// mouse hit-testing it, and a graphical host drawing its own — and a scrollbar
// whose geometry disagreed with the scrolling it drives is the kind of bug that
// shows up as a thumb that will not reach the end of its track.

// MaxScrollTop is the highest first-visible line a viewport of page rows may be
// parked at over a document of lineCount lines: the one that puts the document's
// LAST line on the bottom row. A document that fits its viewport does not scroll
// at all and gets 0 — the blank space below it is already visible, and scrolling
// into it would only push the text off the top.
//
// That is where "one blank line past the end" comes from, without needing a row
// of slack added here: a text file ends with a newline, and mew counts the empty
// line after it as a line of the document (the caret can sit there), so the
// bottom row at maximum scroll IS that blank line. Adding one more would show
// two. A file with no trailing newline shows none, which is the honest answer for
// a document that genuinely has no empty last line.
//
// page <= 0 means the layout has not run yet; there is no page to reason about,
// so the answer is the widest that can never be wrong.
func MaxScrollTop(page, lineCount int) int {
	if page <= 0 {
		if lineCount > 0 {
			return lineCount - 1
		}
		return 0
	}
	if lineCount <= page {
		return 0
	}
	return lineCount - page
}

// ScrollbarNeeded reports whether a document of lineCount lines has anything to
// scroll in a viewport showing page rows — which is also, exactly, whether the
// bar gets a thumb.
//
// A document that fits gets NO thumb: the bar's area paints as bare track and a
// press on it scrolls nothing. A full-length thumb would be the honest
// proportional answer, but it says "you are looking at all of it" in the same
// visual language a scrollable bar uses for "you are looking at nearly all of
// it", and it invites a drag that cannot move.
func ScrollbarNeeded(page, lineCount int) bool {
	return page > 0 && lineCount > page
}

// ScrollbarThumb computes the vertical scrollbar thumb for a track of trackH
// cells over a document of lineCount lines whose first visible line is top,
// with page visible rows. trackH and page are usually equal (one track cell
// per content row) but diverge when the track loses its bottom cell to the
// screen's unwritable corner — the scroll RANGE always comes from page, the
// thumb's size and travel from trackH. It uses the same proportional formula
// as the toolkit scrollbars (thumbSize = track² / (maxScroll + track)), with
// a one-cell minimum. A size of 0 means there is no thumb at all, which is
// what a document that fits its viewport gets (see ScrollbarNeeded). Both the
// renderer (painting '░'/'█') and the mouse path (hit-testing a press) call
// this, so the two can never disagree about where the thumb is.
func ScrollbarThumb(trackH, page, lineCount, top int) (pos, size int) {
	if trackH <= 0 || !ScrollbarNeeded(page, lineCount) {
		return 0, 0
	}
	// The travel is the scroll range, so the thumb reaches the end of its track
	// exactly when the view reaches the end of the document — including the one
	// blank line MaxScrollTop allows.
	maxTop := MaxScrollTop(page, lineCount)
	size = trackH * trackH / (maxTop + trackH)
	if size < 1 {
		size = 1
	}
	if size > trackH {
		size = trackH
	}
	span := trackH - size
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	if span > 0 {
		pos = top * span / maxTop
	}
	return
}

// ScrollbarTopForThumb inverts ScrollbarThumb: given the thumb's desired track
// position (its top cell, from a drag), it returns the document top line that
// puts the thumb there, rounded to the nearest line and clamped to the valid
// range. The thumb moves cell by cell; only the resulting top line quantizes.
func ScrollbarTopForThumb(pos, trackH, page, lineCount int) int {
	if trackH <= 0 || !ScrollbarNeeded(page, lineCount) {
		return 0
	}
	maxTop := MaxScrollTop(page, lineCount)
	if maxTop <= 0 {
		return 0
	}
	_, size := ScrollbarThumb(trackH, page, lineCount, 0)
	span := trackH - size
	if span <= 0 {
		return 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > span {
		pos = span
	}
	return (pos*maxTop + span/2) / span
}
