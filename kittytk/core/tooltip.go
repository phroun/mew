package core

// Saying more than there is room to show.
//
// A tooltip is ASKED FOR, never placed: the trinket that has more to say than
// it could draw says so and says where, and whoever above it knows where such
// a thing belongs answers -- a window with somewhere of its own to put it, a
// desktop with a status bar or a popup layer. A trinket never paints one, and
// so never has to know which kind of surface it is on.
//
// The offer is made the moment the pointer arrives, with no dwell: what is
// being offered is the rest of a word already under the pointer, and waiting
// to show it would only make the reader wait.

// TooltipSide is where a trinket would like its tooltip put, against the text
// it stands for.
//
// It is a PREFERENCE, not a placement: the handler knows how much room there
// is and has the last word, so a trinket near an edge gets the other side
// rather than a tooltip half off the screen. Auto leaves the choice entirely to
// the handler, which is what most things want.
type TooltipSide int

const (
	TooltipAuto   TooltipSide = iota // the handler decides from the room it has
	TooltipAbove                     // above the text
	TooltipBelow                     // below it
	TooltipBefore                    // on the side the reading starts from
	TooltipAfter                     // on the side it runs to
	// TooltipOver puts it exactly where the text is, running past whatever
	// boundary cut it: a cell reads on in place rather than being repeated
	// somewhere else for the eye to go and find.
	TooltipOver
)

// TooltipRequest is one offer: what to say, who is saying it, the rect the
// text it stands for occupies -- in the asker's own local units, so a handler
// that wants to point at it maps the rect the way it maps anything else -- and
// which side of that rect the asker would like it on.
type TooltipRequest struct {
	Text string
	From Trinket
	At   UnitRect
	Side TooltipSide
}

// TooltipHandler is anything above a trinket that knows where a tooltip goes.
//
// ShowTooltip reports whether it took the request. The first one up the chain
// that takes it ends the walk, so a window that routes tooltips somewhere of
// its own is never second-guessed by the desktop behind it.
type TooltipHandler interface {
	ShowTooltip(req TooltipRequest) bool
	// HideTooltip withdraws whatever was shown for this asker. It is sent to
	// every handler above, not only the one that took the request: the asker
	// does not know which one that was, and withdrawing something nobody is
	// showing is nothing at all.
	HideTooltip(from Trinket)
}

// RequestTooltip offers a tooltip up the chain from req.From, and reports
// whether anything took it.
func RequestTooltip(req TooltipRequest) bool {
	if req.From == nil || req.Text == "" {
		return false
	}
	for t, depth := tooltipParent(req.From), 0; t != nil && depth < maxLayoutWalkDepth; t, depth = tooltipParent(t), depth+1 {
		if h, ok := t.(TooltipHandler); ok && h.ShowTooltip(req) {
			return true
		}
	}
	return false
}

// CancelTooltip withdraws whatever was being shown for this trinket.
func CancelTooltip(from Trinket) {
	if from == nil {
		return
	}
	for t, depth := tooltipParent(from), 0; t != nil && depth < maxLayoutWalkDepth; t, depth = tooltipParent(t), depth+1 {
		if h, ok := t.(TooltipHandler); ok {
			h.HideTooltip(from)
		}
	}
}

// tooltipParent is the next trinket up, following an embedded trinket's host
// where it has no parent -- a tree's cell editor is drawn by the tree, and its
// tooltips belong to the same chain as the tree's own.
func tooltipParent(t Trinket) Trinket { return repaintParent(t) }

// TooltipSource is a trinket that may have something to say at a point.
//
// TrinketBase answers for the whole of a trinket, which is what an ordinary
// caption wants. A trinket with parts of its own -- rows, cells -- answers for
// the part under the pointer instead, since that is what the reader is looking
// at.
type TooltipSource interface {
	TooltipAt(local UnitPoint) (text string, at UnitRect, ok bool)
}

// TooltipSide is where this trinket would like its tooltip put.
func (w *TrinketBase) TooltipSide() TooltipSide {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tooltipSide
}

// SetTooltipSide states a preference for where this trinket's tooltip sits.
// The handler still has the last word, since only it knows the room.
func (w *TrinketBase) SetTooltipSide(s TooltipSide) {
	w.mu.Lock()
	w.tooltipSide = s
	w.mu.Unlock()
}

// Tooltip is what this trinket has been told to say when asked.
func (w *TrinketBase) Tooltip() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tooltip
}

// SetTooltip gives this trinket something to say beyond what it draws. A
// trinket with none still offers the whole of any text it had to cut short.
func (w *TrinketBase) SetTooltip(s string) {
	w.mu.Lock()
	w.tooltip = s
	w.mu.Unlock()
}

// noteCut records what this trinket could not show all of, which is what it
// offers when nothing else was set. Called by the eliding, on every paint.
func (w *TrinketBase) noteCut(text string, whole bool) {
	w.mu.Lock()
	if whole {
		w.cutText = ""
	} else {
		w.cutText = text
	}
	w.mu.Unlock()
}

// TooltipAt is the default answer: what this trinket was told to say, else the
// whole of what it had to cut, offered anywhere over it.
func (w *TrinketBase) TooltipAt(local UnitPoint) (string, UnitRect, bool) {
	w.mu.RLock()
	text, cut := w.tooltip, w.cutText
	w.mu.RUnlock()
	if text == "" {
		text = cut
	}
	if text == "" {
		return "", UnitRect{}, false
	}
	b := w.Bounds()
	return text, UnitRect{Width: b.Width, Height: b.Height}, true
}

// TrackTooltipHover is what a trinket does with a pointer passing over it: it
// offers what it cannot show, and withdraws the offer when the pointer leaves
// or moves to a part with nothing to say.
//
// The point is in this trinket's own local units. A container forwards every
// move to every child, so a trinket hears the pointer leave as surely as it
// hears it arrive, and a point outside its own bounds is that leaving.
func (w *TrinketBase) TrackTooltipHover(local UnitPoint) {
	self := w.Self()
	if self == nil {
		return
	}
	b := w.Bounds()
	inside := local.X >= 0 && local.Y >= 0 && local.X < b.Width && local.Y < b.Height

	var text string
	var at UnitRect
	if inside {
		if src, ok := self.(TooltipSource); ok {
			text, at, _ = src.TooltipAt(local)
		}
	}

	w.mu.Lock()
	same := text == w.tooltipShown
	w.tooltipShown = text
	w.mu.Unlock()
	if same {
		return
	}
	if text == "" {
		CancelTooltip(self)
		return
	}
	RequestTooltip(TooltipRequest{Text: text, From: self, At: at, Side: w.TooltipSide()})
}

// WithdrawTooltip takes back whatever this trinket was offering, which is what
// a keystroke does to every tooltip on the screen: the reader has moved on,
// and an idle pointer should not leave one standing for ever.
func (w *TrinketBase) WithdrawTooltip() {
	w.mu.Lock()
	showing := w.tooltipShown != ""
	w.tooltipShown = ""
	w.mu.Unlock()
	if showing {
		CancelTooltip(w.Self())
	}
}
