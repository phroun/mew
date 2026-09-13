// Command kittytk-queryrun puts a query from a file to a running application
// and prints what comes back.
//
// It connects to a display like any other application, and asks the display to
// carry the text to another one:
//
//	ask host relay to="queryapp" text="<the file>"
//
// The display reads none of it. It hands the statements over as a batch and
// sends back every statement that application says in answer, which is what
// arrives here as `relay` events. So this is a wire tap with a file for input
// -- the display has no orchestrated reason to open a query yet, and does not
// need one to carry the question.
//
//	KITTYTK_DEBUG_RELAY=1 kittytk-tui &
//	kittytk-queryrun -to queryapp query.txt
//
// The relay is off unless the display is started with KITTYTK_DEBUG_RELAY set:
// an application that can relay can address another application's objects.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

func main() {
	to := flag.String("to", "", "the application to put the query to, by name")
	endpoint := flag.String("display", "", "display endpoint (default $KITTYTK_DISPLAY)")
	wait := flag.Duration("wait", 5*time.Second, "how long to wait for the answer")
	raw := flag.Bool("raw", false, "print the statements as they crossed, not a table")
	flag.Parse()

	if flag.NArg() != 1 || *to == "" {
		fmt.Fprintln(os.Stderr, "usage: kittytk-queryrun -to <app> <query.txt>")
		flag.PrintDefaults()
		os.Exit(2)
	}
	text, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Refuse a file that will not parse here rather than at the far end, where
	// the only thing that comes back is a refusal with no line in it.
	query := strings.TrimSpace(string(text))
	if _, err := wire.Parse(query); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", flag.Arg(0), err)
		os.Exit(1)
	}

	where := *endpoint
	if where == "" {
		where = client.DefaultEndpoint()
	}
	conn, err := client.Dial(where, "kittytk-queryrun", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()

	lines := make(chan string, 256)
	conn.OnHost(client.EventRelay, func(ev *wire.Event) {
		if s, ok := ev.Text("text"); ok {
			lines <- s
		}
	})
	if err := conn.Host().Ask(fmt.Sprintf("relay to=%s text=%s",
		wire.Quote(*to), wire.Quote(query))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	out := &printer{raw: *raw}
	deadline := time.After(*wait)
	for {
		select {
		case line := <-lines:
			if done := out.take(line); done {
				out.flush()
				return
			}
		case <-deadline:
			out.flush()
			fmt.Fprintf(os.Stderr, "\nnothing more after %s\n", *wait)
			os.Exit(1)
		}
	}
}

// printer turns the statements that came back into rows, or prints them as
// they crossed.
type printer struct {
	raw     bool
	columns []string
	rows    [][]string
	note    string
}

// take reads one statement, and reports whether the answer is over.
func (p *printer) take(line string) bool {
	if p.raw {
		fmt.Println(line)
	}
	script, err := wire.Parse(line)
	if err != nil || len(script.Statements) == 0 {
		return false
	}
	stmt := script.Statements[0]
	switch stmt.Verb {
	case "error":
		if s, ok := argText(stmt, "text"); ok {
			p.note = s
		}
		return true
	case wire.ResultVerb:
	default:
		return false
	}

	var bag wire.Fields
	complete := false
	for _, a := range stmt.Args {
		switch {
		case a.Name == wire.ResultComplete && a.Value == nil:
			complete = true
		case a.Name == "fields" && a.Value != nil:
			bag, _ = wire.ParseFields(a.Value)
		case a.Name == "error" && a.Value != nil:
			p.note = a.Value.Str
		case a.Name == "exhausted" && a.Value == nil:
			p.note = "exhausted: that is every record there is"
		case a.Name == "watermark" && a.Value != nil:
			p.note = "complete up to " + wire.EncodeValue(a.Value)
		}
	}
	if len(bag) > 0 {
		p.add(bag)
	}
	return complete
}

// add folds one record into the table, widening it for a field no earlier
// record had. Records need not carry the same fields: a window may ask for
// fewer than the query does, and a field a record has not got is not an error.
func (p *printer) add(bag wire.Fields) {
	for _, a := range bag {
		if !contains(p.columns, a.Name) {
			p.columns = append(p.columns, a.Name)
			for i := range p.rows {
				p.rows[i] = append(p.rows[i], "")
			}
		}
	}
	row := make([]string, len(p.columns))
	for i, name := range p.columns {
		if v := bag.Get(name); v != nil {
			row[i] = wire.EncodeValue(v)
		}
	}
	p.rows = append(p.rows, row)
}

// flush prints the table, sized to what is in it.
func (p *printer) flush() {
	if !p.raw && len(p.rows) > 0 {
		width := make([]int, len(p.columns))
		for i, name := range p.columns {
			width[i] = len(name)
		}
		for _, row := range p.rows {
			for i, cell := range row {
				if len(cell) > width[i] {
					width[i] = len(cell)
				}
			}
		}
		fmt.Println(rule(p.columns, width))
		fmt.Println(rule(dashes(width), width))
		for _, row := range p.rows {
			fmt.Println(rule(row, width))
		}
	}
	if p.note != "" {
		fmt.Printf("\n%s\n", p.note)
	}
	fmt.Fprintf(os.Stderr, "%d record(s)\n", len(p.rows))
}

func rule(cells []string, width []int) string {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		b.WriteString(strings.Repeat(" ", width[i]-len(cell)))
	}
	return strings.TrimRight(b.String(), " ")
}

func dashes(width []int) []string {
	out := make([]string, len(width))
	for i, w := range width {
		out[i] = strings.Repeat("-", w)
	}
	return out
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func argText(stmt *wire.Statement, name string) (string, bool) {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil && a.Value.Kind == wire.StringValue {
			return a.Value.Str, true
		}
	}
	return "", false
}
