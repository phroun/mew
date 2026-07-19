package display_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// Reproduce the report: with PromptLocal on, a second connection prompts,
// and after approval its window must actually appear.
func TestSecondPromptedConnectionShows(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})

	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.ServeConfig(desktop, display.Config{
			Endpoint:    "unix:" + sock,
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

	dialAndBuild := func(app, title string) *client.Conn {
		type res struct {
			c   *client.Conn
			err error
		}
		ch := make(chan res, 1)
		go func() {
			c, err := client.Dial("unix:"+sock, app, nil)
			ch <- res{c, err}
		}()
		deadline := time.Now().Add(5 * time.Second)
		for !clickPromptButton(t, desktop, "Once Only") {
			if time.Now().After(deadline) {
				t.Fatalf("%s: prompt never appeared", app)
			}
			time.Sleep(20 * time.Millisecond)
		}
		r := <-ch
		if r.err != nil {
			t.Fatalf("%s dial: %v", app, r.err)
		}
		if _, err := r.c.Build(fmt.Sprintf(
			`w=new window title=%q width=200 height=100 children={new label caption="hi"}`, title)); err != nil {
			t.Fatalf("%s build: %v", app, err)
		}
		return r.c
	}

	c1 := dialAndBuild("App 1", "One")
	defer c1.Close()
	c2 := dialAndBuild("App 2", "Two")
	defer c2.Close()

	waitForWindows(t, desktop, "One", "Two")
}

// waitForWindows asserts all named windows appear on the desktop.
func waitForWindows(t *testing.T, desktop *trinkets.Desktop, want ...string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		has := map[string]bool{}
		var titles []string
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					has[w.Title()] = true
					titles = append(titles, w.Title())
				}
			}
		})
		ok := true
		for _, w := range want {
			if !has[w] {
				ok = false
			}
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("windows = %v, want all of %v", titles, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestSecondConnectionFromCommandHandler is the faithful demoapp path:
// "New Window" dials the second connection from inside the FIRST
// connection's command-dispatch thread, which then blocks on that
// connection's approval prompt.
func TestSecondConnectionFromCommandHandler(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})

	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.ServeConfig(desktop, display.Config{
			Endpoint:    "unix:" + sock,
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

	// Dial conn1 and approve its prompt.
	type res struct {
		c   *client.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() { c, err := client.Dial("unix:"+sock, "App 1", nil); ch <- res{c, err} }()
	for !clickPromptButton(t, desktop, "Once Only") {
		time.Sleep(20 * time.Millisecond)
	}
	r := <-ch
	if r.err != nil {
		t.Fatalf("dial 1: %v", r.err)
	}
	c1 := r.c
	defer c1.Close()

	// conn1's command handler dials conn2 (from conn1's event thread).
	secondDone := make(chan error, 1)
	c1.OnCommand("newwin", func() {
		c2, err := client.Dial("unix:"+sock, "App 2", nil)
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

	// Fire the command by clicking conn1's button server-side.
	if !clickButtonByLabel(t, desktop, "New") {
		t.Fatal("could not find conn1's New button to click")
	}

	// Approve conn2's prompt (it is now being dialed on conn1's thread).
	for !clickPromptButton(t, desktop, "Once Only") {
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second connection failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second connection never completed (deadlock?)")
	}

	waitForWindows(t, desktop, "One", "Two")
}

// clickButtonByLabel finds any button on the desktop with the given
// caption (searching every app window) and clicks it.
func clickButtonByLabel(t *testing.T, d *trinkets.Desktop, label string) bool {
	t.Helper()
	clicked := false
	onUI(d, func() {
		for _, a := range d.Applications() {
			for _, w := range a.Windows() {
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
		}
	})
	return clicked
}
