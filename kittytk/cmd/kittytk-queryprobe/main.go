// Command kittytk-queryprobe stands in for a display, so an application that
// serves a query has something to serve.
//
// A display is the end that opens a query: it is the one that knows a list
// needs rows, and what sort and filter the person looking at it asked for. No
// display does that yet, so nothing would ever ask an application anything and
// an example would sit idle. This asks.
//
// It listens, does the handshake, opens one query against a named source, and
// prints what comes back -- the statements as they crossed, so what is on
// screen is the protocol rather than a rendering of it.
//
//	kittytk-queryprobe &
//	KITTYTK_DISPLAY=/tmp/kittytk-queryprobe.sock go run ./examples/queryapp
//
// It is a probe, not a display: it draws nothing, holds no records, and knows
// nothing about what a watermark is for.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/phroun/kittytk/wire"
)

func main() {
	listen := flag.String("listen", "/tmp/kittytk-queryprobe.sock", "unix socket to listen on")
	source := flag.String("source", "files", "the source to open a query against")
	sortBy := flag.String("sort", "name natural", "the sort, as wire text inside the braces")
	filter := flag.String("filter", "", "the filter, as wire text inside the braces")
	fields := flag.String("fields", "", "the fields to ask for, as wire text inside the braces")
	need := flag.Int("need", 10, "how many rows the window is")
	more := flag.Int("more", 0, "ask for a second window of this many rows, past the first")
	resort := flag.String("resort", "", "restate the sort afterwards, as wire text inside the braces")
	wait := flag.Duration("wait", 3*time.Second, "how long to wait for an answer")
	flag.Parse()

	os.Remove(*listen)
	ln, err := net.Listen("unix", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	defer os.Remove(*listen)
	fmt.Printf("# listening. run the application with:\n#   KITTYTK_DISPLAY=%s\n\n", *listen)

	nc, err := ln.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, "accept:", err)
		os.Exit(1)
	}
	defer nc.Close()

	p := &probe{nc: nc, scanner: wire.NewScanner(nc), deadline: *wait}
	if err := p.handshake(); err != nil {
		fmt.Fprintln(os.Stderr, "handshake:", err)
		os.Exit(1)
	}

	// Open the query and ask for its first window in one statement, because a
	// display never wants a sequence without wanting rows of it.
	spec := fmt.Sprintf("source=%s", wire.Quote(*source))
	if *fields != "" {
		spec += " fields={ " + *fields + " }"
	}
	if *filter != "" {
		spec += " filter={ " + *filter + " }"
	}
	if *sortBy != "" {
		spec += " sort={ " + *sortBy + " }"
	}
	p.say(fmt.Sprintf("q=new query %s have=0 need=%d", spec, *need))
	id, ok := p.collect(true)
	if !ok {
		os.Exit(1)
	}

	// A second window, starting from where the first one left off. The probe
	// holds no records, so it asks from the last key it saw.
	if *more > 0 {
		from := ""
		if p.lastRow != "" {
			from = " from=" + p.lastRow
		}
		p.say(fmt.Sprintf("query %d%s have=0 need=%d", id, from, *more))
		p.collect(true)
	}

	// Restating the sort is a new generation of the same query, not a new one.
	if *resort != "" {
		p.say(fmt.Sprintf("set %d sort={ %s }", id, *resort))
		p.collect(false)
		p.say(fmt.Sprintf("query %d have=0 need=%d", id, *need))
		p.collect(true)
	}

	// And letting it go is how the application learns it may drop what it was
	// holding, which is the half of a lifetime that is easy to forget.
	p.say(fmt.Sprintf("destroy %d", id))
	p.collect(false)
}

type probe struct {
	nc       net.Conn
	scanner  *wire.Scanner
	deadline time.Duration
	lastRow  string
}

// handshake is the two statements a display sends before anything else: what
// version it speaks, and what it has handed this connection.
func (p *probe) handshake() error {
	hello, err := p.scanner.Next()
	if err != nil {
		return err
	}
	script, err := wire.Parse(hello)
	if err != nil || len(script.Statements) == 0 || script.Statements[0].Verb != "hello" {
		return fmt.Errorf("expected hello, got %q", strings.TrimSpace(hello))
	}
	p.trace("<-", strings.TrimSpace(hello))
	if _, err := p.scanner.Next(); err != nil { // the `end` that closes it
		return err
	}
	// The handshake is two statements, not a batch: nothing replies to them.
	p.line("welcome version=1 session=1")
	p.line("init app=1 store=2 host=3")
	return nil
}

// line writes one statement on its own, the way a display writes an event.
func (p *probe) line(src string) {
	p.trace("->", src)
	fmt.Fprint(p.nc, src+"\n")
}

// say writes one batch -- terminated, and so answered.
func (p *probe) say(src string) {
	for _, line := range strings.Split(src, "\n") {
		p.trace("->", line)
	}
	fmt.Fprint(p.nc, src+"\nend\n")
}

// collect reads the answer to the batch just sent, printing every statement as
// it crossed. withResults says whether records are expected to follow the
// reply: opening or refilling a query answers with both, a set or a destroy
// with a reply alone.
//
// Anything the application asks for of its own is replied to, because a batch
// is answered by the end that received it -- and an end waiting for a reply
// has to keep reading, or two ends wait on each other forever.
func (p *probe) collect(withResults bool) (uint64, bool) {
	var id uint64
	var inbound []string
	replied := false
	_ = p.nc.SetReadDeadline(time.Now().Add(p.deadline))
	defer p.nc.SetReadDeadline(time.Time{})

	for {
		text, err := p.scanner.Next()
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nread:", err)
			return id, false
		}
		trimmed := strings.TrimSpace(text)
		p.trace("<-", trimmed)
		script, err := wire.Parse(text)
		if err != nil || len(script.Statements) == 0 {
			continue
		}
		stmt := script.Statements[0]
		switch stmt.Verb {
		case "reply":
			replied = true
			if r, err := wire.DecodeReply(stmt); err == nil {
				for _, v := range r.IDs {
					id = v
				}
			}
			// A set or a destroy answers with a reply and nothing after it;
			// the `end` that closes the batch still has to be read.
		case "error":
			return id, false
		case "result":
			p.noteRow(stmt)
			if flagged(stmt, "complete") {
				return id, replied
			}
		case "end":
			// A batch the application sent of its own: answer it, the way a
			// display answers everything it receives.
			if len(inbound) > 0 {
				fmt.Fprint(p.nc, "reply\nend\n")
				p.trace("->", "reply")
				inbound = nil
				continue
			}
			if replied && !withResults {
				return id, true
			}
		default:
			inbound = append(inbound, trimmed)
		}
	}
}

// noteRow remembers the last record, so the next window can ask from it.
//
// A boundary is a record's position -- its sort fields AND its key -- not the
// key alone: without the sort fields there is nothing to compare against the
// levels, and the boundary lands at the very start of the sequence. Passing
// the whole record is the simplest thing that is right, and harmless: a
// boundary is looked up by name, so fields the sort does not mention are
// ignored at the far end.
func (p *probe) noteRow(stmt *wire.Statement) {
	for _, a := range stmt.Args {
		if a.Name == "fields" && a.Value != nil {
			p.lastRow = wire.EncodeValue(a.Value)
		}
	}
}

func flagged(stmt *wire.Statement, name string) bool {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value == nil && a.Flag == wire.FlagTrue {
			return true
		}
	}
	return false
}

func (p *probe) trace(dir, line string) {
	if line == "" {
		return
	}
	fmt.Printf("%s %s\n", dir, line)
}
