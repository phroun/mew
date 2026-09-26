package trinkets

// `SetOnTrouble(func(Trouble) bool)` answers whether a refusal has been HANDLED,
// and a handled refusal is not drawn. That worked for a Go caller and could not
// work for an application, which reaches this only over the wire and could only
// ever be TOLD. So the per-refusal half of the feature existed for the one caller
// it was not designed for.
//
// The event now carries a DECISION, and `do <id> deny` is an application saying it
// has shown the reader itself. `trouble=false` is still there and is a different
// thing: said once, about every refusal.
//
// The one consequence worth knowing: while an application is being ASKED, the line
// WAITS. A Go handler answers inside took and there is nothing to wait for; an
// application answers a round trip later, and drawing a red line to snatch it back
// a few frames afterwards would be worse than the line arriving a moment late.

import (
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/protocol"
)

// asked is a list on a connection real enough to hold a decision: events land in a
// slice, and what the display mints becomes addressable in the session.
type asked struct {
	t   *testing.T
	l   *ListView
	ctx *protocol.BindContext
	s   *protocol.Session

	events []*protocol.Event
}

func askedList(t *testing.T, src string) *asked {
	t.Helper()
	a := &asked{t: t, s: protocol.NewSession()}
	a.ctx = &protocol.BindContext{}
	a.ctx.Emit = func(ev *protocol.Event) { a.events = append(a.events, ev) }
	a.ctx.Adopt = func(obj protocol.Object) { a.s.Register(obj) }
	a.ctx.Drop = func(id uint64) { a.s.Forget(id) }
	a.ctx.Subscribe(0, "")

	f := &captureFactory{inner: protocol.NewRegistryFactory(a.ctx)}
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := a.s.Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	l, _ := f.targets[0].(*ListView)
	if l == nil {
		t.Fatalf("built a %T", f.targets[0])
	}
	a.l = l
	return a
}

// decision reads the id the question about the refusal went out carrying.
func (a *asked) decision() uint64 {
	a.t.Helper()
	for _, ev := range a.events {
		if ev.Type != "trouble" {
			continue
		}
		if id, ok := ev.Uint(protocol.DecisionField); ok {
			return id
		}
		a.t.Fatalf("the refusal carries no %s=: %s", protocol.DecisionField, ev.Encode())
	}
	a.t.Fatal("no trouble event went out")
	return 0
}

func (a *asked) say(src string) error {
	a.t.Helper()
	script, err := protocol.Parse(src)
	if err != nil {
		a.t.Fatalf("parse %q: %v", src, err)
	}
	_, err = a.s.Execute(script, protocol.NewRegistryFactory(a.ctx))
	return err
}

func (a *asked) drewALine() bool {
	a.t.Helper()
	a.l.SetBounds(a.l.Bounds())
	return drewALine(a.t, a.l)
}

// **The refusal goes out carrying a decision**, so an application has something to
// answer rather than something to note.
func TestARefusalCarriesADecision(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})

	a.l.Item(0)
	if _, ok := a.s.Object(a.decision()); !ok {
		t.Error("the refusal named a decision no statement can reach")
	}
}

// **While the application is being asked, the line waits.** Not drawn and then
// taken away: a red line appearing and vanishing for every refusal an application
// handles is worse than one that arrives a moment after the refusal did.
func TestTheLineWaitsWhileTheApplicationDecides(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})

	a.l.Item(0)
	if !a.l.Trouble().Any() {
		t.Fatal("it was refused and holds nothing")
	}
	if a.drewALine() {
		t.Error("the line was drawn while the application was still deciding, so denying it now takes it away again")
	}
}

// `do <id> deny` and the line never appears: the application has shown the reader
// itself, and that is what it was asked.
func TestADeniedRefusalIsNotDrawn(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})
	a.l.Item(0)

	if err := a.say("do " + itoa(a.decision()) + " " + protocol.DecisionDeny); err != nil {
		t.Fatalf("denying: %v", err)
	}
	if a.drewALine() {
		t.Error("the application said it had shown this refusal and the line was drawn anyway")
	}
	if !a.l.Trouble().Any() {
		t.Error("the view forgot the refusal; it is handled, not untrue")
	}
	// And the view still says it draws its own refusals: this was about ONE of
	// them, and `trouble=` is the standing answer.
	if !a.l.ShowsTrouble() {
		t.Error("taking one refusal turned the line off for every future one")
	}
}

// `do <id> allow` and it is drawn: the application heard about it and would rather
// the reader saw the view's own line.
func TestAnAllowedRefusalIsDrawn(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})
	a.l.Item(0)

	if err := a.say("do " + itoa(a.decision()) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing: %v", err)
	}
	if !a.drewALine() {
		t.Error("the application allowed the line and it was not drawn")
	}
}

// **Nobody having answered draws the line**, which is the safe half: an application
// that asked to decide and then said nothing has not shown the reason anywhere, and
// the reason is the whole point of the line existing.
func TestARefusalNobodyDecidedIsDrawn(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})
	a.l.Item(0)

	if a.drewALine() {
		t.Fatal("the line was drawn before the deadline")
	}
	a.ctx.Undecided() // what a connection going does, and what a deadline does
	if !a.drewALine() {
		t.Error("nobody decided and the reader was left with an empty area and no reason")
	}
}

// **A verdict about a refusal that is no longer true changes nothing.** A view that
// was refused, asked, and then answered with records has nothing to draw a line
// about, and a late `allow` must not bring the old one back.
func TestAVerdictAboutAnOldRefusalIsIgnored(t *testing.T) {
	sulky := &sulkySource{why: "not yet", rows: 20, then: plainRows(20)}
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(sulky)

	a.l.Item(0)
	stale := a.decision()

	sulky.relent = true
	if got := a.l.Item(0); got == nil || got.Text != "row00000" {
		t.Fatalf("the records did not arrive: %v", got)
	}
	if a.l.Trouble().Any() {
		t.Fatal("records arrived and the view still holds a refusal")
	}

	if err := a.say("do " + itoa(stale) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("the late answer: %v", err)
	}
	if a.drewALine() {
		t.Error("a verdict about a refusal that had gone away drew a line for it")
	}
}

// **Two refusals can be in flight, and an answer belongs to the one it was about.**
// A view refused twice in a row asks twice, and the first answer arriving after the
// second question must not decide the second question -- which is the whole reason
// a verdict names the refusal it is about rather than just landing on the view.
func TestAnAnswerDecidesTheRefusalItWasAbout(t *testing.T) {
	sulky := &sulkySource{why: "the first refusal", rows: 20}
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(sulky)

	a.l.Item(0)
	first := a.decision()

	// A second refusal, asked about before the first was answered.
	sulky.why = "the second refusal"
	a.events = nil
	a.l.Item(0)
	if second := a.decision(); second == first {
		t.Fatalf("both refusals share decision %d", second)
	}
	if got := a.l.Trouble().Reason; got != "the second refusal" {
		t.Fatalf("the view holds %q, want the second refusal", got)
	}

	// And now the FIRST is denied, which says nothing about the second.
	if err := a.say("do " + itoa(first) + " " + protocol.DecisionDeny); err != nil {
		t.Fatalf("denying the first: %v", err)
	}
	// So the second is still being decided, and gets its line when nobody does.
	a.ctx.Undecided()
	if !a.drewALine() {
		t.Error("an answer about the first refusal took the second one off the view's hands")
	}
}

// **The line arrives when the deadline passes.** A close waits as long as it takes
// because a person may be answering; this does not, because an application deciding
// whether it recognises a refusal has everything it needs the moment it is asked --
// and a reader left with an empty area and no reason is what the line exists for.
func TestTheLineArrivesWhenTheDeadlinePasses(t *testing.T) {
	a := askedList(t, `lv=new listview`)
	// The deadline's verdict is caught on its way to the connection's thread, so
	// this reads it rather than racing it.
	posted := make(chan func(), 1)
	a.ctx.Post = func(fn func()) { posted <- fn }

	a.l.SetSource(&shutSource{why: "no such source: flies"})
	a.l.Item(0)
	if a.drewALine() {
		t.Fatal("the line was drawn before anybody had a chance to take the refusal")
	}

	select {
	case fn := <-posted:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("the deadline never passed, so a silent application hides the reason for ever")
	}
	if !a.drewALine() {
		t.Error("the deadline passed and the line did not arrive")
	}
}

// decidedTrouble reports whether there is anything DIFFERENT to draw, because its
// caller redraws on that answer. A view that draws no line has nothing new to show
// whatever is decided about one.
func TestAVerdictSaysWhetherThereIsAnythingNewToDraw(t *testing.T) {
	refused := Trouble{Reason: "no such source: flies", At: -1}

	var quiet troubled
	quiet.quiet = true
	quiet.announce = func(Trouble) bool { return true }
	quiet.took(refused)
	if quiet.decidedTrouble(refused, false) {
		t.Error("a view that draws no line asked to be redrawn for one")
	}

	var drawing troubled
	drawing.announce = func(Trouble) bool { return true }
	drawing.took(refused)
	if !drawing.decidedTrouble(refused, false) {
		t.Error("the line appeared and nothing asked for a redraw, so it appears whenever something else happens to")
	}
	if drawing.decidedTrouble(refused, false) {
		t.Error("a decision already settled asked for another redraw")
	}
}

// A second refusal is a second question. Permission is not kept, and neither is a
// denial: an application that handled the first one is asked about the next.
func TestEachRefusalIsItsOwnQuestion(t *testing.T) {
	sulky := &sulkySource{why: "the first refusal", rows: 20}
	a := askedList(t, `lv=new listview`)
	a.l.SetSource(sulky)

	a.l.Item(0)
	first := a.decision()
	if err := a.say("do " + itoa(first) + " " + protocol.DecisionDeny); err != nil {
		t.Fatalf("denying the first: %v", err)
	}

	sulky.why = "a refusal it does not know"
	a.events = nil
	a.l.Item(0)
	second := a.decision()
	if second == first {
		t.Fatalf("the second refusal reused decision %d, so the first answer would decide it", second)
	}
	// Unanswered, and the line comes for it.
	a.ctx.Undecided()
	if !a.drewALine() {
		t.Error("the second refusal was not drawn, and nobody said they had it")
	}
}

// `trouble=false` is the standing answer and is not a question at all: an
// application that wants no line ever is not asked about each one.
func TestAViewToldToKeepQuietAsksAnyway(t *testing.T) {
	a := askedList(t, `lv=new listview trouble=false`)
	a.l.SetSource(&shutSource{why: "no such source: flies"})
	a.l.Item(0)

	// It is still TOLD -- the event is how an application learns a source name is
	// wrong, and that is worth knowing whether or not a line is wanted.
	if got := eventsOfType(a.events, "trouble"); len(got) != 1 {
		t.Fatalf("a quiet view raised %d trouble events", len(got))
	}
	// And whatever it answers, no line: the standing answer outranks the one about
	// a single refusal.
	if err := a.say("do " + itoa(a.decision()) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing: %v", err)
	}
	if a.drewALine() {
		t.Error("trouble=false was overruled by an allow about one refusal")
	}
}

// The vocabulary an application is told to answer in is the one it can answer in.
// `describe` publishes the event's own field, and the type's actions behind it.
func TestTheRefusalsDecisionIsDescribed(t *testing.T) {
	for _, typeName := range []string{"listview", "treeview"} {
		var found bool
		for _, ti := range protocol.DescribeVocabulary().Types {
			if ti.Name != typeName {
				continue
			}
			for _, ev := range ti.Events {
				if ev.Name != "trouble" {
					continue
				}
				for _, f := range ev.Fields {
					if f.Name == protocol.DecisionField {
						found = true
						if !strings.Contains(f.Doc, protocol.DecisionDeny) {
							t.Errorf("%s: the field does not say what denying does", typeName)
						}
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: a trouble event carries a decision and does not describe it, so an application cannot know to answer", typeName)
		}
	}
	if !protocol.TypeDoes(protocol.DecisionType, protocol.DecisionDeny) {
		t.Error("a decision does not declare deny, so the statement that denies one is refused")
	}
}
