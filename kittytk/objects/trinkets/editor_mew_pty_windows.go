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
// AND THE CREATOR OF A PSEUDOCONSOLE MUST OWN A CONSOLE ITSELF. This is the
// one that cost days. A GUI-subsystem binary has no console, and from one
// everything above succeeds and means nothing: CreatePseudoConsole returns a
// handle, the attribute takes, CreateProcess returns a pid, conhost renders
// and sets the window title from the child's own name — and the child is
// handed nothing it can read or write, so it exits 0 without a word. Every
// visible signal says the terminal was made correctly.
//
// It was found by elimination and then by accident: the same code in the
// console-subsystem build of the same editor worked perfectly. Not a
// difference in the call — a difference in the PROGRAM. See ensureConsole.
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

// The console-ownership calls behind method 10: this process is a GUI binary
// with no console of its own, which is the one way it differs from every
// working ConPTY example there is.
var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procAllocConsole     = kernel32.NewProc("AllocConsole")
	procFreeConsole      = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
)

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

// conOpts varies the parts of the spawn that are candidates for the fault, so
// the self-test can differ ONE of them at a time. Zero value is what exec
// does, and what Microsoft's own sample does.
type conOpts struct {
	// inherit passes bInheritHandles=TRUE. Microsoft's sample passes FALSE,
	// but the sample's parent is a console application and this one is a GUI
	// process with no console of its own — the one variable a probe cannot
	// rule out by reasoning.
	inherit bool
	// keepEnds holds the console's own ends of both pipes open until AFTER
	// the child exists, instead of closing them the moment
	// CreatePseudoConsole returns. The documentation says they may be closed
	// at once because the console duplicated them; several working
	// implementations keep them anyway.
	keepEnds bool
	// noAppName passes lpApplicationName=NULL and lets CreateProcess resolve
	// the program from the command line, which is the more common spelling
	// and occasionally the one that behaves.
	noAppName bool
	// plainPipes skips the pseudoconsole entirely and gives the child
	// ordinary inherited pipes as its standard handles. Not a terminal — no
	// VT translation, no console API, no resize — but on a machine where the
	// pseudoconsole does not work it is a real, usable shell, which is worth
	// more than a correct one that says nothing.
	plainPipes bool
	// noWindow adds CREATE_NO_WINDOW. A GUI process creating a console child
	// normally gets a console allocated for it; the pseudoconsole attribute
	// is supposed to pre-empt that, and this asks whether it does.
	noWindow bool
	// nullStd sets STARTF_USESTDHANDLES with all three handles NULL, saying
	// explicitly "the parent offers none" rather than leaving the child to
	// inherit whatever a GUI process's std handles are — which is nothing
	// useful, and may be exactly what it is receiving.
	nullStd bool
	// inheritCursor passes PSEUDOCONSOLE_INHERIT_CURSOR to CreatePseudoConsole.
	inheritCursor bool
	// noConsole SKIPS ensuring this process owns a console — the behaviour
	// that failed, kept selectable so the difference can be demonstrated
	// rather than taken on trust.
	noConsole bool
	// clearStd points this process's own standard handles at nothing for the
	// duration of CreateProcess, and puts them back after. THIS IS THE FIX,
	// and it is on by default; the flag exists so its absence can be shown.
	//
	// A child created with no STARTF_USESTDHANDLES takes its standard handles
	// from its parent's. mew launched from Explorer has none, so the console
	// subsystem gives the child the pseudoconsole's and everything works. mew
	// launched by ANOTHER mew — which is exactly what Install does — was
	// started by Go's exec, and Go opens NUL for every std handle a Cmd does
	// not set and passes them with STARTF_USESTDHANDLES. Those handles are
	// therefore REDIRECTED, AllocConsole documents that it fills in the
	// console's handles only when they are not, so they stay NUL, and the
	// child inherits NUL: stdin at end of file, exit 0, not one byte.
	//
	// Offering the child nothing leaves the console subsystem to supply the
	// pseudoconsole's own handles, which is what it does when there is nothing
	// to inherit. It costs nothing in the case that already worked.
	clearStd bool
	// freeAfter gives up this process's console again once the pseudoconsole
	// exists, before the child is created — in case the console is needed to
	// MAKE one and unhelpful when spawning into it.
	freeAfter bool
}

// ensureConsole gives THIS PROCESS a console if it has none, hidden, once.
//
// This is the fault, and it took a console-subsystem build of the same editor
// to show it: mew.exe runs a pseudoconsole perfectly and mew-sdl.exe does not,
// with identical code. A GUI binary has no console, and a pseudoconsole turns
// out to need its creator to have one — the child attaches, conhost renders,
// and the child is handed nothing it can read or write, so it exits 0 without
// a word. Every other candidate was ruled out one at a time; this was the
// difference nobody could see because it is a property of the PROGRAM rather
// than of the call.
//
// The window is hidden the instant it exists. Doing this lazily, at the first
// terminal rather than at startup, means an editor that never runs one never
// acquires a console at all.
var ensureConsole = sync.OnceValue(func() string {
	if h, _, _ := procGetConsoleWindow.Call(); h != 0 {
		return "this process already owns a console (nothing to do)"
	}
	// Worth recording, because it decides whether AllocConsole will hand this
	// process the console's handles or leave what it was given: it fills them
	// in ONLY IF they were not already redirected.
	redirected := ""
	if h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE); err == nil && h != 0 {
		redirected = " (std handles were already redirected at startup — " +
			"AllocConsole will NOT replace them)"
	}
	if r, _, e := procAllocConsole.Call(); r == 0 {
		return fmt.Sprintf("AllocConsole failed: %v — a pseudoconsole may not work", e)
	}
	h, _, _ := procGetConsoleWindow.Call()
	if h != 0 {
		procShowWindow.Call(h, 0 /* SW_HIDE */)
		return "AllocConsole: ok, window hidden" + redirected
	}
	return "AllocConsole: ok, but no window to hide" + redirected
})

// ptyMethods are the ways this host knows to make a terminal, selectable per
// request (exec "cmd.exe" "3") and all of them run by the self-test.
//
// They exist because the fault is on a machine that is not this one, and
// because the round trip to test a guess is measured in half-hours. One
// build that can try every way beats six builds that each try one.
var ptyMethods = []struct {
	Name string
	Desc string
	Opt  conOpts
}{
	{"1", "ConPTY — the real terminal (own console, std handles cleared across the spawn)",
		conOpts{clearStd: true}},
	{"2", "ConPTY, bInheritHandles=TRUE", conOpts{inherit: true}},
	{"3", "ConPTY, console pipe ends kept open past CreateProcess", conOpts{keepEnds: true}},
	{"4", "ConPTY, inherit=TRUE and ends kept", conOpts{inherit: true, keepEnds: true}},
	{"5", "ConPTY, lpApplicationName=NULL (command line only)", conOpts{noAppName: true}},
	{"6", "no pseudoconsole: plain inherited pipes — a shell, but not a terminal", conOpts{plainPipes: true}},
	{"7", "ConPTY + CREATE_NO_WINDOW", conOpts{noWindow: true}},
	{"8", "ConPTY + STARTF_USESTDHANDLES, all three NULL", conOpts{nullStd: true}},
	{"9", "ConPTY + PSEUDOCONSOLE_INHERIT_CURSOR", conOpts{inheritCursor: true}},
	{"10", "ConPTY WITHOUT ensuring a console — what used to fail, kept to show the difference", conOpts{noConsole: true}},
	{"11", "ConPTY, everything at once (inherit, ends held, no window)",
		conOpts{inherit: true, keepEnds: true, noWindow: true}},
	{"12", "ConPTY WITHOUT clearing std handles — what failed from an installed launch", conOpts{}},
	{"13", "ConPTY + give up this process's console once the pseudoconsole exists", conOpts{freeAfter: true}},
	{"14", "ConPTY + cleared std handles AND no window", conOpts{clearStd: true, noWindow: true}},
}

// defaultMethod is what a request that names none gets: the real terminal,
// now that ensureConsole has removed the reason it did not work.
//
// MEW_PTY_METHOD overrides it, so any method can be chosen for a whole
// session without a rebuild by someone who has no Go toolchain to hand —
// including method 6, the plain-pipe shell, if a machine turns up where the
// pseudoconsole still will not go.
func defaultMethod() string {
	if m := strings.TrimSpace(os.Getenv("MEW_PTY_METHOD")); m != "" {
		return m
	}
	// The real terminal. It works with a console allocated — a full shell,
	// resize, the console API — and was seen to fail once, on one launch, in a
	// way that has not recurred. One unexplained event is not worth giving up
	// resize and full-screen programs for; method 6 is a command away if it
	// turns out to be more than that.
	return "1"
}

// optsForMethod resolves a request's method name. An unknown one falls back
// to the default rather than refusing: a mistyped method should still get a
// terminal.
func optsForMethod(name string) (conOpts, string) {
	if name == "" {
		name = defaultMethod()
	}
	for _, m := range ptyMethods {
		if m.Name == name {
			return m.Opt, m.Desc
		}
	}
	return conOpts{}, ptyMethods[0].Desc
}

// settleWait is how long a new pseudoconsole is watched before it is handed
// over. Long enough to catch a child that dies on the spot; short enough that
// opening a terminal still feels immediate.
const settleWait = 400 * time.Millisecond

// conAttempts is how many times a pseudoconsole is tried before falling back.
const conAttempts = 3

func hostPTY(path, dir string, env []string, cols, rows int, method string) (mew.PTYSession, error) {
	opt, desc := optsForMethod(method)
	if opt.plainPipes {
		p, err := newPipeChild(path, nil, dir, env)
		if err != nil {
			return nil, err
		}
		return p, nil
	}

	// The pseudoconsole is INTERMITTENT here: the same binary, the same
	// command, the same machine, and one launch in several produces a child
	// that attaches and exits 0 on the spot while the next works perfectly.
	// The race behind that is not understood yet.
	//
	// It does not have to be understood to be survived. A child that is still
	// alive a moment later is a child that started properly, and one that is
	// not costs nothing to throw away and ask for again — nobody has seen it,
	// because the session has not been handed over. Three tries, and then the
	// plain-pipe shell, which always works and says so.
	//
	// This is a workaround and is labelled one. If the intermittency is ever
	// explained, the retry goes and this comment goes with it.
	var last error
	for attempt := 1; attempt <= conAttempts; attempt++ {
		p, err := newConPTY(path, nil, dir, env, cols, rows, opt)
		if err != nil {
			last = err
			if p != nil {
				_ = p.Close()
			}
			continue
		}
		p.note("method: %s", desc)
		if !p.diedWithin(settleWait) {
			if attempt > 1 {
				p.note("started on attempt %d", attempt)
			}
			return p, nil
		}
		p.note("child exited immediately on attempt %d — discarding and retrying", attempt)
		_ = p.Close()
	}

	// Every attempt died on the spot. A limited shell that works beats a
	// correct one that does not, and the child says which it is.
	pc, err := newPipeChild(path, nil, dir, env)
	if err != nil {
		if last != nil {
			return nil, last
		}
		return nil, err
	}
	pc.note("fell back after %d pseudoconsole attempts all exited immediately", conAttempts)
	pc.emit([]byte("\r\n[mew: the pseudoconsole would not start after " +
		fmt.Sprint(conAttempts) + " tries; this is a plain-pipe shell —\r\n" +
		" no resize, no console API, no full-screen programs]\r\n\r\n"))
	return pc, nil
}

// diedWithin reports whether the child is already gone. False means it is
// still running, which is all a shell has to do to have started properly.
func (p *conPTY) diedWithin(d time.Duration) bool {
	select {
	case <-p.reaped:
		return true
	case <-time.After(d):
		return false
	}
}

// newConPTY is hostPTY with the argument list exposed, so the self-test can
// drive the SAME code with a child that says something and exits. A probe
// through a second implementation would only prove things about itself.
func newConPTY(path string, args []string, dir string, env []string, cols, rows int, opt conOpts) (*conPTY, error) {
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

	if opt.noConsole {
		p.note("console for this process: NOT ensured (the configuration that failed)")
	} else {
		p.note("console for this process: %s", ensureConsole())
	}

	var pcFlags uint32
	if opt.inheritCursor {
		pcFlags |= windows.PSEUDOCONSOLE_INHERIT_CURSOR
	}
	var hpc windows.Handle
	err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, pcFlags, &hpc)
	// The console duplicated what it needs, so our copies of ITS ends can go
	// now — holding them keeps both pipes alive past the child's exit, and the
	// reader then waits for an EOF that has no writer left to come from. Some
	// implementations hold them anyway, so keepEnds defers this until after
	// the child exists and then closes them regardless.
	if !opt.keepEnds {
		windows.CloseHandle(inRead)
		windows.CloseHandle(outWrite)
		p.note("console pipe ends: closed immediately after CreatePseudoConsole")
	} else {
		p.note("console pipe ends: HELD until after CreateProcess")
	}
	if err != nil {
		if opt.keepEnds {
			windows.CloseHandle(inRead)
			windows.CloseHandle(outWrite)
		}
		p.note("CreatePseudoConsole: %v", err)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return p, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	p.note("CreatePseudoConsole: ok, HPCON 0x%x", uintptr(hpc))

	if opt.freeAfter {
		if r, _, e := procFreeConsole.Call(); r == 0 {
			p.note("FreeConsole: %v", e)
		} else {
			p.note("FreeConsole: ok (console given up before spawning)")
		}
	}

	p.hpc, p.in, p.out = hpc, inWrite, outRead
	spawnErr := p.spawn(path, args, dir, env, opt)
	if opt.keepEnds {
		windows.CloseHandle(inRead)
		windows.CloseHandle(outWrite)
	}
	if spawnErr != nil {
		_ = p.Close()
		return p, spawnErr
	}
	return p, nil
}

// spawn starts the child bound to the pseudoconsole.
func (p *conPTY) spawn(path string, args []string, dir string, env []string, opt conOpts) error {
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
	if opt.nullStd {
		si.StartupInfo.Flags |= windows.STARTF_USESTDHANDLES
		si.StartupInfo.StdInput, si.StartupInfo.StdOutput, si.StartupInfo.StdErr = 0, 0, 0
		p.note("STARTF_USESTDHANDLES: set, all three NULL")
	}

	appName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if opt.noAppName {
		appName = nil
		p.note("lpApplicationName: NULL (resolved from the command line)")
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
	// Offer the child nothing of ours to inherit, so the console subsystem
	// gives it the pseudoconsole's own handles rather than this process's.
	if opt.clearStd {
		restore := clearStdHandles()
		defer restore()
		p.note("std handles: cleared for the duration of CreateProcess")
	}

	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if opt.noWindow {
		flags |= windows.CREATE_NO_WINDOW
	}
	p.note("bInheritHandles: %v, creation flags: 0x%x", opt.inherit, flags)
	var pi windows.ProcessInformation
	err = windows.CreateProcess(appName, cmdLine, nil, nil, opt.inherit, flags,
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

// hostPTYProbe runs the Windows self-test as a DIFFERENTIAL: the same child,
// the same code, EVERY method, so one report names the one that works instead
// of leaving a rebuild-and-carry-it-over per guess.
//
// What is already known when this runs: the pseudoconsole is created, the
// child attaches to it (conhost sets the window title from the child's own
// process name), conhost renders — and the child produces nothing and exits 0.
// A cmd.exe that prints no banner and exits 0 at once is a cmd.exe whose stdin
// is at end of file, which is to say not a console. So what these divide up is
// which part of how the child is started decides whether it is given the
// console's handles.
//
// Look for MEW-CONPTY-OK, or for a Microsoft copyright banner. Any method that
// shows one is the answer, and exec can be told to use it by name.
func hostPTYProbe() []probeResult {
	dir, _ := os.UserHomeDir()
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	shell, err := exec.LookPath("cmd.exe")
	if err != nil {
		return []probeResult{{Label: "cmd.exe", Err: err}}
	}

	var out []probeResult
	start := func(path string, args []string, opt conOpts) (probeSession, error) {
		if opt.plainPipes {
			return newPipeChild(path, args, dir, env)
		}
		return newConPTY(path, args, dir, env, 80, 24, opt)
	}

	// Every method, against the case that actually fails: an interactive
	// cmd.exe, which should answer with a banner and a prompt.
	for _, m := range ptyMethods {
		sess, err := start(shell, nil, m.Opt)
		out = append(out, runProbe(
			fmt.Sprintf("METHOD %s — cmd.exe interactive — %s", m.Name, m.Desc), sess, err))
	}

	// And a child that speaks and exits, down the default path: if the banner
	// never appears but this text does, the console works and cmd.exe's own
	// reading of stdin is what does not.
	sess, err := start(shell, []string{"/c", "echo MEW-CONPTY-OK"}, conOpts{})
	out = append(out, runProbe("cmd.exe /c echo — a child that speaks and exits", sess, err))

	// A DIFFERENT child down the identical path. If PowerShell speaks where
	// cmd.exe does not, the console is fine and the fault is cmd.exe's alone.
	if ps, err := exec.LookPath("powershell.exe"); err == nil {
		sess, err := start(ps, nil, conOpts{})
		out = append(out, runProbe("powershell.exe interactive — a different child, default method", sess, err))
	}
	return out
}

// pipeChild runs a process with ordinary inherited pipes for its standard
// handles and no console of any kind.
//
// It began as the control case and became the working configuration on a
// machine whose pseudoconsole a child cannot use. That promotion brings two
// obligations a pseudoconsole would have discharged for us, and without them
// the shell appears and cannot be typed at:
//
// A CONSOLE ECHOES. Nothing else does. A child reading a pipe never sees the
// keystrokes as keystrokes and has nothing to echo back, so what is typed
// must be put on screen here or the person types blind.
//
// AND A CONSOLE HAS A LINE DISCIPLINE. Enter at a terminal is a carriage
// return, and the terminal turns it into the line ending the reader wants. A
// pipe delivers exactly the bytes written, so a bare CR arrives as no line at
// all: the shell waits forever for an end-of-line that was already sent.
type pipeChild struct {
	mu     sync.Mutex
	out    windows.Handle
	in     windows.Handle
	proc   windows.Handle
	trace  []string
	code   int
	ended  bool
	closed bool
	reaped chan struct{}

	// stream carries what the reader saw AND what was echoed, in the order it
	// happened, so a typed line appears where it was typed rather than after
	// whatever the child says next. rest holds a chunk part-way delivered.
	stream chan []byte
	rest   []byte
}

func newPipeChild(path string, args []string, dir string, env []string) (*pipeChild, error) {
	p := &pipeChild{reaped: make(chan struct{}), stream: make(chan []byte, 64)}
	p.note("no CreatePseudoConsole: plain pipes as std handles")
	p.note("local echo ON and CR translated to CRLF (a pipe has neither)")

	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, sa, 0); err != nil {
		p.note("CreatePipe(in): %v", err)
		return p, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, sa, 0); err != nil {
		p.note("CreatePipe(out): %v", err)
		return p, err
	}
	// Only the child's ends may be inherited; ours must not leak into it.
	windows.SetHandleInformation(inWrite, windows.HANDLE_FLAG_INHERIT, 0)
	windows.SetHandleInformation(outRead, windows.HANDLE_FLAG_INHERIT, 0)

	si := &windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput, si.StdOutput, si.StdErr = inRead, outWrite, outWrite

	appName, _ := windows.UTF16PtrFromString(path)
	line := windows.ComposeCommandLine(append([]string{path}, args...))
	cmdLine, _ := windows.UTF16PtrFromString(line)
	var dirPtr *uint16
	if dir != "" {
		dirPtr, _ = windows.UTF16PtrFromString(dir)
	}
	var pi windows.ProcessInformation
	err := windows.CreateProcess(appName, cmdLine, nil, nil, true,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW,
		envBlock(env), dirPtr, si, &pi)
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		p.note("CreateProcess: %v", err)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return p, err
	}
	p.note("CreateProcess: ok, pid %d (STARTF_USESTDHANDLES, inherit=TRUE, CREATE_NO_WINDOW)", pi.ProcessId)
	windows.CloseHandle(pi.Thread)
	p.in, p.out, p.proc = inWrite, outRead, pi.Process

	// One goroutine owns the pipe read, so Read can take from the same queue
	// the echo goes into and the two stay in order.
	go func() {
		buf := make([]byte, 4096)
		for {
			var n uint32
			if err := windows.ReadFile(outRead, buf, &n, nil); err != nil || n == 0 {
				break
			}
			p.emit(append([]byte(nil), buf[:n]...))
		}
		close(p.stream)
	}()

	go func() {
		defer close(p.reaped)
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		windows.GetExitCodeProcess(pi.Process, &code)
		p.mu.Lock()
		p.ended, p.code = true, int(code)
		p.mu.Unlock()
		p.Close()
	}()
	return p, nil
}

// emit queues bytes for the reader, dropping them rather than blocking: a
// display that has stopped consuming must not be able to wedge the child.
func (p *pipeChild) emit(b []byte) {
	defer func() { recover() }() // the stream may have closed under us
	select {
	case p.stream <- b:
	default:
	}
}

func (p *pipeChild) note(format string, a ...any) {
	p.trace = append(p.trace, fmt.Sprintf(format, a...))
}
func (p *pipeChild) Diagnostics() []string { return p.trace }

func (p *pipeChild) Read(b []byte) (int, error) {
	if len(p.rest) == 0 {
		chunk, ok := <-p.stream
		if !ok {
			return 0, io.EOF
		}
		p.rest = chunk
	}
	n := copy(b, p.rest)
	p.rest = p.rest[n:]
	return n, nil
}

// Write sends to the child's stdin, doing the two things the absent console
// would have done: turning Enter into a line ending the reader will accept,
// and putting what was typed on screen.
func (p *pipeChild) Write(b []byte) (int, error) {
	p.mu.Lock()
	closed, h := p.closed, p.in
	p.mu.Unlock()
	if closed || h == 0 {
		return 0, io.ErrClosedPipe
	}

	// CR -> CRLF, and a CR already followed by LF left alone.
	var outb []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' {
			outb = append(outb, '\r', '\n')
			if i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
			continue
		}
		outb = append(outb, b[i])
	}

	var n uint32
	if err := windows.WriteFile(h, outb, &n, nil); err != nil {
		return 0, err
	}
	p.emit(append([]byte(nil), outb...)) // the echo a console would have done
	return len(b), nil
}

// Resize is a courtesy: a plain pipe has no size to set, and saying so is
// more honest than pretending. It is also the clearest thing this
// configuration gives up.
func (p *pipeChild) Resize(cols, rows int) error { return nil }

func (p *pipeChild) ExitStatus() (int, bool) {
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

func (p *pipeChild) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	in, out, proc, ended := p.in, p.out, p.proc, p.ended
	p.in, p.out, p.proc = 0, 0, 0
	p.mu.Unlock()
	if proc != 0 {
		if !ended {
			windows.TerminateProcess(proc, 0)
		}
		windows.CloseHandle(proc)
	}
	if out != 0 {
		windows.CancelIoEx(out, nil)
		windows.CloseHandle(out)
	}
	if in != 0 {
		windows.CloseHandle(in)
	}
	return nil
}

// hostPTYDefaultNote tells the report which way exec will make a terminal
// when nothing asks for a particular one, and how to change it. A diagnostic
// that does not say what the default IS leaves its reader guessing which of
// its own probes matters.
func hostPTYDefaultNote() string {
	_, desc := optsForMethod("")
	env := ""
	if v := strings.TrimSpace(os.Getenv("MEW_PTY_METHOD")); v != "" {
		env = " (from MEW_PTY_METHOD=" + v + ")"
	}
	return "exec default: method " + defaultMethod() + env + " — " + desc +
		"\n  choose another for one session with MEW_PTY_METHOD, or per command:" +
		" exec \"cmd.exe\", \"1\""
}

// clearStdHandles points this process's three standard handles at nothing and
// returns the call that puts them back. A child created in between inherits
// no handles from us, which is the difference between it talking to our
// console and it talking to its own.
func clearStdHandles() func() {
	ids := []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE}
	saved := make([]windows.Handle, len(ids))
	for i, id := range ids {
		saved[i], _ = windows.GetStdHandle(id)
		windows.SetStdHandle(id, 0)
	}
	return func() {
		for i, id := range ids {
			windows.SetStdHandle(id, saved[i])
		}
	}
}
