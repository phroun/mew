package viewport

// ScrollbarThumb computes the vertical scrollbar thumb for a track of trackH
// cells over a document of lineCount lines whose first visible line is top,
// with page visible rows. trackH and page are usually equal (one track cell
// per content row) but diverge when the track loses its bottom cell to the
// screen's unwritable corner — the scroll RANGE always comes from page, the
// thumb's size and travel from trackH. It uses the same proportional formula
// as the toolkit scrollbars (thumbSize = track² / (maxScroll + track)), with
// a one-cell minimum. When the whole document fits, the thumb fills the
// track. Both the renderer (painting '░'/'█') and the mouse path (hit-testing
// a press) call this, so the two can never disagree about where the thumb is.
func ScrollbarThumb(trackH, page, lineCount, top int) (pos, size int) {
	if trackH <= 0 || page <= 0 {
		return 0, 0
	}
	maxTop := lineCount - page
	if maxTop <= 0 {
		return 0, trackH
	}
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
	if trackH <= 0 || page <= 0 {
		return 0
	}
	maxTop := lineCount - page
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
