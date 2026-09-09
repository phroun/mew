package display_test

// What the Connections window's Last Seen column is built on: a real client
// dialling a real host, and the day that leaves behind.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
)

// A client that connects is stamped with today, under the same identity the
// authorization was recorded against -- so the window's date lines up with the
// row it sits on.
func TestConnectingRecordsTheDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last_seen")
	t.Setenv(display.SeenStoreEnv, path)

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

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing was written, so every row would read as never seen: %v", err)
	}
	line := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, admitted.Fingerprint+" ") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no stamp for the client that just connected (%s):\n%s",
			admitted.Fingerprint, raw)
	}

	stamp := strings.TrimSpace(strings.TrimPrefix(line, admitted.Fingerprint))
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("stamped %q, which is not a time: %v", stamp, err)
	}
	if d := time.Since(at); d < 0 || d > time.Minute {
		t.Errorf("stamped %v, which is not when this connection happened", at)
	}
}

// A refused client leaves no stamp: the column says when a client last spoke
// to the display server, and one that was turned away never did.
func TestARefusedClientIsNotDated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last_seen")
	t.Setenv(display.SeenStoreEnv, path)

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

	if raw, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if l != "" && !strings.HasPrefix(l, "#") {
				t.Errorf("a refused client was dated anyway:\n%s", raw)
				break
			}
		}
	}
}
