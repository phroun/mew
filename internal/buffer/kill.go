package buffer

import (
	"strings"

	"github.com/phroun/garland"
)

// Captured is text removed from (or copied out of) a buffer together with the
// decorations (marks) that lived inside it, positioned relative to the start
// of Text in bytes. It is produced by the *Captured delete variants and
// CaptureRange, accumulated into KillBuffers, and re-inserted by
// Caret.InsertCaptured — garland carries the marks along in both directions.
type Captured struct {
	Text  string
	decos []garland.RelativeDecoration
}

// Empty reports whether the capture holds no text.
func (c Captured) Empty() bool {
	return c.Text == ""
}

// newCaptured filters raw decorations returned by a garland delete down to the
// ones that actually live inside the captured text: garland reports removed
// decorations relative to the deletion start, and positions outside [0, len]
// (negative, or in the following-newline zone len+1 and beyond) belong to
// context around the deletion, not to the killed text itself. Internal
// "_"-prefixed marks (block selection, match highlight) are dropped too:
// re-inserting a second _block_begin on yank would corrupt the block-selection
// machinery, and they are transient by design.
func newCaptured(text string, decos []garland.RelativeDecoration) Captured {
	max := int64(len(text)) // byte length; decoration positions are byte-relative
	kept := make([]garland.RelativeDecoration, 0, len(decos))
	for _, d := range decos {
		if d.Position < 0 || d.Position > max {
			continue
		}
		if strings.HasPrefix(d.Key, "_") {
			continue
		}
		kept = append(kept, d)
	}
	return Captured{Text: text, decos: kept}
}

// newCapturedForMove filters like newCaptured — in-range positions only, no
// internal "_" marks — but keeps the block-selection markers themselves: a
// move removes the source copy, so re-placing _block_begin/_block_end at the
// destination cannot duplicate them, and the block stays selected where it
// landed.
func newCapturedForMove(text string, decos []garland.RelativeDecoration) Captured {
	max := int64(len(text))
	kept := make([]garland.RelativeDecoration, 0, len(decos))
	for _, d := range decos {
		if d.Position < 0 || d.Position > max {
			continue
		}
		if strings.HasPrefix(d.Key, "_") && d.Key != "_block_begin" && d.Key != "_block_end" {
			continue
		}
		kept = append(kept, d)
	}
	return Captured{Text: text, decos: kept}
}

// removeCapturedMarks deletes the captured marks from the source garland.
// Garland never deletes marks with a range — they collapse to the deletion
// point and the delete's return value is only a report — but a killed mark
// travels with its text into the kill buffer, so the collapsed original must
// be removed explicitly (a nil Address deletes the decoration).
func removeCapturedMarks(g *garland.Garland, cap Captured) {
	if g == nil || len(cap.decos) == 0 {
		return
	}
	entries := make([]garland.DecorationEntry, 0, len(cap.decos))
	for _, d := range cap.decos {
		entries = append(entries, garland.DecorationEntry{Key: d.Key, Address: nil})
	}
	g.Decorate(entries)
}

// KillBuffer is one kill-ring entry: a standalone garland holding killed text
// and the marks that traveled with it. Being a garland, appends and prepends
// slide the already-stored decorations automatically. Close releases it when
// the entry is evicted from the ring.
type KillBuffer struct {
	g *garland.Garland
	c *garland.Cursor
}

// NewKillBuffer mints an empty kill buffer using the default library, or nil if
// the garland library is unavailable.
func NewKillBuffer() *KillBuffer { return defaultLibrary().NewKillBuffer() }

// NewKillBuffer mints an empty kill buffer in this library.
func (lib *Library) NewKillBuffer() *KillBuffer {
	if lib == nil || lib.g == nil {
		return nil
	}
	g, err := lib.g.Open(garland.FileOptions{DataBytes: []byte{}})
	if err != nil {
		return nil
	}
	c := g.NewCursor()
	c.SeekByte(0)
	return &KillBuffer{g: g, c: c}
}

// Append adds a capture at the end of the kill buffer (a forward delete
// continuing an accumulation).
func (k *KillBuffer) Append(cap Captured) {
	if k == nil || k.g == nil || cap.Empty() {
		return
	}
	k.c.SeekByte(k.g.ByteCount().Value)
	k.c.InsertString(cap.Text, cap.decos, false)
}

// Prepend adds a capture at the start of the kill buffer (a backward delete
// continuing an accumulation). insertBefore=true slides the existing content's
// decorations right along with the text they belong to.
func (k *KillBuffer) Prepend(cap Captured) {
	if k == nil || k.g == nil || cap.Empty() {
		return
	}
	k.c.SeekByte(0)
	k.c.InsertString(cap.Text, cap.decos, true)
}

// Capture returns the kill buffer's full content and decorations, ready to be
// re-inserted into a document (Caret.InsertCaptured).
func (k *KillBuffer) Capture() Captured {
	if k == nil || k.g == nil {
		return Captured{}
	}
	n := k.g.ByteCount().Value
	if n == 0 {
		return Captured{}
	}
	k.c.SeekByte(0)
	data, _ := k.c.ReadBytes(n)
	var decos []garland.RelativeDecoration
	// end n+1 so a mark sitting exactly at the end of the killed text is kept.
	if entries, err := k.g.GetDecorationsInByteRange(0, n+1); err == nil {
		for _, e := range entries {
			if e.Address == nil {
				continue
			}
			decos = append(decos, garland.RelativeDecoration{Key: e.Key, Position: e.Address.Byte})
		}
	}
	return Captured{Text: string(data), decos: decos}
}

// Close releases the kill buffer's garland.
func (k *KillBuffer) Close() {
	if k != nil && k.g != nil {
		k.g.Close()
		k.g = nil
		k.c = nil
	}
}

// DeleteForwardCaptured is DeleteForward returning what was removed: the text
// (read before deleting) and the marks garland reports out of the deleted
// range, filtered to the capture (see newCaptured).
func (k *Caret) DeleteForwardCaptured(count int) Captured {
	if k == nil || k.c == nil || count <= 0 {
		return Captured{}
	}
	text := ""
	if k.b != nil && k.b.readCursor != nil {
		if err := k.b.readCursor.SeekByte(k.c.BytePos()); err == nil {
			text, _ = k.b.readCursor.ReadString(int64(count))
		}
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l)
	}
	decos, _, _ := k.c.DeleteRunes(int64(count), false)
	k.b.modified = true
	return newCaptured(text, decos)
}

// DeleteBackwardCaptured is DeleteBackward returning what was removed.
func (k *Caret) DeleteBackwardCaptured(count int) Captured {
	if k == nil || k.c == nil || count <= 0 {
		return Captured{}
	}
	runePos := k.c.RunePos()
	if int64(count) > runePos {
		count = int(runePos)
	}
	if count == 0 {
		return Captured{}
	}
	text := ""
	if k.b != nil && k.b.readCursor != nil {
		if err := k.b.readCursor.SeekRune(runePos - int64(count)); err == nil {
			text, _ = k.b.readCursor.ReadString(int64(count))
		}
	}
	k.b.beginMutation()
	if l, _ := k.Position(); true {
		k.b.touchContent(l - 1) // a join at line start damages the previous line
	}
	decos, _, _ := k.c.BackDeleteRunes(int64(count), false)
	k.b.modified = true
	return newCaptured(text, decos)
}

// InsertCaptured inserts a capture at the caret: the text is inserted, then
// each captured mark is SET at its offset within the placement via garland's
// Decorate — which treats a key as unique document-wide, so a same-named mark
// already in THIS buffer moves to the new placement (the collapsed remnant of
// the kill rides the yank; the newest placement wins), while the same name in
// any other buffer is a different mark entirely and is never touched. The
// caret advances past the inserted text (like Insert).
func (k *Caret) InsertCaptured(cap Captured) {
	if k == nil || k.c == nil || cap.Empty() {
		return
	}
	k.b.beginMutation()
	// The text insert and the mark placement are two garland ops; group them as
	// ONE revision so undoing the yank restores every mark in a single step
	// (kill_ring_pop undoes the previous placement wholesale — see killring.go).
	k.b.garland.TransactionStart("InsertCaptured")
	defer k.b.garland.TransactionCommit()
	if l, _ := k.Position(); true {
		k.b.touchContent(l)
	}
	start := k.c.BytePos()
	k.c.InsertString(cap.Text, nil, false)
	k.b.modified = true
	if len(cap.decos) == 0 {
		return
	}
	entries := make([]garland.DecorationEntry, 0, len(cap.decos))
	for _, d := range cap.decos {
		addr := garland.ByteAddress(start + d.Position)
		entries = append(entries, garland.DecorationEntry{Key: d.Key, Address: &addr})
	}
	k.b.garland.Decorate(entries)
}

// DeleteTextCaptured is DeleteText returning what was removed.
func (b *Buffer) DeleteTextCaptured(line, runePos, count int) Captured {
	if b.garland == nil || b.readCursor == nil || count <= 0 {
		return Captured{}
	}
	b.beginMutation()
	b.touchContent(line)
	if err := b.readCursor.SeekLine(int64(line), int64(runePos)); err != nil {
		return Captured{}
	}
	text, _ := b.readCursor.ReadString(int64(count))
	if err := b.readCursor.SeekLine(int64(line), int64(runePos)); err != nil {
		return Captured{}
	}
	decos, _, _ := b.readCursor.DeleteRunes(int64(count), false)
	b.modified = true
	return newCaptured(text, decos)
}

// DeleteLineCaptured is DeleteLine returning what was removed (the line's
// content plus whichever terminator the deletion consumed).
func (b *Buffer) DeleteLineCaptured(line int) Captured {
	if b.garland == nil || b.readCursor == nil {
		return Captured{}
	}
	lineCount := b.GetLineCount()
	if line < 0 || line >= lineCount {
		return Captured{}
	}
	b.beginMutation()
	b.touchContent(line)

	b.garland.TransactionStart("DeleteLine")
	defer b.garland.TransactionCommit()

	content := b.GetLine(line)
	contentRunes := int64(len([]rune(content)))

	// Mirror DeleteLine's three cases, reading the doomed text before each
	// delete and capturing the decorations the delete reports.
	capture := func(seek func() error, runes int64) Captured {
		if runes <= 0 {
			return Captured{}
		}
		text := ""
		if seek() == nil {
			text, _ = b.readCursor.ReadString(runes)
		}
		if err := seek(); err != nil {
			return Captured{}
		}
		decos, _, _ := b.readCursor.DeleteRunes(runes, true)
		b.modified = true
		return newCaptured(text, decos)
	}

	if line < lineCount-1 {
		return capture(func() error { return b.readCursor.SeekLine(int64(line), 0) }, contentRunes)
	} else if line > 0 {
		prevContent := b.GetLine(line - 1)
		prevNoTerm := strings.TrimRight(prevContent, "\n\r")
		prevLen := int64(len([]rune(prevNoTerm)))
		termRunes := int64(len([]rune(prevContent))) - prevLen
		return capture(func() error { return b.readCursor.SeekLine(int64(line-1), prevLen) }, termRunes+contentRunes)
	}
	return capture(func() error { return b.readCursor.SeekByte(0) }, contentRunes)
}

// DeleteTextRangeForMove is DeleteTextRange returning what was removed: the
// text plus the in-range marks INCLUDING the block-selection markers (see
// newCapturedForMove — this is the block_move capture, where re-placing the
// markers is safe). The captured marks are removed from the source (garland
// collapses them to the deletion point otherwise) so the caller can re-place
// them at the destination.
func (b *Buffer) DeleteTextRangeForMove(startLine, startRune, endLine, endRune int) Captured {
	return b.deleteTextRangeCaptured(startLine, startRune, endLine, endRune, true)
}

// DeleteTextRangeForKill is DeleteTextRange returning what was removed under
// the KILL filtering decision (newCaptured): in-range user marks travel, all
// internal "_" marks — block markers included — stay behind for the caller to
// clear. This is block_delete's kill-region capture.
func (b *Buffer) DeleteTextRangeForKill(startLine, startRune, endLine, endRune int) Captured {
	return b.deleteTextRangeCaptured(startLine, startRune, endLine, endRune, false)
}

func (b *Buffer) deleteTextRangeCaptured(startLine, startRune, endLine, endRune int, forMove bool) Captured {
	startByte, ok1 := b.lineRuneToByte(startLine, startRune)
	endByte, ok2 := b.lineRuneToByte(endLine, endRune)
	if !ok1 || !ok2 || endByte <= startByte {
		return Captured{}
	}
	b.beginMutation()
	b.touchContent(startLine)

	b.garland.TransactionStart("DeleteTextRange")
	defer b.garland.TransactionCommit()

	if err := b.readCursor.SeekByte(startByte); err != nil {
		return Captured{}
	}
	data, _ := b.readCursor.ReadBytes(endByte - startByte)

	// The block-end marker sits exactly ON the range's end boundary — one past
	// the block's last byte — where a range delete neither removes nor reports
	// it; left alone it would slide back to the deletion point and the moved
	// block would lose its selection. For a MOVE, detect block markers on the
	// boundary BEFORE deleting and fold them into the capture at Position len
	// (the moved text's own end). Only the block markers get this treatment: a
	// user mark on the boundary is outside the block, same as the kill capture.
	var boundary []garland.RelativeDecoration
	if forMove {
		if entries, err := b.garland.GetDecorationsInByteRange(endByte, endByte+1); err == nil {
			for _, en := range entries {
				if en.Address == nil {
					continue
				}
				if en.Key == "_block_begin" || en.Key == "_block_end" {
					boundary = append(boundary, garland.RelativeDecoration{Key: en.Key, Position: endByte - startByte})
				}
			}
		}
	}

	if err := b.readCursor.SeekByte(startByte); err != nil {
		return Captured{}
	}
	decos, _, _ := b.readCursor.DeleteBytes(endByte-startByte, true)
	b.modified = true
	var cap Captured
	if forMove {
		cap = newCapturedForMove(string(data), decos)
		cap.decos = append(cap.decos, boundary...)
	} else {
		cap = newCaptured(string(data), decos)
	}
	removeCapturedMarks(b.garland, cap)
	return cap
}

// InsertCaptured inserts a capture at a line/rune position through the
// buffer's read cursor (mirroring InsertText), placing the captured marks
// along with the text.
func (b *Buffer) InsertCaptured(line, runePos int, cap Captured) {
	if b.garland == nil || b.readCursor == nil || cap.Empty() {
		return
	}
	b.beginMutation()
	b.touchContent(line)

	byteCount := b.garland.ByteCount()
	if byteCount.Value == 0 {
		b.readCursor.SeekByte(0)
	} else if err := b.readCursor.SeekLine(int64(line), int64(runePos)); err != nil {
		b.readCursor.SeekByte(byteCount.Value)
	}
	b.readCursor.InsertString(cap.Text, cap.decos, false)
	b.modified = true
}

// CaptureRange copies the text and marks between two line/rune positions
// WITHOUT deleting anything (kill-ring-save). Decorations are reported by
// garland's byte-range query and rebased to the range start.
func (b *Buffer) CaptureRange(startLine, startRune, endLine, endRune int) Captured {
	startByte, ok1 := b.lineRuneToByte(startLine, startRune)
	endByte, ok2 := b.lineRuneToByte(endLine, endRune)
	if !ok1 || !ok2 || endByte <= startByte {
		return Captured{}
	}
	if err := b.readCursor.SeekByte(startByte); err != nil {
		return Captured{}
	}
	data, _ := b.readCursor.ReadBytes(endByte - startByte)
	var decos []garland.RelativeDecoration
	if entries, err := b.garland.GetDecorationsInByteRange(startByte, endByte); err == nil {
		for _, e := range entries {
			if e.Address == nil || strings.HasPrefix(e.Key, "_") {
				continue
			}
			decos = append(decos, garland.RelativeDecoration{Key: e.Key, Position: e.Address.Byte - startByte})
		}
	}
	return Captured{Text: string(data), decos: decos}
}
