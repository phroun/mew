package ptydriver

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPTYReadThroughput measures the "before the wire" cost: how fast the
// client-side pty read loop drains a fast producer (with a no-op sink, so this
// is the pty + read-loop path alone, no encoding and no wire). Reports MB/s.
// Spawns a child, so it's opt-in: set KITTYTK_PERF=1 to run it.
func TestPTYReadThroughput(t *testing.T) {
	if os.Getenv("KITTYTK_PERF") == "" {
		t.Skip("set KITTYTK_PERF=1 to run the pty throughput measurement")
	}
	const total = 20 << 20 // 20 MB of output from the child
	var got int64
	start := time.Now()
	drv, err := Start("sh", func(b []byte) { atomic.AddInt64(&got, int64(len(b))) },
		"-c", fmt.Sprintf("head -c %d /dev/zero", total))
	if err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	select {
	case <-drv.Done():
	case <-time.After(30 * time.Second):
		drv.Close()
		t.Fatal("timed out draining the pty")
	}
	elapsed := time.Since(start)
	drv.Close()

	n := atomic.LoadInt64(&got)
	t.Logf("pty read: %d bytes in %v = %.1f MB/s", n, elapsed, float64(n)/1e6/elapsed.Seconds())
}

// TestPTYCoalescesUnderSlowFeed confirms the reader/sender decoupling: when the
// feed sink is slow (a stand-in for the synchronous wire round-trip), the many
// small writes the child makes are coalesced into far fewer feed batches -
// everything that piled up during a feed ships in the next one.
func TestPTYCoalescesUnderSlowFeed(t *testing.T) {
	if os.Getenv("KITTYTK_PERF") == "" {
		t.Skip("set KITTYTK_PERF=1 to run the coalescing measurement")
	}
	const lines = 300
	var mu sync.Mutex
	var calls int
	var total int64
	drv, err := Start("sh", func(b []byte) {
		mu.Lock()
		calls++
		total += int64(len(b))
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // stand in for a wire round-trip
	}, "-c", fmt.Sprintf("for i in $(seq 1 %d); do printf 'row %%d of terminal output\\n' $i; done", lines))
	if err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	select {
	case <-drv.Done():
	case <-time.After(30 * time.Second):
		drv.Close()
		t.Fatal("timed out")
	}
	drv.Close()

	mu.Lock()
	c, n := calls, total
	mu.Unlock()
	t.Logf("%d lines -> %d feed batches, %d bytes", lines, c, n)
	if n == 0 {
		t.Fatal("no data reached the feed sink")
	}
	if c >= lines/2 {
		t.Errorf("little coalescing: %d feed batches for %d lines (want far fewer)", c, lines)
	}
}
