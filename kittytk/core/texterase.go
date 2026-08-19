package core

// TextEraseEvent is an input method asking for text already in the document to
// be taken back out.
//
// It is not a keystroke, and that is the whole reason it exists. macOS's
// press-and-hold palette types its selector digit into the document and then
// erases it, because it expects the base letter to be marked text it will
// replace itself. That erase arrives as a Backspace key event — but nobody
// pressed Backspace, and dispatching it as one runs whatever the user has bound
// to that key, which need not be an erase at all.
//
// So it arrives as what it MEANS instead, and the sink decides what that is
// where it is: a character out of a document, the child's own erase byte down a
// terminal session, nothing at all in a field that cannot be edited.
type TextEraseEvent struct {
	// Count is how many runes before the caret to erase. Always at least 1.
	Count int
}

func (TextEraseEvent) isEvent() {}

// TextEraseHandler is implemented by anything that can take text back out on an
// input method's behalf — a text field, an editor, and the containers that
// route to them.
//
// Returning false means "not mine", as everywhere else. A sink that declines
// leaves the text where it is, which is better than the alternative: an erase
// nobody could place would otherwise have to fall back to the keystroke it came
// from, and that is the binding this event exists to avoid running.
type TextEraseHandler interface {
	HandleTextErase(TextEraseEvent) bool
}
