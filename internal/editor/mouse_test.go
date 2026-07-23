package editor

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// Mouse input end to end, through the pseudo-key stream the key layer emits:
// a click sets the caret; a press on a browse-mode button shows the pressed
// style; dragging off cancels the click; press+release on the button follows
// the link exactly as keyboard navigation would.
func TestMouseButtonPressDragFollow(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender() // establish window geometry (ContentX/Y, widths)
	if !w.BrowseActive {
		t.Fatal("the wiki page should have auto-armed browse mode")
	}
	src := w.Buffer

	row := w.ContentY + 1 // line 0 of the buffer, 1-based screen row
	colOf := func(cell int) int { return w.ContentX + 1 + cell }
	click := func(kind string, cell int) {
		if !e.handleMouseKey("Mouse@" + itoa(colOf(cell)) + "," + itoa(row)) {
			t.Fatal("position pseudo-key should be consumed")
		}
		if !e.handleMouseKey(kind) {
			t.Fatal("mouse pseudo-key should be consumed")
		}
	}

	// A plain click sets the caret to the clicked cell.
	click("MouseLeftPress", 1) // the 'o' of "go"
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 1 {
		t.Fatalf("click should set the caret; got %+v", got)
	}
	if e.mousePressed.active {
		t.Fatal("a click outside any button must not arm the pressed state")
	}
	click("MouseLeftRelease", 1)

	// Press ON the button (the display shows "go ⟨ other ⟩▐ now"; cell 5 is
	// inside the button): pressed state arms, the caret parks in the span,
	// and the pressed style paints.
	click("MouseLeftPress", 5)
	if !e.mousePressed.active {
		t.Fatal("pressing a button should arm the pressed state")
	}
	if e.focusedLinkButton(w) == nil {
		t.Fatal("the pressed button should be the focused button")
	}
	var out strings.Builder
	_ = out
	// The pressed color must appear in the next frame.
	eOut := renderTo(e)
	if !strings.Contains(eOut, "\x1b[0;97;44m") {
		t.Fatal("the pressed button should paint in buttonPressed")
	}

	// Dragging off the button: the capture HOLDS but the pressed style
	// reverts (the caret is still in the span, so the focused style shows).
	if !e.handleMouseKey("MouseLeftDrag@" + itoa(colOf(0)) + "," + itoa(row)) {
		t.Fatal("drag pseudo-key should be consumed")
	}
	if !e.mousePressed.active || e.mouseOnCaptured {
		t.Fatal("dragging off must keep the capture but drop the pressed style")
	}
	if out := renderTo(e); !strings.Contains(out, "[0;30;46m") {
		t.Fatal("dragged-off captured button should show the focused style")
	}
	// Dragging back on re-presses.
	if !e.handleMouseKey("MouseLeftDrag@" + itoa(colOf(5)) + "," + itoa(row)) {
		t.Fatal("drag pseudo-key should be consumed")
	}
	if !e.mousePressed.active || !e.mouseOnCaptured {
		t.Fatal("dragging back on must re-press the captured button")
	}
	// Releasing OFF the captured button abandons the click.
	e.handleMouseKey("MouseLeftDrag@" + itoa(colOf(0)) + "," + itoa(row))
	click("MouseLeftRelease", 0)
	if e.mousePressed.active || w.Buffer != src {
		t.Fatal("a release off the captured button must not follow")
	}

	// Press and release on the button: the follow triggers.
	click("MouseLeftPress", 5)
	click("MouseLeftRelease", 6)
	if e.mousePressed.active {
		t.Fatal("release must clear the pressed state")
	}
	wantPath := filepath.Join(root, "w", "other.txt")
	if w.Buffer == src || w.Buffer.GetFilename() != wantPath {
		t.Fatalf("press+release on a button should follow; got %q", w.Buffer.GetFilename())
	}
	// And history returns, as with any follow.
	if !e.navHistory(-1) || w.Buffer != src {
		t.Fatal("a mouse follow is a normal follow: history returns")
	}
}

// The scroll wheel scrolls the window under the pointer.
func TestMouseScroll(t *testing.T) {
	lines := strings.Repeat("line\n", 60)
	e, w, _ := wikiTreeEditor(t, map[string]string{"w/long.txt": lines}, "w/long.txt")
	e.performRender()
	row := w.ContentY + 1
	col := w.ContentX + 1
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseScrollDown")
	if w.ViewState.ViewOffsetY != 3 {
		t.Fatalf("scroll down should advance the view; off=%d", w.ViewState.ViewOffsetY)
	}
	e.handleMouseKey("MouseScrollUp")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("scroll up should return; off=%d", w.ViewState.ViewOffsetY)
	}
}

// renderTo renders a frame and returns what was written to the harness
// terminal.
func renderTo(e *Editor) string {
	type outGetter interface{ String() string }
	e.performRender()
	if og, ok := e.Config.Terminal.Output.(outGetter); ok {
		return og.String()
	}
	return ""
}

// Non-mouse keys pass through untouched.
func TestMouseKeyPassthrough(t *testing.T) {
	e, _, _ := wikiTreeEditor(t, map[string]string{"w/p.txt": "x\n"}, "w/p.txt")
	for _, k := range []string{"a", "return", "C-c", "S-tab", "Mou", "MouseTrap"} {
		want := strings.HasPrefix(k, "Mouse")
		if got := e.handleMouseKey(k); got != want {
			t.Fatalf("handleMouseKey(%q) = %v, want %v", k, got, want)
		}
	}
	_ = window.Position{}
}

// Modal safety: only the FOCUSED window processes mouse actions. With a
// prompt up, clicks in the main window do nothing at all — no focus steal,
// no caret move, no follow.
func TestMouseModalSafety(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender()
	src := w.Buffer
	caretBefore := w.CursorPos()

	// Raise a modal prompt (it takes focus).
	answered := false
	e.PromptMgr.PromptForConfirmationTop("modal test", "Sure? [y/N]: ", false,
		func(accepted, yes bool) { answered = true })
	pw := focusedPrompt(e)
	if pw == nil {
		t.Fatal("prompt should be focused")
	}

	// Click on the wiki window's button cell: ignored outright.
	row := w.ContentY + 1
	col := w.ContentX + 1 + 5
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseLeftPress")
	e.handleMouseKey("MouseLeftRelease")
	if e.WindowManager.GetFocusedWindow() != pw {
		t.Fatal("a click outside the prompt must not steal focus")
	}
	if w.Buffer != src || w.CursorPos() != caretBefore {
		t.Fatal("a click outside the focused window must not act")
	}
	if e.mousePressed.active {
		t.Fatal("no pressed state may arm in an unfocused window")
	}
	answerPrompt(t, e, "")
	if !answered {
		t.Fatal("the prompt should still complete normally")
	}
}

// Hover (all-motion hosts): plain motion over a button paints the hover
// style; motion elsewhere clears it. In caret mode the hovered link paints
// linkHover.
func TestMouseHoverStyles(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender()

	row := w.ContentY + 1
	over := func(cell int) {
		e.handleMouseKey("MouseDrag@" + itoa(w.ContentX+1+cell) + "," + itoa(row))
	}

	// Browse mode (auto-armed): hover over the button.
	over(5)
	if !e.mouseHovered.active {
		t.Fatal("hover should latch over a button")
	}
	if out := renderTo(e); !strings.Contains(out, "\x1b[0;93;45m") {
		t.Fatal("the hovered button should paint in buttonHover")
	}

	// Hover away: clears and repaints without the hover color on the button.
	over(15)
	if e.mouseHovered.active {
		t.Fatal("hover should clear off the button")
	}

	// Caret mode (model a ^C'd user): the hovered link paints linkHover.
	w.BrowseActive = false
	over(5)
	if !e.mouseHovered.active {
		t.Fatal("hover should latch over a caret-mode link")
	}
	if out := renderTo(e); !strings.Contains(out, "\x1b[0;4;92;40m") {
		t.Fatal("the hovered link should paint in linkHover")
	}
}

// syncBuffer is a goroutine-safe output sink for driving the real run loop.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The REAL run loop repaints on mouse input alone: raw SGR reports go in
// through the terminal byte stream, and the pressed style plus the follow's
// destination content come out — with zero keyboard input. Guards the
// mouse -> handler -> render plumbing end to end.
func TestRunLoopRendersOnMouseAlone(t *testing.T) {
	root := t.TempDir()
	pagePath := filepath.Join(root, "page.txt")
	otherPath := filepath.Join(root, "other.txt")
	if err := os.WriteFile(pagePath, []byte("go [[other]] now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("OTHERCONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pr, pw := io.Pipe()
	out := &syncBuffer{}
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	configText := "[options]\nsyntax=dokuwiki\n"
	cfg.ConfigText = &configText
	cfg.Terminal = &TerminalIO{
		Input:  pr,
		Output: out,
		Size:   func() (int, int, error) { return 80, 24, nil },
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	buf, err := buffer.NewFromBytes([]byte("go [[other]] now\n"), pagePath)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.run(buf)
	}()

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	// Wait for the initial frame and window geometry.
	var w *window.Window
	waitFor("initial render", func() bool {
		w = e.WindowManager.GetFocusedWindow()
		return len(out.String()) > 0 && w != nil && w.ContentWidth > 0
	})

	// Raw SGR press on the button cell (no keyboard involved).
	row := w.ContentY + 1
	col := w.ContentX + 1 + 5
	if _, err := pw.Write([]byte("\x1b[<0;" + itoa(col) + ";" + itoa(row) + "M")); err != nil {
		t.Fatal(err)
	}
	waitFor("pressed style to paint", func() bool {
		return strings.Contains(out.String(), "\x1b[0;97;44m")
	})

	// Raw SGR release: the follow runs and the destination paints. The
	// back-buffer diff interleaves escapes mid-word, so compare stripped.
	if _, err := pw.Write([]byte("\x1b[<0;" + itoa(col) + ";" + itoa(row) + "m")); err != nil {
		t.Fatal(err)
	}
	esc := regexp.MustCompile("\x1b\\[[0-9;<>?]*[A-Za-z]|\x1b#[0-9]|\x1b[()][A-Z0-9]")
	waitFor("follow destination to paint", func() bool {
		return strings.Contains(esc.ReplaceAllString(out.String(), ""), "OTHERCONTENT")
	})

	// Best-effort shutdown: closing the input source should end the session
	// (dkh Closed event). Today an embedded pipe reader does not always
	// surface EOF promptly — a separate shutdown nuance, not the mouse path
	// under test — so a hung session is only logged and the goroutine leaks
	// into process exit.
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("session did not end on input close (known embedded-reader shutdown nuance)")
	}
}

// After a follow spawns a NEW window, the old main window keeps stale
// geometry covering the same rows (only the current main window is laid
// out). Hit testing must resolve to the FOCUSED window, so the mouse works
// in the freshly spawned window immediately.
func TestMouseWorksInSpawnedWindow(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("click me [[here]] ok\nand [[gone]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := "file://" + filepath.ToSlash(outside)
	content := "x [[" + link + "]] y\n"
	page := filepath.Join(root, "wiki", "page.txt")
	if err := os.WriteFile(page, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	e, _, _ := renderedEditorWithConfig(t, "seed\n", "[options]\nsyntax=dokuwiki\n")
	buf, err := buffer.NewFromBytes([]byte(content), page)
	if err != nil {
		t.Fatal(err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "wiki", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("wiki")
	e.performRender() // old window gets real geometry

	// Follow the scheme link: a new focused window spawns.
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("follow should spawn the new window")
	}
	nw := e.WindowManager.GetFocusedWindow()
	if nw == w {
		t.Fatal("a scheme follow should focus a fresh window")
	}
	e.performRender() // new window laid out; old geometry now STALE but overlapping

	// A click in the new window must hit the new (focused) window, even
	// though the old window's stale geometry covers the same rows.
	row := nw.ContentY + 1
	col := nw.ContentX + 1 + 2 // the 'i' of "click"
	if hit := e.windowAtRow(row); hit != nw {
		t.Fatalf("hit testing must prefer the focused window; got %v", hit.ID)
	}
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseLeftPress")
	if got := nw.CursorPos(); got.Line != 0 || got.Rune != 2 {
		t.Fatalf("click in the spawned window should set its caret; got %+v", got)
	}
	e.handleMouseKey("MouseLeftRelease")
}

// Clicking PAST the end of a line whose last element is a button places the
// caret at EOL — it must not park inside the trailing button's span (which
// would make the release follow it).
func TestMouseClickPastEOLButton(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]]\nmore\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender()
	src := w.Buffer
	eol := len([]rune("go [[other]]"))

	row := w.ContentY + 1
	col := w.ContentX + 1 + 30 // well past the button
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseLeftPress")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != eol {
		t.Fatalf("past-EOL click should park at EOL (%d); got %+v", eol, got)
	}
	if e.mousePressed.active {
		t.Fatal("past-EOL click must not capture the trailing button")
	}
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseLeftRelease")
	if w.Buffer != src {
		t.Fatal("past-EOL click must not follow")
	}
}

// The pointer-shape hook fires on affordance TRANSITIONS: true entering a
// button (hover or capture), false leaving.
func TestMousePointerShapeHook(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender()

	var pushes []bool
	e.Config.PointerShape = func(iBeam bool) { pushes = append(pushes, iBeam) }

	row := w.ContentY + 1
	move := func(cell int) {
		e.handleMouseKey("MouseDrag@" + itoa(w.ContentX+1+cell) + "," + itoa(row))
	}

	// The I-beam shows over the focused window's text and the arrow over a
	// browse-mode link button (true = I-beam, false = arrow).
	move(0)  // plain text: I-beam (first computation always pushes) -> true
	move(5)  // onto the button: arrow -> false
	move(6)  // still on it: no push
	move(15) // off the button, back on text: I-beam -> true
	if len(pushes) != 3 || !pushes[0] || pushes[1] || !pushes[2] {
		t.Fatalf("i-beam transitions = %v, want [true false true]", pushes)
	}

	// A captured button is a button interaction: it holds the arrow even when
	// the pointer is dragged onto text. (Releasing ON the button would follow
	// the link and land the pointer on the destination's text — legitimately an
	// I-beam — so this drags OFF and releases in the void to isolate the
	// capture behavior.)
	pushes = nil
	e.handleMouseKey("Mouse@" + itoa(w.ContentX+1+5) + "," + itoa(row))         // onto the button -> arrow
	e.handleMouseKey("MouseLeftPress")                                          // capture
	e.handleMouseKey("MouseLeftDrag@" + itoa(w.ContentX+1+0) + "," + itoa(row)) // drag onto text, still captured
	for i, p := range pushes {
		if p {
			t.Fatalf("a captured button must hold the arrow over text; pushes[%d]=true (%v)", i, pushes)
		}
	}
	e.handleMouseKey("MouseLeftRelease") // release off the button (no follow)
}
