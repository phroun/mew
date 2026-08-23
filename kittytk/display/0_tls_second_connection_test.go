package display_test

// Reproduce the reported stall: over tls://, a SECOND connection dialed
// from inside the first connection's command thread is admitted and gets
// its welcome, but (per the user's KITTYTK_DEBUG trace) never sends its
// build batch. This exercises exactly that path.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

func TestSecondTLSConnectionFromCommandHandler(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))
	t.Setenv(client.KnownHostsEnv, filepath.Join(dir, "known_hosts"))

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.ServeConfig(desktop, display.Config{
			Endpoint:    "tls://127.0.0.1:0",
			PromptLocal: true,
			Prompt:      display.NewDesktopAuthorizer(desktop),
		})
		if err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
			return
		}
		ready <- srv
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()
	var srv *display.Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not start")
	}
	defer func() { srv.Close(); desktop.Quit(); <-exited }()

	endpoint := "tls://" + srv.Addr()

	approve := func(app string) {
		deadline := time.Now().Add(5 * time.Second)
		for !clickPromptButton(t, desktop, "Once Only") {
			if time.Now().After(deadline) {
				t.Fatalf("%s: prompt never appeared", app)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// conn1
	type res struct {
		c   *client.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() { c, err := client.Dial(endpoint, "App 1", nil); ch <- res{c, err} }()
	approve("App 1")
	r := <-ch
	if r.err != nil {
		t.Fatalf("dial 1: %v", r.err)
	}
	c1 := r.c
	defer c1.Close()

	// conn2 dialed from conn1's command-dispatch thread.
	secondDone := make(chan error, 1)
	c1.OnCommand("newwin", func() {
		c2, err := client.Dial(endpoint, "App 2", nil)
		if err != nil {
			secondDone <- err
			return
		}
		_, err = c2.Build(`w2=new window title="Two" width=200 height=100 children={new label caption="hi"}`)
		secondDone <- err
	})

	if _, err := c1.Build(`w=new window title="One" width=220 height=120 children={` +
		`p=new panel children={b=new button caption="New" action=newwin}}` + "\nwb=w.p.b"); err != nil {
		t.Fatalf("build 1: %v", err)
	}
	if !clickButtonByLabel(t, desktop, "New") {
		t.Fatal("could not click conn1's New button")
	}
	approve("App 2")

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second TLS connection failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("second TLS connection never completed (reproduced the stall)")
	}
	waitForWindows(t, desktop, "One", "Two")
}
