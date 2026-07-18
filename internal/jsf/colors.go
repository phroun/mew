package jsf

import (
	"fmt"
	"strconv"
	"strings"
)

// attrSGR converts a jsf color definition's attribute words to one ANSI SGR
// sequence. The vocabulary: the flags bold, dim, italic, underline, blink and
// inverse; foreground color names (black red green yellow blue magenta cyan
// white — uppercase for the bright variants); backgrounds as bg_<name> /
// BG_<NAME>; and the 256-color forms fg_RGB / bg_RGB (three digits, each 0-5)
// and fg_NN / bg_NN (grayscale 0-23). Unknown words are ignored, so grammars
// using newer attributes still load. Returns "" for an empty attribute list
// (the class renders in the window's normal text color).
func attrSGR(attrs []string) string {
	var codes []string
	for _, a := range attrs {
		switch a {
		case "bold":
			codes = append(codes, "1")
		case "dim":
			codes = append(codes, "2")
		case "italic":
			codes = append(codes, "3")
		case "underline":
			codes = append(codes, "4")
		case "blink":
			codes = append(codes, "5")
		case "inverse":
			codes = append(codes, "7")
		default:
			if c, ok := colorCode(a); ok {
				codes = append(codes, c)
			}
		}
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[0;" + strings.Join(codes, ";") + "m"
}

// namedColors maps the base color names to their standard ANSI offsets.
var namedColors = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
}

// colorCode resolves one color word to its SGR fragment.
func colorCode(a string) (string, bool) {
	bg := false
	bright := false
	name := a

	if strings.HasPrefix(name, "bg_") || strings.HasPrefix(name, "BG_") {
		bg = true
		name = name[3:]
	}
	if lower := strings.ToLower(name); lower != name {
		if strings.ToUpper(name) == name {
			bright = true
		}
		name = lower
	}

	if off, ok := namedColors[name]; ok {
		base := 30
		switch {
		case bg && bright:
			base = 100
		case bg:
			base = 40
		case bright:
			base = 90
		}
		return strconv.Itoa(base + off), true
	}

	// fg_RGB / fg_NN 256-color forms (after the bg_/fg_ prefix strip: the
	// fg_ prefix is still present for foregrounds).
	if strings.HasPrefix(a, "fg_") {
		name = a[3:]
		bg = false
	} else if !bg {
		return "", false
	}
	sel := "38"
	if bg {
		sel = "48"
	}
	if len(name) == 3 && isDigits(name) {
		r, g, b := int(name[0]-'0'), int(name[1]-'0'), int(name[2]-'0')
		if r <= 5 && g <= 5 && b <= 5 {
			return fmt.Sprintf("%s;5;%d", sel, 16+36*r+6*g+b), true
		}
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 0 && n <= 23 {
		return fmt.Sprintf("%s;5;%d", sel, 232+n), true
	}
	return "", false
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
