// Source-level input virtualization: the editor consumes an event stream,
// not a terminal, so a host can supply the events itself.
package input

import "sync"

// Source is the editor's input surface — the same things the built-in
// direct-key-handler pipeline provides: a blocking stream of key and paste
// events plus lifecycle hooks. KeyboardHandler is the terminal-backed Source
// (raw bytes in, parsed events out); EventFeed is the host-backed one.
type Source interface {
	// Start prepares the source. The terminal-backed source engages raw
	// mode and bracketed paste here; a host feed needs nothing.
	Start() error

	// Stop releases whatever Start acquired.
	Stop()

	// GetEvent blocks until the next key or paste event. A source that has
	// ended returns an event with Closed set, which the editor treats as
	// the end of the session.
	GetEvent() InputEvent
}

// EventFeed is an input Source fed by the host application instead of a
// terminal. It exists for hosts that already run their own key parsing —
// direct-key-handler or equivalent, a window manager, say — and want to
// forward key input to mew only while a mew view is focused, keeping input
// in every other context for themselves.
//
// The host supplies exactly the surfaces the built-in pipeline supplies:
//
//   - SendKey delivers one parsed key per call, in direct-key-handler
//     naming ("a", "^K", "Escape", "M-Left", "F1"); mew's normalized
//     aliases ("esc", "M-left", "return") are accepted unchanged. Raw
//     terminal mode, escape-sequence parsing, and key naming are the
//     host's job.
//   - SendPaste delivers bracketed-paste content in order, chunked however
//     the host likes, with the final chunk flagged. The editor groups the
//     chunks of one paste into a single undo revision and never re-parses
//     paste content as keys. Chunks should split only on rune boundaries.
//   - Close marks the end of input; the editor session winds down as if
//     the terminal had closed (the state snapshot is still delivered).
//
// Rendering, size, and resize stay on the Terminal virtualization; an
// EventFeed replaces only the input half, and any Terminal input reader is
// ignored while a feed is in use.
type EventFeed struct {
	events    chan InputEvent
	done      chan struct{}
	closeOnce sync.Once
}

// NewEventFeed creates a host-fed input source.
func NewEventFeed() *EventFeed {
	return &EventFeed{
		events: make(chan InputEvent, 256),
		done:   make(chan struct{}),
	}
}

// Start implements Source; a host feed has nothing to acquire.
func (f *EventFeed) Start() error { return nil }

// Stop implements Source; a host feed has nothing to release. The feed is
// not closed: the host owns its lifetime via Close.
func (f *EventFeed) Stop() {}

// SendKey delivers one key event. It blocks while the editor is busy and
// the feed's buffer is full, and reports false once the feed is closed.
func (f *EventFeed) SendKey(name string) bool {
	return f.send(InputEvent{Key: normalizeKey(name)})
}

// SendPaste delivers one chunk of pasted content (copied, so the host may
// reuse its buffer), flagging the paste's final chunk. It blocks while the
// editor is busy and the feed's buffer is full, and reports false once the
// feed is closed.
func (f *EventFeed) SendPaste(content []byte, final bool) bool {
	c := append([]byte(nil), content...)
	return f.send(InputEvent{Paste: &PasteChunk{Content: c, IsFinal: final}})
}

// PostAction implements ActionPoster: fn is delivered in order with the key
// and paste stream and runs on the editor's main loop. It blocks while the
// editor is busy and the feed's buffer is full, and reports false once the
// feed is closed.
func (f *EventFeed) PostAction(fn func()) bool {
	if fn == nil {
		return false
	}
	return f.send(InputEvent{Do: fn})
}

// Close ends the feed. Events already sent are still delivered, then the
// editor sees the source as closed. Safe to call more than once.
func (f *EventFeed) Close() {
	f.closeOnce.Do(func() { close(f.done) })
}

func (f *EventFeed) send(ev InputEvent) bool {
	select {
	case <-f.done:
		return false
	default:
	}
	select {
	case f.events <- ev:
		return true
	case <-f.done:
		return false
	}
}

// GetEvent implements Source: it blocks for the next event, draining
// anything sent before Close, then reports the source closed.
func (f *EventFeed) GetEvent() InputEvent {
	select {
	case ev := <-f.events:
		return ev
	case <-f.done:
		select {
		case ev := <-f.events:
			return ev
		default:
			return InputEvent{Closed: true}
		}
	}
}
