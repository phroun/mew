//go:build mew && windows

package trinkets

// A real ConPTY for mew's exec sessions.
//
// Windows has no forkpty. A pseudoconsole is not a file descriptor a child
// inherits: it is a handle that must be BOUND to the child before it starts,
// through a process-thread attribute list, and a child that is not bound to
// one gets an ordinary console of its own instead — which on a desktop means
// a console WINDOW, appearing next to the editor and vanishing again when the
// process dies. That flash is the whole symptom of getting this wrong, and it
// is why this file exists rather than a call into a cross-platform helper: the
// pseudoconsole is created, and then it has to be handed over, and the handing
// over is the part with no POSIX analogue.
//
// The shape mirrors the POSIX side exactly (Read, Write, Resize, Close), so
// mew cannot tell which one it is holding — the same reason the PTYSession
// interface exists at all.

import (
	"fmt"
	"os"
	"sync"
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
	mu     sync.Mutex
	hpc    windows.Handle
	proc   windows.Handle
	in     *os.File // we write; the console reads
	out    *os.File // the console writes; we read
	closed bool
}

func hostPTY(path, dir string, env []string, cols, rows int) (mew.PTYSession, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	// Two pipes, four ends. The console gets one end of each; we keep the
	// others. Getting this backwards is the other classic way to end up with
	// a child that starts and immediately dies.
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, err
	}

	var hpc windows.Handle
	err = windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)},
		windows.Handle(inRead.Fd()), windows.Handle(outWrite.Fd()), 0, &hpc)
	// The console duplicated what it needs, so our copies of ITS ends go now
	// — while the child holds the only other reference, an unclosed end here
	// would keep the pipe alive after the child exits and the reader would
	// never see EOF.
	inRead.Close()
	outWrite.Close()
	if err != nil {
		inWrite.Close()
		outRead.Close()
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}

	p := &conPTY{hpc: hpc, in: inWrite, out: outRead}

	if err := p.spawn(path, dir, env); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

// spawn starts the child bound to the pseudoconsole.
func (p *conPTY) spawn(path, dir string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
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
		return fmt.Errorf("UpdateProcThreadAttribute: %w", e)
	}

	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrs.List(),
	}

	appName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{path}))
	if err != nil {
		return err
	}
	var dirPtr *uint16
	if dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}

	// Handle inheritance stays OFF: the child reaches its console through the
	// attribute, not through an inherited handle, and inheriting anything else
	// would only give it references it has no business holding.
	var pi windows.ProcessInformation
	err = windows.CreateProcess(appName, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		envBlock(env), dirPtr, &si.StartupInfo, &pi)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	windows.CloseHandle(pi.Thread)

	p.mu.Lock()
	p.proc = pi.Process
	p.mu.Unlock()

	// The reader only learns the child is gone when the console lets go of
	// the pipe, and the console only does that when it is closed. So wait for
	// the process here and tear the session down, which ends mew's read loop
	// and closes the buffer's surface.
	go func() {
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		_ = p.Close()
	}()
	return nil
}

// envBlock renders an environment as the double-NUL-terminated UTF-16 block
// CreateProcess wants. nil means "inherit this process's environment", which
// is what an empty list should mean too.
func envBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	var b []uint16
	for _, e := range env {
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

func (p *conPTY) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *conPTY) Write(b []byte) (int, error) { return p.in.Write(b) }

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
	hpc, proc, in, out := p.hpc, p.proc, p.in, p.out
	p.hpc, p.proc = 0, 0
	p.mu.Unlock()

	if proc != 0 {
		// Still running means mew asked, not the child: do not outlive the
		// editor that asked for it.
		if s, err := windows.WaitForSingleObject(proc, 0); err == nil && s == uint32(windows.WAIT_TIMEOUT) {
			windows.TerminateProcess(proc, 0)
		}
		windows.CloseHandle(proc)
	}
	if hpc != 0 {
		// Closing the console releases the output pipe, which is what finally
		// lets a blocked Read return.
		windows.ClosePseudoConsole(hpc)
	}
	if in != nil {
		in.Close()
	}
	if out != nil {
		out.Close()
	}
	return nil
}
