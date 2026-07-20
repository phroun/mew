// Package buffer provides a text buffer interface backed by Garland.
package buffer

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/phroun/garland"
	"github.com/phroun/mew/internal/textwidth"
)

// undoCoalesceIdle is how long a typing/deleting pause before the next edit
// forces a fresh undo step. Adjacent same-kind edits within this window
// collapse into one revision (garland auto-bakes the run past it).
const undoCoalesceIdle = 5 * time.Second

// enableEditingUndo turns on garland's undo coalescing for an editable buffer:
// runs of adjacent inserts (typing) or adjacent deletes (backspacing /
// forward-deleting at one caret) collapse into a single revision, with a
// time-based break after undoCoalesceIdle. The editor additionally bakes the
// run whenever a non-editing command runs (see executeCommand), so moving the
// caret always starts a fresh undo step.
func enableEditingUndo(g *garland.Garland) {
	if g != nil {
		g.SetUndoCoalescing(true, undoCoalesceIdle)
	}
}

// Library owns one garland.Library (its own cold-storage tier). Each editor
// instance holds its own Library so many mews can run in one process without
// sharing garland state (see NewLibrary). The package-level constructors below
// (New, NewFromString, OpenFile, ...) operate on a lazily-created process
// default for callers and tests that don't manage their own.
type Library struct {
	g *garland.Library
}

// NewLibrary creates an independent library backed by its own cold-storage
// directory. An empty path uses the OS temp dir (falling back to
// ~/.mew/cold-storage). Give each instance a DISTINCT directory so their cold
// storage never collides.
func NewLibrary(coldStoragePath string) (*Library, error) {
	if coldStoragePath == "" {
		coldStoragePath = os.TempDir()
		if coldStoragePath == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				homeDir = "."
			}
			coldStoragePath = homeDir + "/.mew/cold-storage"
		}
	}
	os.MkdirAll(coldStoragePath, 0755)
	g, err := garland.Init(garland.LibraryOptions{ColdStoragePath: coldStoragePath})
	if err != nil {
		return nil, err
	}
	return &Library{g: g}, nil
}

// Close releases the library's garland resources. Safe on a nil or
// already-closed library.
func (lib *Library) Close() error {
	if lib != nil && lib.g != nil {
		err := lib.g.Close()
		lib.g = nil
		return err
	}
	return nil
}

// defaultLib backs the package-level constructors for callers that don't hold
// their own Library (tests, simple embedders). Editors use their own.
var defaultLib *Library

// defaultLibrary lazily initializes the process default Library. If init fails
// it returns a Library with a nil garland, and the constructors degrade the way
// they always did (empty buffer / "not initialized" error).
func defaultLibrary() *Library {
	if defaultLib == nil {
		_ = InitLibrary()
	}
	if defaultLib == nil {
		return &Library{}
	}
	return defaultLib
}

// InitLibrary initializes the default Garland library with the default cold
// storage path.
func InitLibrary() error { return InitLibraryWithPath("") }

// InitLibraryWithPath initializes the default Garland library with a specific
// cold storage path (empty = OS temp, then ~/.mew/cold-storage).
func InitLibraryWithPath(path string) error {
	lib, err := NewLibrary(path)
	if err != nil {
		return err
	}
	defaultLib = lib
	return nil
}

// CloseLibrary closes the default Garland library.
func CloseLibrary() {
	if defaultLib != nil {
		_ = defaultLib.Close()
		defaultLib = nil
	}
}

// Buffer represents a text buffer backed by Garland.
type Buffer struct {
	garland    *garland.Garland
	readCursor *garland.Cursor // For reading operations - doesn't disturb carets
	// Per-window carets: each editor window owns its own Caret (see NewCaret),
	// so there is no single buffer-wide edit cursor and two windows can edit one
	// buffer independently.
	modified bool
	filename string

	// Command-scoped transaction. Groups all mutations from a single user
	// command into one named garland revision. Opened lazily on the first
	// mutation (see beginMutation), so a command that edits nothing — a cursor
	// move, a failed search, a PawScript command doing non-document work —
	// creates no revision and never enters the undo history. The name records
	// the command's "kind" (garland stores it in the revision) for future
	// preference-driven undo/redo granularity.
	cmdDepth   int
	cmdName    string
	cmdTxnOpen bool
	cmdCancel  bool // the current user command asked to roll back, not commit

	// changeSeq advances on every content mutation, and dirtyLow tracks the
	// lowest line such a mutation touched since the watermark was last taken
	// (see ChangeSeq, TakeDirtyLow). Decoration-only mutations (marks) move
	// neither: they cannot change what any content-derived cache holds.
	changeSeq int64
	dirtyLow  int

	// hasSource records that garland has a tracked source file for this
	// buffer (opened from a file, or adopted by a save-as), so in-place
	// saves, consistency checks, backups and reverts apply. srcFS is the
	// bridged host file system the source lives on (nil = the real OS).
	hasSource bool
	srcFS     garland.FileSystemInterface

	// savedFork/savedRev anchor the buffer's last agreement with its source:
	// captured when the file is opened (the loaded content equals the file on
	// disk) and refreshed after each in-place or adopting save. buffer_revert
	// seeks here, so reverting an opened-but-never-saved buffer returns to the
	// opened state rather than acting as if there were no prior state.
	// garland's own save history only records explicit saves, so this is
	// tracked mew-side. hasSaved is false for scratch buffers with no source.
	savedFork garland.ForkID
	savedRev  garland.RevisionID
	hasSaved  bool
}

// captureSavePoint records the current garland fork/revision as the buffer's
// revert baseline (called on open and after a successful save).
func (b *Buffer) captureSavePoint() {
	if b.garland == nil {
		return
	}
	b.savedFork = b.garland.CurrentFork()
	b.savedRev = b.garland.CurrentRevision()
	b.hasSaved = true
}

// New creates a new empty buffer using the default library.
func New() *Buffer { return defaultLibrary().New() }

// New creates a new empty buffer in this library.
func (lib *Library) New() *Buffer {
	if lib == nil || lib.g == nil {
		return &Buffer{filename: ""}
	}
	g, err := lib.g.Open(garland.FileOptions{
		DataBytes: []byte{}, // Empty byte slice is not nil, so counts as data source
	})
	if err != nil {
		return &Buffer{filename: ""}
	}
	enableEditingUndo(g)

	readCursor := g.NewCursor()
	readCursor.SeekByte(0)
	return &Buffer{garland: g, readCursor: readCursor}
}

// NewFromString creates a buffer from string content using the default library.
func NewFromString(content string) *Buffer { return defaultLibrary().NewFromString(content) }

// NewFromString creates a buffer from string content in this library.
func (lib *Library) NewFromString(content string) *Buffer {
	if lib == nil || lib.g == nil {
		return &Buffer{filename: ""}
	}
	// Use DataBytes for empty content since empty DataString doesn't count as a data source.
	var g *garland.Garland
	var err error
	if content == "" {
		g, err = lib.g.Open(garland.FileOptions{DataBytes: []byte{}})
	} else {
		g, err = lib.g.Open(garland.FileOptions{DataString: content})
	}
	if err != nil {
		return &Buffer{filename: ""}
	}
	enableEditingUndo(g)

	readCursor := g.NewCursor()
	readCursor.SeekByte(0)
	return &Buffer{garland: g, readCursor: readCursor}
}

// NewFromFile opens a file through Garland's own lazy-loading path: the file
// becomes warm storage, only a window of it is brought into memory, and the
// rest is paged on demand — so multi-gigabyte files are never slurped whole.
// Use this whenever the file lives on the real file system.
func NewFromFile(filename string) (*Buffer, error) {
	return OpenFile(filename, OpenOptions{})
}

// OpenOptions controls how a file-backed buffer is opened.
type OpenOptions struct {
	// UseEmacsLocks maintains an emacs-compatible ".#<name>" lock file next
	// to the source while the buffer holds unsaved modifications, and makes
	// garland report foreign locks it finds.
	UseEmacsLocks bool

	// LockOwner is the "user@host.pid" identity stamped into the emacs lock
	// file (and used to recognize our own lock). Empty lets garland derive it
	// from the environment. Only meaningful with UseEmacsLocks.
	LockOwner string
}

// OpenFile is NewFromFile with options, using the default library.
func OpenFile(filename string, opts OpenOptions) (*Buffer, error) {
	return defaultLibrary().OpenFile(filename, opts)
}

// OpenFile opens a file-backed buffer in this library.
func (lib *Library) OpenFile(filename string, opts OpenOptions) (*Buffer, error) {
	if lib == nil || lib.g == nil {
		return nil, fmt.Errorf("buffer library not initialized")
	}
	g, err := lib.g.Open(garland.FileOptions{
		FilePath:      filename,
		UseEmacsLocks: opts.UseEmacsLocks,
		LockOwner:     opts.LockOwner,
	})
	if err != nil {
		return nil, err
	}
	enableEditingUndo(g)

	readCursor := g.NewCursor()
	readCursor.SeekByte(0)

	b := &Buffer{
		garland:    g,
		readCursor: readCursor,
		filename:   filename,
		hasSource:  true,
	}
	b.captureSavePoint() // the opened content equals the file: revert baseline
	return b, nil
}

// NewFromHostFile opens a host-virtualized file through garland, with the
// host's read/write callbacks bridged in as the source file system. The
// buffer gets garland's full save engine (history preservation, scars, save
// points, revert) instead of the raw content writes NewFromBytes implies;
// metadata-based change detection is off (the host volunteers none).
func NewFromHostFile(host HostFS, filename string) (*Buffer, error) {
	return defaultLibrary().NewFromHostFile(host, filename)
}

// NewFromHostFile opens a host-virtualized file in this library.
func (lib *Library) NewFromHostFile(host HostFS, filename string) (*Buffer, error) {
	if lib == nil || lib.g == nil {
		return nil, fmt.Errorf("buffer library not initialized")
	}

	fs := BridgeFS(host)
	g, err := lib.g.Open(garland.FileOptions{
		FilePath:   filename,
		FileSystem: fs,
	})
	if err != nil {
		return nil, err
	}
	enableEditingUndo(g)

	readCursor := g.NewCursor()
	readCursor.SeekByte(0)

	b := &Buffer{
		garland:    g,
		readCursor: readCursor,
		filename:   filename,
		hasSource:  true,
		srcFS:      fs,
	}
	b.captureSavePoint()
	return b, nil
}

// NewFromBytes creates a buffer from byte content, tagged with the given
// filename. The content lives in memory (with Garland's cold storage as the
// only spill tier); use NewFromFile for real files so Garland can page them
// lazily. This is the path for host-virtualized file systems.
func NewFromBytes(data []byte, filename string) (*Buffer, error) {
	return defaultLibrary().NewFromBytes(data, filename)
}

// NewFromBytes creates an in-memory buffer in this library.
func (lib *Library) NewFromBytes(data []byte, filename string) (*Buffer, error) {
	if lib == nil || lib.g == nil {
		return nil, fmt.Errorf("buffer library not initialized")
	}

	g, err := lib.g.Open(garland.FileOptions{
		DataBytes: data,
	})
	if err != nil {
		return nil, err
	}
	enableEditingUndo(g)

	// Create read cursor for reading operations
	readCursor := g.NewCursor()
	readCursor.SeekByte(0)

	return &Buffer{
		garland:    g,
		readCursor: readCursor,
		filename:   filename,
	}, nil
}

// LineRuneLen returns the number of runes on a line, excluding its terminating
// newline/CR. Callers clamp a caret's column to this so it never seeks past a
// line's end: garland's SeekLine does not clamp (or reject) an out-of-range
// rune-within-line, leaving the cursor's byte position and reported line/rune
// inconsistent.
func (b *Buffer) LineRuneLen(line int) int {
	return len([]rune(strings.TrimRight(b.GetLine(line), "\n\r")))
}

// GetLineCount returns the number of lines.
// Note: A buffer always has at least 1 line, even if empty.
// For text without a trailing newline, we count the partial line as a line.
func (b *Buffer) GetLineCount() int {
	if b.garland == nil {
		return 1
	}
	result := b.garland.LineCount()
	count := int(result.Value)

	// Garland counts lines by newlines, so "hello" (no newline) = 0 lines,
	// "hello\n" = 1 line, "hello\nworld" = 1 line (+ partial)
	// We need to add 1 if there's content after the last newline (partial line)
	// or if the buffer is completely empty (still counts as 1 line)
	byteCount := b.garland.ByteCount()
	if byteCount.Value == 0 {
		// Empty buffer still has 1 line
		return 1
	}

	// Garland's LineCount() returns the number of complete lines (newlines counted).
	// A line exists for each newline, PLUS one more for content after the last newline
	// (or the empty line that the trailing newline creates).
	//
	// Examples:
	// - "hello" (no newline): LineCount()=0, has content -> 1 line
	// - "hello\n": LineCount()=1, trailing newline means empty line exists -> 2 lines
	// - "hello\nworld": LineCount()=1, has trailing content -> 2 lines
	// - "hello\nworld\n": LineCount()=2, trailing newline -> 3 lines
	//
	// So we always add 1 to account for either:
	// - The partial line after the last newline, OR
	// - The empty line created by a trailing newline
	return count + 1
}

// GetLine returns the content of a specific line (0-indexed).
// Uses readCursor to avoid disturbing window carets.
func (b *Buffer) GetLine(line int) string {
	if b.garland == nil || b.readCursor == nil {
		return ""
	}

	// For line 0, always use byte-based seeking to avoid SeekLine issues with partial lines
	if line == 0 {
		b.readCursor.SeekByte(0)
		// Note: ReadLine may return content AND an error (e.g., io.EOF on final partial line)
		// We should use the content if it was returned, regardless of error
		content, err := b.readCursor.ReadLine()
		if content != "" {
			return content
		}
		// Only fall back to reading all bytes if ReadLine returned no content
		if err != nil {
			b.readCursor.SeekByte(0)
			byteCount := b.garland.ByteCount()
			if byteCount.Value > 0 {
				bytes, _ := b.readCursor.ReadBytes(byteCount.Value)
				return string(bytes)
			}
		}
		return ""
	}

	// For other lines, use SeekLine
	err := b.readCursor.SeekLine(int64(line), 0)
	if err != nil {
		return ""
	}

	// Read the line content
	// Note: ReadLine may return content AND an error (e.g., io.EOF on final partial line)
	// We should use the content if it was returned, regardless of error
	content, _ := b.readCursor.ReadLine()
	return content
}

// ReplaceText deletes deleteRunes runes at (line, runePos) and inserts text
// there, as one transaction. The cursor is positioned freshly at the target
// (never a stale byte offset), then the delete and insert happen at that same
// point, so garland slides decorations by the net rune delta rather than
// collapsing an entire line's decorations the way a whole-line rewrite would.
// text may contain newlines (garland splits into lines). Invalidates the
// went through readCursor; window carets are slid by garland.
func (b *Buffer) ReplaceText(line, runePos, deleteRunes int, text string) {
	if b.garland == nil || b.readCursor == nil {
		return
	}
	b.beginMutation()
	b.touchContent(line)

	b.garland.TransactionStart("ReplaceText")
	defer b.garland.TransactionCommit()

	if err := b.readCursor.SeekLine(int64(line), int64(runePos)); err != nil {
		return
	}
	// Delete leaves the cursor at the deletion point, so the insert lands at
	// the same position without a second seek.
	if deleteRunes > 0 {
		b.readCursor.DeleteRunes(int64(deleteRunes), false)
	}
	if text != "" {
		b.readCursor.InsertString(text, nil, false)
	}

	b.modified = true
}

// InsertLine inserts a new line at the given position.
// Uses readCursor for complex operations.
func (b *Buffer) InsertLine(line int, content string) {
	if b.garland == nil || b.readCursor == nil {
		return
	}
	b.beginMutation()
	b.touchContent(line)

	b.garland.TransactionStart("InsertLine")
	defer b.garland.TransactionCommit()

	lineCount := b.GetLineCount()

	if line < 0 {
		line = 0
	}
	if line > lineCount {
		line = lineCount
	}

	// If inserting at end, go to end of buffer and add newline + content
	if line >= lineCount {
		byteCount := b.garland.ByteCount()
		b.readCursor.SeekByte(byteCount.Value)
		if byteCount.Value > 0 || line > 0 {
			// There's existing content OR we're inserting past line 0, add newline first
			b.readCursor.InsertString("\n"+content, nil, false)
		} else {
			// Empty buffer and inserting at line 0, just add content
			b.readCursor.InsertString(content, nil, false)
		}
	} else {
		// Use SeekLine directly - more efficient than computing rune position
		err := b.readCursor.SeekLine(int64(line), 0)
		if err != nil {
			return
		}
		b.readCursor.InsertString(content+"\n", nil, false)
	}

	b.modified = true
}

// DeleteLine removes a line.
// Uses readCursor for complex operations.
func (b *Buffer) DeleteLine(line int) {
	if b.garland == nil || b.readCursor == nil {
		return
	}

	lineCount := b.GetLineCount()
	if line < 0 || line >= lineCount {
		return
	}
	b.beginMutation()

	b.garland.TransactionStart("DeleteLine")
	defer b.garland.TransactionCommit()

	// Get line content (includes trailing newline if present)
	content := b.GetLine(line)
	contentRunes := int64(len([]rune(content)))

	if line < lineCount-1 {
		// Not the last line - delete line content including its trailing newline
		b.readCursor.SeekLine(int64(line), 0)
		b.readCursor.DeleteRunes(contentRunes, true)
	} else if line > 0 {
		// Last line - delete the previous line's terminator plus this line's
		// content so line-1 becomes the final, unterminated line. Compute the
		// terminator length rather than assuming a single '\n', so a CRLF
		// terminator ("\r\n") is removed whole instead of orphaning the '\r'.
		prevContent := b.GetLine(line - 1)
		prevNoTerm := strings.TrimRight(prevContent, "\n\r")
		termRunes := int64(len([]rune(prevContent)) - len([]rune(prevNoTerm)))
		b.readCursor.SeekLine(int64(line-1), int64(len([]rune(prevNoTerm))))
		b.readCursor.DeleteRunes(termRunes+contentRunes, true)
	} else {
		// line == 0 and it's the only line - just delete content
		b.readCursor.SeekByte(0)
		b.readCursor.DeleteRunes(contentRunes, true)
	}

	b.modified = true
}

// InsertText inserts text at a position.
// This is the legacy line-based API. For better performance, use
// SyncUserCursor + InsertAtCursor for repeated insertions at the same position.
func (b *Buffer) InsertText(line, runePos int, text string) {
	if b.garland == nil || b.readCursor == nil {
		return
	}
	b.beginMutation()
	b.touchContent(line)

	// Check if buffer is empty
	byteCount := b.garland.ByteCount()

	if byteCount.Value == 0 {
		// Empty buffer - just insert at beginning
		b.readCursor.SeekByte(0)
	} else {
		// Use SeekLine directly - much more efficient than computing rune position
		err := b.readCursor.SeekLine(int64(line), int64(runePos))
		if err != nil {
			// If exact position fails, seek to end of buffer
			b.readCursor.SeekByte(byteCount.Value)
		}
	}

	// Insert text
	b.readCursor.InsertString(text, nil, false)
	b.modified = true
}

// DeleteText deletes count runes from a position.
// This is the legacy line-based API. For better performance, use
// SyncUserCursor + DeleteAtCursor for repeated deletions at the same position.
func (b *Buffer) DeleteText(line, runePos, count int) {
	if b.garland == nil || b.readCursor == nil || count <= 0 {
		return
	}
	b.beginMutation()
	b.touchContent(line)

	// Use SeekLine directly
	err := b.readCursor.SeekLine(int64(line), int64(runePos))
	if err != nil {
		return
	}

	// Delete runes
	b.readCursor.DeleteRunes(int64(count), false)

	b.modified = true
}

// walkBlockLineStarts positions the readCursor at the start of each line
// spanned by the begin/end decoration pair and calls visit there. The walk is
// anchored entirely to garland-maintained positions: the starting line comes
// from the (auto-sliding) decorations, the cursor advances relative to its own
// LIVE line, and the terminating bound is re-read from the decorations every
// step. No line number is ever captured before a mutation and reused after, so
// visit may change line lengths (indent/unindent) — and, in a future
// concurrent-edit world, another writer may shift the buffer — without the
// walk losing its place. visit must leave the readCursor on the same line it
// was called at. Returns the number of lines visited.
func (b *Buffer) walkBlockLineStarts(beginMark, endMark string, visit func()) int {
	if b.garland == nil || b.readCursor == nil {
		return 0
	}
	// bounds re-reads both decorations and returns the ordered [start,end] byte
	// span they currently delimit (either mark may be the earlier one).
	bounds := func() (start, end int64, ok bool) {
		a, e1 := b.garland.GetDecorationPosition(beginMark)
		z, e2 := b.garland.GetDecorationPosition(endMark)
		if e1 != nil || e2 != nil {
			return 0, 0, false
		}
		if a.Byte <= z.Byte {
			return a.Byte, z.Byte, true
		}
		return z.Byte, a.Byte, true
	}

	start, _, ok := bounds()
	if !ok || b.readCursor.SeekByte(start) != nil {
		return 0
	}
	b.readCursor.SeekLineStart()

	visited := 0
	for {
		_, end, ok := bounds()
		if !ok || b.readCursor.BytePos() > end {
			break
		}
		line, _ := b.readCursor.LinePos()
		visit()
		visited++
		// Advance to the next line's start, addressed from the cursor's LIVE
		// line (never a value captured before visit ran). SeekLine clamps at the
		// last line, so a non-advance means we are done.
		cur, _ := b.readCursor.LinePos()
		if b.readCursor.SeekLine(cur+1, 0) != nil {
			break
		}
		if next, _ := b.readCursor.LinePos(); next <= line {
			break
		}
	}
	return visited
}

// IndentBlock inserts indent at the start of every line spanned by the
// begin/end decoration pair, as one transaction. The edit is a localized
// insert at each line start (garland slides the surrounding decorations and
// every cursor, including each window's caret) and the walk is
// decoration-anchored (see walkBlockLineStarts) rather than addressed by
// captured line numbers. Returns the number of lines indented.
func (b *Buffer) IndentBlock(beginMark, endMark, indent string) int {
	if b.garland == nil || b.readCursor == nil || indent == "" {
		return 0
	}
	b.beginMutation()
	if l, _, ok := b.GetMark(beginMark); ok {
		b.touchContent(l)
	} else {
		b.touchContent(-1)
	}
	b.garland.TransactionStart("indent")
	defer b.garland.TransactionCommit()

	edited := 0
	b.walkBlockLineStarts(beginMark, endMark, func() {
		// Skip blank lines: an empty or whitespace-only line gains nothing from
		// indentation and would just accumulate trailing whitespace. ReadLine
		// peeks the current line without moving the cursor off its start.
		line, _ := b.readCursor.ReadLine()
		if strings.TrimSpace(line) == "" {
			return
		}
		b.readCursor.InsertString(indent, nil, false)
		edited++
	})
	if edited > 0 {
		b.modified = true
	}
	return edited
}

// UnindentBlock removes up to tabSize leading spaces (or one leading tab) from
// every line spanned by the begin/end decoration pair, as one transaction.
// Decoration-anchored and localized like IndentBlock; garland slides every
// cursor (including window carets) with the deletions. Returns lines visited.
func (b *Buffer) UnindentBlock(beginMark, endMark string, tabSize int) int {
	if b.garland == nil || b.readCursor == nil || tabSize <= 0 {
		return 0
	}
	b.beginMutation()
	if l, _, ok := b.GetMark(beginMark); ok {
		b.touchContent(l)
	} else {
		b.touchContent(-1)
	}
	b.garland.TransactionStart("unindent")
	defer b.garland.TransactionCommit()

	n := b.walkBlockLineStarts(beginMark, endMark, func() {
		lineStart := b.readCursor.BytePos()
		peek, _ := b.readCursor.ReadString(int64(tabSize))
		removed := 0
		for _, r := range peek {
			if removed >= tabSize {
				break
			}
			if r == ' ' {
				removed++
			} else if r == '\t' {
				removed++
				break // a tab counts as a full indent step
			} else {
				break
			}
		}
		// ReadString advanced the cursor; return to the line start to delete.
		b.readCursor.SeekByte(lineStart)
		if removed > 0 {
			b.readCursor.DeleteRunes(int64(removed), false)
		}
	})
	if n > 0 {
		b.modified = true
	}
	return n
}

// Anchor is a caller-owned garland cursor handle for tracking a document
// position that must slide with edits — a window's viewport top, say. Unlike
// the buffer's built-in user/read/block cursors, an Anchor is owned by the
// caller (one per window), which is why it must be Released when the caller is
// done: garland adjusts every live cursor on every edit, so a leaked anchor is
// a permanent per-edit cost.
type Anchor struct {
	c *garland.Cursor
	b *Buffer
}

// NewAnchor mints a tracking cursor at the start of the buffer, or nil if the
// buffer has no backing garland.
func (b *Buffer) NewAnchor() *Anchor {
	if b == nil || b.garland == nil {
		return nil
	}
	c := b.garland.NewCursor()
	c.SeekByte(0)
	return &Anchor{c: c, b: b}
}

// SeekLine parks the anchor at the start of the given line.
func (a *Anchor) SeekLine(line int) {
	if a != nil && a.c != nil {
		a.c.SeekLine(int64(line), 0)
	}
}

// Line reports the anchor's current line; it slides as the buffer is edited.
func (a *Anchor) Line() int {
	if a == nil || a.c == nil {
		return 0
	}
	l, _ := a.c.LinePos()
	return int(l)
}

// BytePos reports the anchor's current byte offset; it slides with edits. This
// is the position's identity for the cursor ring, which tracks history purely
// by byte offset rather than resolving line/rune.
func (a *Anchor) BytePos() int64 {
	if a == nil || a.c == nil {
		return 0
	}
	return a.c.BytePos()
}

// SeekByte parks the anchor at a byte offset. Used to copy another cursor's
// position into a ring entry or the lastEditPoint.
func (a *Anchor) SeekByte(pos int64) {
	if a != nil && a.c != nil {
		a.c.SeekByte(pos)
	}
}

// Release removes the anchor's cursor from garland so it is no longer adjusted
// on edits. Safe to call more than once.
func (a *Anchor) Release() {
	if a != nil && a.c != nil {
		if a.b != nil && a.b.garland != nil {
			a.b.garland.RemoveCursor(a.c)
		}
		a.c = nil
	}
}

// Caret is a caller-owned garland cursor that both edits and tracks a caret
// position. Each editor window owns one, so it is the window's own edit
// cursor: garland maintains it across every edit (including edits made through
// another window on the same buffer), so it never goes stale, and there is no
// shared buffer-wide edit cursor to conflate two windows. Release it when the
// window is gone.
type Caret struct {
	c *garland.Cursor
	b *Buffer
}

// NewCaret mints a caret cursor at the start of the buffer, in Human mode for
// edit optimizations, or nil if the buffer has no backing garland.
func (b *Buffer) NewCaret() *Caret {
	if b == nil || b.garland == nil {
		return nil
	}
	c := b.garland.NewCursor()
	c.SetMode(garland.CursorModeHuman)
	c.SeekByte(0)
	return &Caret{c: c, b: b}
}

// Seek positions the caret at a line/rune (before an edit, or to mirror a
// navigation move made in the window's cached CursorPos).
func (k *Caret) Seek(line, runePos int) {
	if k != nil && k.c != nil {
		k.c.SeekLine(int64(line), int64(runePos))
	}
}

// Position reports the caret's current line/rune; it slides as the buffer is
// edited (by this window or another sharing the buffer).
func (k *Caret) Position() (line, runePos int) {
	if k == nil || k.c == nil {
		return 0, 0
	}
	l, r := k.c.LinePos()
	return int(l), int(r)
}

// BytePos reports the caret's current byte offset. The cursor ring copies this
// into its anchors, and compares against it to tell whether a move has returned
// the caret to the lastEditPoint.
func (k *Caret) BytePos() int64 {
	if k == nil || k.c == nil {
		return 0
	}
	return k.c.BytePos()
}

// SeekByte moves the caret to a byte offset. Used by go_pos_prior/go_pos_next
// to jump the caret to a remembered ring position.
func (k *Caret) SeekByte(pos int64) {
	if k != nil && k.c != nil {
		k.c.SeekByte(pos)
	}
}

// Insert inserts text at the caret; the caret advances past it. text may
// contain newlines.
func (k *Caret) Insert(text string) {
	if k == nil || k.c == nil || text == "" {
		return
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l)
	}
	k.c.InsertString(text, nil, false)
	k.b.modified = true
}

// DeleteForward deletes count runes at the caret (the caret stays put).
func (k *Caret) DeleteForward(count int) {
	if k == nil || k.c == nil || count <= 0 {
		return
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l)
	}
	k.c.DeleteRunes(int64(count), false)
	k.b.modified = true
}

// DeleteBackward deletes count runes before the caret (the caret moves back).
func (k *Caret) DeleteBackward(count int) {
	if k == nil || k.c == nil || count <= 0 {
		return
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l - 1) // a join at line start damages the previous line
	}
	k.c.BackDeleteRunes(int64(count), false)
	k.b.modified = true
}

// Overwrite replaces oldByteLen bytes at the caret with text (overwrite-mode
// typing). Unlike a delete+insert pair, garland tags this as a single overwrite
// mutation, so a run of overwrites coalesces into one undo step — and an
// appending insert at the run's end continues that run (the one-way overtype ->
// append transition). The caret is not moved; the caller advances it.
func (k *Caret) Overwrite(oldByteLen int64, text string) {
	if k == nil || k.c == nil {
		return
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l)
	}
	k.c.OverwriteBytes(oldByteLen, []byte(text))
	k.b.modified = true
}

// Release removes the caret cursor from garland. Safe to call more than once.
func (k *Caret) Release() {
	if k != nil && k.c != nil {
		if k.b != nil && k.b.garland != nil {
			k.b.garland.RemoveCursor(k.c)
		}
		k.c = nil
	}
}

// lineRuneToByte converts a line/rune position to an absolute byte offset using
// the readCursor (does not disturb window carets).
func (b *Buffer) lineRuneToByte(line, runePos int) (int64, bool) {
	if b.garland == nil || b.readCursor == nil {
		return 0, false
	}
	if err := b.readCursor.SeekLine(int64(line), int64(runePos)); err != nil {
		return 0, false
	}
	return b.readCursor.BytePos(), true
}

// GetTextRange returns the exact text between two line/rune positions,
// terminators included and unaltered. Backed by garland's byte-range read, so
// it reproduces newlines faithfully rather than reconstructing them line by
// line (which is prone to doubling the terminators GetLine already returns).
func (b *Buffer) GetTextRange(startLine, startRune, endLine, endRune int) string {
	startByte, ok1 := b.lineRuneToByte(startLine, startRune)
	endByte, ok2 := b.lineRuneToByte(endLine, endRune)
	if !ok1 || !ok2 || endByte <= startByte {
		return ""
	}
	if err := b.readCursor.SeekByte(startByte); err != nil {
		return ""
	}
	data, _ := b.readCursor.ReadBytes(endByte - startByte)
	return string(data)
}

// GetLineRange returns the lines in [startLine, endLine) as a slice, read in a
// single sequential pass. Calling GetLine in a loop re-seeks to an absolute
// line each time, and seeking to line i costs O(i), so walking a whole file
// that way is O(n²); this reads the span's bytes once (O(span)) and splits
// them. Each returned string is one line's content without its trailing '\n'
// (a trailing '\r' is left in place, as GetLine leaves it for the caller to
// trim). The range is clamped to the buffer, and a request for k lines always
// returns exactly k entries (trailing empty lines included).
func (b *Buffer) GetLineRange(startLine, endLine int) []string {
	if b.garland == nil || b.readCursor == nil {
		return nil
	}
	n := b.GetLineCount()
	if startLine < 0 {
		startLine = 0
	}
	if endLine > n {
		endLine = n
	}
	if startLine >= endLine {
		return nil
	}
	want := endLine - startLine
	startByte, ok := b.lineRuneToByte(startLine, 0)
	if !ok {
		return nil
	}
	var endByte int64
	if endLine < n {
		if endByte, ok = b.lineRuneToByte(endLine, 0); !ok {
			return nil
		}
	} else {
		endByte = b.garland.ByteCount().Value
	}
	if endByte <= startByte {
		// No bytes span the range (e.g. the trailing empty line after a final
		// newline): still return the requested count of empty lines.
		return make([]string, want)
	}
	if err := b.readCursor.SeekByte(startByte); err != nil {
		return nil
	}
	data, _ := b.readCursor.ReadBytes(endByte - startByte)
	parts := strings.Split(string(data), "\n")
	// Reading up to the start of endLine ends right after the newline that
	// terminates line endLine-1, so Split yields one trailing "" to drop; when
	// reading to EOF the counts already line up. Normalize to exactly `want`.
	if len(parts) > want {
		parts = parts[:want]
	}
	for len(parts) < want {
		parts = append(parts, "")
	}
	return parts
}

// DeleteTextRange deletes the exact text between two line/rune positions using
// garland's byte-range delete. This joins the start position's prefix with the
// end position's suffix in a single operation, without the terminator doubling
// of line-by-line reconstruction.
func (b *Buffer) DeleteTextRange(startLine, startRune, endLine, endRune int) {
	startByte, ok1 := b.lineRuneToByte(startLine, startRune)
	endByte, ok2 := b.lineRuneToByte(endLine, endRune)
	if !ok1 || !ok2 || endByte <= startByte {
		return
	}
	b.beginMutation()
	b.touchContent(startLine)

	b.garland.TransactionStart("DeleteTextRange")
	defer b.garland.TransactionCommit()

	if err := b.readCursor.SeekByte(startByte); err != nil {
		return
	}
	b.readCursor.DeleteBytes(endByte-startByte, true)

	b.modified = true
}

// IsModified returns whether the buffer has been modified.
func (b *Buffer) IsModified() bool {
	return b.modified
}

// SetModified sets the modified flag.
func (b *Buffer) SetModified(modified bool) {
	b.modified = modified
}

// GetFilename returns the associated filename.
func (b *Buffer) GetFilename() string {
	return b.filename
}

// SetFilename sets the associated filename.
func (b *Buffer) SetFilename(filename string) {
	b.filename = filename
}

// GetContent returns the entire buffer content as a string.
// Uses readCursor to avoid disturbing window carets.
func (b *Buffer) GetContent() string {
	if b.garland == nil || b.readCursor == nil {
		return ""
	}

	// Seek to start
	b.readCursor.SeekByte(0)

	// Read entire content
	byteCount := b.garland.ByteCount()
	content, err := b.readCursor.ReadBytes(byteCount.Value)
	if err != nil {
		return ""
	}

	return string(content)
}

// =============================================================================
// Cursor-aware methods for efficient editing
// These methods avoid the O(n) position recalculation of the line-based API
// =============================================================================

// BeginUserCommand marks the start of a user-initiated command named `name`.
// All buffer mutations until the matching EndUserCommand are grouped into a
// single garland revision named `name`. The transaction is opened lazily on the
// first mutation, so a command that edits nothing creates no revision. Nesting
// is supported (re-entrant commands); the outermost name is used.
func (b *Buffer) BeginUserCommand(name string) {
	if b.garland == nil {
		return
	}
	if b.cmdDepth == 0 {
		b.cmdName = name
	}
	b.cmdDepth++
}

// EndUserCommand ends the current user command, committing the grouped
// transaction if a mutation opened it. If nothing was edited, no revision is
// created. When the command (or a nested one) asked to cancel, the grouped
// mutations are rolled back instead.
func (b *Buffer) EndUserCommand() {
	if b.garland == nil || b.cmdDepth == 0 {
		return
	}
	b.cmdDepth--
	if b.cmdDepth != 0 {
		return
	}
	if b.cmdTxnOpen {
		if b.cmdCancel {
			b.garland.TransactionRollback()
			b.touchContent(-1) // content reverted: invalidate derived caches wholesale
		} else {
			b.garland.TransactionCommit()
		}
		b.cmdTxnOpen = false
	}
	b.cmdCancel = false
	b.cmdName = ""
}

// CancelUserCommand ends the current user command by rolling its grouped
// mutations back (garland restores the pre-command content) instead of
// committing. Honored at the outermost level: a cancel anywhere in the nest
// makes the whole command roll back. If nothing was edited it is a no-op.
func (b *Buffer) CancelUserCommand() {
	if b.garland == nil || b.cmdDepth == 0 {
		return
	}
	b.cmdCancel = true
	b.EndUserCommand()
}

// CloseUserCommand force-commits any user command still open (a script that
// ran buffer_tx_start without a matching commit/cancel), unwinding the whole
// nest. A no-op when nothing is open — the common case. The editor calls this
// after every command dispatch so a stray open transaction can never leak past
// it and swallow later edits.
func (b *Buffer) CloseUserCommand() {
	for b.cmdDepth > 0 {
		b.EndUserCommand()
	}
}

// SetUndoCoalescing configures garland's undo coalescing for this buffer (see
// enableEditingUndo); autoBake 0 disables the time-based break.
func (b *Buffer) SetUndoCoalescing(enabled bool, autoBake time.Duration) {
	if b.garland != nil {
		b.garland.SetUndoCoalescing(enabled, autoBake)
	}
}

// BakeUndo forces a hard edge in the undo history: the current coalescing run
// (a typing/deleting streak) is finalized, so the next edit begins a fresh
// revision no matter how adjacent it is. The editor calls this after any
// non-editing command, so moving the caret always starts a new undo step.
func (b *Buffer) BakeUndo() {
	if b.garland != nil {
		b.garland.Bake()
	}
}

// beginMutation lazily opens the command's transaction on the first mutation
// within a user command, so all of the command's mutations collapse into one
// named revision. Called at the top of every mutating buffer method. Outside a
// user command the transaction part is a no-op. Content mutations
// additionally call touchContent; decoration-only mutations (marks) do not.
func (b *Buffer) beginMutation() {
	if b.cmdDepth > 0 && !b.cmdTxnOpen {
		b.garland.TransactionStart(b.cmdName)
		b.cmdTxnOpen = true
	}
}

// dirtyClean is the watermark's rest value: no content touched.
const dirtyClean = int(^uint(0) >> 1)

// touchContent records a content mutation whose damage begins at line: it
// advances the change sequence and lowers the dirty watermark, telling
// derived caches both THAT content changed and WHERE the change starts
// (lines below the watermark still hold what they held before). A negative
// line means "unknown extent" (undo/redo) and dirties the whole buffer.
func (b *Buffer) touchContent(line int) {
	if b.changeSeq == 0 {
		b.dirtyLow = dirtyClean // first touch: initialize the rest value
	}
	b.changeSeq++
	if line < 0 {
		line = 0
	}
	if line < b.dirtyLow {
		b.dirtyLow = line
	}
}

// ChangeSeq is a monotonic counter bumped on every content mutation
// (including undo/redo) — and only content: decoration changes (marks)
// leave it alone. Equal values mean the content has not changed.
func (b *Buffer) ChangeSeq() int64 {
	return b.changeSeq
}

// TakeDirtyLow returns the lowest line touched by content mutations since
// the previous call, resetting the watermark. Lines below the returned
// value still hold exactly what they held before the mutations. Meant for
// the buffer's single derived-cache consumer (the editor's highlight
// cache); pair each call with a ChangeSeq read.
func (b *Buffer) TakeDirtyLow() int {
	low := b.dirtyLow
	if b.changeSeq == 0 {
		low = dirtyClean // never touched: the zero value means clean here
	}
	b.dirtyLow = dirtyClean
	return low
}

// Undo undoes the last change.
func (b *Buffer) Undo() bool {
	if b.garland == nil {
		return false
	}

	currentRev := b.garland.CurrentRevision()
	if currentRev <= 0 {
		return false
	}

	err := b.garland.UndoSeek(currentRev - 1)
	if err != nil {
		return false
	}
	// UndoSeek changes content and garland slides every registered cursor,
	// including each window's caret; the editor reads the focused window's
	// caret back afterward (see syncCursorAfterUndoRedo).
	b.modified = true
	b.touchContent(-1)
	return true
}

// Redo redoes the last undone change.
func (b *Buffer) Redo() bool {
	if b.garland == nil {
		return false
	}

	// Try to seek forward to next revision
	currentRev := b.garland.CurrentRevision()
	err := b.garland.UndoSeek(currentRev + 1)
	if err != nil {
		return false
	}
	b.modified = true
	b.touchContent(-1)
	return true
}

// Close closes the buffer and releases resources.
func (b *Buffer) Close() error {
	if b.garland != nil {
		return b.garland.Close()
	}
	return nil
}

// SaveTo writes the buffer's content to the named file through Garland's
// save engine, streaming from all storage tiers without materializing the
// document: saving onto the buffer's original file routes through the
// in-place engine (zero-copy warm spans, history preservation), any other
// path is a streamed write. Scar warnings report blocks lost to storage
// failures. Only for the real OS file system - virtualized hosts write
// through their own callbacks instead.
func (b *Buffer) SaveTo(filename string) (warnings []string, err error) {
	if b.garland == nil {
		return nil, fmt.Errorf("buffer has no content store")
	}
	report, err := b.garland.SaveAs(nil, filename)
	if err != nil {
		return nil, err
	}
	for _, scar := range report.Scars {
		reason := scar.Reason
		if reason == "" {
			reason = "data lost"
		}
		warnings = append(warnings, fmt.Sprintf("Save scar at byte %d (%d bytes): %s", scar.Offset, scar.Length, reason))
	}
	b.modified = false
	return warnings, nil
}

// SetMark sets a named mark at a specific line and rune position.
// Uses readCursor to avoid disturbing window carets.
func (b *Buffer) SetMark(name string, line, runePos int) error {
	_, err := b.SetMarkDebug(name, line, runePos)
	return err
}

// SetMarkDebug sets a mark and returns the byte position used.
// Useful for debugging mark positioning issues.
func (b *Buffer) SetMarkDebug(name string, line, runePos int) (bytePos int64, err error) {
	if b.garland == nil || b.readCursor == nil {
		return 0, nil
	}
	b.beginMutation()

	// Convert line/rune position to byte position
	err = b.readCursor.SeekLine(int64(line), int64(runePos))
	if err != nil {
		return 0, err
	}
	bytePos = b.readCursor.BytePos()

	// Create decoration entry
	entry := garland.DecorationEntry{
		Key:     name,
		Address: &garland.AbsoluteAddress{Byte: bytePos},
	}

	_, err = b.garland.Decorate([]garland.DecorationEntry{entry})
	return bytePos, err
}

// GetMark gets the position of a named mark.
// Returns line, runePos, and whether the mark exists.
// Uses readCursor to avoid disturbing window carets.
func (b *Buffer) GetMark(name string) (line, runePos int, exists bool) {
	if b.garland == nil || b.readCursor == nil {
		return 0, 0, false
	}

	addr, err := b.garland.GetDecorationPosition(name)
	if err != nil {
		return 0, 0, false
	}

	// Convert byte position to line/rune position
	b.readCursor.SeekByte(addr.Byte)
	lineNum, runeNum := b.readCursor.LinePos()

	return int(lineNum), int(runeNum), true
}

// GetMarkDebug returns detailed debug info about a mark's position.
// Returns: line, runePos, bytePos, exists
func (b *Buffer) GetMarkDebug(name string) (line, runePos int, bytePos int64, exists bool) {
	if b.garland == nil || b.readCursor == nil {
		return 0, 0, 0, false
	}

	addr, err := b.garland.GetDecorationPosition(name)
	if err != nil {
		return 0, 0, 0, false
	}

	// Convert byte position to line/rune position
	b.readCursor.SeekByte(addr.Byte)
	lineNum, runeNum := b.readCursor.LinePos()

	return int(lineNum), int(runeNum), addr.Byte, true
}

// MarksOnLine returns the sorted, de-duplicated rune (column) positions of the
// marks (garland decorations) on the given document line. Internal marks — keys
// beginning with "_", e.g. the block/match selection anchors — are skipped
// unless includeInternal is set (the showMarks "all" mode). Used by the
// renderer's showMarks indicator ("*" per mark position).
func (b *Buffer) MarksOnLine(docLine int, includeInternal bool) []int {
	if b.garland == nil || b.readCursor == nil {
		return nil
	}
	entries, err := b.garland.GetDecorationsOnLine(int64(docLine))
	if err != nil || len(entries) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(entries))
	var cols []int
	for _, e := range entries {
		if e.Address == nil {
			continue
		}
		if !includeInternal && strings.HasPrefix(e.Key, "_") {
			continue
		}
		b.readCursor.SeekByte(e.Address.Byte)
		ln, rn := b.readCursor.LinePos()
		if int(ln) != docLine {
			continue
		}
		col := int(rn)
		if _, dup := seen[col]; dup {
			continue
		}
		seen[col] = struct{}{}
		cols = append(cols, col)
	}
	sort.Ints(cols)
	return cols
}

// ClearMark removes a named mark.
func (b *Buffer) ClearMark(name string) error {
	if b.garland == nil {
		return nil
	}
	b.beginMutation()

	// Pass nil address to delete the decoration
	entry := garland.DecorationEntry{
		Key:     name,
		Address: nil,
	}

	_, err := b.garland.Decorate([]garland.DecorationEntry{entry})
	return err
}

// HasBlockMarks returns true if both _block_begin and _block_end marks are set.
func (b *Buffer) HasBlockMarks() bool {
	_, _, beginExists := b.GetMark("_block_begin")
	_, _, endExists := b.GetMark("_block_end")
	return beginExists && endExists
}

// GetBlockRange returns the normalized start and end positions of the marked block.
// Returns startLine, startRune, endLine, endRune, exists.
func (b *Buffer) GetBlockRange() (startLine, startRune, endLine, endRune int, exists bool) {
	return b.markRange("_block_begin", "_block_end")
}

// GetMatchRange returns the normalized range of the transient find/replace
// match highlight (_match_begin/_match_end). While it exists it is shown as
// the selection instead of the user's block, which stays set underneath.
func (b *Buffer) GetMatchRange() (startLine, startRune, endLine, endRune int, exists bool) {
	return b.markRange("_match_begin", "_match_end")
}

// markRange returns the normalized range spanned by a begin/end mark pair.
func (b *Buffer) markRange(beginName, endName string) (startLine, startRune, endLine, endRune int, exists bool) {
	beginLine, beginRune, beginExists := b.GetMark(beginName)
	endLine, endRune, endExists := b.GetMark(endName)

	if !beginExists || !endExists {
		return 0, 0, 0, 0, false
	}

	// Normalize so start is before end
	if beginLine > endLine || (beginLine == endLine && beginRune > endRune) {
		return endLine, endRune, beginLine, beginRune, true
	}
	return beginLine, beginRune, endLine, endRune, true
}

// ClearBlockMarks removes both block marks.
func (b *Buffer) ClearBlockMarks() {
	b.ClearMark("_block_begin")
	b.ClearMark("_block_end")
}

// ClearMatchMarks removes both match-highlight marks.
func (b *Buffer) ClearMatchMarks() {
	b.ClearMark("_match_begin")
	b.ClearMark("_match_end")
}

// Cursor provides a position-based interface to the buffer.
type Cursor struct {
	buffer        *Buffer
	garlandCursor *garland.Cursor
}

// NewCursor creates a cursor for the buffer.
func (b *Buffer) NewCursor() *Cursor {
	if b.garland == nil {
		return &Cursor{buffer: b}
	}
	return &Cursor{
		buffer:        b,
		garlandCursor: b.garland.NewCursor(),
	}
}

// SeekLine moves the cursor to a specific line.
func (c *Cursor) SeekLine(line int) {
	if c.garlandCursor == nil {
		return
	}
	c.garlandCursor.SeekLine(int64(line), 0)
}

// SeekLineRune moves the cursor to a specific line and rune position.
func (c *Cursor) SeekLineRune(line, runePos int) {
	if c.garlandCursor == nil {
		return
	}
	c.garlandCursor.SeekLine(int64(line), int64(runePos))
}

// ReadLine reads the current line content.
func (c *Cursor) ReadLine() (string, error) {
	if c.garlandCursor == nil {
		return "", nil
	}
	return c.garlandCursor.ReadLine()
}

// InsertString inserts text at the cursor position.
func (c *Cursor) InsertString(text string, _ interface{}, _ bool) {
	if c.garlandCursor == nil {
		return
	}
	c.garlandCursor.InsertString(text, nil, false)
}

// GetPosition returns the current cursor position.
func (c *Cursor) GetPosition() (line, runePos int) {
	if c.garlandCursor == nil {
		return 0, 0
	}
	l, r := c.garlandCursor.LinePos()
	return int(l), int(r)
}

// calculateAnsiAwareLength calculates visible length of ANSI-colored string.
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

// Balance forces a rebalance of the garland's internal tree structure.
// This can improve performance after many edits have made the tree unbalanced.
func (b *Buffer) Balance() {
	if b.garland == nil {
		return
	}
	b.garland.ForceRebalance()
	// Invalidate cursor sync since tree structure changed
}

// NodeManipulations returns the cumulative count of node manipulations (fragmentation metric).
func (b *Buffer) NodeManipulations() int64 {
	if b.garland == nil {
		return 0
	}
	return b.garland.NodeManipulations()
}

// Utility to split string by newlines (kept for compatibility)
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
