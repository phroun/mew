package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/phroun/mew/internal/window"
)

// --- go_line ---

func TestGoLineDirectArg(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\nl4\nl5\n")
	e.PawScript.ExecuteAsync(`go_line 4 & verbose_log "went"`)
	if w.CursorPos().Line != 3 {
		t.Fatalf("cursor line %d, want 3", w.CursorPos().Line)
	}
	if !strings.Contains(verboseLogContent(e), "went") {
		t.Fatal("go_line 4 should resolve true for &")
	}
}

func TestGoLineDirectArgInvalid(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\n")
	e.PawScript.ExecuteAsync(`go_line "abc" | verbose_log "fallback"`)
	if w.CursorPos().Line != 0 {
		t.Fatalf("cursor should not move, line %d", w.CursorPos().Line)
	}
	if !hasWarning(e, "Invalid line number") {
		t.Fatal("expected Invalid line number warning")
	}
	if !strings.Contains(verboseLogContent(e), "fallback") {
		t.Fatal("go_line abc should resolve false for |")
	}
}

func TestGoLinePromptSuspendsAndResumes(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\nl4\n")
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync(`go_line & verbose_log "went"`)
	if strings.Contains(verboseLogContent(e), "went") {
		t.Fatal("sequence must stay suspended while the prompt is open")
	}
	answerPrompt(t, e, "3")
	if w.CursorPos().Line != 2 {
		t.Fatalf("cursor line %d, want 2", w.CursorPos().Line)
	}
	if !strings.Contains(verboseLogContent(e), "went") {
		t.Fatal("resumed sequence should run the & branch")
	}
}

func TestGoLinePromptInvalidEntry(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\n")
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync(`go_line | verbose_log "fallback"`)
	answerPrompt(t, e, "not-a-number")
	if w.CursorPos().Line != 0 {
		t.Fatalf("cursor should not move, line %d", w.CursorPos().Line)
	}
	if !hasWarning(e, "Invalid line number") {
		t.Fatal("expected Invalid line number warning")
	}
	if !strings.Contains(verboseLogContent(e), "fallback") {
		t.Fatal("invalid entry should resolve false for |")
	}
}

func TestGoLinePromptCancelAndBlank(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\n")
	w.SetCursorPos(window.Position{})
	// Cancel: resolves false, no warning.
	e.PawScript.ExecuteAsync(`go_line | verbose_log "cancelled"`)
	cancelPrompt(t, e)
	if !strings.Contains(verboseLogContent(e), "cancelled") {
		t.Fatal("cancel should resolve false for |")
	}
	if hasWarning(e, "Invalid line number") {
		t.Fatal("cancel must not warn")
	}
	// Blank accept: resolves false, no warning, cursor unmoved.
	e.PawScript.ExecuteAsync(`go_line | verbose_log "blank"`)
	answerPrompt(t, e, "")
	if w.CursorPos().Line != 0 {
		t.Fatal("blank accept should not move the cursor")
	}
	if hasWarning(e, "Invalid line number") {
		t.Fatal("blank accept must not warn")
	}
	if !strings.Contains(verboseLogContent(e), "blank") {
		t.Fatal("blank accept should resolve false for |")
	}
}

func TestGoLineHistoryAccessibleNotDefaulted(t *testing.T) {
	e, _ := newTestEditor(t, "l1\nl2\nl3\nl4\n")
	e.PawScript.ExecuteAsync("go_line")
	answerPrompt(t, e, "4")
	// Second prompt: history holds "4", cursor line is a fresh blank.
	e.PawScript.ExecuteAsync("go_line")
	fw := focusedPrompt(e)
	if fw == nil {
		t.Fatal("expected prompt")
	}
	cursorLine := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line), "\n\r")
	if cursorLine != "" {
		t.Fatalf("prompt must start blank, cursor line %q", cursorLine)
	}
	if !strings.Contains(fw.Buffer.GetContent(), "4") {
		t.Fatalf("history should contain the previous entry: %q", fw.Buffer.GetContent())
	}
	cancelPrompt(t, e)
}

// --- go_match ---

func TestGoMatchFalseChains(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\n")
	w.SetCursorPos(window.Position{}) // on 'l', not a bracket
	e.PawScript.ExecuteAsync(`go_match | verbose_log "nomatch"`)
	if !hasWarning(e, "Nothing to match here") {
		t.Fatal("expected nothing-to-match warning")
	}
	if !strings.Contains(verboseLogContent(e), "nomatch") {
		t.Fatal("go_match should resolve false for |")
	}
}

func TestGoMatchBracketJump(t *testing.T) {
	e, w := newTestEditor(t, "a (b [c] d) e\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 2}) // on '('
	e.PawScript.ExecuteAsync("go_match")
	if w.CursorPos().Rune != 10 {
		t.Fatalf("should jump to ')', got %v", w.CursorPos())
	}
	e.PawScript.ExecuteAsync("go_match")
	if w.CursorPos().Rune != 2 {
		t.Fatalf("should jump back to '(', got %v", w.CursorPos())
	}
}

// --- Nested suspension (Esc X command prompt) ---

// Typing a suspending command into the command prompt must not deadlock:
// both layers run on the main-loop goroutine via the async path.
func TestCmdPromptNestedSuspension(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		e, w := newTestEditor(t, "l1\nl2\nl3\nl4\n")
		w.SetCursorPos(window.Position{})
		e.executeCommand("cmd") // Esc X
		fw := focusedPrompt(e)
		if fw == nil {
			t.Error("expected command prompt")
			return
		}
		e.PawScript.ExecuteAsync(`insert "go_line"`)
		e.PawScript.ExecuteAsync("accept")
		e.PawScript.ExecuteAsync(`insert "3"`)
		e.PawScript.ExecuteAsync("accept")
		if w.CursorPos().Line != 2 {
			t.Errorf("cursor line %d, want 2", w.CursorPos().Line)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlocked: cmd prompt blocked on nested suspension")
	}
}

// --- Prompt timeout ---

// A prompt whose suspension token has expired must default to failure:
// answering it performs no action and warns.
func TestGoLinePromptTimeoutFailsSafe(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\nl4\n")
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync("go_line")

	// Simulate the timeout: force-clean every plausible token ID
	// ("fiber-F-token-N"), which runs the same cleanup path (including the
	// command's cleanup callback) that expiry runs.
	for f := 0; f < 8; f++ {
		for i := 0; i < 64; i++ {
			e.PawScript.ForceCleanupToken(fmt.Sprintf("fiber-%d-token-%d", f, i))
		}
	}

	answerPrompt(t, e, "3")
	if w.CursorPos().Line != 0 {
		t.Fatalf("expired prompt must not act: cursor moved to line %d", w.CursorPos().Line)
	}
	if !hasWarning(e, "Prompt timed out") {
		t.Fatal("expected 'Prompt timed out' warning")
	}
}

// promptTimeout=1 exercises the genuine expiry path end to end.
func TestPromptTimeoutConfigurable(t *testing.T) {
	e, w := newTestEditor(t, "l1\nl2\nl3\nl4\n", "promptTimeout=1")
	if e.Config.PromptTimeout != 1 {
		t.Fatalf("promptTimeout not loaded: %d", e.Config.PromptTimeout)
	}
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync("go_line")
	time.Sleep(1500 * time.Millisecond)
	answerPrompt(t, e, "3")
	if w.CursorPos().Line != 0 {
		t.Fatalf("expired prompt acted: cursor line %d", w.CursorPos().Line)
	}
	if !hasWarning(e, "Prompt timed out") {
		t.Fatal("expected 'Prompt timed out' warning after real expiry")
	}
}

func TestTimeoutOptions(t *testing.T) {
	e, _ := newTestEditor(t, "x\n", "promptTimeout=17", "scriptTimeout=0")
	if e.Config.PromptTimeout != 17 || e.Config.ScriptTimeout != 0 {
		t.Fatalf("config: prompt=%d script=%d", e.Config.PromptTimeout, e.Config.ScriptTimeout)
	}
	if e.pawConfig.DefaultTokenTimeout > 0 {
		t.Fatalf("scriptTimeout=0 should disable the token timeout: %v", e.pawConfig.DefaultTokenTimeout)
	}
	e.PawScript.ExecuteAsync("set_option scriptTimeout, 42")
	if e.Config.ScriptTimeout != 42 || e.pawConfig.DefaultTokenTimeout != 42*time.Second {
		t.Fatalf("set_option scriptTimeout: cfg=%d paw=%v", e.Config.ScriptTimeout, e.pawConfig.DefaultTokenTimeout)
	}
	e.PawScript.ExecuteAsync("set_option promptTimeout, 9")
	if v, _ := e.getOption(nil, "promptTimeout"); v != "9" {
		t.Fatalf("get_option promptTimeout: %s", v)
	}
}
