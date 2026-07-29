package viewport

// ScrollbarThumb computes the vertical scrollbar thumb for a track of trackH
// cells (one per content row) over a document of lineCount lines whose first
// visible line is top. It uses the same proportional formula as the toolkit
// scrollbars (thumbSize = visible² / total, where total = maxScroll + visible),
// with a one-cell minimum. When the whole document fits, the thumb fills the
// track. Both the renderer (painting '░'/'█') and the mouse path (hit-testing
// a press) call this, so the two can never disagree about where the thumb is.
func ScrollbarThumb(trackH, lineCount, top int) (pos, size int) {
	if trackH <= 0 {
		return 0, 0
	}
	maxTop := lineCount - trackH
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
func ScrollbarTopForThumb(pos, trackH, lineCount int) int {
	if trackH <= 0 {
		return 0
	}
	maxTop := lineCount - trackH
	if maxTop <= 0 {
		return 0
	}
	_, size := ScrollbarThumb(trackH, lineCount, 0)
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
