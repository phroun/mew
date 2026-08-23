//go:build sdl

package main

import (
	"testing"

	"github.com/phroun/argwild"
)

// Command-line switches override kittytk.ini's renderer; the last one on
// the line wins, --sdl aliases --software, --renderer=NAME passes
// through, and an explicitly disabled switch is ignored.
func TestRendererFromArgs(t *testing.T) {
	cases := []struct {
		line       string
		configured string
		want       string
	}{
		{"", "software", "software"},
		{"--webgpu", "software", "webgpu"},
		{"--software", "webgpu", "software"},
		{"--sdl", "webgpu", "software"},
		{"--renderer=webgpu", "software", "webgpu"},
		{"--webgpu --software", "webgpu", "software"}, // last wins
		{"--webgpu-", "software", "software"},         // off state ignored
		{"--verbose other.file", "webgpu", "webgpu"},  // unrelated args
	}
	for _, c := range cases {
		r, err := argwild.ParseString(c.line)
		if err != nil {
			t.Fatalf("ParseString(%q): %v", c.line, err)
		}
		if got := rendererFromArgs(r, c.configured); got != c.want {
			t.Errorf("rendererFromArgs(%q, %q) = %q, want %q", c.line, c.configured, got, c.want)
		}
	}
}
