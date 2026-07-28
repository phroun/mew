package client

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Handle is the generic typed reference to one display-side object.
// Per the read audit, handles expose class-A and class-B state only;
// geometry and other server-authoritative reads deliberately do not
// exist here.
type Handle struct {
	c  *Conn
	id uint64
}

// ID returns the object's wire identity.
func (h Handle) ID() uint64 { return h.id }

// Valid reports whether the handle references anything.
func (h Handle) Valid() bool { return h.c != nil && h.id != 0 }

// Set applies raw property text to the object: h.Set(`caption="Hi" !enabled`).
// The typed setters below are preferred; this is the escape hatch
// that keeps the full vocabulary reachable.
func (h Handle) Set(args string) error { return h.c.set(h.id, args) }

// Destroy removes the object (detaches trinkets, closes windows).
func (h Handle) Destroy() error {
	_, err := h.c.Exec(fmt.Sprintf("destroy %d", h.id))
	return err
}

// On subscribes to an event from this object.
func (h Handle) On(event string, fn func(*protocol.Event)) {
	h.c.on(h.id, event, fn)
}

// Target returns the in-process constructed object (the real trinket).
// IN-PROCESS ESCAPE HATCH ONLY: nil under a remote transport. Exists
// so hybrid apps can hand a built tree to imperative code (window
// managers, AddWindow) while both sides live in one process.
func (h Handle) Target() any {
	h.c.mu.Lock()
	defer h.c.mu.Unlock()
	return h.c.targets[h.id]
}

// handle constructs a Handle for a surfaced name, subscribing the
// events listed (replica maintenance per the veneer contract).
func (u *UI) handle(name string, mirrors ...string) Handle {
	h := Handle{c: u.conn, id: u.ids[name]}
	if h.id != 0 {
		for _, ev := range mirrors {
			u.conn.ensureSub(h.id, ev)
		}
	}
	return h
}

// Object returns an untyped handle (no auto-subscriptions).
func (u *UI) Object(name string) Handle { return u.handle(name) }

// --- Typed handles ---

// Button: activation surface. Its state is fire-and-observe.
type Button struct{ Handle }

func (u *UI) Button(name string) Button { return Button{u.handle(name)} }

// OnClick fires on click events from this button.
func (b Button) OnClick(fn func()) {
	b.On("click", func(*protocol.Event) { fn() })
}

// SetCaption relabels the button.
func (b Button) SetCaption(s string) error {
	return b.Set("caption=" + protocol.Quote(s))
}

// Label: display text.
type Label struct{ Handle }

func (u *UI) Label(name string) Label { return Label{u.handle(name)} }

// SetCaption replaces the label text (write-through; no echo).
func (l Label) SetCaption(s string) error {
	return l.Set("caption=" + protocol.Quote(s))
}

// Checkbox: tri-capable checked state, event-mirrored.
type Checkbox struct{ Handle }

func (u *UI) Checkbox(name string) Checkbox {
	return Checkbox{u.handle(name, "toggle")}
}

// State returns the replica's tri-state (FlagFalse until any toggle
// or write).
func (cb Checkbox) State() protocol.FlagState {
	if s := cb.c.stateOf(cb.id).checked; s != protocol.FlagNone {
		return s
	}
	return protocol.FlagFalse
}

// Checked is the two-state convenience over State.
func (cb Checkbox) Checked() bool { return cb.State() == protocol.FlagTrue }

// SetChecked writes through to the replica and the display.
func (cb Checkbox) SetChecked(v bool) error {
	st := cb.c.stateOf(cb.id)
	arg := "checked"
	if v {
		st.checked = protocol.FlagTrue
	} else {
		st.checked = protocol.FlagFalse
		arg = "!checked"
	}
	return cb.Set(arg)
}

// OnToggle fires with the new tri-state after user toggles.
func (cb Checkbox) OnToggle(fn func(protocol.FlagState)) {
	cb.On("toggle", func(ev *protocol.Event) { fn(ev.Flag("checked")) })
}

// TextInput: editable text, event-mirrored.
type TextInput struct{ Handle }

func (u *UI) TextInput(name string) TextInput {
	return TextInput{u.handle(name, "change")}
}

// Text returns the replica's text.
func (t TextInput) Text() string { return t.c.stateOf(t.id).text }

// SetText writes through to the replica and the display.
func (t TextInput) SetText(s string) error {
	t.c.stateOf(t.id).text = s
	return t.Set("text=" + protocol.Quote(s))
}

// OnChange fires with the new text after user edits.
func (t TextInput) OnChange(fn func(string)) {
	t.On("change", func(ev *protocol.Event) {
		if s, ok := ev.Text("text"); ok {
			fn(s)
		}
	})
}

// Selector is the shared shape of index-selected trinkets: combobox,
// listview, treeview, tabs.
type Selector struct{ Handle }

func (u *UI) Selector(name string) Selector {
	return Selector{u.handle(name, "change")}
}

// Selected returns the replica's selection index (-1 = none/unknown).
func (s Selector) Selected() int { return s.c.stateOf(s.id).selected }

// Select writes the selection through.
func (s Selector) Select(index int) error {
	s.c.stateOf(s.id).selected = index
	return s.Set(fmt.Sprintf("selected=%d", index))
}

// OnChange fires with the new index on user selection.
func (s Selector) OnChange(fn func(int)) {
	s.On("change", func(ev *protocol.Event) {
		if n, ok := ev.Int("selected"); ok {
			fn(n)
		}
	})
}

// Window: top-level handle.
type Window struct{ Handle }

func (u *UI) Window(name string) Window { return Window{u.handle(name)} }

// OnClosed fires after the window finishes closing (window_closed
// carries window=, which subscription routing already honors).
func (w Window) OnClosed(fn func()) {
	w.On("window_closed", func(*protocol.Event) { fn() })
}

// Close closes the window (destroy verb).
func (w Window) Close() error { return w.Destroy() }

// SetTitle retitles the window.
func (w Window) SetTitle(s string) error {
	return w.Set("title=" + protocol.Quote(s))
}
