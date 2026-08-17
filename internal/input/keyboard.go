// Package input provides keyboard input handling using direct-key-handler.
package input

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/phroun/direct-key-handler/keyboard"
)

// mewKeyNames is mew's key vocabulary, handed to direct-key-handler so keys
// arrive already spelled the way bindings spell them. These are the primary
// names of the key fallback groups in sequence.go, and the names the help topics
// document — mew's binding syntax, not display strings.
//
// Given to keyboard.Options.KeyNames rather than translated afterwards: the
// handler builds a name from a keycode and modifier bits, so rewriting it here
// meant re-parsing the prefixes it had just added, and matching on its
// spelling meant a rename upstream broke mew silently (see the coverage test,
// which fails if a key is left without a mew name).
var mewKeyNames = map[keyboard.Key]string{
	keyboard.KeyEscape: "esc",
	// Space arrives as its literal character " " (printables are named by
	// themselves), but a bare space can't be a token in mew's space-separated
	// binding syntax — bindings must spell it "space" (e.g. "^B space").
	keyboard.KeySpace:     "space",
	keyboard.KeyTab:       "tab",
	keyboard.KeyBackspace: "back", // Primary for {"back", "^H", "backspace"} group
	// The other erase byte. Upstream names BS (8) and DEL (127) apart because
	// a terminal sends one or the other for its backspace by lineage and it
	// cannot know which — so mew, which has always had two names here, takes
	// delivery of both rather than letting one arrive under upstream's
	// spelling. "del" is the DEL character; both bind to del_char_prior
	// (keydefaults.go), so they behave as the synonyms they are.
	keyboard.KeyDEL:      "del", // Primary for {"del", "^8"} group
	keyboard.KeyUp:       "up",
	keyboard.KeyDown:     "down",
	keyboard.KeyLeft:     "left",
	keyboard.KeyRight:    "right",
	keyboard.KeyHome:     "home",
	keyboard.KeyEnd:      "end",
	keyboard.KeyInsert:   "ins",
	keyboard.KeyDelete:   "fdel", // forward delete; no long spelling, see keydefaults.go
	keyboard.KeyPageUp:   "pgup",
	keyboard.KeyPageDown: "pgdn",

	// The home-row key and the keypad's are distinct keys upstream. mew binds
	// one "return": the help topic says as much ("Computers with a numeric
	// keypad may have an additional smaller key also labeled enter which is not
	// the one being referred to"), so fold them deliberately rather than
	// leaving the keypad's to arrive under its own name and match by accident.
	keyboard.KeyReturn:      "return",
	keyboard.KeyKeypadEnter: "return",

	// Function keys keep their upstream spelling: bindings write "F1".
	keyboard.KeyF1: "F1", keyboard.KeyF2: "F2", keyboard.KeyF3: "F3",
	keyboard.KeyF4: "F4", keyboard.KeyF5: "F5", keyboard.KeyF6: "F6",
	keyboard.KeyF7: "F7", keyboard.KeyF8: "F8", keyboard.KeyF9: "F9",
	keyboard.KeyF10: "F10", keyboard.KeyF11: "F11", keyboard.KeyF12: "F12",
	keyboard.KeyF13: "F13", keyboard.KeyF14: "F14", keyboard.KeyF15: "F15",
	keyboard.KeyF16: "F16", keyboard.KeyF17: "F17", keyboard.KeyF18: "F18",
	keyboard.KeyF19: "F19", keyboard.KeyF20: "F20",

	// Lock and system keys: lowercase, matching the rest of the vocabulary.
	keyboard.KeyCapsLock:    "capslock",
	keyboard.KeyScrollLock:  "scrolllock",
	keyboard.KeyNumLock:     "numlock",
	keyboard.KeyPrintScreen: "printscreen",
	keyboard.KeyPause:       "pause",
	keyboard.KeyMenu:        "menu",
	keyboard.KeyPower:       "power",

	// The keypad's 5 with NumLock off. It arrives prefixed — "P-begin" — like
	// every other pad key, but the base name is its own: it is the one key on
	// the pad that duplicates nothing in the main cluster.
	keyboard.KeyBegin: "begin",

	// Keys an American keyboard does not have. They are here to be BOUND, not
	// yet to insert: pressing one still produces no character, exactly as
	// pressing Q on a Korean layout does not yet insert ᄈ. What the entry buys
	// is that the key reaches a keymap under mew's spelling instead of
	// upstream's, which is the only thing this table has ever been for.
	//
	// They need names at all because their characters are already spoken for.
	// Zag prints "<" and ">", which on a US board are Shift+comma and
	// Shift+period; Zig, Ro and Yen print "\" and "|", which belong to the
	// backslash key. A DEC LK201 has both Zig and Zag, and settles the point:
	// its comma and period do not shift to "<" and ">" at all, so which key
	// those characters live on is a property of the keyboard, not of the
	// character. Only a position can name them.
	keyboard.KeyZig: "zig", // ISO, beside Return
	keyboard.KeyZag: "zag", // ISO, between LeftShift and Z
	keyboard.KeyRo:  "ro",  // JIS, beside RightShift
	keyboard.KeyYen: "yen", // JIS, beside the erase key

	// The input-method keys. The two locks latch a mode, as CapsLock does, and
	// mew does not surface their state; the other three fire once.
	keyboard.KeyKanaLock:   "kanalock",
	keyboard.KeyHangulLock: "hangullock",
	keyboard.KeyHenkan:     "henkan",   // converts the pending kana
	keyboard.KeyMuhenkan:   "muhenkan", // commits it unconverted
	keyboard.KeyHanja:      "hanja",    // converts the preceding Hangul
}

// hostKeyNames maps direct-key-handler's own spellings to mew's, derived from
// mewKeyNames so the vocabulary is still declared exactly once.
//
// The terminal path needs no such table — the handler is given mewKeyNames and
// emits mew's names directly. A host feed does: an embedding host parses its
// own key events and sends them through KeyFeed.SendKey under
// direct-key-handler's naming (see docs/embedding-mew.md), so those arrive
// unmapped and are translated here instead.
var hostKeyNames = func() map[string]string {
	m := make(map[string]string, len(mewKeyNames)+1)
	for k, name := range mewKeyNames {
		if def := k.DefaultName(); def != "" {
			m[def] = name
		}
	}
	// A host may send the spacebar as its literal character rather than by
	// name; both have to reach the "space" token bindings are written with.
	m[" "] = mewKeyNames[keyboard.KeySpace]
	return m
}()

// normalizeHostKey converts a key name a host fed in (direct-key-handler's
// naming) to mew's, preserving the modifier prefixes the host put in front of
// it: "S-PageUp" becomes "S-pgup". A name mew has no entry for is passed
// through unchanged.
func normalizeHostKey(key string) string {
	if mapped, ok := hostKeyNames[key]; ok {
		return mapped
	}
	prefix, base := "", key
	for {
		matched := false
		// The modifier vocabulary, in canonical order. The order is for
		// reading only -- every prefix is two characters and no two can match
		// the same text, so at most one applies per pass whatever order they
		// are tried in. Membership is what matters: m- (Micro) was missing, so
		// a key spelled with it never had its prefix peeled and its base never
		// reached the name table; A- was here and is not a modifier this
		// vocabulary has. (^ is one character and is handled below.)
		//
		// P- and p- are the keypad, and they matter here for the same reason:
		// a host feeding "P-Home" would otherwise miss the table entirely and
		// deliver upstream's spelling, where the keymap expects "P-home".
		for _, p := range []string{"C-", "G-", "M-", "m-", "S-", "s-", "H-", "P-", "p-"} {
			if strings.HasPrefix(base, p) {
				prefix, base = prefix+p, base[2:]
				matched = true
				break
			}
		}
		if !matched {
			// Control prefix, only when something follows it.
			if strings.HasPrefix(base, "^") && len(base) > 1 {
				prefix, base = prefix+"^", base[1:]
				continue
			}
			break
		}
	}
	if prefix != "" {
		if mapped, ok := hostKeyNames[base]; ok {
			return prefix + mapped
		}
	}
	return key
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
		KeyNames:      mewKeyNames,
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

// keyEventReporting is the kitty keyboard protocol's "report event types"
// flag, pushed on the terminal's flag stack for the length of the session.
//
// It is the ONLY flag asked for, and that is the whole point of asking. A key
// coming back UP has no legacy encoding — there are no bytes for it — so a
// terminal sends one only when an application has said it wants event types,
// and mew never said so. Every release therefore stopped at mew's own front
// door, and a child in one of mew's terminal panes could not be given what mew
// was never sent: a browser hosted there saw keydown without keyup, forever.
//
// Event types also mark a held key's ":Repeat", which arrives here untouched.
// mew's keymap has no repeat in it and must not be shown one, but the child in
// a terminal pane does — a browser reports a repeat as keydown with repeat set,
// and cannot tell a held key from a drummed one without it. The marker is
// therefore set aside and put back at the one boundary that can use it; see
// Editor.dispatchKey.
//
// Disambiguation (flag 1) is deliberately NOT asked for. It would re-encode
// presses that arrive as plain bytes today — Escape, Tab, Enter, Backspace and
// every Control chord — which is a change to the wire mew has always read, for
// no gain here. Event reporting on its own leaves the presses alone and adds
// releases beside them: the one press that changes shape is F1, which an
// emulator sends as CSI P rather than SS3 P once any flag is set, and which
// direct-key-handler reads as F1 either way.
const keyEventReporting = "\x1b[>2u"

// Start begins listening for keyboard input.
func (kh *KeyboardHandler) Start() error {
	// Enable bracketed paste mode - terminal will wrap pastes with ESC[200~ ... ESC[201~
	io.WriteString(kh.termOut, "\x1b[?2004h")

	// Ask for key release events (see keyEventReporting). A terminal that does
	// not implement the protocol ignores the sequence, and mew then runs
	// exactly as it did before — presses only.
	io.WriteString(kh.termOut, keyEventReporting)

	return kh.handler.Start()
}

// Stop stops listening for keyboard input.
func (kh *KeyboardHandler) Stop() {
	kh.handler.Stop()

	// Pop the keyboard flags this session pushed, so the shell mew hands the
	// terminal back to is not left receiving events it never asked for.
	// Popping an empty stack is a no-op on a terminal that ignored the push.
	io.WriteString(kh.termOut, "\x1b[<u")

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
			return InputEvent{Key: key}
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
