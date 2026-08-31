package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/phroun/argwild"
	"github.com/phroun/mew/internal/viewport"
)

// evalTargets maps each queued launch eval to its viewport's filename, in
// order, for asserting the sticky walk.
func evalTargets(e *Editor) []string {
	var out []string
	for _, le := range e.launchEvals {
		name := "?"
		if w := e.ViewportManager.GetViewport(le.winID); w != nil && w.Buffer != nil {
			name = w.Buffer.GetFilename()
		}
		out = append(out, name+"|"+le.script)
	}
	return out
}

// --eval applies to the following file and stays sticky across later files;
// --eval= turns it off; a later --eval replaces the script.
func TestEvalPlanStickyAndClear(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")
	c := writeTemp(t, "c.txt", "ccc\n")
	d := writeTemp(t, "d.txt", "ddd\n")

	if err := launch(t, e, "--eval=cmd_one", a, b, "--eval=", c, "--eval=cmd_two", d); err != nil {
		t.Fatalf("launch: %v", err)
	}
	got := evalTargets(e)
	want := []string{a + "|cmd_one", b + "|cmd_one", d + "|cmd_two"}
	if len(got) != len(want) {
		t.Fatalf("evals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("eval[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The natural shell spelling `--eval="insert 'hi'"` reaches argv as one token
// with interior spaces; argwild degrades it to an operand, and the plan
// builder reclaims it as the eval switch.
func TestEvalOperandRescue(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")

	if err := launch(t, e, `--eval=insert 'hi'`, a); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e.launchEvals) != 1 || e.launchEvals[0].script != "insert 'hi'" {
		t.Fatalf("evals = %v, want one insert script", evalTargets(e))
	}

	// Literal quotes passed through by a host lose one symmetric layer.
	e2 := newBareEditor(t)
	if err := launch(t, e2, `--eval="echo test"`, writeTemp(t, "b.txt", "b\n")); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e2.launchEvals) != 1 || e2.launchEvals[0].script != "echo test" {
		t.Fatalf("evals = %v, want unquoted echo script", evalTargets(e2))
	}

	// A rescued blank --eval= clears the sticky script.
	e3 := newBareEditor(t)
	a3 := writeTemp(t, "a.txt", "a\n")
	b3 := writeTemp(t, "b.txt", "b\n")
	if err := launch(t, e3, `--eval=insert 'x'`, a3, "--eval=", b3); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e3.launchEvals) != 1 {
		t.Fatalf("evals = %v, want only the first file's", evalTargets(e3))
	}
}

// A parenthesized PSL list value is a list of commands, joined into one
// sequence.
func TestEvalPSLListJoins(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")
	if err := launch(t, e, `--eval=(cmd_one,cmd_two)`, a); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e.launchEvals) != 1 || e.launchEvals[0].script != "cmd_one; cmd_two" {
		t.Fatalf("evals = %v, want joined sequence", evalTargets(e))
	}
}

// Like a trailing +N, a trailing --eval that no file followed runs against the
// last-opened viewport; with no files at all it runs against the implicit
// empty buffer.
func TestEvalTrailingAndImplicitBuffer(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")
	if err := launch(t, e, a, "--eval=cmd_late"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e.launchEvals) != 1 {
		t.Fatalf("trailing eval should attach to the last viewport, got %v", evalTargets(e))
	}
	if w := e.ViewportManager.GetViewport(e.launchEvals[0].winID); w == nil || w.Buffer.GetFilename() != a {
		t.Fatalf("trailing eval should target %s, got %v", a, evalTargets(e))
	}

	e2 := newBareEditor(t)
	if err := launch(t, e2, "--eval=cmd_solo"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(e2.launchEvals) != 1 {
		t.Fatalf("no-file eval should attach to the implicit buffer, got %v", evalTargets(e2))
	}
	if w := e2.ViewportManager.GetViewport(e2.launchEvals[0].winID); w == nil || w.Buffer.GetFilename() != "" {
		t.Fatal("no-file eval should target the implicit unnamed buffer")
	}
}

// A batch whose scripts emit output runs each script focused on its own file,
// captures #out instead of inserting it at the cursor, and dumps the
// accumulated text into one final fresh buffer, focused.
func TestEvalRunsCapturesAndDumpsToBuffer(t *testing.T) {
	e := newBareEditor(t)
	e.Running = true
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")
	if err := launch(t, e, `--eval=echo "hello"`, a, b); err != nil {
		t.Fatalf("launch: %v", err)
	}
	before := len(e.contentViewports())

	e.continueLaunchEvals(0)

	// The echoes never landed in the documents.
	for _, w := range e.contentViewports() {
		if w.Buffer.GetFilename() != "" && strings.Contains(w.Buffer.GetContent(), "hello") {
			t.Fatalf("echo output leaked into %s", w.Buffer.GetFilename())
		}
	}
	after := e.contentViewports()
	if len(after) != before+1 {
		t.Fatalf("want one results viewport added, had %d now %d", before, len(after))
	}
	fw := e.ViewportManager.GetFocusedViewport()
	if fw == nil || fw.Buffer == nil {
		t.Fatal("results buffer should be focused")
	}
	if got := fw.Buffer.GetContent(); got != "hello\nhello\n" {
		t.Fatalf("results buffer content = %q, want two hello lines", got)
	}
	if fw.Buffer.GetFilename() != "" {
		t.Fatal("results buffer should be unnamed")
	}
	// Capture is off again: later script output inserts normally.
	if e.evalCap.active {
		t.Fatal("capture should be off after the batch")
	}
}

// A batch with no output creates no results buffer and restores the launch
// focus (the first file).
func TestEvalBlankOutputRestoresFocus(t *testing.T) {
	e := newBareEditor(t)
	e.Running = true
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")
	if err := launch(t, e, a, "--eval=go_line_end", b); err != nil {
		t.Fatalf("launch: %v", err)
	}
	before := len(e.contentViewports())

	e.continueLaunchEvals(0)

	if len(e.contentViewports()) != before {
		t.Fatal("blank batch should not add a results viewport")
	}
	fw := e.ViewportManager.GetFocusedViewport()
	if fw == nil || fw.Buffer == nil || fw.Buffer.GetFilename() != a {
		t.Fatal("launch focus (first file) should be restored")
	}
}

// A script that tells mew to exit ends the batch; the captured output skips
// the results buffer and lands on the real streams, in occurrence order.
func TestEvalExitDumpsToStdio(t *testing.T) {
	e := newBareEditor(t)
	e.Running = true
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")
	if err := launch(t, e, `--eval=(echo "bye"; exit)`, a, "--eval=echo \"never\"", b); err != nil {
		t.Fatalf("launch: %v", err)
	}
	before := len(e.contentViewports())

	e.continueLaunchEvals(0)

	if e.Running {
		t.Fatal("exit should stop the session")
	}
	if len(e.contentViewports()) != before {
		t.Fatal("exit batch should not add a results viewport")
	}
	var out, errOut bytes.Buffer
	e.dumpEvalOutput(&out, &errOut)
	if got := out.String(); got != "bye\n" {
		t.Fatalf("stdout = %q, want %q (second eval must not run)", got, "bye\n")
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}
	// The dump drains the log: a second dump emits nothing.
	out.Reset()
	e.dumpEvalOutput(&out, &errOut)
	if out.Len() != 0 {
		t.Fatal("dump should drain the capture log")
	}
}

// Each script runs focused on the file it was declared for.
func TestEvalRunsAgainstItsOwnFile(t *testing.T) {
	e := newBareEditor(t)
	e.Running = true
	a := writeTemp(t, "a.txt", "aaa\n")
	b := writeTemp(t, "b.txt", "bbb\n")
	if err := launch(t, e, "--eval=insert 'X'", a, b); err != nil {
		t.Fatalf("launch: %v", err)
	}

	e.continueLaunchEvals(0)

	var wa, wb *viewport.Viewport
	for _, w := range e.contentViewports() {
		switch w.Buffer.GetFilename() {
		case a:
			wa = w
		case b:
			wb = w
		}
	}
	if wa == nil || wb == nil {
		t.Fatal("both files should be open")
	}
	if got := wa.Buffer.GetContent(); !strings.HasPrefix(got, "X") {
		t.Fatalf("a.txt content = %q, want leading X", got)
	}
	if got := wb.Buffer.GetContent(); !strings.HasPrefix(got, "X") {
		t.Fatalf("b.txt content = %q, want leading X", got)
	}
}

// The parenthesized multi-command spelling arrives as plain text (it is not
// valid PSL) and executes as one PawScript sequence.
func TestEvalParenSequenceExecutes(t *testing.T) {
	e := newBareEditor(t)
	e.Running = true
	a := writeTemp(t, "a.txt", "aaa\n")
	if err := launch(t, e, `--eval=(echo "one"; echo "two")`, a); err != nil {
		t.Fatalf("launch: %v", err)
	}

	e.continueLaunchEvals(0)

	fw := e.ViewportManager.GetFocusedViewport()
	if fw == nil || fw.Buffer == nil || fw.Buffer.GetFilename() != "" {
		t.Fatal("results buffer should be focused")
	}
	if got := fw.Buffer.GetContent(); got != "one\ntwo\n" {
		t.Fatalf("results = %q, want both echoes in order", got)
	}
}

// --eval survives a full parse through argwild's string front end (the host
// EditArgs path), where the quoted value carries spaces with full fidelity.
func TestEvalParseStringPath(t *testing.T) {
	e := newBareEditor(t)
	a := writeTemp(t, "a.txt", "aaa\n")
	r, err := argwild.ParseString(`--eval="insert 'hi'" ` + a)
	if err != nil {
		t.Fatalf("argwild parse: %v", err)
	}
	plan, err := buildLaunchPlan(r)
	if err != nil {
		t.Fatalf("buildLaunchPlan: %v", err)
	}
	if _, err := e.applyLaunch(plan); err != nil {
		t.Fatalf("applyLaunch: %v", err)
	}
	if len(e.launchEvals) != 1 || e.launchEvals[0].script != "insert 'hi'" {
		t.Fatalf("evals = %v, want the quoted script verbatim", evalTargets(e))
	}
}
