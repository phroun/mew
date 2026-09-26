package wire

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Wire encoding (D22): the socket carries protocol text in both
// directions. This file provides the statement forms that exist only
// on the wire - replies, errors - and the incremental scanner that
// frames statements out of a byte stream using the language's own
// brace/string awareness.

// The names a connection's given objects answer to from the moment it opens:
// its application, its store, and its handle on the display everyone shares.
// The handshake still carries their ids, and naming an id is still how anything
// else the host hands over gets a name; these three are common enough to be
// waiting.
//
// They are session keys, not reserved words: a client that wants one of these
// names for something of its own takes it, and the object it displaced is still
// there under the id the handshake gave.
const (
	AppName   = "app"
	StoreName = "store"
	HostName  = "host"
)

// A DECISION is a question the display is holding open, carried as an object.
//
// Most events are announcements and want nothing back. A few are the display
// saying *I am about to do this* and stopping until the application says whether it
// may: a window asking to close is one, because only the application knows there is
// unsaved work. Such an event carries one extra field, the id of a decision, and
// the application answers it with the verb it already has:
//
//	event window_closing window=17 decision=94
//	do 94 allow
//
// So nothing about events changed. No verb was added and nothing in a client had to
// be taught: `do` was already there. The names are here, in the language, because
// the applications that answer decisions depend on this package and not on the
// host's -- see protocol/decisions.go for what holding one costs the display.
//
// Two words, because every decidable event asks the same thing. Deny is the
// application taking the matter on itself: the window stays open, the refusal is
// not drawn.
const (
	DecisionType  = "decision"
	DecisionField = "decision"

	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// InitVerb names the statement that follows the welcome, saying what objects
// this connection was handed and what each is called:
//
//	welcome version=1 session=6
//	init app=6 store=1099511627777 host=2199023255553
//
// It is a statement of its own because then it needs no marker: every field of
// it is an object, whereas the welcome carries integers that are not ids and
// will carry more as it grows. A display that hands over a fourth object adds
// a field here and every client can already reach it, without one of them
// being taught the name.
//
// The names are the session keys the host bound with Session.RegisterAs, so
// this is a report of what a client can already say rather than a second
// namespace.
const InitVerb = "init"

// Reply reports server-assigned IDs for a request: top-level
// correlation keys plus explicitly surfaced names (D11/D15). Extra
// carries additional raw wire statements a verb wants delivered ahead
// of the reply line (the describe verb's flat vocabulary stream, D24).
type Reply struct {
	IDs   map[string]uint64
	Extra []string

	// Trouble is what the display said went wrong on this batch's behalf without
	// stopping it, gathered from the `trouble` statements that came before the
	// reply line. Empty for a batch nothing had to say about. See TroubleVerb.
	Trouble []Trouble
}

// EncodeReply renders a Reply as a wire statement:
//
//	reply k1=17 wcb=19
//
// Names are sorted for deterministic output.
func EncodeReply(r *Reply) string {
	var sb strings.Builder
	sb.WriteString("reply")
	names := make([]string, 0, len(r.IDs))
	for n := range r.IDs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&sb, " %s=%d", n, r.IDs[n])
	}
	return sb.String()
}

// DecodeReply parses a `reply` statement back into a Reply.
func DecodeReply(stmt *Statement) (*Reply, error) {
	if stmt.Verb != "reply" {
		return nil, fmt.Errorf("not a reply statement: %q", stmt.Verb)
	}
	r := &Reply{IDs: make(map[string]uint64)}
	for _, a := range stmt.Args {
		if a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt {
			return nil, fmt.Errorf("reply %s: expected integer id", a.Name)
		}
		r.IDs[a.Name] = uint64(a.Value.Int)
	}
	return r, nil
}

// TroubleVerb is what the display says about something that went wrong on a
// batch's behalf WITHOUT stopping it.
//
// A refusal is what a batch is answered with instead of a reply. A trouble is not
// that: the statements ran, the objects exist, and something along the way is worth
// the author knowing -- an optional include a bundle could not find, a hint that
// cannot mean what it says. It travels immediately before the reply, so it is part
// of the answer to the batch that caused it rather than news arriving out of the
// blue.
//
// **It is not an event, for the reason answers are not events.** A complaint is
// solicited: it belongs to the statement that provoked it. An event would pass the
// subscription filter and the emission suppression, so an application that had not
// asked to hear about the object would never be told -- and a complaint nobody is
// told is the silence this exists to break.
const TroubleVerb = "trouble"

// A Trouble is one thing that went wrong and did not stop anything.
type Trouble struct {
	// About is what it was about, in the words the statement used: the bundle or
	// source name that was being loaded. Empty where it is about nothing nameable.
	About string

	// Text is the reason, in the words of whoever reported it. Nothing rewrites it
	// on the way: a reason passed through is a reason a programmer can search their
	// own code for.
	Text string
}

// EncodeTrouble renders one as a wire statement:
//
//	trouble about="bundle:papers" text="papers/extra: no such include"
func EncodeTrouble(t Trouble) string {
	var sb strings.Builder
	sb.WriteString(TroubleVerb)
	if t.About != "" {
		sb.WriteString(" about=" + Quote(t.About))
	}
	sb.WriteString(" text=" + Quote(t.Text))
	return sb.String()
}

// DecodeTrouble parses a `trouble` statement back into what it says.
//
// A statement with no `text=` is a complaint with nothing in it, which is refused:
// a reader shown a row with no reason on it is told there is a problem and nothing
// else, which is worse than not being told.
func DecodeTrouble(stmt *Statement) (Trouble, error) {
	if stmt.Verb != TroubleVerb {
		return Trouble{}, fmt.Errorf("not a trouble statement: %q", stmt.Verb)
	}
	var t Trouble
	said := false
	for _, a := range stmt.Args {
		// **Only the two it reads are checked.** An argument this version does not
		// know is a later version saying more, and a client that refused the whole
		// statement over one would stop hearing complaints the day the display
		// learned to say where they came from.
		switch a.Name {
		case "about", "text":
		default:
			continue
		}
		if a.Value == nil || a.Value.Kind != StringValue {
			return Trouble{}, fmt.Errorf("trouble %s: expected a string", a.Name)
		}
		if a.Name == "about" {
			t.About = a.Value.Str
		} else {
			t.Text, said = a.Value.Str, true
		}
	}
	if !said {
		return Trouble{}, fmt.Errorf("trouble: no text=, so it says nothing")
	}
	return t, nil
}

// EncodeError renders a batch failure as a wire statement.
func EncodeError(msg string) string {
	return "error text=" + Quote(msg)
}

// DecodeError parses an `error` statement back into the reason it carries.
//
// **A refusal is what a batch is answered WITH**, in place of the reply that would
// have named what it made. So it is read the same way a reply is, by whoever was
// waiting for one -- there is nothing thrown, nothing to unwind, and no path through
// a program that exists only when something goes wrong.
//
// A refusal with no reason on it is still a refusal, and says so: an answer that
// reached the far end and was turned down tells a reader more than silence does,
// whatever else it manages to say.
func DecodeError(stmt *Statement) (string, error) {
	if stmt.Verb != "error" {
		return "", fmt.Errorf("not an error statement: %q", stmt.Verb)
	}
	for _, a := range stmt.Args {
		if a.Name != "text" {
			continue
		}
		if a.Value == nil {
			break
		}
		return a.Value.Str, nil
	}
	return "a refusal with no reason on it", nil
}

// Scanner frames complete statements out of a stream. "Complete"
// means a newline (or EOF) reached at brace depth zero outside a
// string - the language frames itself; no length prefixes (D22).
type Scanner struct {
	r *bufio.Reader
}

// NewScanner wraps a reader for statement framing.
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{r: bufio.NewReader(r)}
}

// Next returns the text of the next complete statement (possibly
// spanning lines via {} blocks), skipping blank lines and comment
// lines. io.EOF when the stream ends cleanly between statements.
func (s *Scanner) Next() (string, error) {
	var sb strings.Builder
	depth := 0
	inString := false
	escaped := false
	sawContent := false

	for {
		ch, err := s.r.ReadByte()
		if err != nil {
			if err == io.EOF && sawContent && depth == 0 && !inString {
				return sb.String(), nil
			}
			if err == io.EOF && !sawContent {
				return "", io.EOF
			}
			return "", err
		}

		switch {
		case escaped:
			escaped = false
		case inString:
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			case '\n':
				return "", fmt.Errorf("wire: newline inside string")
			}
		case ch == '"':
			inString = true
			sawContent = true
		case ch == '{':
			depth++
			sawContent = true
		case ch == '}':
			depth--
			sawContent = true
		case ch == '#':
			// Comment to end of line; the newline still terminates.
			for {
				c, err := s.r.ReadByte()
				if err != nil || c == '\n' {
					ch = '\n'
					break
				}
			}
			if sawContent && depth == 0 {
				sb.WriteByte('\n')
				return sb.String(), nil
			}
			continue
		case ch == '\n':
			if depth == 0 {
				if sawContent {
					sb.WriteByte('\n')
					return sb.String(), nil
				}
				continue // blank line between statements
			}
		case ch != ' ' && ch != '\t' && ch != '\r' && ch != ';':
			sawContent = true
		}
		sb.WriteByte(ch)
	}
}
