// Package input provides keyboard input handling using direct-key-handler.
package input

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/phroun/direct-key-handler/keyboard"
)

// keyNameMap maps handler key names to mew's expected key names.
// These should match the primary names in the key alias groups in sequence.go
var keyNameMap = map[string]string{
	"Escape": "esc",
	// Space arrives as its literal character " " (printables are named by
	// themselves), but a bare space can't be a token in mew's space-separated
	// binding syntax — bindings must spell it "space" (e.g. "^B space"). Map the
	// character to that word so a pressed spacebar matches, and modified forms
	// ("M- " -> "M-space") fall out of the prefix logic below.
	" ":         "space",
	"Tab":       "tab",
	"Enter":     "return",
	"Backspace": "back", // Primary for {"back", "^H", "backspace"} group
	"Up":        "up",
	"Down":      "down",
	"Left":      "left",
	"Right":     "right",
	"Home":      "home",
	"End":       "end",
	"Insert":    "ins",
	"Delete":    "fdel", // Primary for {"fdel", "delete"} group
	"PageUp":    "pgup",
	"PageDown":  "pgdn",
}

// PasteChunk represents a chunk of pasted content.
type PasteChunk struct {
	Content []byte
	IsFinal bool
}

// InputEvent represents a key press, a paste chunk, or a posted action.
type InputEvent struct {
	Key    string      // Non-empty if this is a key event
	Paste  *PasteChunk // Non-nil if this is a paste event
	Do     func()      // Non-nil if this is a posted action (see ActionPoster)
	Closed bool        // The source has ended; no further events will come
}

// ActionPoster is the optional Source capability of accepting closures from
// other goroutines, delivered in order as Do events on the editor's main
// loop. It is how asynchronous host callbacks — a clipboard read resolving, a
// host menu item firing — marshal their work onto the editor thread instead
// of touching editor state from outside. Post blocks briefly when the editor
// is busy and the queue is full (the same contract as EventFeed.SendKey) and
// reports false when the source cannot deliver.
type ActionPoster interface {
	PostAction(fn func()) bool
}

// KeyboardHandler wraps the direct-key-handler for keyboard input.
type KeyboardHandler struct {
	handler *keyboard.Handler

	// termOut receives terminal control sequences (bracketed paste on/off).
	// A virtual host's terminal sees the same protocol a real one would.
	termOut io.Writer

	// Paste handling - chunks arrive via channel for real-time processing
	PasteChunks chan PasteChunk

	// pasteCarry holds an incomplete trailing UTF-8 sequence between chunks so a
	// rune split across a chunk boundary is not corrupted. Paste content arrives
	// only via the OnPasteChunk callbacks: direct-key-handler is configured with
	// EmitPasteKeys=false, so it does NOT also re-emit paste as key events, and
	// there is nothing to swallow off the Keys channel. Touched only from
	// GetEvent (the single input-consuming goroutine), so it needs no locking.
	pasteCarry []byte

	// actions carries closures posted from other goroutines (ActionPoster),
	// surfaced by GetEvent as Do events so they run on the editor main loop.
	actions chan func()
}

// NewKeyboardHandler creates a new keyboard handler reading from input and
// writing terminal control sequences to termOut. Nil values mean the real
// terminal (os.Stdin / os.Stdout); direct-key-handler only engages raw
// terminal mode when the input is a real terminal, so virtual readers are
// consumed as-is.
func NewKeyboardHandler(input io.Reader, termOut io.Writer) *KeyboardHandler {
	if input == nil {
		input = os.Stdin
	}
	if termOut == nil {
		termOut = os.Stdout
	}
	kh := &KeyboardHandler{
		termOut:     termOut,
		PasteChunks: make(chan PasteChunk, 4096), // Large buffer to handle big pastes
		actions:     make(chan func(), 64),
	}

	noPasteKeys := false
	h := keyboard.New(keyboard.Options{
		InputReader:    input,
		KeyBufferSize:  256,
		PasteChunkSize: 4096, // Larger chunks for efficiency, visual updates still happen per chunk
		// Take paste only through OnPasteChunk; do not also re-emit it as a burst
		// of key events. That echo is redundant (we insert from the chunks) and,
		// on a large paste, would overflow the Keys channel and drop events.
		EmitPasteKeys: &noPasteKeys,
	})

	// Set up chunked paste callback for real-time paste processing
	h.OnPasteChunk = func(chunk keyboard.PasteChunk) {
		// Blocking send - ensures no chunks are lost
		// If buffer fills, paste just slows down to match processing speed
		kh.PasteChunks <- PasteChunk{Content: chunk.Content, IsFinal: chunk.IsFinal}
	}

	kh.handler = h
	return kh
}

// SetDecodeMacOSOption enables or disables decoding of macOS Option+key
// Unicode characters into M-key notation (forwarded to direct-key-handler,
// which defaults it to on for Darwin).
func (kh *KeyboardHandler) SetDecodeMacOSOption(enabled bool) {
	kh.handler.SetDecodeMacOSOption(enabled)
}

// Start begins listening for keyboard input.
func (kh *KeyboardHandler) Start() error {
	// Enable bracketed paste mode - terminal will wrap pastes with ESC[200~ ... ESC[201~
	io.WriteString(kh.termOut, "\x1b[?2004h")

	return kh.handler.Start()
}

// Stop stops listening for keyboard input.
func (kh *KeyboardHandler) Stop() {
	kh.handler.Stop()

	// Disable bracketed paste mode
	io.WriteString(kh.termOut, "\x1b[?2004l")
}

// GetEvent waits for and returns either a key press or a paste chunk.
// This allows the main loop to handle both types of input without blocking
// on one while the other is available.
//
// Paste chunks are given priority over keys so a paste in progress is drained
// promptly. Paste content never appears on the Keys channel (the handler runs
// with EmitPasteKeys=false), so key events are always genuine keystrokes.
func (kh *KeyboardHandler) GetEvent() InputEvent {
	for {
		// Priority pass: drain any already-queued paste chunk before blocking.
		select {
		case raw := <-kh.PasteChunks:
			return kh.handlePasteChunk(raw)
		default:
		}

		select {
		case raw := <-kh.PasteChunks:
			return kh.handlePasteChunk(raw)
		case key := <-kh.handler.Keys:
			return InputEvent{Key: normalizeKey(key)}
		case fn := <-kh.actions:
			return InputEvent{Do: fn}
		}
	}
}

// PostAction implements ActionPoster: fn is queued and later surfaced by
// GetEvent as a Do event, running on the editor's main loop. Safe from any
// goroutine; blocks briefly when the editor is busy and the queue is full.
func (kh *KeyboardHandler) PostAction(fn func()) bool {
	if fn == nil {
		return false
	}
	kh.actions <- fn
	return true
}

// handlePasteChunk rejoins a multibyte rune split across a chunk boundary and
// returns a rune-complete paste event for the editor to insert.
func (kh *KeyboardHandler) handlePasteChunk(raw PasteChunk) InputEvent {
	data := raw.Content
	if len(kh.pasteCarry) > 0 {
		data = append(kh.pasteCarry, data...)
		kh.pasteCarry = nil
	}

	// Hold back an incomplete trailing UTF-8 sequence until the next chunk so a
	// rune split across the 4096-byte chunk boundary is not corrupted into
	// replacement characters by string(data). The final chunk keeps everything
	// (any leftover partial bytes are a genuinely truncated paste).
	if !raw.IsFinal {
		var carry []byte
		data, carry = splitCompleteUTF8(data)
		if len(carry) > 0 {
			kh.pasteCarry = append([]byte(nil), carry...)
		}
	}

	return InputEvent{Paste: &PasteChunk{Content: data, IsFinal: raw.IsFinal}}
}

// splitCompleteUTF8 splits b at the last rune boundary, returning the complete
// prefix and any incomplete trailing bytes (an unfinished multibyte sequence)
// to carry into the next chunk.
func splitCompleteUTF8(b []byte) (complete, carry []byte) {
	if len(b) == 0 {
		return b, nil
	}
	// Scan back up to UTFMax bytes to find the start of the final rune.
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		start := len(b) - i
		if utf8.RuneStart(b[start]) {
			if utf8.FullRune(b[start:]) {
				return b, nil // final rune is complete
			}
			return b[:start], b[start:] // incomplete trailing sequence
		}
	}
	// No rune start within the last UTFMax bytes: malformed data, don't hold it.
	return b, nil
}

// normalizeKey converts handler key names to mew's expected format.
// Examples:
//   - "Escape" → "esc"
//   - "M-Left" → "M-left"
//   - "S-PageUp" → "S-pgup"
//   - "a", "A", "^A", "F1" → unchanged
func normalizeKey(key string) string {
	// Check for direct match first (e.g., "Escape", "Tab")
	if mapped, ok := keyNameMap[key]; ok {
		return mapped
	}

	// Handle modifier prefixes: M-, S-, ^, or combinations like S-M-, M-^, etc.
	// Find the base key by stripping known prefixes
	prefix := ""
	base := key

	for {
		if strings.HasPrefix(base, "M-") {
			prefix += "M-"
			base = base[2:]
		} else if strings.HasPrefix(base, "S-") {
			prefix += "S-"
			base = base[2:]
		} else if strings.HasPrefix(base, "^") && len(base) > 1 {
			// Control prefix - but only if there's something after it
			prefix += "^"
			base = base[1:]
		} else {
			break
		}
	}

	// If we extracted a prefix, try to normalize the base key
	if prefix != "" {
		if mapped, ok := keyNameMap[base]; ok {
			return prefix + mapped
		}
	}

	// No normalization needed
	return key
}
