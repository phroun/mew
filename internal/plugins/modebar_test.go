package plugins

import (
	"strings"
	"testing"
)

func TestExpandModebar(t *testing.T) {
	vals := map[string]string{
		"FN":      "notes.txt",
		"FORTUNE": "mew edits words",
		"LINE":    "42",
	}
	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"single code", "%FN%", "notes.txt"},
		{"code in context", "Line:%LINE%", "Line:42"},
		{"multiple codes", "%FN% @ %LINE%", "notes.txt @ 42"},
		{"case-insensitive code", "%fn%", "notes.txt"},
		{"literal percent", "50%% done", "50% done"},
		{"unknown code left literal", "%NOPE%", "%NOPE%"},
		{"unknown mixed with known", "%FN% %NOPE%", "notes.txt %NOPE%"},
		{"dangling percent", "abc %", "abc %"},
		{"dangling percent after code", "%FN% 100%", "notes.txt 100%"},
		{"empty template", "", ""},
		{"adjacent codes", "%FN%%LINE%", "notes.txt42"},
		{"literal percent then code", "%%%FN%", "%notes.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandModebar(tt.tmpl, vals); got != tt.want {
				t.Errorf("expandModebar(%q) = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

func TestVisualColumn(t *testing.T) {
	tests := []struct {
		name    string
		runes   string
		upto    int
		tabSize int
		want    int
	}{
		{"empty", "", 0, 4, 0},
		{"ascii", "hello", 3, 4, 3},
		{"leading tab", "\tx", 1, 4, 4},
		{"tab mid-stop", "ab\t", 3, 4, 4},
		{"tab already at stop", "abcd\t", 5, 4, 8},
		{"upto past end", "ab", 10, 4, 2},
		{"zero tabsize defaults to 4", "\t", 1, 0, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := visualColumn([]rune(tt.runes), tt.upto, tt.tabSize); got != tt.want {
				t.Errorf("visualColumn(%q, %d, %d) = %d, want %d", tt.runes, tt.upto, tt.tabSize, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0k"},
		{2048, "2k"},
		{1024 * 1024 * 2, "2m"},
		{1024 * 1024 * 1024 * 3, "3g"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEllipsizeRight(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"fits exactly", "hello", 5, "hello"},
		{"fits under", "hi", 5, "hi"},
		{"too long", "hello world", 5, "hell…"},
		{"max one", "hello", 1, "…"},
		{"max zero", "hello", 0, ""},
		{"negative", "hello", -3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ellipsizeRight(tt.s, tt.max); got != tt.want {
				t.Errorf("ellipsizeRight(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestFitPad(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"pad short", "ab", 5, "ab   "},
		{"exact", "abcde", 5, "abcde"},
		{"ellipsize long", "abcdefgh", 5, "abcd…"},
		{"zero width", "abc", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fitPad(tt.s, tt.width)
			if got != tt.want {
				t.Errorf("fitPad(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
			if tt.width > 0 && calculateVisibleLength(got) != tt.width {
				t.Errorf("fitPad(%q, %d) visible length = %d, want %d", tt.s, tt.width, calculateVisibleLength(got), tt.width)
			}
		})
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 3, "hel"},
		{"hello", 5, "hello"},
		{"hello", 10, "hello"},
		{"hello", 0, ""},
	}
	for _, tt := range tests {
		if got := truncateToWidth(tt.s, tt.max); got != tt.want {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

func TestSetTemplatesBlankLeavesCurrent(t *testing.T) {
	m := NewModebar(nil)
	m.SetTemplates("%FN%", "%FORTUNE%", "Line:%LINE%")
	inner, def, outer := m.Templates()
	if inner != "%FN%" || def != "%FORTUNE%" || outer != "Line:%LINE%" {
		t.Fatalf("SetTemplates not applied: %q %q %q", inner, def, outer)
	}
	// Blank values must leave the current template untouched.
	m.SetTemplates("", "", "%LINE%/%RUNE%")
	inner, def, outer = m.Templates()
	if inner != "%FN%" {
		t.Errorf("blank inner overwrote current: got %q", inner)
	}
	if def != "%FORTUNE%" {
		t.Errorf("blank default overwrote current: got %q", def)
	}
	if outer != "%LINE%/%RUNE%" {
		t.Errorf("outer not updated: got %q", outer)
	}
}

func TestExpandModebarRoundTripsThroughTemplate(t *testing.T) {
	vals := map[string]string{"FRAG": "1,234", "HEAP": "5m", "LINE": "7", "RUNE": "3"}
	got := expandModebar("Frag:%FRAG% Heap:%HEAP% Line:%LINE% Rune:%RUNE%", vals)
	want := "Frag:1,234 Heap:5m Line:7 Rune:3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "%") {
		t.Errorf("stray %% in %q", got)
	}
}
