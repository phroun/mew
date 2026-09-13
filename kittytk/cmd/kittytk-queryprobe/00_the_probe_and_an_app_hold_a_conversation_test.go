package main_test

// The probe and the example application, talking to each other.
//
// Neither is library code, but between them they are the only thing that runs
// the whole reverse direction over a real socket -- handshake, open, reply,
// results, refill from a boundary, re-sort, destroy -- and docs/hosting-a-query.md
// shows their trace as what the protocol looks like. A change that quietly
// breaks that makes the documentation wrong, so it fails here instead.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// converse builds both, runs the probe with the given arguments, runs the
// application against it, and returns what the probe printed.
func converse(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not available: %v", err)
	}
	dir := t.TempDir()
	root := filepath.Join("..", "..")
	probe := filepath.Join(dir, "probe")
	app := filepath.Join(dir, "app")
	for _, b := range []struct{ out, pkg string }{
		{probe, "./cmd/kittytk-queryprobe"},
		{app, "./examples/queryapp"},
	} {
		build := exec.Command("go", "build", "-o", b.out, b.pkg)
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b.pkg, err, out)
		}
	}

	sock := filepath.Join(dir, "probe.sock")
	pc := exec.Command(probe, append([]string{"-listen", sock, "-wait", "5s"}, args...)...)
	var trace strings.Builder
	pc.Stdout = &trace
	pc.Stderr = &trace
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Process.Kill()

	// The application dials, so the probe has to be listening first.
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ac := exec.Command(app)
	ac.Env = append(os.Environ(), "KITTYTK_DISPLAY="+sock)
	if err := ac.Start(); err != nil {
		t.Fatal(err)
	}
	defer ac.Process.Kill()

	done := make(chan struct{})
	go func() { pc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the probe never finished:\n" + trace.String())
	}
	return trace.String()
}

// inOrder reports the first line that is missing or out of order.
func inOrder(t *testing.T, trace string, want []string) {
	t.Helper()
	rest := trace
	for _, line := range want {
		i := strings.Index(rest, line)
		if i < 0 {
			t.Errorf("missing, or out of order:\n  %s\nin:\n%s", line, trace)
			return
		}
		rest = rest[i+len(line):]
	}
}

// The whole conversation, in the order it happens.
func TestTheProbeAndAnAppHoldAConversation(t *testing.T) {
	trace := converse(t, "-need", "3", "-more", "3",
		"-filter", `not { starts name "." }`, "-resort", "size desc")

	inOrder(t, trace, []string{
		// The handshake, then the open: the sequence and its first window in
		// one statement, because a display never wants one without the other.
		`<- hello version=1 app="queryapp"`,
		`-> welcome version=1`,
		`-> q=new query source="files" filter={ not { starts name "." } } sort={ name natural } have=0 need=3`,
		// The application names it, and the reply carries the name before
		// anything that uses it.
		"<- reply q=1",
		`<- result 1 fields={ key 2; name "build.sh"; size 310 }`,
		`<- result 1 complete ordered watermark={ name "README.md"; key 1 }`,
		// A second window from where the first stopped. file2 before file10 is
		// the natural collation the sort asked for.
		`-> query 1 from={ key 1; name "README.md"; size 2048 } have=0 need=3`,
		`<- result 1 fields={ key 6; name "src/file2.go"; size 1200 }`,
		`<- result 1 fields={ key 7; name "src/file10.go"; size 880 }`,
		// Restating the sort is a new generation of the same query.
		"-> set 1 sort={ size desc }",
		"-> query 1 have=0 need=3",
		`<- result 1 fields={ key 4; name "src/parser.go"; size 14022 }`,
		`<- result 1 complete ordered watermark={ size 6100; key 8 }`,
		// And letting it go, which is how the application learns it may drop
		// what it was holding.
		"-> destroy 1",
	})
}

// The least an implementation can do, over the wire: ignore the window, send
// everything, say so.
func TestTheSimplestSourceOverServesAndSaysSo(t *testing.T) {
	trace := converse(t, "-source", "colours", "-need", "2")
	inOrder(t, trace, []string{
		`-> q=new query source="colours" sort={ name natural } have=0 need=2`,
		`<- result 1 fields={ key 0; name "amber" }`,
		`<- result 1 fields={ key 4; name "vermilion" }`,
		"<- result 1 complete exhausted",
	})
	// It said nothing about order, which is what leaves the display to sort.
	if strings.Contains(trace, "complete ordered") {
		t.Error("the simplest source claimed its records were in order")
	}
	// Five records for a window of two: the display asked for a window and got
	// a superset, which is correct.
	if n := strings.Count(trace, "<- result 1 fields="); n != 5 {
		t.Errorf("it sent %d records, want all 5", n)
	}
}

// A source the application does not serve is refused, and the refusal answers
// the batch.
func TestAnUnknownSourceIsRefused(t *testing.T) {
	trace := converse(t, "-source", "ledgers")
	if !strings.Contains(trace, `<- error text="query: I serve nothing called \"ledgers\""`) {
		t.Errorf("the batch was not refused:\n%s", trace)
	}
}
