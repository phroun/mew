package trinkets

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// tabClosing is a closable strip built the way the wire builds it, with a
// session that can answer the decisions it puts.
type tabClosing struct {
	t      *testing.T
	tt     *TabTrinket
	s      *protocol.Session
	f      *captureFactory
	events []*protocol.Event
}

func newTabClosing(t *testing.T, listening bool) *tabClosing {
	t.Helper()
	core.SetTextMeasurer(nil)
	c := &tabClosing{t: t, s: protocol.NewSession()}
	ctx := &protocol.BindContext{}
	ctx.Emit = func(ev *protocol.Event) { c.events = append(c.events, ev) }
	ctx.Adopt = func(obj protocol.Object) { c.s.Register(obj) }
	ctx.Drop = func(id uint64) { c.s.Forget(id) }
	c.f = &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	c.run(`s=new tabs closable children={
		a=new tab caption="Grid" children={new panel}
		b=new tab caption="Flex" children={new panel}
		c=new tab caption="Limit" children={new panel}
	} selected=1`)
	for _, tg := range c.f.targets {
		if tw, ok := tg.(*TabTrinket); ok {
			c.tt = tw
		}
	}
	if c.tt == nil {
		t.Fatal("no tab strip was built")
	}
	m := c.tt.EffectiveCellMetrics()
	c.tt.SetBounds(core.UnitRect{Width: 40 * m.UnitsPerCellWidth, Height: 5 * m.UnitsPerCellHeight})
	c.f.Subscribe(trinketID(c.tt), "closed")
	if listening {
		c.f.Subscribe(trinketID(c.tt), "closing")
	}
	return c
}

func (c *tabClosing) run(src string) {
	c.t.Helper()
	script, err := protocol.Parse(src)
	if err != nil {
		c.t.Fatalf("%s: %v", src, err)
	}
	if _, err := c.s.Execute(script, c.f); err != nil {
		c.t.Fatalf("%s: %v", src, err)
	}
}

// press clicks tab i's close button.
func (c *tabClosing) press(i int) {
	c.t.Helper()
	paintCloseGrid(c.t, c.tt)
	for _, sp := range c.tt.stripSpans {
		if sp.owner == i && sp.closeW > 0 {
			clickStrip(c.tt, sp.closeX+sp.closeW/2)
			return
		}
	}
	c.t.Fatalf("tab %d has no close button", i)
}

// said is every event of a type, oldest first, and forgets them.
func (c *tabClosing) said(typ string) []*protocol.Event {
	var out, rest []*protocol.Event
	for _, ev := range c.events {
		if ev.Type == typ {
			out = append(out, ev)
		} else {
			rest = append(rest, ev)
		}
	}
	c.events = rest
	return out
}

func (c *tabClosing) answer(ev *protocol.Event, word string) {
	c.t.Helper()
	id, ok := ev.Uint(protocol.DecisionField)
	if !ok {
		c.t.Fatalf("%s carries no decision", ev.Type)
	}
	c.run("do " + strconv.FormatUint(id, 10) + " " + word)
}

func (c *tabClosing) order() string {
	var names []string
	for i := 0; i < c.tt.Count(); i++ {
		names = append(names, c.tt.TabText(i))
	}
	return strings.Join(names, " ")
}

// A strip nobody asked to hear closing from closes the tab at once, and says
// it closed.
func TestATabNobodyIsAskedAboutClosesAtOnce(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	c := newTabClosing(t, false)
	c.press(0)
	if got := c.order(); got != "Flex Limit" {
		t.Errorf("after the press: %q", got)
	}
	if asked := c.said("closing"); len(asked) != 0 {
		t.Errorf("nobody listening, yet closing went out %d times", len(asked))
	}
	closed := c.said("closed")
	if len(closed) != 1 {
		t.Fatalf("closed went out %d times", len(closed))
	}
	if i, _ := closed[0].Int("index"); i != 0 {
		t.Errorf("closed says index %d, want 0", i)
	}
}

// A strip whose application listens for closing asks it: denied, the tab
// stays and nothing says it closed; allowed, it goes and closed says where it
// stood. The question names the tab, so it is the right one that goes even if
// the tabs moved while it was open.
func TestATabCloseIsTheApplicationsToAllow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	c := newTabClosing(t, true)
	c.press(2)
	asked := c.said("closing")
	if len(asked) != 1 {
		t.Fatalf("closing went out %d times", len(asked))
	}
	if i, _ := asked[0].Int("index"); i != 2 {
		t.Errorf("closing says index %d, want 2", i)
	}
	if c.order() != "Grid Flex Limit" {
		t.Errorf("the tab went before it was allowed: %q", c.order())
	}
	c.answer(asked[0], protocol.DecisionDeny)
	if c.order() != "Grid Flex Limit" || len(c.said("closed")) != 0 {
		t.Errorf("denied, the strip reads %q", c.order())
	}

	c.press(2)
	asked = c.said("closing")
	if len(asked) != 1 {
		t.Fatalf("pressed again, closing went out %d times", len(asked))
	}
	c.tt.MoveTab(2, 0)
	c.answer(asked[0], protocol.DecisionAllow)
	if got := c.order(); got != "Grid Flex" {
		t.Errorf("allowed after the tab moved to the front: %q, want Limit gone", got)
	}
	closed := c.said("closed")
	if len(closed) != 1 {
		t.Fatalf("closed went out %d times", len(closed))
	}
	if i, _ := closed[0].Int("index"); i != 0 {
		t.Errorf("closed says index %d, want 0, where Limit stood when it went", i)
	}
}

// Pressed from the keyboard, the close button asks the same question.
func TestAKeyboardCloseAsksToo(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	c := newTabClosing(t, true)
	c.tt.SetFocus()
	c.tt.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	c.tt.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	asked := c.said("closing")
	if len(asked) != 1 {
		t.Fatalf("closing went out %d times", len(asked))
	}
	if i, _ := asked[0].Int("index"); i != 1 {
		t.Errorf("closing says index %d, want the current tab, 1", i)
	}
}

// A tab already being asked about is not asked about again while the question
// is open, and a question nobody answers leaves the tab where it is -- after
// which pressing again asks again.
func TestAnUnansweredTabCloseKeepsTheTab(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	was := tabCloseDecision
	tabCloseDecision = 20 * time.Millisecond
	t.Cleanup(func() { tabCloseDecision = was })
	c := newTabClosing(t, true)
	c.press(0)
	c.press(0)
	if asked := c.said("closing"); len(asked) != 1 {
		t.Errorf("two presses while the question was open asked %d times", len(asked))
	}
	time.Sleep(100 * time.Millisecond)
	if c.order() != "Grid Flex Limit" || len(c.said("closed")) != 0 {
		t.Errorf("unanswered, the strip reads %q", c.order())
	}
	c.press(0)
	if asked := c.said("closing"); len(asked) != 1 {
		t.Errorf("pressed after the question lapsed, closing went out %d times", len(asked))
	}
}

// An application closes a tab itself by destroying it, and is not told it
// closed, since it did it.
func TestAnApplicationClosesATabByDestroyingIt(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	c := newTabClosing(t, true)
	c.run(`destroy s.b`)
	if got := c.order(); got != "Grid Limit" {
		t.Errorf("after destroying Flex: %q", got)
	}
	if len(c.said("closed")) != 0 || len(c.said("closing")) != 0 {
		t.Error("destroying a tab raised closing or closed")
	}
}
