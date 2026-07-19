package protocol

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
		r.IDs[a.Name] = uint64(a.Value.Number)
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
