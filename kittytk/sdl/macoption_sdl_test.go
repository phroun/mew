//go:build sdl

package sdl

import "testing"

func TestDecodeMacOSOptionChar(t *testing.T) {
	cases := map[rune]string{
		'å': "M-a", // Option+a  -> Select All shortcut
		'ç': "M-c", // Option+c
		'∂': "M-d", // Option+d
		'¬': "M-l", // Option+l
		'Ω': "M-z", // Option+z
		'Å': "M-A", // Option+Shift+a
		'¡': "M-1", // Option+1
		'≠': "M-=", // Option+=
	}
	for r, want := range cases {
		got, ok := decodeMacOSOptionChar(r)
		if !ok || got != want {
			t.Errorf("decode %q = %q, %v; want %q, true", r, got, ok, want)
		}
	}

	// Ordinary characters are not decoded - including the two ASCII entries
	// (" and `) that the table lists but which collide with plain typing.
	for _, r := range []rune{'a', 'A', '1', ' ', 'z', '"', '`'} {
		if got, ok := decodeMacOSOptionChar(r); ok {
			t.Errorf("decode %q unexpectedly matched %q", r, got)
		}
	}
}
