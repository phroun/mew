package sdl

// The word the fragment stage reads a layer's opacity from, and what an
// unset one means.

import (
	"strings"
	"testing"
)

// wordOfMember is the word index a named member of CombinedUniforms starts
// at, counted the way WGSL lays the block out.
func wordOfMember(t *testing.T, body, name string) (int, bool) {
	t.Helper()
	words := 0
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			t.Fatalf("unparsable struct member %q", line)
		}
		member := strings.TrimSpace(parts[0])
		switch typeText := strings.TrimSpace(parts[1]); typeText {
		case "f32":
			if member == name {
				return words, true
			}
			words++
		case "vec2<f32>":
			if words%2 != 0 {
				words++ // 8-byte alignment
			}
			if member == name {
				return words, true
			}
			words += 2
		default:
			t.Fatalf("member %q has type %q; this test only models f32 and vec2<f32>", line, typeText)
		}
	}
	return 0, false
}

// setOpacity writes at a fixed word, so the shader had better read it from
// the same one. Nothing errors if they disagree -- the layer simply draws at
// whatever unrelated number happens to sit there.
func TestOpacityIsTheWordTheGoSideWrites(t *testing.T) {
	for _, stage := range []struct {
		name string
		src  string
	}{
		{"vertex", blitVertexShader},
		{"fragment", blitFragmentShader},
	} {
		body := combinedUniformsBody(t, stage.src)
		at, ok := wordOfMember(t, body, "opacity")
		if !ok {
			t.Fatalf("the %s stage's block declares no opacity", stage.name)
		}
		if at != combinedUniformOpacityWord {
			t.Errorf("the %s stage reads opacity from word %d; setOpacity writes word %d",
				stage.name, at, combinedUniformOpacityWord)
		}
	}
}

// Every layer shares this block, and one that never thought about opacity
// leaves the word zero. Taken at face value that would make it invisible, so
// zero reads as solid -- on both sides of the boundary.
func TestALayerThatSaysNothingIsSolid(t *testing.T) {
	var block combinedUniformData
	block.setOpacity(1)
	if got := block[combinedUniformOpacityWord]; got != 0 {
		t.Errorf("a solid layer wrote %v; it should leave the word as the zero that means solid", got)
	}
	block.setOpacity(0)
	if got := block[combinedUniformOpacityWord]; got != 0 {
		t.Errorf("an invisible layer wrote %v; it is not drawn at all, so it says nothing", got)
	}
	block.setOpacity(0.25)
	if got := block[combinedUniformOpacityWord]; got != 0.25 {
		t.Errorf("a quarter-solid layer wrote %v, want 0.25", got)
	}

	// And the shader's own guard says the same.
	if !strings.Contains(blitFragmentShader, "uniforms.opacity > 0.0") {
		t.Error("the fragment stage does not treat a zero opacity as solid")
	}
}

// The blend is SrcAlpha/OneMinusSrcAlpha, so scaling the sampled alpha is the
// whole of fading a layer. A stage that reads opacity and then returns its
// texture untouched draws every layer solid and errors nowhere.
func TestTheFragmentStageScalesAlphaByOpacity(t *testing.T) {
	if !strings.Contains(blitFragmentShader, "tex.a * uniforms.opacity") {
		t.Error("the fragment stage does not scale its sampled alpha by opacity")
	}
	// The colour goes through unscaled: scaling it as well would darken a
	// fading layer toward black instead of letting the blend carry it.
	if !strings.Contains(blitFragmentShader, "vec4<f32>(tex.rgb, tex.a * uniforms.opacity)") {
		t.Error("the faded layer's colour is not passed through as sampled")
	}
}
