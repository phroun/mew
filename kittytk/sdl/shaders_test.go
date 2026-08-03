package sdl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These tests guard the two things about the compositor's shaders that
// no compiler and no GPU validation layer will catch, and that fail by
// drawing the wrong thing rather than by erroring:
//
//  1. The uniform block must be declared identically in both stages and
//     must match the Go struct written into the buffer.
//  2. Every binding must land on the Metal slot the encoder binds it to.
//
// Both are pure text and arithmetic, so they run with no SDL library,
// no GPU, and no build tags.

// shaderDecl is one `@group(g) @binding(b) var ...` from a WGSL module.
type shaderDecl struct {
	group   int
	binding int
	kind    bindingKind
	name    string
}

var declPattern = regexp.MustCompile(
	`@group\((\d+)\)\s*@binding\((\d+)\)\s*var(?:<[^>]*>)?\s+(\w+)\s*:\s*([^;]+);`)

// parseShaderDecls extracts the module-scope resource declarations from
// WGSL source, in source order.
func parseShaderDecls(t *testing.T, src string) []shaderDecl {
	t.Helper()
	var decls []shaderDecl
	for _, m := range declPattern.FindAllStringSubmatch(src, -1) {
		group, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("unparsable @group in %q: %v", m[0], err)
		}
		binding, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("unparsable @binding in %q: %v", m[0], err)
		}
		typeText := strings.TrimSpace(m[4])
		var kind bindingKind
		switch {
		case strings.HasPrefix(typeText, "texture_"):
			kind = bindingTexture
		case typeText == "sampler" || typeText == "sampler_comparison":
			kind = bindingSampler
		default:
			kind = bindingBuffer
		}
		decls = append(decls, shaderDecl{group: group, binding: binding, kind: kind, name: m[3]})
	}
	return decls
}

// encoderSlots reproduces how the command encoder assigns Metal slots
// from a PIPELINE LAYOUT: buffers, textures and samplers each count up
// independently, walking every group in order and every binding within
// a group in order. Keyed by "group/binding".
func encoderSlots(groups [][]bindingKind) map[string]int {
	slots := make(map[string]int)
	next := map[bindingKind]int{}
	for g, kinds := range groups {
		for b, kind := range kinds {
			slots[fmt.Sprintf("%d/%d", g, b)] = next[kind]
			next[kind]++
		}
	}
	return slots
}

// moduleSlots reproduces how the WGSL→MSL translator assigns Metal slots
// for ONE shader module: its own declarations sorted by (group, binding),
// then numbered sequentially per resource type. The translator never sees
// the pipeline layout, which is exactly why the two can disagree.
func moduleSlots(decls []shaderDecl) map[string]int {
	sorted := append([]shaderDecl(nil), decls...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			a, b := sorted[j-1], sorted[j]
			if a.group < b.group || (a.group == b.group && a.binding <= b.binding) {
				break
			}
			sorted[j-1], sorted[j] = b, a
		}
	}
	slots := make(map[string]int)
	next := map[bindingKind]int{}
	for _, d := range sorted {
		slots[fmt.Sprintf("%d/%d", d.group, d.binding)] = next[d.kind]
		next[d.kind]++
	}
	return slots
}

// The blit pipeline's two stages must each name the same Metal slot the
// encoder binds their resources to. The translator numbers a module's
// bindings over that module's own globals; the encoder numbers them over
// the whole pipeline layout. They agree only while no group before a
// binding's group contributes a resource of the same kind.
//
// This is the regression for drop shadows drawing nothing: a separate
// shadow pipeline put a uniform in group 0, so the vertex module — which
// declares only the group 1 uniform and therefore names buffer 0 — read
// the shadow parameters as its position uniforms and threw every shadow
// quad off screen. No error, no validation warning, just no shadows.
func TestBlitShaderBindingSlotsMatchPipelineLayout(t *testing.T) {
	want := encoderSlots(blitBindGroups)

	for _, stage := range []struct {
		name string
		src  string
	}{
		{"vertex", blitVertexShader},
		{"fragment", blitFragmentShader},
	} {
		decls := parseShaderDecls(t, stage.src)
		if len(decls) == 0 {
			t.Fatalf("%s stage: no @group/@binding declarations found", stage.name)
		}
		got := moduleSlots(decls)

		for _, d := range decls {
			key := fmt.Sprintf("%d/%d", d.group, d.binding)
			expected, ok := want[key]
			if !ok {
				t.Errorf("%s stage: %s declares @group(%d) @binding(%d), which blitBindGroups does not describe",
					stage.name, d.name, d.group, d.binding)
				continue
			}
			if kinds := blitBindGroups[d.group]; d.binding < len(kinds) && kinds[d.binding] != d.kind {
				t.Errorf("%s stage: %s at @group(%d) @binding(%d) is kind %d, blitBindGroups says %d",
					stage.name, d.name, d.group, d.binding, d.kind, kinds[d.binding])
			}
			if got[key] != expected {
				t.Errorf("%s stage: %s at @group(%d) @binding(%d) compiles to slot %d "+
					"but the encoder binds it at slot %d — the stage would read the wrong resource",
					stage.name, d.name, d.group, d.binding, got[key], expected)
			}
		}
	}
}

// A checker that can only ever pass is worth nothing, so run the two
// numbering rules against the layout that actually broke: the retired
// drop-shadow pipeline, whose group 0 was a uniform buffer and group 1
// the shared position block. The vertex module declares only the group 1
// uniform, so it compiles to buffer slot 0 while the encoder binds it at
// slot 1 — and that gap is the whole bug.
func TestBindingSlotCheckerCatchesGroupZeroBuffer(t *testing.T) {
	broken := [][]bindingKind{
		0: {bindingBuffer}, // shadow parameters
		1: {bindingBuffer}, // shared position/effect block
	}
	vertexOnly := []shaderDecl{{group: 1, binding: 0, kind: bindingBuffer, name: "uniforms"}}

	fromEncoder := encoderSlots(broken)["1/0"]
	fromModule := moduleSlots(vertexOnly)["1/0"]
	if fromEncoder == fromModule {
		t.Fatalf("expected the two numbering rules to disagree on a group-0 buffer layout, "+
			"both said slot %d — the checker cannot detect the bug it exists for", fromEncoder)
	}
	if fromModule != 0 || fromEncoder != 1 {
		t.Errorf("module slot = %d, encoder slot = %d; want 0 and 1", fromModule, fromEncoder)
	}
}

// A buffer in group 0 is the specific arrangement that breaks the vertex
// stage, so name it directly: the failure above is a puzzle, this one is
// an instruction.
func TestBlitGroupZeroHasNoBuffer(t *testing.T) {
	for binding, kind := range blitBindGroups[0] {
		if kind == bindingBuffer {
			t.Fatalf("blitBindGroups group 0 binding %d is a buffer; group 0 must hold only "+
				"textures and samplers so the group 1 uniform lands on Metal buffer slot 0", binding)
		}
	}
}

// wgslBlockFloats returns the size, in 4-byte words, of a WGSL struct
// whose members are all f32 or vec2<f32> — the shape of CombinedUniforms.
// vec2<f32> needs 8-byte alignment, which can insert a word of padding
// the Go side has to account for.
func wgslBlockFloats(t *testing.T, body string) int {
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
		switch typeText := strings.TrimSpace(parts[1]); typeText {
		case "f32":
			words++
		case "vec2<f32>":
			if words%2 != 0 {
				words++ // 8-byte alignment
			}
			words += 2
		default:
			t.Fatalf("member %q has type %q; this test only models f32 and vec2<f32>", line, typeText)
		}
	}
	return words
}

// combinedUniformsBody extracts the CombinedUniforms struct body.
func combinedUniformsBody(t *testing.T, src string) string {
	t.Helper()
	const header = "struct CombinedUniforms {"
	start := strings.Index(src, header)
	if start < 0 {
		t.Fatal("shader does not declare struct CombinedUniforms")
	}
	start += len(header)
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatal("struct CombinedUniforms is not closed")
	}
	return src[start : start+end]
}

// Both stages read the SAME uniform buffer, so both must declare the
// block identically — a field added to one alone silently reinterprets
// every field after it in the other. And the Go side writes a fixed
// [combinedUniformFloats]float32 into that buffer, so the block must
// fit it exactly, padding included.
func TestCombinedUniformsBlockMatchesGoLayout(t *testing.T) {
	vertexBody := combinedUniformsBody(t, blitVertexShader)
	fragmentBody := combinedUniformsBody(t, blitFragmentShader)
	if vertexBody != fragmentBody {
		t.Error("CombinedUniforms differs between the vertex and fragment stages; " +
			"they read one buffer and must agree field for field")
	}

	words := wgslBlockFloats(t, vertexBody)
	if words > combinedUniformFloats {
		t.Errorf("CombinedUniforms needs %d words (%d bytes) but the Go block writes only %d (%d bytes)",
			words, words*4, combinedUniformFloats, combinedUniformSize)
	}
	if combinedUniformSize%16 != 0 {
		t.Errorf("combinedUniformSize = %d, want a multiple of 16 for uniform buffer alignment",
			combinedUniformSize)
	}

	var block combinedUniformData
	if got := len(combinedUniformBytes(&block)); got != combinedUniformSize {
		t.Errorf("combinedUniformBytes wrote %d bytes, want %d", got, combinedUniformSize)
	}
}

// The shadow branch is reached by the uniform block's mode field, and
// mode is written by createShadowUniformBuffer at a fixed word index.
// If the block's leading fields move, mode moves with them and ordinary
// layers start drawing as shadows (or shadows as textures).
func TestShadowModeIsWordEight(t *testing.T) {
	body := combinedUniformsBody(t, blitVertexShader)

	var names []string
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" {
			continue
		}
		names = append(names, strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
	}

	const modeWord = 8
	if len(names) <= modeWord || names[modeWord] != "mode" {
		t.Fatalf("CombinedUniforms member %d is %v, want \"mode\" — "+
			"drawShadow and writeCombinedUniforms both write mode at word %d",
			modeWord, names[min(modeWord, len(names)-1)], modeWord)
	}
	for i, want := range []string{
		"angle", "enabled", "scale", "aspect", "pos_x", "pos_y", "size_w", "size_h",
	} {
		if names[i] != want {
			t.Errorf("CombinedUniforms member %d is %q, want %q", i, names[i], want)
		}
	}
}
