package buffer

import (
	"fmt"

	"github.com/phroun/garland"
)

// This file is the buffer-level face of garland's source-safety cluster:
// in-place saves, save-as with adoption, external-change detection, backups,
// locks, and revert-to-last-save. Editor code stays garland-free; reports
// come back as small mew-shaped values.

// HasSource reports whether garland tracks a source file for this buffer
// (opened from a file or adopted by a save-as) — the states in which
// in-place saves, consistency checks, backups, and revert apply.
func (b *Buffer) HasSource() bool {
	return b.garland != nil && b.hasSource
}

// SourceStatus is a mew-shaped snapshot of garland's external-change report.
type SourceStatus struct {
	// State: "untracked", "clean", "appended", "modified", "truncated",
	// "replaced", or "missing".
	State string

	// LockedBy is the owner of a foreign emacs-style lock on the source
	// ("user@host.pid"), when one is known.
	LockedBy string
}

// Changed reports whether the source is known to differ from the baseline
// (anything past clean/untracked — the states worth warning about before an
// overwrite).
func (s SourceStatus) Changed() bool {
	switch s.State {
	case "modified", "truncated", "replaced", "missing", "appended":
		return true
	}
	return false
}

func consistencyStateName(st garland.SourceConsistencyState) string {
	switch st {
	case garland.ConsistencyClean:
		return "clean"
	case garland.ConsistencyAppended:
		return "appended"
	case garland.ConsistencyModified:
		return "modified"
	case garland.ConsistencyTruncated:
		return "truncated"
	case garland.ConsistencyReplaced:
		return "replaced"
	case garland.ConsistencyMissing:
		return "missing"
	default:
		return "untracked"
	}
}

// SourceConsistency stats the source and reports whether it still matches
// what this buffer last agreed with it on (open, save, adoption).
func (b *Buffer) SourceConsistency() SourceStatus {
	if !b.HasSource() {
		return SourceStatus{State: "untracked"}
	}
	report, err := b.garland.SourceConsistency()
	if err != nil {
		return SourceStatus{State: "untracked"}
	}
	return SourceStatus{State: consistencyStateName(report.State), LockedBy: report.LockedBy}
}

// SourceConsistencyCached is SourceConsistency without touching the disk —
// it reports the most recent observation (for status displays).
func (b *Buffer) SourceConsistencyCached() SourceStatus {
	if !b.HasSource() {
		return SourceStatus{State: "untracked"}
	}
	report := b.garland.SourceConsistencyCached()
	return SourceStatus{State: consistencyStateName(report.State), LockedBy: report.LockedBy}
}

// Save writes the buffer back to its tracked source in place (garland's save
// engine: warm-span reuse, history preservation, save point recorded). A
// buffer without a tracked source but with a filename adopts that filename
// instead. Scar warnings (data lost to storage failure, written as visible
// placeholders) are returned for display.
func (b *Buffer) Save() (warnings []string, err error) {
	if b.garland == nil {
		return nil, fmt.Errorf("buffer has no content store")
	}
	if !b.hasSource {
		if b.filename == "" {
			return nil, fmt.Errorf("buffer has no filename")
		}
		return b.SaveAsAdopt(b.filename)
	}
	report, err := b.garland.Save()
	if err != nil {
		return nil, err
	}
	b.modified = false
	b.captureSavePoint() // the current state now matches disk: revert baseline
	return scarWarnings(report), nil
}

// SaveAsAdopt writes the buffer to filename and adopts it as the buffer's
// new source: future saves land there, change detection re-baselines against
// it, and undo history is preserved across the move. The buffer's filename
// is updated to match.
func (b *Buffer) SaveAsAdopt(filename string) (warnings []string, err error) {
	if b.garland == nil {
		return nil, fmt.Errorf("buffer has no content store")
	}
	report, err := b.garland.SaveAsWith(b.srcFS, filename, garland.SaveAsOptions{
		AdoptAsSource:   true,
		PreserveHistory: true,
	})
	if err != nil {
		return scarWarnings(report), err
	}
	b.filename = filename
	b.hasSource = true
	b.modified = false
	b.captureSavePoint() // adopted new source; current state matches it
	return scarWarnings(report), nil
}

// SaveCopyTo writes the buffer's content to filename WITHOUT adopting it:
// the buffer keeps working from its original source (an export).
func (b *Buffer) SaveCopyTo(filename string) (warnings []string, err error) {
	if b.garland == nil {
		return nil, fmt.Errorf("buffer has no content store")
	}
	report, err := b.garland.SaveAsWith(b.srcFS, filename, garland.SaveAsOptions{})
	if err != nil {
		return scarWarnings(report), err
	}
	return scarWarnings(report), nil
}

func scarWarnings(report garland.SaveReport) []string {
	var warnings []string
	for _, scar := range report.Scars {
		reason := scar.Reason
		if reason == "" {
			reason = "data lost"
		}
		warnings = append(warnings, fmt.Sprintf("Save scar at byte %d (%d bytes): %s", scar.Offset, scar.Length, reason))
	}
	return warnings
}

// RevertToLastSave seeks the buffer's history back to its revert baseline:
// the state it last agreed with its source — the most recent save, or the
// opened state if it has not been saved since open. It is a pure history
// move: redo still reaches the abandoned edits. Unlike garland's own
// RevertToLastSave (which knows only explicit saves), this also handles the
// opened-but-never-saved case, since the baseline is captured at open.
func (b *Buffer) RevertToLastSave() error {
	if b.garland == nil {
		return fmt.Errorf("buffer has no content store")
	}
	if !b.hasSaved {
		return fmt.Errorf("no saved state to revert to")
	}
	if err := b.garland.ForkSeek(b.savedFork); err != nil {
		return err
	}
	if err := b.garland.UndoSeek(b.savedRev); err != nil {
		return err
	}
	// The whole document may have changed shape: full content damage, like
	// undo/redo.
	b.touchContent(-1)
	b.modified = false
	return nil
}

// ConfigureBackups points garland's automatic backup machinery at dir with
// the given backup filename (empty name = "<basename>~"). The first content
// modification streams a pre-session copy there in the background; a save
// commits it; viewing without editing leaves nothing behind.
func (b *Buffer) ConfigureBackups(dir, name string) error {
	if !b.HasSource() {
		return fmt.Errorf("buffer has no tracked source")
	}
	return b.garland.SetBackupLocation(nil, dir, garland.BackupOptions{Name: name})
}

// BackupStatus is a mew-shaped snapshot of garland's backup state.
type BackupStatus struct {
	// State: "disabled", "armed", "pending", "ready", "committed", or
	// "failed".
	State string
	Path  string // destination backup file (once resolved)
	Err   string // failure detail when State == "failed"
}

// BackupStatus reports the buffer's automatic-backup state.
func (b *Buffer) BackupStatus() BackupStatus {
	if b.garland == nil {
		return BackupStatus{State: "disabled"}
	}
	info := b.garland.BackupInfo()
	state := "disabled"
	switch info.State {
	case garland.BackupArmed:
		state = "armed"
	case garland.BackupPending:
		state = "pending"
	case garland.BackupReady:
		state = "ready"
	case garland.BackupCommitted:
		state = "committed"
	case garland.BackupFailed:
		state = "failed"
	}
	return BackupStatus{State: state, Path: info.Path, Err: info.Err}
}

// HoldsSourceLock reports whether garland currently holds the emacs-style
// lock on this buffer's source (locks engage on the first unsaved edit).
func (b *Buffer) HoldsSourceLock() bool {
	return b.garland != nil && b.garland.HoldsSourceLock()
}

// SourceLockOwner returns the owner of a foreign lock seen on the source.
func (b *Buffer) SourceLockOwner() (string, bool) {
	if b.garland == nil {
		return "", false
	}
	return b.garland.SourceLockOwner()
}

// BreakSourceLock force-removes a foreign emacs-style lock on the source (the
// "steal" choice) and takes the lock for this buffer when it is modified.
func (b *Buffer) BreakSourceLock() error {
	if b.garland == nil {
		return nil
	}
	return b.garland.BreakSourceLock()
}
