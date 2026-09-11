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

// EncodeError renders a batch failure as a wire statement.
func EncodeError(msg string) string {
	return "error text=" + Quote(msg)
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
