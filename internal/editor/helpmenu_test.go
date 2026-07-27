package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

func helpTestEditor(t *testing.T, files map[string]string) *Editor {
	t.Helper()
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", files)
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: buffer.NewFromString("start\n"), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})
	return e
}

// buffer_open_file with an argument opens THAT file directly — an ORDINARY,
// UNtagged help viewport (stackable), NOT the docked help slot that help_toggle
// owns.
func TestBufferOpenFileHelpIsUntagged(t *testing.T) {
	e := helpTestEditor(t, nil)
	e.executeCommand(`buffer_open_file "help:/"`)

	if e.helpViewport() != nil {
		t.Fatal("buffer_open_file must not create a tagged help-slot viewport")
	}
	var hw *viewport.Viewport
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.WikiName == "help" {
			hw = w
		}
	}
	if hw == nil {
		t.Fatal(`buffer_open_file "help:/" should still open a help wiki viewport`)
	}
	// It is a real help PAGE, so it wears the "Help" title bar — the title-less
	// chrome is reserved for Quick Help. (This is what ^B O / ^B F must match,
	// not the Quick Help look.)
	if hw.MessageTopCenter != "Help" {
		t.Fatalf("an opened help page should carry the Help title bar, got %q", hw.MessageTopCenter)
	}
	if hw.Class == quickHelpClass {
		t.Fatal("an opened help page must not use the Quick Help class")
	}
}

// A FOLLOWED help link (nav_follow into help://) lands in a docked help page
// with the "Help" title bar too — the same page chrome as buffer_open_file,
// never the title-less Quick Help form.
func TestFollowHelpLinkHasTitle(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/start.txt": "=== Start ===\nbody\n"})
	// A buffer whose only content is a help:// link; caret on it, then follow.
	e.executeCommand("buffer_new")
	w := e.ViewportManager.GetFocusedViewport()
	w.ViewState.LinkBrowsing = true // the hyperlink layer (global default), as on a real page
	e.executeCommand("insert '[[help://]]'")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 3}) // inside the [[help://]] source
	if !e.navFollow(true) {
		t.Fatal("nav_follow should follow the help:// link")
	}
	var hw *viewport.Viewport
	for _, v := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if v.WikiName == "help" {
			hw = v
		}
	}
	if hw == nil {
		t.Fatal("following help:// should open a docked help page")
	}
	if hw.MessageTopCenter != "Help" {
		t.Fatalf("a followed help page should carry the Help title bar, got %q", hw.MessageTopCenter)
	}
}

// help_toggle with no argument toggles the Quick Help location in the docked
// help viewport: it opens the viewport at Quick Help, and toggling again (still at
// Quick Help) closes it.
func TestHelpToggleQuickHelp(t *testing.T) {
	e := helpTestEditor(t, nil)
	if e.helpViewport() != nil {
		t.Fatal("no help viewport at start")
	}
	e.executeCommand("help_toggle")
	if !e.quickHelpViewportOpen() || e.helpViewport() == nil {
		t.Fatal("help_toggle should open the docked help viewport at Quick Help")
	}
	e.executeCommand("help_toggle")
	if e.helpViewport() != nil {
		t.Fatal("help_toggle at Quick Help should close the docked help viewport")
	}
}

// help_toggle <page> navigates the SAME docked viewport to that help page,
// growing its nav history so BACK returns to where the reader came from; the
// checkmark turns off (a page is not Quick Help).
func TestHelpToggleNavigatesWithHistory(t *testing.T) {
	// The manual page is a DISTINCT id from the Quick Help topic, which has no
	// file here, so Quick Help stays the built-in reference — a location apart
	// from the page we navigate to.
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== Manual ===\nbindings\n"})

	e.executeCommand("help_toggle") // Quick Help
	hw := e.helpViewport()
	if hw == nil {
		t.Fatal("Quick Help should open the docked viewport")
	}
	quickURL := e.bufferCanonicalURL(hw.Buffer)

	e.executeCommand(`help_toggle "manual"`) // navigate to the page
	if e.helpViewport() != hw {
		t.Fatal("help_toggle should navigate the SAME docked viewport")
	}
	if e.quickHelpViewportOpen() {
		t.Fatal("a wiki page is not Quick Help (checkmark off)")
	}
	if e.bufferCanonicalURL(hw.Buffer) == quickURL {
		t.Fatal("the viewport should have navigated off Quick Help")
	}

	// Back returns to Quick Help via the viewport's nav history.
	if !hw.NavHistoryPrior() {
		t.Fatal("the help viewport should carry back history after navigating")
	}
	if e.bufferCanonicalURL(hw.Buffer) != quickURL {
		t.Fatal("back should return to the Quick Help location")
	}
}

// help_toggle <page> when the viewport is already showing that page closes it.
func TestHelpToggleClosesAtCurrentLocation(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/keys.txt": "=== Keys ===\n"})
	e.executeCommand(`help_toggle "keys"`)
	if e.helpViewport() == nil {
		t.Fatal(`help_toggle "keys" should open the page`)
	}
	e.executeCommand(`help_toggle "keys"`)
	if e.helpViewport() != nil {
		t.Fatal("help_toggle at the current location should close the docked viewport")
	}
}

// helpURL resolves a "help:/..." ref to the canonical buffer URL it opens, so
// tests can assert what a help viewport is showing.
func helpURL(t *testing.T, e *Editor, ref string) string {
	t.Helper()
	res := e.resolveFollow(nil, ref)
	if res.url == "" {
		t.Fatalf("resolveFollow(%q) found no page: %s", ref, res.message)
	}
	return res.url
}

// Quick Help follows the key context: it shows help:/<quickHelpTopic> — the
// topic being the deepest "help" virtual binding for the active key prefix —
// and when the context deepens while it is open it re-navigates IN PLACE (no
// history churn — Quick Help is a single dynamic slot).
func TestQuickHelpFollowsTopic(t *testing.T) {
	e := helpTestEditor(t, map[string]string{
		"help/keys.txt":        "=== Root Keys ===\nroot\n",
		"help/keys_buffer.txt": "=== Buffer Keys ===\nbuffer\n",
	})
	e.KeyProcessor.MapKey("help", "keys")
	e.KeyProcessor.MapKey("^B help", "keys_buffer")

	// Root context -> topic "keys". Open Quick Help; it shows help:/keys.
	e.ActiveSequence = ""
	e.executeCommand("help_toggle")
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should be open in quick mode")
	}
	if got := e.bufferCanonicalURL(hw.Buffer); got != helpURL(t, e, "help:/keys") {
		t.Fatalf("Quick Help should show help:/keys, showing %q", got)
	}

	// Context deepens to "^B" -> topic "keys_buffer". A follow re-navigates the
	// SAME viewport in place, WITHOUT adding back history.
	backBefore, _ := hw.NavHistoryDepths()
	e.ActiveSequence = "^B"
	e.updateQuickHelp()
	if e.helpViewport() != hw {
		t.Fatal("Quick Help should follow the topic in the SAME viewport")
	}
	if got := e.bufferCanonicalURL(hw.Buffer); got != helpURL(t, e, "help:/keys_buffer") {
		t.Fatalf("Quick Help should have followed to help:/keys_buffer, showing %q", got)
	}
	if backAfter, _ := hw.NavHistoryDepths(); backAfter != backBefore {
		t.Fatalf("following the topic must not grow back history: %d -> %d", backBefore, backAfter)
	}
	if !e.quickHelpViewportOpen() {
		t.Fatal("still Quick Help after following the topic")
	}
}

// Quick Help is ephemeral: as the key context walks from topic to topic, each
// departing page is RELEASED, never buried. So the docked viewport's graveyard
// stays empty however many contexts are visited — nothing accumulates to
// resurface (as a would-be unsaved document) when the viewport later closes.
func TestQuickHelpDoesNotAccumulateGraveyard(t *testing.T) {
	e := helpTestEditor(t, map[string]string{
		"help/keys.txt":        "=== Root ===\nroot\n",
		"help/keys_buffer.txt": "=== Buffer ===\nbuffer\n",
		"help/keys_win.txt":    "=== Viewport ===\nviewport\n",
	})
	e.KeyProcessor.MapKey("help", "keys")
	e.KeyProcessor.MapKey("^B help", "keys_buffer")
	e.KeyProcessor.MapKey("^W help", "keys_win")

	e.ActiveSequence = ""
	e.executeCommand("help_toggle")
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should be open in quick mode")
	}

	// Walk the context across several distinct pages, then back to the root.
	for _, seq := range []string{"^B", "^W", ""} {
		e.ActiveSequence = seq
		e.updateQuickHelp()
	}

	if n := len(hw.GraveyardBuffers()); n != 0 {
		t.Fatalf("Quick Help must not bury departed pages; graveyard holds %d", n)
	}
	if back, _ := hw.NavHistoryDepths(); back != 0 {
		t.Fatalf("Quick Help is a single dynamic slot; back history grew to %d", back)
	}
}

// The main help ("Using mew" = help_toggle <page>) is never affected by the
// quickHelpTopic, and opening it leaves quick mode.
func TestMainHelpIgnoresQuickHelpTopic(t *testing.T) {
	e := helpTestEditor(t, map[string]string{
		"help/start.txt":       "=== Index ===\nindex\n",
		"help/keys_buffer.txt": "=== Buffer Keys ===\nbuffer\n",
	})
	e.KeyProcessor.MapKey("^B help", "keys_buffer")
	e.ActiveSequence = "^B" // context topic is "keys_buffer"

	e.executeCommand(`help_toggle "help:/"`) // Using mew -> the index
	hw := e.helpViewport()
	if hw == nil {
		t.Fatal("Using mew should open the docked help viewport")
	}
	if e.quickHelpViewportOpen() {
		t.Fatal("the main help is not Quick Help (checkmark off), regardless of topic")
	}
	startURL := helpURL(t, e, "help:/start")
	if got := e.bufferCanonicalURL(hw.Buffer); got != startURL {
		t.Fatalf("Using mew should show the help index, not the quick topic; showing %q", got)
	}

	// A context change must NOT move the main help.
	e.updateQuickHelp()
	if got := e.bufferCanonicalURL(hw.Buffer); got != startURL {
		t.Fatalf("the quick topic must never steer the main help; moved to %q", got)
	}
}

// Following a link (browsing) out of Quick Help leaves quick mode: the topic
// stops steering the viewport thereafter.
func TestBrowsingLeavesQuickMode(t *testing.T) {
	e := helpTestEditor(t, map[string]string{
		"help/keys.txt": "=== Root Keys ===\nroot\n",
	})
	e.KeyProcessor.MapKey("help", "keys")
	e.executeCommand("help_toggle")
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should be open")
	}

	// Simulate a browse away: swap the viewport to another buffer, as link-follow
	// does. Quick mode must then read as off, and the topic must not drag it.
	e.swapBuffer(hw, buffer.NewFromString("elsewhere\n"))
	if e.quickHelpViewportOpen() {
		t.Fatal("browsing away should leave quick mode")
	}
	e.updateQuickHelp()
	if got := e.bufferCanonicalURL(hw.Buffer); got == helpURL(t, e, "help:/keys") {
		t.Fatal("after leaving quick mode the topic must not re-navigate the viewport")
	}
}

// Quick Help drops the top message bar and fits its viewport height to the loaded
// file; a regular help page restores the "Help" title bar and standard height.
// The same docked slot flips chrome as its role changes.
func TestQuickHelpChromeAndFit(t *testing.T) {
	e := helpTestEditor(t, map[string]string{
		"help/keys.txt":   "l1\nl2\nl3\n", // 3 content lines
		"help/manual.txt": "=== Manual ===\nbody\n",
	})
	e.KeyProcessor.MapKey("help", "keys")

	e.executeCommand("help_toggle") // Quick Help -> help:/keys
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should open")
	}
	if hw.MessageTopCenter != "" {
		t.Errorf("Quick Help must hide the top message bar, got %q", hw.MessageTopCenter)
	}
	if hw.MaxHeight != 3 {
		t.Errorf("Quick Help MaxHeight should fit the 3-line file, got %d", hw.MaxHeight)
	}
	if hw.MinHeight > hw.MaxHeight {
		t.Errorf("MinHeight %d must not exceed the fitted MaxHeight %d", hw.MinHeight, hw.MaxHeight)
	}

	// Navigate to a regular help page: the title bar and standard height return.
	e.executeCommand(`help_toggle "manual"`)
	if e.helpViewport() != hw {
		t.Fatal("should stay the same docked viewport")
	}
	if hw.MessageTopCenter != "Help" {
		t.Errorf("a help page must show the Help title bar, got %q", hw.MessageTopCenter)
	}
	if hw.MaxHeight != 20 {
		t.Errorf("a help page uses the standard height envelope, got MaxHeight %d", hw.MaxHeight)
	}
}

// Browsing away from Quick Help (a link swap, not a help_toggle) restores the
// page chrome the dynamic view had stripped.
func TestQuickHelpChromeRestoredOnBrowseAway(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/keys.txt": "a\nb\n"})
	e.KeyProcessor.MapKey("help", "keys")
	e.executeCommand("help_toggle")
	hw := e.helpViewport()
	if hw == nil || hw.MessageTopCenter != "" {
		t.Fatal("Quick Help should open chromeless")
	}

	e.swapBuffer(hw, buffer.NewFromString("elsewhere\n")) // as link-follow does
	e.syncQuickHelpMode()                                 // reconcile (key loop / render does this)
	if e.quickHelpViewportOpen() {
		t.Fatal("browsing away should leave quick mode")
	}
	if hw.MessageTopCenter != "Help" || hw.MaxHeight != 20 {
		t.Errorf("browsing away should restore page chrome, got title %q maxH %d",
			hw.MessageTopCenter, hw.MaxHeight)
	}
}

// help_toggle with a PAGE argument is a request to read: it focuses the help
// viewport, so the keyboard drives the page the reader just asked for.
func TestHelpTogglePageFocuses(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== M ===\nbody\n"})
	e.executeCommand(`help_toggle "manual"`)
	hw := e.helpViewport()
	if hw == nil {
		t.Fatal("help_toggle should open the page")
	}
	if e.ViewportManager.GetFocusedViewport() != hw {
		t.Fatal("help_toggle with a page should focus the help viewport")
	}
}

// Quick Help (help_toggle with NO argument) stays a peek: it follows the key
// prefix being typed, so it must never take the caret.
func TestHelpToggleQuickHelpDoesNotFocus(t *testing.T) {
	e := helpTestEditor(t, nil)
	doc := e.ViewportManager.GetFocusedViewport()
	if doc == nil {
		t.Fatal("a doc viewport should be focused at start")
	}
	e.executeCommand("help_toggle")
	if e.helpViewport() == nil {
		t.Fatal("help_toggle should open Quick Help")
	}
	if e.ViewportManager.GetFocusedViewport() != doc {
		t.Fatal("Quick Help must not steal focus")
	}
}

// Toggling a focused help page back off returns focus to the viewport last
// focused in the blank viewport set — the document the reader came from, even
// when another document was created after it.
func TestHelpToggleOffRestoresFocus(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== M ===\nbody\n"})
	// A second document, created LAST, so a fallback that picks by creation
	// order would land here instead of on the reader's own document.
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: buffer.NewFromString("other\n"),
	})
	doc := e.ViewportManager.GetFocusedViewport()

	e.executeCommand(`help_toggle "manual"`)
	if e.ViewportManager.GetFocusedViewport() == doc {
		t.Fatal("help_toggle with a page should have moved focus into help")
	}
	e.executeCommand(`help_toggle "manual"`) // same location: toggles off

	if e.helpViewport() != nil {
		t.Fatal("the second toggle should close the help viewport")
	}
	if got := e.ViewportManager.GetFocusedViewport(); got != doc {
		t.Fatalf("focus returned to %v, want the document the reader came from", got)
	}
}

// Closing help that does NOT hold the keyboard leaves focus alone.
func TestHelpToggleOffLeavesUnfocusedCaret(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== M ===\nbody\n"})
	doc := e.ViewportManager.GetFocusedViewport()
	e.executeCommand(`help_toggle "manual"`) // focuses help
	e.ViewportManager.SetFocus(doc.ID)       // reader clicks back into the document

	e.executeCommand(`help_toggle "manual"`) // closes the slot

	if got := e.ViewportManager.GetFocusedViewport(); got != doc {
		t.Fatalf("focus = %v, want the document to keep the caret", got)
	}
}

// help_open focuses the help viewport and never closes it (a second invocation at
// the same location keeps it open, unlike help_toggle).
func TestHelpOpenFocusesAndNeverCloses(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== M ===\nbody\n"})
	e.executeCommand(`help_open "manual"`)
	hw := e.helpViewport()
	if hw == nil {
		t.Fatal("help_open should open the page")
	}
	if e.ViewportManager.GetFocusedViewport() != hw {
		t.Fatal("help_open should focus the help viewport")
	}
	e.executeCommand(`help_open "manual"`) // same location again
	if e.helpViewport() == nil {
		t.Fatal("help_open must never close the help viewport")
	}
}

// help_open focuses even Quick Help (CanFocus=false) — explicit focus is not
// gated by CanFocus, only the switcher is.
func TestHelpOpenFocusesQuickHelp(t *testing.T) {
	e := helpTestEditor(t, nil) // no files: Quick Help is the built-in reference
	e.executeCommand("help_open")
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("help_open should open Quick Help")
	}
	if hw.CanFocus {
		t.Fatal("Quick Help should have CanFocus=false")
	}
	if e.ViewportManager.GetFocusedViewport() != hw {
		t.Fatal("help_open should focus Quick Help on explicit request")
	}
}

// The focus switcher (viewport_next / viewport_prior) skips Quick Help.
func TestFocusSwitcherSkipsQuickHelp(t *testing.T) {
	e := helpTestEditor(t, nil)
	// A second focusable doc viewport, so cycling has a real destination.
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: buffer.NewFromString("two\n"),
	})
	e.executeCommand("help_toggle") // Quick Help, unfocused
	hw := e.helpViewport()
	if hw == nil || hw.CanFocus {
		t.Fatal("Quick Help should be open with CanFocus=false")
	}
	// Cycle through every viewport twice; the switcher must never land on it.
	for i := 0; i < 4; i++ {
		e.ViewportManager.FocusNextViewport()
		if e.ViewportManager.GetFocusedViewport() == hw {
			t.Fatal("the focus switcher must skip Quick Help")
		}
	}
}

// While Quick Help is open, following the context to a topic whose page does
// NOT exist keeps the current page showing — it does not replace it with the
// no-help notice.
func TestQuickHelpKeepsCurrentWhenTopicMissing(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/root.txt": "=== Root ===\nroot page\n"})
	e.KeyProcessor.MapKey("help", "root")                // root topic -> a real page
	e.KeyProcessor.MapKey("^Q help", "no_such_page_xyz") // deeper topic -> missing page

	e.ActiveSequence = ""
	e.executeCommand("help_toggle") // Quick Help at the root topic
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should open")
	}
	rootURL := e.bufferCanonicalURL(hw.Buffer)
	if rootURL != helpURL(t, e, "help:/root") {
		t.Fatalf("Quick Help should show help:/root, showing %q", rootURL)
	}

	// Context deepens to "^Q" -> topic "no_such_page_xyz" (no page). Keep root.
	e.ActiveSequence = "^Q"
	e.updateQuickHelp()
	if got := e.bufferCanonicalURL(hw.Buffer); got != rootURL {
		t.Fatalf("a missing follow-topic must keep the current page; changed to %q", got)
	}
	if !e.quickHelpViewportOpen() {
		t.Fatal("still Quick Help after a missing follow-topic")
	}

	// Back to the root context -> the valid page stays.
	e.ActiveSequence = ""
	e.updateQuickHelp()
	if got := e.bufferCanonicalURL(hw.Buffer); got != rootURL {
		t.Fatalf("returning to a valid topic should still show help:/root, got %q", got)
	}
}

// When the matched topic names no existing page, Quick Help shows the plain
// "no help" notice — not the old hardcoded reference.
func TestQuickHelpNoHelpAvailable(t *testing.T) {
	e := helpTestEditor(t, nil)
	e.KeyProcessor.MapKey("help", "no_such_help_page_xyz") // resolves to nothing
	e.executeCommand("help_toggle")
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should open")
	}
	if got := hw.Buffer.GetContent(); !strings.Contains(got, "No quick help is available.") {
		t.Fatalf("expected the no-help notice, got %q", got)
	}
}

// The docked slot carries class "quickhelp" while showing Quick Help and "help"
// while showing a page, so per-class config can target each. The Tag stays
// "help" so helpViewport() finds the slot in either role.
func TestHelpViewportClassFlipsWithRole(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/manual.txt": "=== M ===\nbody\n"})
	e.KeyProcessor.MapKey("help", "no_such_help_page_xyz") // Quick Help -> the notice

	e.executeCommand("help_toggle") // Quick Help
	hw := e.helpViewport()
	if hw == nil || !e.quickHelpViewportOpen() {
		t.Fatal("Quick Help should open")
	}
	if hw.Class != "quickhelp" {
		t.Errorf("Quick Help class = %q, want %q", hw.Class, "quickhelp")
	}

	e.executeCommand(`help_toggle "manual"`) // navigate to a page
	if hw.Class != "help" {
		t.Errorf("a help page's class = %q, want %q", hw.Class, "help")
	}
}

// Opening a help page that does not exist reports a transient error toast and
// opens no viewport — it never creates a stub page.
func TestHelpOpenMissingPageErrors(t *testing.T) {
	e := helpTestEditor(t, nil)
	e.executeCommand(`help_open "definitely_not_a_real_page"`)
	if e.helpViewport() != nil {
		t.Fatal("a missing help page must not open or create a help viewport")
	}
	found := false
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Class == "error" && strings.Contains(w.MessageTopInner, "not found") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a transient error toast for a missing help page")
	}
}

// notifyHelpState pushes whether the docked help viewport is showing Quick Help,
// on the first render and on transitions (a host syncs the checkmark to it).
func TestNotifyHelpStateTransitions(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "hi\n", "[options]\n")
	var states []bool
	e.Config.HelpState = func(open bool) { states = append(states, open) }
	e.createPluginViewports()

	e.performRender() // first push: closed
	e.executeCommand("help_toggle")
	e.performRender() // Quick Help -> open
	e.executeCommand("help_toggle")
	e.performRender() // -> closed

	if len(states) != 3 || states[0] || !states[1] || states[2] {
		t.Fatalf("help-state transitions = %v, want [false true false]", states)
	}
}
