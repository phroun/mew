package display_test

// The interactive approval prompt, driven headlessly: the authorizer
// posts its protocol-built window onto a running desktop; clicking one
// of the six buttons returns the mapped decision.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

func findButtons(root core.Trinket, out *[]*trinkets.Button) {
	if b, ok := root.(*trinkets.Button); ok {
		*out = append(*out, b)
	}
	if c, ok := root.(core.Container); ok {
		for _, k := range c.Children() {
			findButtons(k, out)
		}
	}
}

func clickPromptButton(t *testing.T, d *trinkets.Desktop, label string) bool {
	t.Helper()
	clicked := false
	onUI(d, func() {
		wm := d.WindowManager()
		if wm == nil {
			return
		}
		for _, w := range wm.Windows() {
			if w.Title() != "Connection Request" {
				continue
			}
			var btns []*trinkets.Button
			findButtons(w.Content(), &btns)
			for _, b := range btns {
				if b.Text() == label {
					b.Click()
					clicked = true
					return
				}
			}
		}
	})
	return clicked
}

func TestDesktopAuthorizerPrompt(t *testing.T) {
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	go func() { desktop.Run() }()
	defer desktop.Quit()

	// Let the run loop come up.
	deadline := time.Now().Add(3 * time.Second)
	for {
		up := false
		onUI(desktop, func() { up = desktop.WindowManager() != nil })
		if up {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("desktop did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	authz := display.NewDesktopAuthorizer(desktop)

	cases := []struct {
		button string
		want   display.AuthDecision
	}{
		{"Always", display.AuthAllowApp},
		{"Block Client", display.AuthDenyClient},
		{"Once Only", display.AuthAllowOnce},
	}
	for _, tc := range cases {
		req := display.AuthRequest{
			AppName:     "Test App",
			Fingerprint: "sha256:deadbeef",
			Transport:   "tls",
			RemoteAddr:  "198.51.100.7:40000",
		}
		got := make(chan display.AuthDecision, 1)
		go func() { got <- authz(req) }()

		// Wait for the prompt window to appear, then click.
		cd := time.Now().Add(5 * time.Second)
		for !clickPromptButton(t, desktop, tc.button) {
			if time.Now().After(cd) {
				t.Fatalf("prompt button %q never appeared", tc.button)
			}
			time.Sleep(20 * time.Millisecond)
		}
		select {
		case d := <-got:
			if d != tc.want {
				t.Errorf("button %q -> %v, want %v", tc.button, d, tc.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no decision after clicking %q", tc.button)
		}
	}
}
