//go:build sdl

package sdl

import (
	"runtime"

	"github.com/phroun/kittytk/core"
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// macOS Option-key decoding.
//
// On macOS the Option key composes text rather than acting as a plain Mega
// modifier: pressing Option+a produces the Unicode character "å", which arrives
// as an SDLTextInput event. Left untranslated that character is simply
// typed, so Meta shortcuts (Select All is M-a, etc.) never fire.
//
// The TUI backend already solves this in the direct-key-handler, whose
// gold-standard table maps each Option-composed character (US layout) back to
// its "M-key" notation. We mirror that table verbatim here and apply it to
// the SDLTextInput characters when running on macOS, so both backends emit the
// same key names for the same physical keystroke.

// macOSOptionChars maps the Unicode characters a US-layout macOS keyboard
// produces under Option (and Option+Shift) to the toolkit's M-key notation.
// Kept in sync with direct-key-handler's table of the same name.
var macOSOptionChars = map[rune]string{
	// Lowercase Option+letter
	'å': "M-a", // Option+a
	'∫': "M-b", // Option+b
	'ç': "M-c", // Option+c
	'∂': "M-d", // Option+d
	'´': "M-e", // Option+e (dead key - acute accent)
	'ƒ': "M-f", // Option+f
	'©': "M-g", // Option+g
	'˙': "M-h", // Option+h
	'ˆ': "M-i", // Option+i (dead key - circumflex)
	'∆': "M-j", // Option+j
	'˚': "M-k", // Option+k
	'¬': "M-l", // Option+l
	'µ': "M-m", // Option+m
	'˜': "M-n", // Option+n (dead key - tilde)
	'ø': "M-o", // Option+o
	'π': "M-p", // Option+p
	'œ': "M-q", // Option+q
	'®': "M-r", // Option+r
	'ß': "M-s", // Option+s
	'†': "M-t", // Option+t
	'¨': "M-u", // Option+u (dead key - diaeresis)
	'√': "M-v", // Option+v
	'∑': "M-w", // Option+w
	'≈': "M-x", // Option+x
	'¥': "M-y", // Option+y
	'Ω': "M-z", // Option+z

	// Uppercase Option+Shift+letter (use M-X for uppercase, not M-S-x)
	'Å': "M-A", // Option+Shift+a
	'ı': "M-B", // Option+Shift+b
	'Ç': "M-C", // Option+Shift+c
	'Î': "M-D", // Option+Shift+d
	// Option+Shift+E produces ´ (same as Option+e) - handled above
	'Ï': "M-F", // Option+Shift+f
	'˝': "M-G", // Option+Shift+g
	'Ó': "M-H", // Option+Shift+h
	// Option+Shift+I produces ˆ (same as Option+i) - handled above
	'Ô': "M-J", // Option+Shift+j
	'': "M-K", // Option+Shift+k (Apple logo, private use area)
	'Ò': "M-L", // Option+Shift+l
	'Â': "M-M", // Option+Shift+m
	// Option+Shift+N produces ˜ (same as Option+n) - handled above
	'Ø': "M-O", // Option+Shift+o
	'∏': "M-P", // Option+Shift+p
	'Œ': "M-Q", // Option+Shift+q
	'‰': "M-R", // Option+Shift+r
	'Í': "M-S", // Option+Shift+s
	'ˇ': "M-T", // Option+Shift+t
	// Option+Shift+U produces ¨ (same as Option+u) - handled above
	'◊': "M-V", // Option+Shift+v
	'„': "M-W", // Option+Shift+w
	'˛': "M-X", // Option+Shift+x
	'Á': "M-Y", // Option+Shift+y
	'¸': "M-Z", // Option+Shift+z

	// Option+number
	'¡': "M-1", // Option+1
	'™': "M-2", // Option+2
	'£': "M-3", // Option+3
	'¢': "M-4", // Option+4
	'∞': "M-5", // Option+5
	'§': "M-6", // Option+6
	'¶': "M-7", // Option+7
	'•': "M-8", // Option+8
	'ª': "M-9", // Option+9
	'º': "M-0", // Option+0

	// Option+symbol
	'–': "M--",  // Option+minus (en dash)
	'≠': "M-=",  // Option+equals
	'"': "M-[",  // Option+[ (left double quote)
	'’': "M-]",  // Option+] (right single quote)
	'«': "M-\\", // Option+backslash
	'…': "M-;",  // Option+semicolon
	'æ': "M-'",  // Option+quote
	'≤': "M-,",  // Option+comma
	'≥': "M-.",  // Option+period
	'÷': "M-/",  // Option+slash
	'`': "M-`",  // Option+backtick (same as backtick on some layouts)
}

// decodeMacOSOptionChar returns the M-key notation for an Option-composed
// character and true, or "" and false when the rune is ordinary text.
//
// Only non-ASCII runes are decoded. Every genuine Option composition on a US
// layout yields a non-ASCII symbol; the table's two ASCII entries (0x22 " and
// 0x60 `) collide with characters SDL also delivers for ordinary, unmodified
// typing, so decoding them would swallow a plain quote or backtick. Guarding
// on the high bit keeps those keystrokes literal while still catching every
// real Meta shortcut.
func decodeMacOSOptionChar(r rune) (string, bool) {
	if r < 0x80 {
		return "", false
	}
	decoded, ok := macOSOptionChars[r]
	return decoded, ok
}

// pendingOptionKey is an Option chord whose KEY_DOWN has been read but not yet
// dispatched, because the character it composes has not arrived.
//
// macOS hands over both halves of the keystroke: the KEY_DOWN carries the
// physical key and KMOD_ALT — which is what NAMES the chord, authoritatively —
// and a TEXT_INPUT (or, for the five dead keys, a TEXT_EDITING) follows with
// the character it composed. Waiting the one event costs nothing perceptible
// and buys two things: the chord is dispatched with its character attached, so
// the first press of it is as good as the hundredth, and the pairing is
// observed rather than looked up.
//
// A chord that composes nothing is flushed unchanged, so nothing is lost by
// waiting for a character that never comes.
type pendingOptionKey struct {
	key      string
	scancode uint32
	repeat   bool
	surface  *sdlSurface
}

// macOptionMayCompose reports whether this key-down is one this platform
// answers with a composed character, and so should be held for it.
func macOptionMayCompose(sym sdl3.Keysym) bool {
	return runtime.GOOS == "darwin" && optionComposes(sym)
}

// optionComposes reports whether the keystroke is the kind macOS composes for:
// Option held on a printable key, with nothing else that would claim it first.
//
// The conditions mirror encodeKey's own alt branch. Ctrl and Command are
// excluded because macOS composes nothing for those, so waiting for a character
// there would only delay the chord until the next event flushed it. Kept apart
// from the platform test so the conditions can be read — and tested — anywhere.
func optionComposes(sym sdl3.Keysym) bool {
	if sym.Mod&sdl3.KMOD_ALT == 0 {
		return false
	}
	if sym.Mod&(sdl3.KMOD_CTRL|sdl3.KMOD_GUI) != 0 {
		return false
	}
	return sym.Sym >= 32 && sym.Sym < 127
}

// takePendingOption claims the held chord, if there is one.
func (p *Platform) takePendingOption() *pendingOptionKey {
	pending := p.pendingOption
	p.pendingOption = nil
	return pending
}

// dispatchOption sends a held chord with the character it composed, records
// the pairing, and registers the press so its release names it the same.
//
// composed is empty when nothing was composed — the chord is still the
// keystroke that happened, and is dispatched exactly as it would have been.
func (p *Platform) dispatchOption(pending *pendingOptionKey, composed string) {
	if pending == nil {
		return
	}
	// What the keyboard composed is recorded before anything else: it is a
	// fact about the keyboard, and stays true whether or not there is still a
	// surface to deliver the keystroke to.
	if composed != "" {
		p.noteOptionChar(pending.key, composed)
	}
	if pending.surface == nil || pending.surface.handler == nil {
		return
	}
	mods, name := core.ParseKeyModifiers(pending.key)
	text := composed
	if text == "" && len(name) == 1 && name[0] >= 32 && name[0] < 127 {
		// Nothing composed: the chord types what the key itself shows, which
		// is what every other platform does with Option held.
		text = name
	}
	p.holdKey(pending.scancode, pending.key)
	pending.surface.handler.Event(core.KeyPressEvent{
		Key: pending.key, Modifiers: mods, Text: text, Repeat: pending.repeat,
	})
}

// flushPendingOption dispatches a held chord that no composition followed.
//
// Called before any other event is handled and again when the queue drains, so
// a chord that composes nothing waits for one event at most and never for an
// idle keyboard.
func (p *Platform) flushPendingOption() {
	if pending := p.takePendingOption(); pending != nil {
		p.dispatchOption(pending, "")
	}
}

// noteOptionChar records what the keyboard composed for a chord.
//
// Written afresh every time: if the same chord starts composing something else
// mid-session, the new character is what this keyboard does now, and the old
// one was only ever a record of what it did before.
func (p *Platform) noteOptionChar(chord, composed string) {
	if chord == "" || composed == "" {
		return
	}
	p.optionMu.Lock()
	defer p.optionMu.Unlock()
	if p.optionChars == nil {
		p.optionChars = make(map[string]string)
	}
	p.optionChars[chord] = composed
}

// OptionChar implements core.OptionCharSource.
func (p *Platform) OptionChar(chord string) (string, bool) {
	p.optionMu.Lock()
	defer p.optionMu.Unlock()
	ch, ok := p.optionChars[chord]
	return ch, ok
}

// OptionChars implements core.OptionCharSource.
func (p *Platform) OptionChars() map[string]string {
	p.optionMu.Lock()
	defer p.optionMu.Unlock()
	out := make(map[string]string, len(p.optionChars))
	for k, v := range p.optionChars {
		out[k] = v
	}
	return out
}

// macOSDeadKeys maps the composition a macOS dead-key Option chord opens back
// to its M-key notation.
//
// Option+E, I, N, U and ` do not produce a character of their own: they arm an
// accent for the NEXT keystroke, which macOS reports as an in-flight
// composition rather than as text. That means they never reach
// decodeMacOSOptionChar, whose input is composed text - so without this table
// the accent picker opened over whatever had focus and the shortcut was lost.
//
// The composed text is the accent character itself, which is the same rune the
// Option table already lists; this is a separate lookup only because the two
// arrive by different routes.
var macOSDeadKeys = map[string]string{
	"´": "M-e", // acute
	"ˆ": "M-i", // circumflex
	"˜": "M-n", // tilde
	"¨": "M-u", // diaeresis
	"`": "M-`", // grave
}

// decodeMacOSDeadKey returns the M-key notation for a dead-key composition and
// true, or "" and false for an ordinary input-method composition -- a CJK
// candidate being typed, say, which must be left alone.
func decodeMacOSDeadKey(composition string) (string, bool) {
	key, ok := macOSDeadKeys[composition]
	return key, ok
}
