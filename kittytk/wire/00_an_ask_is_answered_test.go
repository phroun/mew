package wire

// The third pair, read and written.
//
// `query` is answered by `result`, `sub` by `event`, and `ask` by `answer` -- which
// was named when `ask` was added and left unbuilt, with the reasons written down in
// that commit. So the assertions here are about the two things it fixes and the one
// thing that decided its spelling.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// answerOf writes an answer, parses the statement back, and hands over what came
// out -- so nothing here can pass by agreeing with itself about a struct.
func answerOf(t *testing.T, a *Answer) *Answer {
	t.Helper()
	text := AnswerVerb
	for _, arg := range a.Args() {
		text += " " + EncodeArg(arg)
	}
	script, err := Parse(text)
	if err != nil {
		t.Fatalf("%s does not parse: %v", text, err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("%s is %d statements", text, len(script.Statements))
	}
	if got := script.Statements[0].Verb; got != AnswerVerb {
		t.Fatalf("%s reads as a %q", text, got)
	}
	got, err := ParseAnswer(script.Statements[0].Args)
	if err != nil {
		t.Fatalf("%s is not an answer: %v", text, err)
	}
	return got
}

// An answer says which question it is answering, carries the question's own words,
// and can end itself -- all three on one statement, so an answer of one piece
// crosses as a single line.
func TestAnAnswerCarriesItsQuestionsOwnWords(t *testing.T) {
	got := answerOf(t, &Answer{
		To: "a1",
		Carries: []*Arg{
			{Name: IDArg, Value: NewInt(42)},
			{Name: "how", Value: &Value{Kind: WordValue, Word: "altered"}},
			Named("note", "a reason"),
		},
		Complete: true,
	})

	if got.To != "a1" {
		t.Errorf("it answers %q, want the key the ask was given", got.To)
	}
	if !got.Complete {
		t.Error("it does not end the question")
	}
	if got.Error != "" {
		t.Errorf("it carries the error %q", got.Error)
	}
	// The payload comes back whole and in order, and the envelope is not in it.
	if len(got.Carries) != 3 {
		t.Fatalf("it carries %d arguments, want the three that were put in", len(got.Carries))
	}
	for _, name := range []string{ToArg, ResultComplete} {
		if got.Arg(name) != nil {
			t.Errorf("the envelope's %q is among the question's own arguments", name)
		}
	}
	if arg := got.Arg("how"); arg == nil || arg.Value == nil || arg.Value.Word != "altered" {
		t.Errorf("`how` reads %v", arg)
	}
}

// **This is why the correlation is named rather than a bare operand.**
//
// `result` puts its query id bare, and gets away with it because a query id is a
// NUMBER: nothing else in the statement can be mistaken for one. A correlation key
// is a name, so `answer complete` would read the terminator as the question it was
// answering -- and an answer that ends a question nobody asked would be dropped,
// silently, leaving whoever did ask waiting forever.
func TestATerminatorIsNotMistakenForAQuestion(t *testing.T) {
	got := answerOf(t, &Answer{Complete: true})
	if !got.Complete {
		t.Error("a bare terminator does not end the question")
	}
	if got.To != "" {
		t.Errorf("it says it answers %q, which is the terminator read as a key", got.To)
	}
}

// An UNKEYED ask is answered without a correlation, which keeps every ask written
// before this verb existed parsing, and suits a question whose asker is not waiting
// for the answer.
func TestAnUnkeyedAskIsAnsweredWithoutOne(t *testing.T) {
	got := answerOf(t, &Answer{
		Carries:  []*Arg{Named("count", "0")},
		Complete: true,
	})
	if got.To != "" {
		t.Errorf("it invented the key %q", got.To)
	}
	if !got.Complete || got.Arg("count") == nil {
		t.Error("the rest of the answer did not survive having no key")
	}
}

// **A refusal is still an answer**, and rides the same verb. The question was put,
// so it has an answer; an asker waiting for one must not wait forever because the
// answer happened to be no.
func TestARefusalIsStillAnAnswer(t *testing.T) {
	got := answerOf(t, &Answer{
		To: "a1", Error: "no question called amendments", Complete: true,
	})
	if got.Error != "no question called amendments" {
		t.Errorf("the refusal reads %q", got.Error)
	}
	if !got.Complete {
		t.Error("a refusal does not end the question, so the asker keeps waiting")
	}
}

// A record on an answer is spelled the way a result spells one, so "a record's
// identity" has one spelling in the language rather than one per verb.
func TestARecordOnAnAnswerIsSpelledAsAResultsIs(t *testing.T) {
	for _, c := range []struct {
		what  string
		arg   string
		whole bool
	}{
		{"a whole record", RecordArg, true},
		{"some of one", FieldsArg, false},
	} {
		fields := serval.Record{serval.Named("kind", "Archive")}
		got := answerOf(t, &Answer{
			To: "a1",
			Carries: []*Arg{
				{Name: IDArg, Value: NewInt(7)},
				{Name: c.arg, Value: &Value{Kind: BlockValue, Block: asScript(fields)}},
			},
		})
		id, back, whole, ok := got.Record()
		if !ok {
			t.Errorf("%s did not read back as a record", c.what)
			continue
		}
		if id == nil || !id.IsInt || id.Int != 7 {
			t.Errorf("%s carries the identity %v", c.what, id)
		}
		if whole != c.whole {
			t.Errorf("%s says whole=%v", c.what, whole)
		}
		if v := back.Get("kind"); v == nil || !serval.Equal(v, serval.NewText("Archive")) {
			t.Errorf("%s carries %v", c.what, v)
		}
	}

	// An answer that carries no record says so, rather than an empty one -- a
	// question answering with something other than records is the ordinary case.
	plain := answerOf(t, &Answer{To: "a1", Carries: []*Arg{Named("count", "2")}})
	if _, _, _, ok := plain.Record(); ok {
		t.Error("an answer with no record claims to carry one")
	}
}

// The reserved names are refused rather than taken for the question's own, because
// a payload using one would mean two different things by the same argument.
func TestTheEnvelopesNamesAreNotTheQuestionsToUse(t *testing.T) {
	for _, bad := range []string{
		`answer to=1`,
		`answer to="a1"`,
		`answer complete="yes"`,
		`answer error=nothing`,
	} {
		script, err := Parse(bad)
		if err != nil {
			continue // the grammar turned it away first, which is also a refusal
		}
		if _, err := ParseAnswer(script.Statements[0].Args); err == nil {
			t.Errorf("%s was read as an answer", bad)
		}
	}
}

// The readers answer for an argument of the kind asked for, and for nothing else.
//
// **"It is there, in another kind" is not an answer.** An asker reading `size=`
// wants a size, and a word where a number was expected is a question answered
// wrongly rather than a number to be salvaged -- so each reader turns it away the
// way an event's does, which is what lets a question move off events without its
// answer being read differently.
func TestAnAnswersArgumentsAreReadByKind(t *testing.T) {
	got := answerOf(t, &Answer{
		To: "a1",
		Carries: []*Arg{
			{Name: "size", Value: NewInt(1024)},
			{Name: "under", Value: NewInt(-3)},
			{Name: "key", Value: &Value{Kind: StringValue, Str: "notes"}},
			{Name: "type", Value: &Value{Kind: WordValue, Word: "txt"}},
			{Name: "last", Flag: FlagTrue},
			{Name: "cached", Flag: FlagFalse},
			Blob("data", []byte{0, 1, 0xff}),
		},
		Complete: true,
	})

	if v, ok := got.Int("size"); !ok || v != 1024 {
		t.Errorf("size reads %d (%v)", v, ok)
	}
	if v, ok := got.Uint("size"); !ok || v != 1024 {
		t.Errorf("size reads %d as unsigned (%v)", v, ok)
	}
	// A negative number is an Int and is NOT a Uint: a reader that took it would
	// hand back an enormous count for a figure that was below zero.
	if v, ok := got.Int("under"); !ok || v != -3 {
		t.Errorf("under reads %d (%v)", v, ok)
	}
	if _, ok := got.Uint("under"); ok {
		t.Error("a negative number was read as unsigned")
	}
	if v, ok := got.Text("key"); !ok || v != "notes" {
		t.Errorf("key reads %q (%v)", v, ok)
	}
	if v, ok := got.Word("type"); !ok || v != "txt" {
		t.Errorf("type reads %q (%v)", v, ok)
	}
	if got.Flag("last") != FlagTrue {
		t.Errorf("last reads %v", got.Flag("last"))
	}
	// A NEGATED flag is not an asserted one, which is the whole reason a flag has
	// three states rather than being present or absent.
	if got.Flag("cached") != FlagFalse {
		t.Errorf("a negated flag reads %v", got.Flag("cached"))
	}
	// Bytes come back as bytes, NULs and all.
	if b, ok := got.Blob("data"); !ok || string(b) != string([]byte{0, 1, 0xff}) {
		t.Errorf("data reads %v (%v)", b, ok)
	}

	// And each reader turns away the kinds that are not its own.
	if _, ok := got.Int("key"); ok {
		t.Error("a string was read as a number")
	}
	if _, ok := got.Text("type"); ok {
		t.Error("a word was read as a string")
	}
	if _, ok := got.Word("key"); ok {
		t.Error("a string was read as a word")
	}
	if _, ok := got.Text("last"); ok {
		t.Error("a flag was read as a string")
	}
	if got.Flag("size") != FlagNone {
		t.Error("an argument carrying a value was read as a flag")
	}

	// An argument the answer has not got reads as absent rather than as empty.
	if _, ok := got.Text("hash"); ok {
		t.Error("an absent argument read as a string")
	}
	if got.Flag("hash") != FlagNone {
		t.Error("an absent flag did not read as unsaid")
	}

	// The envelope is not among them: `to` and `complete` are the wire's, so a
	// question whose own vocabulary used those words could not be answered.
	if _, ok := got.Word(ToArg); ok {
		t.Error("the correlation key is readable as one of the question's arguments")
	}
	if got.Flag(ResultComplete) != FlagNone {
		t.Error("the terminator is readable as one of the question's arguments")
	}
}

// --- the shared corpus ----------------------------------------------------

// testdata/answer.wire is the text three client libraries read. What is agreed
// there is the ENVELOPE -- which question, whether this is the last piece, whether
// it is a refusal -- and that the payload survives untouched and in order, since no
// implementation reads it.
//
// The Go side answers it here; `c/interop` and `python/tests` answer the same file.
func TestTheAnswerCorpusIsAnswered(t *testing.T) {
	path := filepath.Join("..", "testdata", "answer.wire")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var in string
	var line int
	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		verb, rest, _ := strings.Cut(text, " ")
		switch verb {
		case AnswerVerb:
			if in != "" {
				t.Fatalf("%s:%d: a case with no answer", path, line)
			}
			in, line = text, i+1
		case "want", "bad":
			if in == "" {
				t.Fatalf("%s:%d: an answer with no case", path, i+1)
			}
			got, err := answerFrom(in)
			if verb == "bad" {
				if err == nil {
					t.Errorf("%s:%d: %s was read as an answer", path, line, in)
				}
				in = ""
				continue
			}
			if err != nil {
				t.Errorf("%s:%d: %s: %v", path, line, in, err)
				in = ""
				continue
			}
			// Written back from the structure, so a part dropped on the way in is a
			// part missing on the way out.
			var sb strings.Builder
			for i, arg := range got.Args() {
				if i > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(EncodeArg(arg))
			}
			if sb.String() != rest {
				t.Errorf("%s:%d: %s\n  reads back as %s\n  want          %s",
					path, line, in, sb.String(), rest)
			}
			in = ""
		default:
			t.Fatalf("%s:%d: %q is not a corpus line", path, i+1, verb)
		}
	}
	if in != "" {
		t.Fatalf("%s:%d: a case with no answer", path, line)
	}
}

// answerFrom parses one corpus line.
func answerFrom(text string) (*Answer, error) {
	script, err := Parse(text)
	if err != nil {
		return nil, err
	}
	if len(script.Statements) != 1 {
		return nil, errCorpus
	}
	return ParseAnswer(script.Statements[0].Args)
}

var errCorpus = errAnswerCorpus{}

type errAnswerCorpus struct{}

func (errAnswerCorpus) Error() string { return "not one statement" }
