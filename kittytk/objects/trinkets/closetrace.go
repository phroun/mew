package trinkets

// Tracing for one question: what the display does when it has to ask a person
// whether to force a window closed, and where that question ends up.
//
// Set KITTYTK_TRACE_CLOSE=1 to get it on stderr. Off by default and free when off.
//
// It exists because this path could not be debugged from the source. It runs across the
// desktop, the window manager, the tear-off hosts and the platform, and the parts that
// matter are re-entrant: creating an OS surface drains the post queue from inside, so
// what runs next is not what the code above it reads like. Five attempts at one reported
// fault each looked right here and were wrong on the machine it was reported from.
//
// So this reports FACTS rather than intentions -- who the primary host is before and
// after, whether the dialog actually got a surface, and who called the thing that
// changed either -- and `closeTraceFrom` names the caller, because "something promoted
// another application onto the primary surface" is only answerable by knowing what
// asked for it.

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/phroun/kittytk/objects/window"
)

var closeTraceOn atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITTYTK_TRACE_CLOSE"))) {
	case "1", "true", "yes", "on":
		closeTraceOn.Store(true)
	}
}

// closeTracing reports whether the trace is on, so a caller can skip gathering
// anything that costs more than the call itself.
func closeTracing() bool { return closeTraceOn.Load() }

func closeTrace(format string, args ...any) {
	if closeTraceOn.Load() {
		fmt.Fprintf(os.Stderr, "kittytk/close: "+format+"\n", args...)
	}
}

// closeTraceFrom is the same with the callers named, for the events where WHO asked is
// the whole question: a window being dropped, an application being promoted.
func closeTraceFrom(format string, args ...any) {
	if !closeTraceOn.Load() {
		return
	}
	fmt.Fprintf(os.Stderr, "kittytk/close: "+format+"\n\tfrom %s\n", append(args, closeTraceCallers())...)
}

// closeTraceCallers is the interesting part of the stack: this package and its
// neighbours, skipping the trace machinery itself.
func closeTraceCallers() string {
	pcs := make([]uintptr, 12)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		f, more := frames.Next()
		if f.Function != "" {
			name := f.Function
			if i := strings.LastIndex(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			out = append(out, fmt.Sprintf("%s:%d", name, f.Line))
		}
		if !more || len(out) >= 8 {
			break
		}
	}
	return strings.Join(out, " <- ")
}

// closeTraceWindow names a window the way a person reading the log can match it to
// something on their screen.
func closeTraceWindow(win *window.Window) string {
	if win == nil {
		return "<none>"
	}
	return fmt.Sprintf("%q[id=%d detached=%v visible=%v type=%d]",
		win.Title(), win.ObjectID(), win.IsDetached(), win.IsVisible(), win.Type())
}

// closeTraceDesktop is the state that decides where a question can go: whether the
// desktop is on the screen at all, which window owns the primary surface, and what else
// has a surface of its own.
func (d *Desktop) closeTraceDesktop(what string) {
	if !closeTracing() {
		return
	}
	d.mu.RLock()
	solo, env := d.solo, d.desktopEnvironment
	primary := d.soloPrimaryHost
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	wm := d.windowManager
	d.mu.RUnlock()

	var primaryWin *window.Window
	if primary != nil {
		primaryWin = primary.Window()
	}
	var torn []string
	for _, h := range hosts {
		torn = append(torn, closeTraceWindow(h.Window()))
	}
	var docked []string
	if wm != nil {
		for _, w := range wm.Windows() {
			docked = append(docked, closeTraceWindow(w))
		}
	}
	closeTrace("%s: solo=%v desktopEnvironment=%v primaryHost=%s\n\ttorn:   %s\n\tdocked: %s",
		what, solo, env, closeTraceWindow(primaryWin),
		strings.Join(torn, ", "), strings.Join(docked, ", "))
}
