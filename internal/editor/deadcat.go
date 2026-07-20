package editor

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

// DEADCAT is mew's DEADJOE: when the editor dies unexpectedly (a terminal
// hang-up, a kill signal, a panic, or a host's sudden shutdown), every
// modified buffer is dumped to a recoverable file so unsaved work survives.
// It complements garland's pre-session backups (which capture the file BEFORE
// this session) by capturing the CURRENT unsaved state at the moment of death.
//
// The destination is resolved once, at startup (resolveDeadcat) — the death
// path must never compute a path or make a decision. In a standalone session
// mew writes the full dump to a private location (the config folder) and
// leaves a breadcrumb in the working directory for discoverability; a host
// embedding mew opts in and receives the dump through its own FileSystem.

const deadcatFilename = "DEADCAT"

// deadcatPlan is the pre-resolved crash-dump destination.
type deadcatPlan struct {
	// configTarget is the primary full-dump path in OS mode: [storage] deadcat
	// (a directory) joined with DEADCAT, else <config dir>/DEADCAT. Empty when
	// no config-dir target applies.
	configTarget string
	// cwd is the working directory captured at startup (OS mode), where the
	// breadcrumb (or a fallback full dump) is written.
	cwd string
	// hostName is the dump target written through the host FileSystem in module
	// mode. Empty means the host did not opt in (WithDeadcat).
	hostName string
	// useHost routes writes through the host FileSystem instead of the OS.
	useHost bool
}

// resolveDeadcat computes the crash-dump destination. Called once from New.
func (e *Editor) resolveDeadcat() {
	if wd, err := os.Getwd(); err == nil {
		e.launchDir = wd // also the completion fallback anchor (completion.go)
	}
	var p deadcatPlan
	if e.usingOSFS {
		switch {
		case e.configFromDisk && e.LoadedConfig.Storage.Deadcat != "":
			p.configTarget = filepath.Join(e.LoadedConfig.Storage.Deadcat, deadcatFilename)
		case e.home != "":
			p.configTarget = filepath.Join(e.home, ".mew", deadcatFilename)
		}
		if wd, err := os.Getwd(); err == nil {
			p.cwd = wd
		}
	} else {
		p.hostName = e.Config.DeadcatName
		p.useHost = p.hostName != ""
	}
	e.deadcat = p
}

// DumpDeadcat writes every modified buffer to the resolved DEADCAT location and
// returns the path written (empty when nothing was modified, or no destination
// resolved). A host calls this during its own sudden shutdown; the standalone
// editor calls it from its signal/panic handlers. Dumping never panics — a
// rescue that crashes is worse than none.
func (e *Editor) DumpDeadcat(reason string) (path string, err error) {
	defer func() { _ = recover() }() // a dump must not itself take us down

	bufs := e.modifiedBufferDump()
	if len(bufs) == 0 {
		return "", nil
	}
	full := formatDeadcatFull(reason, bufs)

	if e.deadcat.useHost {
		if err := e.hostAppend(e.deadcat.hostName, full); err != nil {
			return "", err
		}
		return e.deadcat.hostName, nil
	}

	// OS mode. Primary: the private config-folder file. On success, drop a
	// breadcrumb in the working directory (where the user will trip over it),
	// unless the working directory IS the config folder.
	if e.deadcat.configTarget != "" {
		if err := osAppendFile(e.deadcat.configTarget, full); err == nil {
			if cwdPath := e.deadcatCwdPath(); cwdPath != "" && !samePath(cwdPath, e.deadcat.configTarget) {
				_ = osAppendFile(cwdPath, formatDeadcatPointer(reason))
			}
			return e.deadcat.configTarget, nil
		}
	}
	// Config folder unwritable (or none): full dump straight to the working
	// directory, then the home directory as a last resort.
	if cwdPath := e.deadcatCwdPath(); cwdPath != "" {
		if err := osAppendFile(cwdPath, full); err == nil {
			return cwdPath, nil
		}
	}
	if e.home != "" {
		hp := filepath.Join(e.home, deadcatFilename)
		if werr := osAppendFile(hp, full); werr == nil {
			return hp, nil
		}
	}
	return "", fmt.Errorf("no writable DEADCAT location")
}

// deadcatLaunchNotice shows a transient error, on any launch, when a DEADCAT
// from a prior crash is present — so the user knows unsaved work is waiting to
// be recovered. It is deliberately not a prompt (too intrusive); the user goes
// and deals with the file at their leisure.
func (e *Editor) deadcatLaunchNotice() {
	found := ""
	switch {
	case e.deadcat.configTarget != "" && osFileExists(e.deadcat.configTarget):
		found = e.deadcat.configTarget
	case e.deadcatCwdPath() != "" && osFileExists(e.deadcatCwdPath()):
		found = e.deadcatCwdPath()
	}
	if found != "" {
		e.ShowError("Unsaved buffers from a previous crash are in " + found + " (DEADCAT) — review and delete it")
	}
}

// installDeadcatSignals arms the catchable-death signals (deadcatSignals) to
// dump DEADCAT and exit — but only for a standalone real-terminal session. A
// host owning the process (a virtualized terminal) gets no signal handlers and
// is expected to call DumpDeadcat from its own shutdown. Returns a stop func.
func (e *Editor) installDeadcatSignals() func() {
	if !e.usingOSFS || e.Config.Terminal != nil {
		return func() {}
	}
	sigs := deadcatSignals()
	if len(sigs) == 0 {
		return func() {}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	go func() {
		if sig, ok := <-ch; ok {
			e.emergencyExit("received " + sig.String())
		}
	}()
	return func() { signal.Stop(ch) }
}

// emergencyExit is the signal path: a signal skips the normal deferred
// cleanup, so this dumps DEADCAT, restores the terminal by hand, reports where
// the dump landed, and exits.
func (e *Editor) emergencyExit(reason string) {
	name, _ := e.DumpDeadcat(reason)
	if e.KeyHandler != nil {
		e.KeyHandler.Stop()
	}
	if e.Renderer != nil {
		e.Renderer.Cleanup()
	}
	if name != "" {
		fmt.Fprintf(os.Stderr, "\nmew: aborted (%s); unsaved buffers written to %s\n", reason, name)
	} else {
		fmt.Fprintf(os.Stderr, "\nmew: aborted (%s)\n", reason)
	}
	os.Exit(1)
}

func (e *Editor) deadcatCwdPath() string {
	if e.deadcat.cwd == "" {
		return ""
	}
	return filepath.Join(e.deadcat.cwd, deadcatFilename)
}

// deadBuf is one modified buffer captured for the dump.
type deadBuf struct{ name, content string }

// modifiedBufferDump collects the content of every modified main buffer, each
// buffer once (window_clone shares a buffer across windows), including buffers
// stacked in a window's nav history — unsaved work parked behind a link
// follow must survive a crash like any other.
func (e *Editor) modifiedBufferDump() []deadBuf {
	var out []deadBuf
	for _, b := range e.openMainBuffers() {
		if !b.IsModified() {
			continue
		}
		name := b.GetFilename()
		if name == "" {
			name = "(unnamed)"
		}
		out = append(out, deadBuf{name: name, content: b.GetContent()})
	}
	return out
}

// formatDeadcatFull renders the JOE-style full dump: a header, then a labeled
// block per modified buffer.
func formatDeadcatFull(reason string, bufs []deadBuf) []byte {
	var b strings.Builder
	ts := time.Now().Format("Mon Jan _2 15:04:05 2006")
	fmt.Fprintf(&b, "*** These modified files were found in mew when it aborted on %s.\n", ts)
	b.WriteString("*** You can delete this file if you don't want them.\n")
	if reason != "" {
		fmt.Fprintf(&b, "*** (%s)\n", reason)
	}
	for _, d := range bufs {
		fmt.Fprintf(&b, "\n*** File '%s'\n", d.name)
		b.WriteString(d.content)
		if !strings.HasSuffix(d.content, "\n") {
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// formatDeadcatPointer renders the working-directory breadcrumb that points at
// the private full dump.
func formatDeadcatPointer(reason string) []byte {
	var b strings.Builder
	ts := time.Now().Format("Mon Jan _2 15:04:05 2006")
	fmt.Fprintf(&b, "*** Modified files were found in mew when it aborted on %s.\n", ts)
	b.WriteString("See DEADCAT in your mew configuration folder for details.\n")
	return []byte(b.String())
}

func osAppendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func osFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hostAppend emulates append through the host FileSystem (which only exposes
// whole-file read/write): read what's there, concatenate, write it back.
func (e *Editor) hostAppend(name string, data []byte) error {
	existing, _ := e.FS.ReadFile(name)
	return e.FS.WriteFile(name, append(existing, data...))
}

// samePath reports whether two paths resolve to the same file (best-effort;
// enough to keep the breadcrumb from clobbering the full dump when the working
// directory is the config folder).
func samePath(a, b string) bool {
	ca, err1 := filepath.Abs(a)
	cb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return filepath.Clean(ca) == filepath.Clean(cb)
}
