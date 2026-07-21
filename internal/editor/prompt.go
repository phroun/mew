// Package editor provides the core text editor functionality.
package editor

import (
	"strings"
	"sync"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/window"
)

// PromptCallback is the callback signature for prompt completion.
// Parameters:
//   - accepted: true if user pressed accept (Enter), false if cancelled
//   - bufferContent: content of the first line (for backward compatibility)
//   - cursorLineText: content of the line where cursor was positioned
type PromptCallback func(accepted bool, bufferContent, cursorLineText string)

// PromptManager handles user prompts with consistent UI behavior and history management.
type PromptManager struct {
	editor        *Editor
	promptHistory map[string][]string // History by window class
	mu            sync.Mutex
	maxHistory    int
}

// NewPromptManager creates a new prompt manager.
func NewPromptManager(editor *Editor) *PromptManager {
	return &PromptManager{
		editor:        editor,
		promptHistory: make(map[string][]string),
		maxHistory:    10,
	}
}

// PromptForInput prompts the user for input with optional history support.
// Prompts display a single row; history lines above the cursor line remain
// reachable by arrow, just not shown.
func (pm *PromptManager) PromptForInput(message, defaultValue string, callback PromptCallback, windowClass string) {
	pm.promptForInput(message, defaultValue, callback, windowClass, 1, "")
}

// promptForInput is PromptForInput with an optional display-row cap (maxRows 0
// means the default content-based height) and an optional top message bar
// (topMessage), which adds a row above the input for a fuller description.
func (pm *PromptManager) promptForInput(message, defaultValue string, callback PromptCallback, windowClass string, maxRows int, topMessage string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Prepare initial content based on history and default value
	var initialContent string

	if windowClass != "" {
		history := pm.getHistoryLocked(windowClass)
		if len(history) > 0 {
			// Start with history
			initialContent = strings.Join(history, "\n")

			// Add default value if it doesn't match the last history entry
			if defaultValue != "" && defaultValue != history[len(history)-1] {
				initialContent += "\n" + defaultValue
			} else {
				// Add blank line for new input
				initialContent += "\n"
			}
		} else {
			// No history, just use default value
			initialContent = defaultValue
		}
	} else {
		// No window class, use traditional behavior
		initialContent = defaultValue
	}

	// Calculate cursor line - always position at the LAST line (matches TypeScript)
	// This allows: Enter = accept current/default, Up = navigate history
	cursorLine := 0
	if initialContent != "" {
		cursorLine = strings.Count(initialContent, "\n")
	}

	// Add (PI) prefix to message for prompt indicator
	promptMessage := "(PI)" + message

	// Create the prompt buffer
	pm.createPromptWindow(promptMessage, initialContent, cursorLine, windowClass, callback, maxRows, topMessage)
}

// PromptForFilename prompts for a filename with history.
func (pm *PromptManager) PromptForFilename(action, defaultFilename string, callback PromptCallback) {
	pm.PromptForInput("(F) "+action+": ", defaultFilename, callback, "filename")
}

// PromptForConfirmation prompts for a yes/no confirmation.
func (pm *PromptManager) PromptForConfirmation(message string, defaultValue bool, callback func(accepted bool, response bool)) {
	defaultAnswer := "Y"
	if !defaultValue {
		defaultAnswer = "N"
	}

	var promptSuffix string
	var initialContent string
	if defaultValue {
		promptSuffix = " (Y/n): "
		initialContent = "N\nY\n"
	} else {
		promptSuffix = " (y/N): "
		initialContent = "Y\nN\n"
	}

	promptMessage := "(C) " + message + promptSuffix

	// Confirmation prompts display a single row; the Y/N alternatives above
	// the cursor line remain reachable by arrow, just not shown.
	pm.promptForInput(promptMessage, initialContent, func(accepted bool, bufferContent, cursorLineText string) {
		if !accepted {
			callback(false, false)
			return
		}

		// Parse response
		answer := strings.TrimSpace(cursorLineText)
		if answer == "" {
			answer = defaultAnswer
		}

		normalizedAnswer := strings.ToUpper(answer)
		if normalizedAnswer == "Y" || normalizedAnswer == "YES" {
			callback(true, true)
		} else if normalizedAnswer == "N" || normalizedAnswer == "NO" {
			callback(true, false)
		} else {
			// Invalid response - treat as default
			callback(true, defaultValue)
		}
	}, "", 1, "")
}

// PromptForConfirmationTop is a two-row confirmation in the lock-prompt
// style: a descriptive top message bar, with the short question alone on the
// input row. The prompt buffer is populated with the reachable answers — "y"
// and "n" above the blank input line — so the arrows pick one and a bare
// Enter takes the default.
func (pm *PromptManager) PromptForConfirmationTop(topMessage, question string, defaultValue bool, callback func(accepted, response bool)) {
	defaultAnswer := "N"
	if defaultValue {
		defaultAnswer = "Y"
	}
	pm.promptForInput("(C) "+question, "y\nn\n", func(accepted bool, _, cursorLineText string) {
		if !accepted {
			callback(false, false)
			return
		}
		answer := strings.TrimSpace(cursorLineText)
		if answer == "" {
			answer = defaultAnswer
		}
		switch strings.ToUpper(answer) {
		case "Y", "YES":
			callback(true, true)
		case "N", "NO":
			callback(true, false)
		default:
			callback(true, defaultValue)
		}
	}, "", 1, topMessage)
}

// createPromptWindow creates the actual prompt buffer window. maxRows caps
// the displayed content height (0 means the default cap). When topMessage is
// non-empty, an extra row is reserved above the input for a top message bar
// (the window's MessageTopInner) — e.g. a lock prompt's description of who
// already holds the file.
func (pm *PromptManager) createPromptWindow(prompt, initialContent string, cursorLine int, windowClass string, callback PromptCallback, maxRows int, topMessage string) {
	wm := pm.editor.WindowManager

	// Find highest priority bottom window. A bottom-located modebar is
	// excluded: its fixed priority pins it to the last screen line, and
	// prompts must stack above it, not outbid it.
	bottomWindows := wm.GetWindowsByDock(window.DockBottom)
	highestPriority := 0
	for _, w := range bottomWindows {
		if w.Class == "modebar" {
			continue
		}
		if w.Priority > highestPriority {
			highestPriority = w.Priority
		}
	}

	// Calculate prompt length (ANSI-aware)
	promptLength := calculateAnsiAwareLength(prompt)

	// Create buffer with initial content
	var buf *buffer.Buffer
	if initialContent != "" {
		buf = pm.editor.lib.NewFromString(initialContent)
	} else {
		buf = pm.editor.lib.New()
	}

	// Ensure buffer has at least one line
	if buf.GetLineCount() == 0 {
		buf.InsertLine(0, "")
	}

	// Calculate height based on content (for multi-line history)
	lineCount := buf.GetLineCount()
	height := lineCount
	if height < 1 {
		height = 1
	}
	maxHeight := 10 // Default cap for history display
	if maxRows > 0 {
		maxHeight = maxRows
	}
	if height > maxHeight {
		height = maxHeight
	}
	// A top message bar occupies its own row above the input, so grow the window
	// by one to keep the input row (and any history) their full height.
	if topMessage != "" {
		height++
	}

	id := wm.CreateWindow(window.WindowOptions{
		Type:            window.PromptWindow,
		Class:           windowClass,
		Dock:            window.DockBottom,
		Priority:        highestPriority + 10,
		MinHeight:       1,
		MaxHeight:       height,
		Height:          height,
		MarginInner:     promptLength,
		RowMessages:     []string{prompt},
		MessageTopInner: topMessage,
		Buffer:          buf,
		SetFocus:        true,
	})

	// Set up the window with proper cursor position and callback
	wm.UpdateWindow(id, func(w *window.Window) {
		// Position cursor at the specified line, at end of that line
		w.SetCursorLine(cursorLine)
		if cursorLine < buf.GetLineCount() {
			lineContent := strings.TrimRight(buf.GetLine(cursorLine), "\n\r")
			w.SetCursorRune(len([]rune(lineContent)))
		} else {
			w.SetCursorRune(0)
		}

		// Force viewOffsetX to 0 for prompts
		w.ViewState.ViewOffsetX = 0

		// Ensure viewOffsetY shows the cursor line
		if cursorLine >= height {
			w.SetViewTop(cursorLine - height + 1)
		} else {
			w.SetViewTop(0)
		}

		// Set content height explicitly
		w.ContentHeight = lineCount

		// Store the window class for history updates
		w.Class = windowClass

		// Filename prompts complete paths on the completion command (tab), and
		// resolve their accepted result against the same directory completion
		// searches — so a plain name opens/saves next to the anchoring file,
		// not in the process's working directory. The anchor is captured now
		// (it does not depend on the typed text). History keeps the raw text.
		if windowClass == "filename" {
			win := w
			w.CompletionCallback = func() bool { return pm.editor.completeFilename(win) }
			baseDir := pm.editor.completionBaseDir(win)
			inner := callback
			callback = func(accepted bool, bufferContent, cursorLineText string) {
				if accepted {
					cursorLineText = pm.editor.resolvePromptPath(cursorLineText, baseDir)
					bufferContent = pm.editor.resolvePromptPath(bufferContent, baseDir)
				}
				if inner != nil {
					inner(accepted, bufferContent, cursorLineText)
				}
			}
		}

		// Wrap callback to handle history update
		w.PromptCallback = func(accepted bool, bufferContent, cursorLineText string) {
			// Update history if accepted and we have a window class
			if accepted && windowClass != "" && cursorLineText != "" {
				pm.updateHistory(windowClass, cursorLineText)
			}

			// Call the original callback
			if callback != nil {
				callback(accepted, bufferContent, cursorLineText)
			}
		}
	})

	pm.editor.RequestRender()
}

// getHistoryLocked returns history for a window class. Must be called with lock held.
func (pm *PromptManager) getHistoryLocked(windowClass string) []string {
	if history, exists := pm.promptHistory[windowClass]; exists {
		return history
	}
	return nil
}

// updateHistory adds a value to the history for a window class.
func (pm *PromptManager) updateHistory(windowClass, value string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	history := pm.promptHistory[windowClass]

	// Don't add if it matches the last entry
	if len(history) > 0 && history[len(history)-1] == value {
		return
	}

	// Add the new value
	history = append(history, value)

	// Keep only the last maxHistory items
	if len(history) > pm.maxHistory {
		history = history[1:]
	}

	pm.promptHistory[windowClass] = history
}

// GetHistory returns the history for a window class.
func (pm *PromptManager) GetHistory(windowClass string) []string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if history, exists := pm.promptHistory[windowClass]; exists {
		result := make([]string, len(history))
		copy(result, history)
		return result
	}
	return nil
}

// calculateAnsiAwareLength calculates the visible column width of a string
// with ANSI codes (combining/zero-width runes count 0, wide runes 2).
func calculateAnsiAwareLength(s string) int {
	length := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length += textwidth.Rune(r)
	}
	return length
}
