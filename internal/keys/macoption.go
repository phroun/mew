package keys

// macOptionChars maps Meta key names back to the character macOS Option
// produces for that key (US layout) — the reverse of direct-key-handler's
// decode table. When Option decoding turns ∂ into M-d and no binding claims
// M-d, the default handling re-inserts the ∂, so bindings steal individual
// Option combos while the rest keep typing characters seamlessly. On other
// platforms Alt+key arrives as the same M- names, giving every terminal
// mac-style Option typing for free.
var macOptionChars = map[string]string{
	// Option+letter
	"M-a": "å", "M-b": "∫", "M-c": "ç", "M-d": "∂",
	"M-e": "´", // dead key: acute accent
	"M-f": "ƒ", "M-g": "©", "M-h": "˙",
	"M-i": "ˆ", // dead key: circumflex
	"M-j": "∆", "M-k": "˚", "M-l": "¬", "M-m": "µ",
	"M-n": "˜", // dead key: tilde
	"M-o": "ø", "M-p": "π", "M-q": "œ", "M-r": "®", "M-s": "ß",
	"M-t": "†",
	"M-u": "¨", // dead key: diaeresis
	"M-v": "√", "M-w": "∑", "M-x": "≈", "M-y": "¥", "M-z": "Ω",

	// Option+Shift+letter (E/I/N/U produce the same dead keys as lowercase,
	// so they have no distinct entries — mirroring the decode table).
	"M-A": "Å", "M-B": "ı", "M-C": "Ç", "M-D": "Î", "M-F": "Ï",
	"M-G": "˝", "M-H": "Ó", "M-J": "Ô",
	"M-K": "", // the Apple logo (private use area)
	"M-L": "Ò", "M-M": "Â", "M-O": "Ø", "M-P": "∏", "M-Q": "Œ",
	"M-R": "‰", "M-S": "Í", "M-T": "ˇ", "M-V": "◊", "M-W": "„",
	"M-X": "˛", "M-Y": "Á", "M-Z": "¸",

	// Option+number
	"M-1": "¡", "M-2": "™", "M-3": "£", "M-4": "¢", "M-5": "∞",
	"M-6": "§", "M-7": "¶", "M-8": "•", "M-9": "ª", "M-0": "º",

	// Option+symbol
	"M--": "–", "M-=": "≠", "M-[": "“", "M-]": "’",
	"M-\\": "«", "M-;": "…", "M-'": "æ", "M-,": "≤", "M-.": "≥",
	"M-/": "÷", "M-`": "`",
}
