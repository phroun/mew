package display_test

// What the Connections window's rows are built on: a real client dialling a
// real host, and what that leaves behind.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
)

// knownLines is what the known-clients file holds, minus its header.
func knownLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// A client admitted for this session only is still a client that has been here.
// It leaves no rule behind it -- "Once Only" writes nothing to the
// authorizations file -- so without a record of its own it would draw on the
// desktop and appear in the window nowhere.
//
// Both it and the app it came as are written down, each dated, each under the
// name its folder will take.
func TestConnectingOnceRecordsTheClientAndTheApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known")
	t.Setenv(display.KnownStoreEnv, path)

	var admitted display.AuthRequest
	_, srv, stop := startHost(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true, // loopback would otherwise be admitted without a word
		Authorize: func(r display.AuthRequest) display.AuthDecision {
			admitted = r
			return display.AuthAllowOnce
		},
	})
	defer stop()

	conn, err := client.Dial("tls://"+srv.Addr(), "Dated App", nil)
	if err != nil {
		t.Fatalf("dial tls: %v", err)
	}
	defer conn.Close()

	lines := knownLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("connecting once left %d records, want the client and its app:\n%v",
			len(lines), lines)
	}

	var host, app string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "host "+admitted.Fingerprint+" "):
			host = l
		case strings.HasPrefix(l, "app "+admitted.Fingerprint+" "):
			app = l
		}
	}
	if host == "" || app == "" {
		t.Fatalf("no record for the client that just connected (%s):\n%v",
			admitted.Fingerprint, lines)
	}
	if !strings.HasSuffix(app, " Dated App") {
		t.Errorf("the app record does not name the app it connected as: %q", app)
	}
	if got := strings.Fields(app)[2]; got != "dated-app" {
		t.Errorf("the app is filed under %q, want dated-app", got)
	}

	// Both are dated with this connection, not with nothing and not with the
	// day the file was made.
	for _, line := range []string{host, app} {
		stamp := strings.Fields(line)[3]
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			t.Fatalf("%q is stamped %q, which is not a time: %v", line, stamp, err)
		}
		if d := time.Since(at); d < 0 || d > time.Minute {
			t.Errorf("%q is stamped %v, which is not when this connection happened",
				line, at)
		}
	}
}

// A refused client leaves nothing. The window lists what has been let in, and
// one turned away for the moment never was -- it is not a client this desktop
// knows, and it gets no folder and no row.
func TestARefusedClientIsNotRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known")
	t.Setenv(display.KnownStoreEnv, path)

	_, srv, stop := startHost(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthDenyOnce },
	})
	defer stop()

	if conn, err := client.Dial("tls://"+srv.Addr(), "Turned Away", nil); err == nil {
		conn.Close()
		t.Fatal("the host admitted a client its authorizer denied")
	}

	if lines := knownLines(t, path); len(lines) != 0 {
		t.Errorf("a refused client was recorded anyway:\n%v", lines)
	}
}
