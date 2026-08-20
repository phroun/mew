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

// pendingKeyPress is a key press that has been NAMED but not yet dispatched,
// because the text it produces has not arrived.
//
// SDL splits one keystroke in two: the KEY_DOWN carries the physical key and
// the modifiers held, which is what names it, and a TEXT_INPUT (or, for a dead
// key, a TEXT_EDITING) follows with whatever it produced. Waiting the one event
// costs nothing perceptible and buys two things: the pairing is observed rather
// than looked up, and — because the observation is recorded BEFORE the press is
// dispatched — anything that reads KeyChordText for this chord sees this
// keystroke's answer rather than the last one's. The first press of a chord is
// as good as the hundredth.
//
// A press that produces nothing is flushed unchanged, so nothing is lost by
// waiting for text that never comes.
//
// It began as the macOS Option case and is general because that case was never
// special: it was a press whose text arrives in a separate event, which is
// every printable key.
type pendingKeyPress struct {
	key      string
	scancode uint32
	repeat   bool
	surface  *sdlSurface

	// optionChord marks a macOS Option chord, where the keystroke is a CHORD
	// that happens to compose — M-e is a shortcut whether or not an accent
	// comes of it.
	//
	// A plain printable is the opposite: it is nothing but the text it makes.
	// The distinction decides both of the questions asked when no text arrives:
	//
	// A composition that follows it is the chord's own output, so the chord is
	// dispatched carrying it. A composition following a plain key belongs to
	// the input method taking that keystroke over — hold a letter down on macOS
	// and its accent palette opens one — and is not the key's to report.
	//
	// And no text at all still leaves a chord to report, because the shortcut
	// was pressed. A plain key with no text typed nothing: macOS hands a held
	// letter to its palette as marked text rather than committing it.
	optionChord bool
}

// macOptionMayCompose reports whether this key-down is one this platform
// answers with a composed character, and so should be held for it.
func macOptionMayCompose(sym sdl3.Keysym) bool {
	return runtime.GOOS == "darwin" && optionComposes(sym)
}

// keyAwaitsText reports whether SDL will follow this key-down with text, and so
// whether its press should be held until the text arrives.
//
// Two cases, and they are the same case. A macOS Option chord composes a
// character; a plain or shifted printable produces the character it shows. Both
// are a press whose text comes in the next event, and both want the text
// recorded before the press goes out.
//
// Everything else — a named key, a Control chord, a Command chord — produces no
// text at all and is dispatched where it is read.
func keyAwaitsText(sym sdl3.Keysym) bool {
	if macOptionMayCompose(sym) {
		return true
	}
	if sym.Mod&(sdl3.KMOD_CTRL|sdl3.KMOD_GUI|sdl3.KMOD_ALT) != 0 {
		return false
	}
	// A pad cap is named from the key, prefix and all, and the text event it
	// produces is thrown away (padTyped) so the one press does not arrive
	// twice. There is no text coming that this press could wait for — waiting
	// would strand it until some later event flushed it.
	if _, _, _, isPad := keypadKey(sym, sym.Mod&sdl3.KMOD_NUM != 0); isPad {
		return false
	}
	return sym.Sym >= 32 && sym.Sym < 127
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

// takePendingPress claims the held chord, if there is one.
func (p *Platform) takePendingPress() *pendingKeyPress {
	pending := p.pendingPress
	p.pendingPress = nil
	return pending
}

// dispatchPendingPress sends a held chord with the character it composed, records
// the pairing, and registers the press so its release names it the same.
//
// composed is empty when nothing was composed — the chord is still the
// keystroke that happened, and is dispatched exactly as it would have been.
func (p *Platform) dispatchPendingPress(pending *pendingKeyPress, composed string) {
	if pending == nil {
		return
	}
	p.emitKeyPress(pending.surface, keyPress{
		chord: pending.key,
		// Nothing composed leaves the memo alone rather than recording a
		// silence: the text may simply not have come, and the chord still types
		// what the key itself shows.
		produced: composed,
		observed: composed != "",
		scancode: pending.scancode,
		held:     true,
		repeat:   pending.repeat,
		origin:   "held",
	})
}

// flushPendingPress dispatches a held chord that no composition followed.
//
// Called before any other event is handled and again when the queue drains, so
// a chord that composes nothing waits for one event at most and never for an
// idle keyboard.
func (p *Platform) flushPendingPress() {
	pending := p.takePendingPress()
	if pending == nil {
		return
	}
	// A plain key that produced no text produced NOTHING, and there is nothing
	// to report.
	//
	// A plain printable is only the text it makes. When the text does not come,
	// an input method has the keystroke: macOS hands a held letter over as
	// marked text rather than committing it, and goes on delivering repeat
	// key-downs behind the palette. Dispatching those committed the very
	// character the palette had opened to replace, and then went on committing
	// one per repeat.
	//
	// An Option chord is not that. It is a chord that happens to compose, so it
	// is dispatched whether or not anything came of it — M-e is the shortcut
	// the user pressed either way.
	if !pending.optionChord {
		// And this is where the takeover OPENS the composition, over the
		// character the key had already committed and showing that character.
		//
		// It matters most on the route where SDL reports no composition at
		// all: picking from the palette by number arrives as a synthesized
		// Backspace and the replacement, with nothing marked in between. The
		// sink would have no standing composition to replace, and the commit
		// would append. Announced here, both routes are the same thing.
		if p.ime.armFor(pending) && pending.surface != nil && pending.surface.handler != nil {
			pending.surface.handler.Event(core.TextEditingEvent{
				Text:   pending.key,
				Start:  -1,
				Length: -1,
				Covers: p.ime.covers,
			})
		}
		// Swallowed only if this press is the PALETTE'S OWN — a repeat of the
		// key being held down behind it. That is the case the swallowing was
		// for: those repeats each committed a copy of the very character the
		// palette had opened to replace.
		//
		// A FRESH press is somebody typing. Reaching for "." or "/" with a
		// palette up dismisses it and types the character, and swallowing that
		// press ate the keystroke outright — it produced no text because the
		// palette had the keyboard for a moment, not because it produced
		// nothing.
		if pending.repeat {
			return
		}
	}
	p.dispatchPendingPress(pending, "")
}

// imeState tracks what an input method is doing to text that has already been
// committed.
//
// It exists for macOS's press-and-hold accent palette, which is not a
// composition at all in the ordinary sense: the held letter is COMMITTED the
// moment the key goes down, and choosing an accent then has to remove a
// character that is already in the document. A native client is told which one
// through NSTextInputClient's replacementRange. SDL's composition event has no
// field for it, so we are told nothing, and the count has to be inferred.
//
// The inference rests on an observation this platform already makes: a held
// plain printable whose text never arrives has been TAKEN OVER by an input
// method (see flushPendingPress). What the palette has opened over is the
// character that key committed before the hold began, which is exactly one rune
// — only plain printables are held for text.
//
// So the takeover OPENS A COMPOSITION covering that character, and everything
// after it is ordinary composition handling. The sink hides the letter and
// paints the composition in its place, which is what the palette shows on
// screen; whichever way the choice is confirmed — a synthesized Backspace and
// the replacement for a number, marked text and the replacement for the arrows
// — the commit replaces what the composition covered. Neither the extent nor
// the confirmation route has to be described to the sink twice.
type imeState struct {
	// covers is how many committed runes the standing composition was opened
	// over, and armed says whether an input method has taken a key over at all.
	// They are separate because zero is a real answer: an ordinary composition
	// is opened over nothing.
	armed  bool
	covers int

	// composing is whether a composition is in flight, from the first non-empty
	// update to the empty one that ends it. It is what keeps a key-down from
	// disarming: while the input method has the keyboard, the keystrokes
	// driving its palette are not the user typing.
	composing bool

	// shown is what the composition is displaying, kept because the palette's
	// selector passes through the sink as ordinary typed text and ends the
	// composition there — so it has to be put back once the selector is erased.
	shown string

	// typed is how many runes the palette has put in the document on its own
	// behalf since it opened — its selector, which it erases again.
	//
	// Counted only to tell the palette's one selector apart from somebody
	// typing on: it is NOT part of what the commit replaces, because the
	// palette's own erase takes it back out (see TextEraseEvent).
	typed int
}

// armFor records a takeover if this is the press that constitutes one, and
// says whether it did — which is also the caller's cue to open a composition
// over the character that press's key had committed.
//
// A REPEAT is what identifies it. The palette opens on the hold, so the presses
// it swallows are the repeats behind it — the first press of that key committed
// its character and was dispatched normally, and that character is the one the
// palette will replace.
//
// The repeat test is what keeps a dead key's completion out. Option+i arms a
// circumflex and the "u" that completes it is a FRESH press whose composition
// arrives the same way; arming for that would delete the character before it.
// True only on the transition, so the repeats that keep arriving behind an
// open palette do not re-announce a composition that is already standing.
func (m *imeState) armFor(pending *pendingKeyPress) bool {
	if pending == nil || pending.optionChord || !pending.repeat || m.armed {
		return false
	}
	// One rune: only plain printables are held for text, so the character the
	// first press committed is exactly one.
	m.armed, m.covers, m.shown = true, 1, pending.key
	core.KeyTracef("1 sdl      ime     OPEN over %q covers=1", pending.key)
	return true
}

// disarm forgets a takeover WITHOUT deleting anything, which is what a
// cancelled palette needs: the letter the key committed is what the user typed
// and it stays. Called wherever the input method loses the keystroke — a real
// key, a click, focus leaving, text input being switched off.
func (m *imeState) disarm() {
	m.armed = false
	m.covers = 0
	m.typed = 0
	m.shown = ""
}

// noteTyped records a rune this platform put in the document on the palette's
// behalf, and reports whether the palette is still plausibly open.
//
// The palette takes exactly ONE selector. A first typed keystroke while it is
// open is that selector — macOS types it, backspaces it, and sends the accent —
// so it does not end the composition. A SECOND is somebody typing on, which no
// palette does, and ends it.
//
// Without the first exemption, picking by number cancelled the takeover before
// its own accent arrived and the accent appended: "oœ". Without the second, a
// palette dismissed by typing rather than confirmed never ended at all, and the
// letter under it stayed painted as provisional text.
func (m *imeState) noteTyped() bool {
	if !m.armed {
		return false
	}
	m.typed++
	return m.typed == 1
}

// takeBackTyped reports whether the palette has a rune of its OWN in the
// document to take back, and spends it if so.
//
// It is what tells the palette's two erases apart. Picking by NUMBER types a
// selector digit and backspaces that; picking with the MOUSE types nothing and
// backspaces the base letter instead — the very character the composition
// already stands over, which the sink is hiding and the commit is about to
// replace. Forwarding that one as an erase deleted a rune the commit then
// replaced as well, one character too many: "Hi" held on the "i" and clicked
// came out "ï".
//
// Zero is also the answer while an ordinary composition is up, where Backspace
// is the input method's own key for shortening what it holds. Nothing of the
// document is being taken back there either.
func (m *imeState) takeBackTyped() bool {
	if m.typed <= 0 {
		return false
	}
	m.typed--
	return true
}

// spend ends the takeover on a commit, so a second commit does not open over a
// second character.
//
// The composition goes with it. A commit IS the composition finishing, and an
// input method that delivers one without the empty update that usually follows
// would otherwise leave this standing true for good — and a key that types
// nothing is swallowed while it stands (see emitKeyPress), so Return would stop
// working for the rest of the session. Another update sets it again.
func (m *imeState) spend() {
	m.composing = false
	m.disarm()
}

// imeBackspace reports the palette's own erase as what it MEANS, rather than as
// the key it arrived on.
//
// Picking from the press-and-hold palette by number types the selector digit
// into the document and takes it back out this way, because macOS expects the
// base letter to be marked text it will replace itself. Nobody pressed
// Backspace, so dispatching it as a keystroke would run whatever the user has
// bound to that key — which need not be an erase at all — and the sink is the
// only thing that knows what erasing means where it is: a character out of a
// document, or the child's own erase byte down a terminal session.
//
// The takeover STANDS. The letter the palette opened over is still there and
// still what the accent will replace; only the palette's own selector is being
// taken back. And nothing is held for this press, so its release drops on its
// own.
func (p *Platform) imeBackspace(s *sdlSurface) {
	if !p.ime.takeBackTyped() {
		// Nothing of the palette's OWN is in the document, so this erase is
		// aimed at the character it opened over — which the sink is already
		// hiding behind the composition, and which the commit replaces. There
		// is nothing here to take back: doing it anyway deleted the rune before
		// the region as well, and "Hi" held on the "i" came out "ï".
		core.KeyTracef("1 sdl      ime     erase swallowed (nothing of its own typed)")
		return
	}
	core.KeyTracef("1 sdl      ime     erase 1 (the palette's own)")
	if s == nil || s.handler == nil {
		return
	}
	s.handler.Event(core.TextEraseEvent{Count: 1})

	// And the composition goes back up. The selector reached the sink as
	// ordinary typed text, which ENDS a composition there — that is what
	// committed characters do — so by now the sink has forgotten what the
	// palette is standing over. Restored once the selector is gone and the
	// caret is back where the composition belongs, so the accent still has a
	// region to replace.
	if p.ime.armed {
		s.handler.Event(core.TextEditingEvent{
			Text:   p.ime.shown,
			Start:  -1,
			Length: -1,
			Covers: p.ime.covers,
		})
	}
}

// cancelComposition ends a takeover AND tells the sink, which is what makes
// cancelling whole.
//
// Disarming alone is not enough once the takeover opens a composition: the sink
// is hiding a committed character behind it, and a composition nobody ends
// leaves that character hidden for good. Nothing is deleted — the letter comes
// back exactly as it was typed, which is what dismissing a palette means.
//
// Sending an empty composition rather than relying on SDL to send one, because
// on the route where the palette is driven by number SDL never reported a
// composition in the first place: ours was the only one, and only we can end
// it.
func (p *Platform) cancelComposition(s *sdlSurface) {
	if !p.ime.armed {
		// Nothing of ours is standing. A composition SDL owns is left alone —
		// a dead key deliberately leaves one open for the next keystroke to
		// compose against.
		p.ime.disarm()
		return
	}
	core.KeyTracef("1 sdl      ime     CANCEL (covers=%d composing=%v)",
		p.ime.covers, p.ime.composing)
	// composing goes with it. It is set from the updates SDL sends, and a
	// palette dismissed by a keystroke rather than confirmed produces no
	// ending update at all — so left alone it stands true for good, and the
	// cancel that should have run on the next key never would.
	p.ime.composing = false
	p.ime.disarm()
	if s != nil && s.handler != nil {
		s.handler.Event(core.TextEditingEvent{Start: -1, Length: -1})
	}
}

// holdsKeyboard reports whether an input method currently has the keystrokes.
//
// It answers both of the questions this state exists for, because they are the
// same question. A key arriving now is the palette's rather than the
// document's, and text arriving now is a COMMIT rather than ordinary typing —
// a character chosen from a palette or a candidate list, with no key behind it.
func (m *imeState) holdsKeyboard() bool { return m.armed || m.composing }

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

// deadKeyChords is macOSDeadKeys read the other way: the chords that arm an
// accent rather than typing one, by name.
//
// Built from the same table so the two cannot disagree about which five keys
// these are.
var deadKeyChords = func() map[string]bool {
	chords := make(map[string]bool, len(macOSDeadKeys))
	for _, chord := range macOSDeadKeys {
		chords[chord] = true
	}
	return chords
}()

// isDeadKeyChord reports whether a chord is one of the five that arm an accent.
//
// Known from the chord alone, at the moment it is pressed, which is the point.
// Waiting to learn it from what the keystroke produced means never learning it
// at all when the keystroke produces nothing — and macOS often delivers neither
// text nor a composition for these, keeping the armed accent to itself. The
// chord then went out unrecorded, a consumer fell through to a table of what
// such a chord types, and the accent was inserted.
//
// That failure was invisible after the first success: once anything recorded
// the chord, every later press of it behaved, so the bug showed only as the
// first few presses of each dead key misbehaving and then stopping.
func isDeadKeyChord(chord string) bool { return deadKeyChords[chord] }

// macOptionChordFor names the M- chord a macOS Option keystroke makes, from the
// key and the modifier held.
//
// It exists because translateKey does not always get the chance. macOS can
// report Option as a level-3 shift, and can report the key itself as the
// character it composed — either of which makes translateKey yield the key-down
// entirely and leave the chord to be recovered later from the text. That works
// for a chord that produces text. A dead key produces none, so there is nothing
// to recover it from, and the only moment it can be known is the press.
//
// Deliberately not a substitute for translateKey: it answers one narrow
// question — which M- chord is this — for the one caller that has to ask before
// anything has been produced.
func macOptionChordFor(sym sdl3.Keysym) (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	if sym.Mod&(sdl3.KMOD_ALT|sdl3.KMOD_MODE) == 0 {
		return "", false
	}
	if sym.Mod&(sdl3.KMOD_CTRL|sdl3.KMOD_GUI) != 0 {
		return "", false
	}
	if sym.Sym < 32 || sym.Sym >= 127 {
		return "", false
	}
	return "M-" + string(rune(sym.Sym)), true
}

// isArmedAccent reports whether text an Option chord produced is one of the
// accents a dead key ARMS rather than types.
//
// Which event carries it is not something to depend on. The comment above says
// macOS reports these as an in-flight composition, and that is what the
// composition path was built for — but the same accent also arrives as ordinary
// committed text, and then it was recorded as what the chord typed. Anything
// falling through to what M-i types then inserted the accent, and the next
// keystroke composed the accented character behind it: "ˆû".
//
// So the accent is what identifies the case, not the event it came on. A dead
// key types nothing whichever way its accent is delivered.
func isArmedAccent(text string) bool {
	_, ok := macOSDeadKeys[text]
	return ok
}
