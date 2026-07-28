//go:build mew && windows

package trinkets

// A real ConPTY for mew's exec sessions.
//
// Windows has no forkpty. A pseudoconsole is not a file descriptor a child
// inherits: it is a handle that must be BOUND to the child before it starts,
// through a process-thread attribute list, and a child that is not bound to
// one gets an ordinary console of its own instead — which on a desktop means
// a console WINDOW, appearing next to the editor and vanishing again when the
// process dies.
//
// The pipes are RAW Win32 handles, deliberately, and not os.Pipe. Go does not
// hand out an inert handle: os.Pipe registers what it opens with the runtime's
// I/O completion port and calls SetFileCompletionNotificationModes on it. That
// is invisible and harmless while Go owns both ends — but these two ends are
// given away to conhost, which does its own synchronous I/O on them, and a
// handle whose completions have been re-pointed at another process's poller is
// not a handle it can use. Nothing fails loudly: the console simply never
// produces output. Every byte here goes through ReadFile/WriteFile for the
// same reason.
//
// The shape mirrors the POSIX side exactly (Read, Write, Resize, Close), so
// mew cannot tell which one it is holding — the same reason the PTYSession
// interface exists at all.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/phroun/mew"
	"golang.org/x/sys/windows"
)

// procUpdateProcThreadAttribute is called directly rather than through
// x/sys's Update wrapper. The PSEUDOCONSOLE attribute's value IS the handle,
// not a pointer to one (it is the exception in UpdateProcThreadAttribute's
// attribute table), and the wrapper's parameter is an unsafe.Pointer that it
// also retains for the GC to scan. Passing a kernel handle through that slot
// would mean fabricating a pointer out of a number and then parking it
// somewhere the collector looks; passing it as a plain uintptr here does not.
var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("UpdateProcThreadAttribute")

// conPTY is one child bound to one pseudoconsole.
type conPTY struct {
	mu    sync.Mutex
	hpc   windows.Handle
	proc  windows.Handle
	in    windows.Handle // we write; the console reads
	out   windows.Handle // the console writes; we read
	name  string
	ended bool
	code  int
	// trace is the account of how this session was built, for pty_diag: each
	// platform call and what it said. Kept because a ConPTY that fails
	// silently is otherwise unobservable from mew's side of the pipe.
	trace  []string
	closed bool
	// reaped closes once the watcher has the exit status, so ExitStatus can
	// wait the moment out rather than answering before it knows.
	reaped chan struct{}
}

func (p *conPTY) note(format string, a ...any) {
	p.trace = append(p.trace, fmt.Sprintf(format, a...))
}

// Diagnostics is the account of how this session was built, for pty_diag.
func (p *conPTY) Diagnostics() []string { return p.trace }

// ExitStatus implements mew.PTYExitStatus: it tells a child that ran and
// exited from a stream that ended under one still running. Those two look
// identical from mew's side of the pipe and want opposite debugging.
//
// It allows the watcher a moment first, for the same reason the POSIX side
// does: this is asked at exactly the instant the two race. The stream can end
// before WaitForSingleObject has returned, and an immediate read then reports
// a child that has just died as still running.
func (p *conPTY) ExitStatus() (int, bool) {
	if p.reaped != nil {
		select {
		case <-p.reaped:
		case <-time.After(reapGrace):
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, p.ended
}

// reapGrace is how long ExitStatus waits for the watcher to land: long enough
// for a process that has already died, short enough that a session ending is
// not a pause anyone notices.
const reapGrace = 200 * time.Millisecond

func hostPTY(path, dir string, env []string, cols, rows int) (mew.PTYSession, error) {
	p, err := newConPTY(path, nil, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// newConPTY is hostPTY with the argument list exposed, so the self-test can
// drive the SAME code with a child that says something and exits. A probe
// through a second implementation would only prove things about itself.
func newConPTY(path string, args []string, dir string, env []string, cols, rows int) (*conPTY, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	// Two pipes, four ends. The console gets one end of each; we keep the
	// others. Nil security attributes, so nothing here is inheritable: the
	// child reaches its console through the attribute list, not through an
	// inherited handle.
	p := &conPTY{name: filepath.Base(path), reaped: make(chan struct{})}
	p.note("CreateProcess target: %s %v (dir %q, %d env entries, %dx%d)",
		path, args, dir, len(env), cols, rows)

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		p.note("CreatePipe(in): %v", err)
		return p, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		p.note("CreatePipe(out): %v", err)
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return p, err
	}
	p.note("CreatePipe: ok (raw handles, not os.Pipe)")

	var hpc windows.Handle
	err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &hpc)
	// The console duplicated what it needs, so our copies of ITS ends go now.
	// Holding them would keep both pipes alive past the child's exit, and the
	// reader would wait forever for an EOF that no longer has a writer to
	// come from.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		p.note("CreatePseudoConsole: %v", err)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return p, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	p.note("CreatePseudoConsole: ok, HPCON 0x%x", uintptr(hpc))

	p.hpc, p.in, p.out = hpc, inWrite, outRead
	if err := p.spawn(path, args, dir, env); err != nil {
		_ = p.Close()
		return p, err
	}
	return p, nil
}

// spawn starts the child bound to the pseudoconsole.
func (p *conPTY) spawn(path string, args []string, dir string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		p.note("NewProcThreadAttributeList: %v", err)
		return fmt.Errorf("NewProcThreadAttributeList: %w", err)
	}
	defer attrs.Delete()

	r, _, e := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrs.List())),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(p.hpc),
		unsafe.Sizeof(p.hpc),
		0,
		0,
	)
	if r == 0 {
		p.note("UpdateProcThreadAttribute(PSEUDOCONSOLE): %v", e)
		return fmt.Errorf("UpdateProcThreadAttribute: %w", e)
	}
	p.note("UpdateProcThreadAttribute(PSEUDOCONSOLE): ok")

	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrs.List(),
	}

	appName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	line := windows.ComposeCommandLine(append([]string{path}, args...))
	cmdLine, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return err
	}
	p.note("command line: %s", line)
	var dirPtr *uint16
	if dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}

	// Handle inheritance stays OFF, matching the pipes' nil security
	// attributes: the child reaches its console through the attribute, and
	// inheriting anything else would only give it references it has no
	// business holding.
	var pi windows.ProcessInformation
	err = windows.CreateProcess(appName, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		envBlock(env), dirPtr, &si.StartupInfo, &pi)
	if err != nil {
		p.note("CreateProcess: %v", err)
		return fmt.Errorf("%s: %w", path, err)
	}
	p.note("CreateProcess: ok, pid %d", pi.ProcessId)
	windows.CloseHandle(pi.Thread)

	p.mu.Lock()
	p.proc = pi.Process
	p.mu.Unlock()

	// The reader only learns the child is gone when the console lets go of
	// the pipe, and the console only does that when it is closed. So wait for
	// the process here, leave a word about how it went, and tear the session
	// down — which ends mew's read loop and closes the buffer's surface.
	go func() {
		defer close(p.reaped)
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		windows.GetExitCodeProcess(pi.Process, &code)
		p.mu.Lock()
		p.ended, p.code = true, int(code)
		p.mu.Unlock()
		_ = p.Close()
	}()
	return nil
}

// envBlock renders an environment as the double-NUL-terminated UTF-16 block
// CreateProcess wants. nil means "inherit this process's environment", which
// is what an empty list should mean too.
//
// The entries are SORTED: Windows documents an environment block as sorted
// case-insensitively by name, and cmd.exe is one of the programs that reads
// the block back rather than taking the API's word for it. Go's own
// environment slice is in no particular order.
func envBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	sorted := append([]string(nil), env...)
	// Sorting whole strings uppercased orders by name: a name is a prefix
	// ending in "=", and "=" sorts below every character a name may contain,
	// so FOO=1 precedes FOOBAR=2 the way the rule intends. Windows' own
	// hidden per-drive entries (=C:=C:\...) sort to the front, where they
	// belong.
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToUpper(sorted[i]) < strings.ToUpper(sorted[j])
	})
	var b []uint16
	for _, e := range sorted {
		u, err := windows.UTF16FromString(e)
		if err != nil {
			continue // an entry with an interior NUL is not one
		}
		b = append(b, u...) // UTF16FromString already terminates each entry
	}
	if len(b) == 0 {
		return nil
	}
	b = append(b, 0) // and the block itself ends with a second NUL
	return &b[0]
}

// Read delivers the console's output.
func (p *conPTY) Read(b []byte) (int, error) {
	p.mu.Lock()
	closed, h := p.closed, p.out
	p.mu.Unlock()
	if closed {
		return 0, io.EOF
	}

	var n uint32
	err := windows.ReadFile(h, b, &n, nil)
	if err != nil {
		if err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF
		}
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func (p *conPTY) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.mu.Lock()
	closed, h := p.closed, p.in
	p.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	var n uint32
	if err := windows.WriteFile(h, b, &n, nil); err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (p *conPTY) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.hpc == 0 {
		return nil
	}
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Close ends the session. Called both by mew (the editor is shutting down, or
// the buffer is done with it) and by the watcher goroutine when the child
// exits on its own, so it has to survive being called twice.
func (p *conPTY) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	hpc, proc, in, out, ended := p.hpc, p.proc, p.in, p.out, p.ended
	p.hpc, p.proc = 0, 0
	p.mu.Unlock()

	if proc != 0 {
		if !ended {
			// mew asked, not the child: do not outlive the editor that asked
			// for it.
			windows.TerminateProcess(proc, 0)
		}
		windows.CloseHandle(proc)
	}
	if hpc != 0 {
		// Closing the console releases its ends of the pipes, which is what
		// finally lets a blocked ReadFile return.
		windows.ClosePseudoConsole(hpc)
	}
	// ...and if one is still sitting in the syscall, cancel it BEFORE the
	// handle goes. Closing a handle out from under a blocked read is the
	// usual way this is done and it usually works, but the handle value is
	// free for reuse the moment it closes, and a read that resumes against a
	// reused handle is reading someone else's file.
	if out != 0 {
		windows.CancelIoEx(out, nil)
	}
	if in != 0 {
		windows.CloseHandle(in)
	}
	if out != 0 {
		windows.CloseHandle(out)
	}
	return nil
}

// diagShellNames are the shells worth looking for on this platform.
func diagShellNames() []string {
	return []string{"cmd.exe", "powershell.exe", "pwsh.exe", "bash.exe", "wsl.exe"}
}

// hostPTYProbe runs the Windows self-test. Three sessions, differing one
// variable at a time, so the report says WHICH thing is wrong rather than
// that something is:
//
//   - a child that speaks and exits, with everything exec passes;
//   - the same child with no environment block and no working directory —
//     the minimal shape Microsoft's own sample uses, so a difference here
//     indicts one of those two;
//   - an interactive cmd.exe, which is the case that actually fails.
//
// All three go through newConPTY: the same code exec uses, so a probe cannot
// pass while the real path fails.
func hostPTYProbe() []probeResult {
	dir, _ := os.UserHomeDir()
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	shell, err := exec.LookPath("cmd.exe")
	if err != nil {
		return []probeResult{{Label: "cmd.exe", Err: err}}
	}

	var out []probeResult
	s1, e1 := newConPTY(shell, []string{"/c", "echo MEW-CONPTY-OK"}, dir, env, 80, 24)
	out = append(out, runProbe("cmd.exe /c echo (full: our env + home dir)", s1, e1))

	s2, e2 := newConPTY(shell, []string{"/c", "echo MEW-CONPTY-OK"}, "", nil, 80, 24)
	out = append(out, runProbe("cmd.exe /c echo (minimal: inherited env, no dir)", s2, e2))

	s3, e3 := newConPTY(shell, nil, dir, env, 80, 24)
	out = append(out, runProbe("cmd.exe interactive (what exec does)", s3, e3))
	return out
}
