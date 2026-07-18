# TypeScript to Go Port Audit Plan

This document tracks the systematic audit of the TypeScript Mew editor implementation
compared to the Go rewrite.

## Audit Order (by priority/dependency)

### Phase 1: Core Infrastructure (HIGH PRIORITY)
These are foundational - errors here cascade everywhere.

1. [ ] **core/preference-manager.js** → Go equivalent?
   - Color system for all window types
   - Buffer type preferences (mainBuffer, workBuffer, promptBuffer)
   - Window class preferences (statusLine, columnRuler, etc.)

2. [ ] **core/window-manager.js** → internal/window/manager.go
   - Window creation with proper defaults
   - Focus management
   - Dock positions (top, bottom, none)
   - Priority handling
   - Event system
   - Peek offsets (statPeek, promptPeek)

3. [ ] **core/layout-manager.js** → internal/window/layout.go
   - Top dock: sort descending (highest priority at screen top)
   - Bottom dock: sort ascending (highest priority at screen bottom)
   - Main area calculation
   - Peek indicator logic

4. [ ] **core/buffer-manager.js** → internal/buffer/buffer.go
   - Line operations (get, set, insert, delete)
   - Text operations (insert, delete at position)
   - Content loading
   - Cursor management

### Phase 2: Rendering System
5. [ ] **io/screen-renderer.js** → internal/render/screen.go
   - Window rendering order
   - Custom renderer dispatch (statusLine, columnRuler)
   - Line number rendering
   - Cursor positioning
   - Ghost cursor rendering

6. [ ] **io/window-renderer.js** → (part of screen.go?)
   - Individual window content rendering
   - Message rendering (top_left, top_center, etc.)
   - Color application

7. [ ] **plugins/plugins.js** → internal/plugins/*.go
   - StatusLine plugin
   - ColumnRuler plugin
   - Custom rendering integration

### Phase 3: Input & Command System
8. [ ] **io/key-mappings.js** → internal/keys/sequence.go
   - Key sequence definitions
   - Command mappings

9. [ ] **core/key-sequence-processor.js** → internal/input/keyboard.go
   - Multi-key sequence handling
   - Escape sequences
   - Control key handling

10. [ ] **core/command-processor.js** → internal/editor/editor.go
    - Command execution
    - Error handling and display

### Phase 4: Status & Messages
11. [ ] **io/status-manager.js** → MISSING IN GO
    - Error message windows (bottom dock, priority 0)
    - Success/cancelled messages
    - Notification window class

12. [ ] **io/prompt-manager.js** → internal/window/manager.go (CreatePromptBuffer)
    - Prompt creation
    - Input handling
    - Callback system

### Phase 5: Commands
13. [ ] **commands/movement.js** → internal/editor/editor.go
    - Cursor movement (up, down, left, right)
    - Word movement
    - Line start/end
    - Page up/down
    - Document start/end

14. [ ] **commands/edit.js** → internal/editor/editor.go
    - Character insertion
    - Backspace/delete
    - Line operations
    - Word deletion

15. [ ] **commands/block.js** → NOT IMPLEMENTED
    - Mark begin/end
    - Copy/move/delete block
    - Indent/unindent block

16. [ ] **commands/file.js** → internal/editor/editor.go
    - Save/load
    - New file
    - Quit

17. [ ] **commands/search.js** → NOT IMPLEMENTED?
    - Find
    - Find next
    - Go to line

18. [ ] **commands/ui.js** → internal/editor/editor.go
    - Toggle help
    - Options menu
    - Window navigation

19. [ ] **commands/system.js** → internal/editor/editor.go
    - Undo/redo
    - Abort

### Phase 6: Utilities
20. [ ] **utils/coordinate-utils.js** → ?
    - Document to screen coordinate conversion
    - Screen to document conversion
    - Visual column calculations

21. [ ] **utils/cursor-utils.js** → ?
    - Cursor visibility
    - Viewport optimization

22. [ ] **utils/scroll-management.js** → ?
    - Scroll behavior

### Phase 7: Configuration
23. [ ] **config/config-manager.js** → internal/config/config.go
    - Config file loading
    - Default values

---

## Known Issues Found

### Critical
- [ ] StatusManager/notification windows not implemented (bottom dock error messages)
- [ ] Block operations not implemented
- [ ] Command prompt (Esc X) not implemented

### Color System
- [x] Main editor: white on BLACK (40) - FIXED
- [x] Status line: white on BLUE (44) - FIXED
- [x] Column ruler: white on MAGENTA (45) - FIXED
- [x] Prompts: white on GREEN (42) - FIXED
- [ ] Notifications: need to verify color scheme

### Layout
- [x] Top dock sort order - FIXED (descending by priority)
- [ ] Bottom dock sort order - need to verify

### Buffer Operations
- [x] Empty buffer text insertion - FIXED
- [ ] Line operations need verification

---

## Audit Progress Log

### Session: 2025-12-18
- Created audit plan
- Found and fixed corner cut bug: Bottom row was causing terminal scroll
  - TypeScript window-renderer.js line 261: `cornerCut = (screenLineY == height) ? 1 : 0`
  - Fixed in Go render/screen.go: reduce contentWidth by 1 when screenY == sr.Height

- Audited preference-manager.js vs Go color system:
  - Go has: DefaultColors, StatusLineColors, ColumnRulerColors, PromptColors, WorkBufferColors
  - Missing window classes: notification, message, multiline, filename
  - Need to add MessageColors for yellow background (43) notification windows

- Found StatusManager creates bottom-dock windows with:
  - windowClass: 'notification'
  - dock: 'bottom'
  - priority: 0 (very low)
  - minHeight: 1, maxHeight: 1
  - message_top_left: error message text

- Added NotificationColors function for yellow background notification windows
- Implemented ShowNotification, ShowError, ClearNotifications methods in editor
- Updated PawScript stderr capture to use ShowNotification instead of UpdateStatus
- Notifications now appear as bottom-dock windows with priority 0 (lowest)

- Audited window-manager.js:
  - TypeScript has event system (on/off/emit) - Go doesn't need this
  - TypeScript has preference manager integration - Go has color functions
  - Both have lastMainBufferWindow tracking
  - Both have peek offset support (statPeek/promptPeek)
  - Both have ghost cursor handling

- Audited layout-manager.js:
  - TypeScript createTopLayout sorts descending (highest priority at top) - Go matches
  - TypeScript createBottomLayout sorts ascending (lowest priority at top of bottom section) - Go matches
  - Peek offset handling similar in both

- Audited commands:

  **IMPLEMENTED IN GO:**
  - Movement: cursor_up/down/left/right, home, end, top, bottom, page_up/down ✓
  - Word movement: go_word_next, go_word_prior ✓
  - Editing: del_char_prior, delete_char, delete_line, del_word_beg, del_word_end ✓
  - File: file_save, file_open, file_new, buffer_close ✓
  - Undo/redo: undo, redo ✓
  - Peek: stat_peek_up/down, prompt_peek_up/down ✓
  - Help: help_toggle ✓
  - Find: find, find_next ✓ (basic implementation)

  **STUBBED/NOT IMPLEMENTED:**
  - block_begin - ✓ IMPLEMENTED (uses Garland decorations)
  - block_end - ✓ IMPLEMENTED (uses Garland decorations)
  - block_copy - ✓ IMPLEMENTED
  - block_delete - ✓ IMPLEMENTED
  - block_move - ✓ IMPLEMENTED
  - block_write - STUB (file writing pending)
  - block_indent - ✓ IMPLEMENTED (^K .)
  - block_unindent - ✓ IMPLEMENTED (^K ,)
  - find_replace - NOT IMPLEMENTED
  - go_to_line (^K L) - NOT FOUND
  - command_prompt (Esc X) - ✓ IMPLEMENTED

- Implemented block operations using Garland decorations for persistent markers

### Session: 2025-12-19
- Fixed linefeed rendering issue: Garland ReadLine includes line terminator
  - Added strings.TrimRight to strip \n\r before rendering
  - Translated prepareLineForDisplay from TypeScript for control char handling
  - Control chars shown as ^X format (^J for LF, ^M for CR, etc.)

- Added block_indent/block_unindent commands with key mappings (^K . and ^K ,)

- Implemented command prompt (Esc X) for direct PawScript command entry

- Fixed map iteration nondeterminism in window manager
  - Added secondary sort by ID for stable ordering in GetWindowsByDock and AllWindows

- Fixed cursor navigation for Garland line lengths
  - Added getEffectiveLineLen helper to strip trailing newlines
  - Updated all cursor positioning code to use effective length

- Added navigation commands:
  - go_line (^K L): Jump to line number with prompt
  - scroll_left (Esc <): Scroll view left by 8 columns
  - scroll_right (Esc >): Scroll view right by 8 columns
  - window_next (Esc ]): Cycle focus to next window
  - window_prior (Esc [): Cycle focus to previous window

- Added mark commands:
  - set_mark [name]: Set named mark at cursor position
  - go_mark [name]: Jump to named mark position

- Added go_match command for matching bracket navigation
  - Handles ()[]{}
  - Searches across line boundaries
  - Tracks bracket nesting depth

- Fixed cursor placement to use visual columns correctly:
  - Added runeToVisualColumn helper function
  - Added visualColumnToRune helper function
  - Fixed positionCursor in screen.go to convert rune position to visual column
  - Fixed afterVerticalMovement to properly compare/set visual columns
  - Fixed afterHorizontalMovement to convert rune to visual column when updating ideal
  - Added TabSize to ViewState struct for proper tab width calculation

- Fixed newline insertion at end of line:
  - Strip trailing newline from Garland line before splitting in insertText

- Fixed statusWriter to accumulate all complete lines (joining with spaces)

- Added key sequence completion display:
  - When there's an active sequence (e.g., ^K), show possible completions in status line
  - Uses GetPossibleCompletions() from KeySequenceProcessor

- Fixed SetLine in buffer.go:
  - Was deleting trailing newline when replacing line content
  - Caused lines to merge when pressing Enter (next line absorbed into current)
  - Now strips newline from oldContent before calculating delete length

- Fixed deleteToWordStart and deleteToWordEnd:
  - Added bounds checking to prevent panic when cursor beyond line length
  - Strip trailing newline from line content before processing

- Fixed tab width handling:
  - getTabWidth now uses getTabSize(w) to respect window's TabSize setting
  - Previously hardcoded to 8

- Added ghost cursor rendering:
  - Shows '|' character at ideal visual column when cursor is on shorter line
  - Uses GhostCursor color from window colors

**STILL MISSING:**
- buffer_list (list all open buffers)
- file_save_as (save with filename argument)
- editor_options menu
- find_replace (two-prompt find/replace)

### Session: 2025-12-19 (continued)
- Fixed terminal resize not triggering repaint:
  - Added SetOnResize callback to ScreenRenderer
  - Callback calls performRender directly when resize signal received
  - Editor registers callback before starting renderer

- Fixed partial line handling in Garland buffers:
  - Updated GetLineCount to properly count partial lines (text without trailing newline)
  - Checks if buffer ends with newline; if not, adds 1 to line count
  - Updated GetLine to handle SeekLine failures for line 0 by falling back to SeekByte(0)
  - Added fallback to ReadBytes if ReadLine fails

- Fixed ghost cursor not resolving after typing/editing:
  - Added afterHorizontalMovement call to insertText
  - Added afterHorizontalMovement call to deleteCharBefore
  - Added afterHorizontalMovement call to deleteCharAt
  - Added afterHorizontalMovement call to deleteLine
  - Added afterHorizontalMovement call to deleteToWordStart
  - Added afterHorizontalMovement call to deleteToWordEnd
  - Added afterHorizontalMovement call to deleteToLineStart
  - Added afterHorizontalMovement call to deleteToLineEnd

