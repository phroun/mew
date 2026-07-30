package editor

import "strings"

// Typographic text, and getting it onto a terminal that may not want it.
//
// mew's user-facing strings should be written in their CORRECT typographic form
// — a real apostrophe in "can't", real quotation marks, a real em dash — because
// that is what the text IS, and a string is the wrong place to compromise about
// what a particular terminal can show. Whether those characters actually reach
// the screen is a display decision, and display decisions belong at the display
// boundary, where the terminal's capabilities, the environment, and the user's
// own preference are all known.
//
// This file models that split with the smallest thing that can model it: the
// correct string lives in the source, and degenerateToASCII stands where the
// decision will eventually be made. Today it always degenerates, because mew
// has not yet learned to ask. When it does, this is the function that grows a
// condition — and every caller already routes through it, so none of them has to
// change.
//
// Deliberately NOT yet done here: consulting termcaps, a [general] option, or
// the locale; per-string opt-out; anything about width or bidi. This is a
// placeholder with one real caller, not the audit.

// asciiDegenerations maps typographic characters to their ASCII stand-ins.
// Quotation and spacing only — the forms that turn up in ordinary prose. A
// glyph with no honest ASCII equivalent (an arrow, a bullet, a mathematical
// symbol) is deliberately absent: silently replacing one of those loses meaning
// rather than only losing polish, and the audit can decide case by case.
var asciiDegenerations = strings.NewReplacer(
	"‘", "'", // ' left single quotation mark
	"’", "'", // ' right single quotation mark (the apostrophe)
	"‚", "'", // ‚ single low-9 quotation mark
	"‛", "'", // ‛ single high-reversed-9
	"“", `"`, // " left double quotation mark
	"”", `"`, // " right double quotation mark
	"„", `"`, // „ double low-9 quotation mark
	"‟", `"`, // ‟ double high-reversed-9
	"‹", "<", // ‹ single left-pointing angle quotation mark
	"›", ">", // › single right-pointing angle quotation mark
	"«", "<<", // « left-pointing double angle quotation mark
	"»", ">>", // » right-pointing double angle quotation mark
	"′", "'", // ′ prime
	"″", `"`, // ″ double prime
	"‐", "-", // ‐ hyphen
	"‑", "-", // ‑ non-breaking hyphen
	"‒", "-", // ‒ figure dash
	"–", "-", // – en dash
	"—", "--", // — em dash
	"―", "--", // ― horizontal bar
	"−", "-", // − minus sign
	"…", "...", // … horizontal ellipsis
	" ", " ", // no-break space
	" ", " ", // figure space
	" ", " ", // thin space
	" ", " ", // narrow no-break space
)

// degenerateToASCII returns text with typographic characters replaced by their
// ASCII stand-ins.
//
// Call it on user-facing text at the point of display, and write the text itself
// correctly. It is unconditional for now; see the file comment for what it is
// standing in for.
func degenerateToASCII(text string) string {
	return asciiDegenerations.Replace(text)
}
