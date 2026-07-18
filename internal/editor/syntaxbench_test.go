package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// bigGoSource synthesizes n lines of plausible Go so the highlighter has real
// keywords, strings, and comments to tokenize.
func bigGoSource(n int) string {
	var b strings.Builder
	b.WriteString("package big\n\nimport \"fmt\"\n\n")
	i := 4
	for i < n {
		fmt.Fprintf(&b, "// doc comment for widget %d\n", i)
		fmt.Fprintf(&b, "func Widget%d(x int) string {\n", i)
		fmt.Fprintf(&b, "\ts := fmt.Sprintf(\"value=%%d\", x*%d)\n", i)
		fmt.Fprintf(&b, "\tif x > %d {\n\t\treturn s + \"big\"\n\t}\n", i)
		b.WriteString("\treturn s\n}\n")
		i += 6
	}
	return b.String()
}

func newSyntaxBenchEditor(b *testing.B, content string) (*Editor, *window.Window) {
	b.Helper()
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = b.TempDir()
	e, err := New(cfg)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString(content), SetFocus: true,
	})
	if !e.setSyntax("go") {
		b.Fatal("setSyntax(go) failed")
	}
	return e, e.WindowManager.GetWindow("doc")
}

// Cost to reach the LAST line from a cold cache — this is what "jump to end of
// a 2000-line file" pays today: the whole dense prefix is tokenized.
func BenchmarkSyntaxColdReachBottom(b *testing.B) {
	src := bigGoSource(2000)
	e, w := newSyntaxBenchEditor(b, src)
	last := w.Buffer.GetLineCount() - 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.resetSyntaxCaches()
		if c := e.ensureSynCache(w.Buffer, last); c == nil {
			b.Fatal("nil cache")
		}
	}
}

// Cost to color just the top viewport from a cold cache (the common case).
func BenchmarkSyntaxColdReachTop(b *testing.B) {
	src := bigGoSource(2000)
	e, w := newSyntaxBenchEditor(b, src)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.resetSyntaxCaches()
		if c := e.ensureSynCache(w.Buffer, 40); c == nil {
			b.Fatal("nil cache")
		}
	}
}

// Cost of a keystroke at the TOP of a large file while viewing the bottom:
// the watermark drops to line 0 region and everything down to the viewport is
// retokenized. This is the pathological per-edit case.
func BenchmarkSyntaxEditTopViewBottom(b *testing.B) {
	src := bigGoSource(2000)
	e, w := newSyntaxBenchEditor(b, src)
	last := w.Buffer.GetLineCount() - 1
	// Warm the cache all the way down.
	e.ensureSynCache(w.Buffer, last)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate a content edit near the top: touch line 2, then re-reach bottom.
		w.Buffer.BeginUserCommand("bench")
		w.SetCursorPos(window.Position{Line: 2, Rune: 0})
		e.insertText("x")
		w.Buffer.EndUserCommand()
		if c := e.ensureSynCache(w.Buffer, last); c == nil {
			b.Fatal("nil cache")
		}
	}
}

// Outline breadcrumb recompute at a deep caret line — runs only when a grammar
// applies (grammar-gated), and scans backward with per-line GetLine.
func BenchmarkOutlineDeepLine(b *testing.B) {
	src := bigGoSource(2000)
	e, w := newSyntaxBenchEditor(b, src)
	last := w.Buffer.GetLineCount() - 1
	e.ensureSynCache(w.Buffer, last) // warm highlight cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.outlineMemoVal = nil // force recompute (as a caret-line change would)
		w.SetCursorPos(window.Position{Line: last - (i % 3), Rune: 0})
		_ = e.outlineContext(w)
	}
}
