//go:build kittytk && !windows

package main

// Temporary diagnostic: SIGUSR1 writes a heap profile (inuse_space) and a full
// goroutine dump to /tmp, for tracing the hosted-terminal resize runaway. mew
// does not otherwise use SIGUSR1. Remove once the runaway is fixed.

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"
)

func installDebugDump() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			ts := time.Now().Format("150405")
			heapPath := "/tmp/mew-heap-" + ts + ".pprof"
			if f, err := os.Create(heapPath); err == nil {
				runtime.GC() // inuse_space reflects live memory after a GC
				_ = pprof.WriteHeapProfile(f)
				_ = f.Close()
			}
			grPath := "/tmp/mew-goroutines-" + ts + ".txt"
			if f, err := os.Create(grPath); err == nil {
				buf := make([]byte, 1<<24)
				n := runtime.Stack(buf, true)
				_, _ = f.Write(buf[:n])
				_ = f.Close()
			}
			fmt.Fprintf(os.Stderr, "debugdump: wrote %s and %s\n", heapPath, grPath)
		}
	}()
}
