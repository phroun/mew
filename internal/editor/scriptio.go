package editor

import (
	"bytes"
	"strings"
	"sync"
)

// Where PawScript's output goes: statusWriter collects #err for display, and
// insertWriter puts #out into the focused buffer at the caret.

// statusWriter captures stderr output and stores it for display.
type statusWriter struct {
	editor *Editor
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (sw *statusWriter) Write(p []byte) (n int, err error) {
	// A running launch-eval batch captures #err instead (see eval.go).
	if sw.editor.evalCaptureWrite(true, p) {
		return len(p), nil
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.buf.Write(p)

	// Check for complete lines
	content := sw.buf.String()
	if idx := strings.LastIndex(content, "\n"); idx != -1 {
		// Extract lines and combine them into a single message
		lines := strings.Split(content[:idx], "\n")
		var messageParts []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				messageParts = append(messageParts, line)
			}
		}
		if len(messageParts) > 0 {
			// Join all lines with spaces and show as notification
			fullMessage := strings.Join(messageParts, " ")
			sw.editor.ShowError(fullMessage)
		}
		// Keep any remaining partial line
		sw.buf.Reset()
		if idx+1 < len(content) {
			sw.buf.WriteString(content[idx+1:])
		}
	}

	return len(p), nil
}

// insertWriter captures stdout output and inserts it at the cursor position.
type insertWriter struct {
	editor *Editor
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (iw *insertWriter) Write(p []byte) (n int, err error) {
	// A running launch-eval batch captures #out instead (see eval.go).
	if iw.editor.evalCaptureWrite(false, p) {
		return len(p), nil
	}

	iw.mu.Lock()
	defer iw.mu.Unlock()

	iw.buf.Write(p)

	// Insert complete content (including newlines) at cursor
	content := iw.buf.String()
	if content != "" {
		iw.editor.insertText(content)
		iw.buf.Reset()
	}

	return len(p), nil
}
