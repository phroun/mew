package editor

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// newTestEditor builds a headless editor with a single focused main-buffer
// window ("doc") holding content. Extra [general] config lines can be
// passed as "key=value" strings.
func newTestEditor(t *testing.T, content string, generalConfig ...string) (*Editor, *window.Window) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	if len(generalConfig) > 0 {
		cfg.ConfigText = ptrTo(sectionizeConfig(generalConfig))
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Editing an opened file arms garland's background backup copy; wait for
	// every buffer's backup to reach a terminal state before the test's
	// TempDir is torn down, so an in-flight write can't race the RemoveAll
	// ("directory not empty"). Registered after the TempDir cleanups, so it
	// runs first (cleanup is LIFO).
	t.Cleanup(func() { settleBackups(e) })
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString(content), SetFocus: true,
	})
	return e, e.WindowManager.GetWindow("doc")
}

func ptrTo[T any](v T) *T { return &v }

// sectionizeConfig routes each "key=value" test config line to the right
// section: structural keys go under [general], everything else (the
// set_option surface) under [options].
func sectionizeConfig(lines []string) string {
	structural := map[string]bool{
		"projectconfig": true, "uselocks": true, "useemacslocks": true,
		"layout": true, "mappings": true, "sequencelength": true,
	}
	var gen, opt []string
	for _, line := range lines {
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(line, "=", 2)[0]))
		if structural[key] {
			gen = append(gen, line)
		} else {
			opt = append(opt, line)
		}
	}
	var b strings.Builder
	if len(gen) > 0 {
		b.WriteString("[general]\n" + strings.Join(gen, "\n") + "\n")
	}
	if len(opt) > 0 {
		b.WriteString("[options]\n" + strings.Join(opt, "\n") + "\n")
	}
	return b.String()
}

// settleBackups waits (briefly) for every open buffer's automatic backup to
// finish streaming, so background writes don't outlive the test.
func settleBackups(e *Editor) {
	deadline := time.Now().Add(3 * time.Second)
	for _, w := range e.getMainBuffers() {
		if w.Buffer == nil {
			continue
		}
		for {
			switch w.Buffer.BackupStatus().State {
			case "pending":
				if time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
					continue
				}
			}
			break
		}
	}
}

// newRenderedEditor is newTestEditor with a virtual terminal attached, for
// tests that inspect the rendered ANSI stream. Returns the output buffer.
func newRenderedEditor(t *testing.T, content string) (*Editor, *window.Window, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.Terminal = &TerminalIO{
		Input:  bytes.NewReader(nil),
		Output: &out,
		Size:   func() (int, int, error) { return 80, 24, nil },
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString(content), SetFocus: true,
	})
	return e, e.WindowManager.GetWindow("doc"), &out
}

// answerPrompt types text into the focused prompt using the real insert
// command and accepts it with the real accept command.
func answerPrompt(t *testing.T, e *Editor, text string) {
	t.Helper()
	fw := e.WindowManager.GetFocusedWindow()
	if fw == nil || fw.Type != window.PromptBuffer {
		t.Fatalf("expected a focused prompt window")
	}
	if text != "" {
		e.PawScript.ExecuteAsync(fmt.Sprintf("insert %q", text))
	}
	e.PawScript.ExecuteAsync("accept")
}

// cancelPrompt cancels the focused prompt with the real cancel command.
func cancelPrompt(t *testing.T, e *Editor) {
	t.Helper()
	e.PawScript.ExecuteAsync("cancel")
}

// focusedPrompt returns the focused prompt window, or nil.
func focusedPrompt(e *Editor) *window.Window {
	fw := e.WindowManager.GetFocusedWindow()
	if fw != nil && fw.Type == window.PromptBuffer {
		return fw
	}
	return nil
}

// windowByClass returns the first window with the given class, or nil.
func windowByClass(e *Editor, class string) *window.Window {
	for _, w := range e.WindowManager.AllWindows() {
		if w.Class == class {
			return w
		}
	}
	return nil
}

// verboseLogContent returns the verbose-log window's content ("" if none).
func verboseLogContent(e *Editor) string {
	if w := windowByClass(e, "verboseLog"); w != nil {
		return w.Buffer.GetContent()
	}
	return ""
}

// hasWarning reports whether a transient warning window contains text.
func hasWarning(e *Editor, text string) bool {
	for _, w := range e.WindowManager.AllWindows() {
		if w.Class == "warning" && strings.Contains(w.MessageTopInner, text) {
			return true
		}
	}
	return false
}

// hasNotification reports whether a transient notification window contains text.
func hasNotification(e *Editor, text string) bool {
	for _, w := range e.WindowManager.AllWindows() {
		if w.Class == "notification" && strings.Contains(w.MessageTopInner, text) {
			return true
		}
	}
	return false
}

// clearNotifications removes all transient notification windows, so a test
// can assert that a LATER step does (or does not) raise a fresh one.
func clearNotifications(e *Editor) {
	for _, w := range e.WindowManager.AllWindows() {
		if w.Class == "notification" {
			e.WindowManager.RemoveWindow(w.ID)
		}
	}
}

// docContent returns the doc window's buffer content without the final
// trailing newline.
func docContent(w *window.Window) string {
	return strings.TrimRight(w.Buffer.GetContent(), "\n")
}

// layoutByClass finds a window layout by class in a calculated layout.
func layoutByClass(l window.Layout, class string) *window.WindowLayout {
	for _, group := range [][]window.WindowLayout{l.TopLayout, l.MainLayout, l.BottomLayout} {
		for i := range group {
			if group[i].Window.Class == class {
				return &group[i]
			}
		}
	}
	return nil
}

// layoutByID finds a window layout by window ID in a calculated layout.
func layoutByID(l window.Layout, id string) *window.WindowLayout {
	for _, group := range [][]window.WindowLayout{l.TopLayout, l.MainLayout, l.BottomLayout} {
		for i := range group {
			if group[i].Window.ID == id {
				return &group[i]
			}
		}
	}
	return nil
}

var cursorSeqRe = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// lastCursor parses the final hardware-cursor position (1-based row, col)
// from a rendered ANSI stream.
func lastCursor(out []byte) (row, col int) {
	ms := cursorSeqRe.FindAllSubmatch(out, -1)
	if len(ms) == 0 {
		return -1, -1
	}
	m := ms[len(ms)-1]
	fmt.Sscanf(string(m[1]), "%d", &row)
	fmt.Sscanf(string(m[2]), "%d", &col)
	return row, col
}
