package main

// Answering the query here, out of a PSL file.
//
// The same query text, the same structure taken off it, and the same table
// printed at the end -- but nothing is dialled and nobody is asked. A source
// that holds its own records answers the questions an application would have
// been asked, which is the whole of what makes the two interchangeable.

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/source"
	"github.com/phroun/kittytk/wire"
)

// readings names the two ways a PSL record's contents can be addressed.
var readings = map[string]source.Reading{
	"whole":   source.Whole,
	"members": source.Members,
}

// fromPSL runs a query file against a PSL file and prints what comes back.
func fromPSL(path, reading, query string, raw bool) {
	which, ok := readings[reading]
	if !ok {
		fail("no reading called %q: whole or members", reading)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	src, err := source.ParsePSL(string(text), which)
	if err != nil {
		fail("%s: %v", path, err)
	}

	script, err := wire.Parse(query)
	if err != nil {
		fail("%v", err)
	}
	out := &printer{raw: raw}
	if err := run(src, script, out); err != nil {
		out.flush()
		fail("%v", err)
	}
	out.flush()
}

// run drives the source through the statements the file holds, which are the
// ones a display would have sent.
func run(src source.Source, script *wire.Script, out *printer) error {
	var set source.ResultSet
	defer func() {
		if set != nil {
			set.Close()
		}
	}()

	for _, stmt := range script.Statements {
		switch stmt.Verb {
		case "end":
			continue

		case "new":
			if len(stmt.Args) == 0 || stmt.Args[0].Name != wire.QueryVerb {
				return fmt.Errorf("new: expected a query")
			}
			args := stmt.Args[1:]
			spec, err := wire.ParseSpec(args)
			if err != nil {
				return err
			}
			if set != nil {
				set.Close()
			}
			if set, err = src.Open(spec); err != nil {
				return err
			}
			// Opening carries the first window, because a display never wants a
			// sequence without wanting rows of it.
			if err := window(set, args, out); err != nil {
				return err
			}

		case wire.QueryVerb:
			if set == nil {
				return fmt.Errorf("query: nothing has been opened")
			}
			if err := window(set, afterTarget(stmt), out); err != nil {
				return err
			}

		case "set":
			return fmt.Errorf("set: a query is the sequence it was opened " +
				"with; a different sequence is another `new query`")

		case "destroy":
			if set != nil {
				set.Close()
				set = nil
			}

		default:
			return fmt.Errorf("%s: a query file holds new, query and destroy", stmt.Verb)
		}
	}
	return nil
}

// window draws one and writes it out as the statements it would have crossed
// as, so what is printed comes off the wire language either way.
func window(set source.ResultSet, args []*wire.Arg, out *printer) error {
	f, err := wire.ParseFill(args)
	if err != nil {
		return err
	}
	return set.Fill(f, &results{out: out})
}

// results writes each record as the result statement that carries one, and the
// terminator when the stretch ends.
type results struct{ out *printer }

// Ordered leads the answer, in the spelling the wire leads one with.
func (r *results) Ordered() { r.out.take(result(&wire.Arg{Name: "ordered", Flag: wire.FlagTrue})) }

func (r *results) Done(done source.Complete) { r.out.take(terminator(done)) }

// Record and Subset write a record out under the word that says how much of it
// came back: `record` for every field it has, `fields` for the ones this
// window asked for.
func (r *results) Record(key *wire.Value, fields wire.Fields) error {
	return r.write(wire.RecordArg, key, fields)
}

func (r *results) Subset(key *wire.Value, fields wire.Fields) error {
	return r.write(wire.FieldsArg, key, fields)
}

func (r *results) write(what string, key *wire.Value, fields wire.Fields) error {
	bag := make(wire.Fields, 0, len(fields)+1)
	bag = append(bag, &wire.Arg{Name: wire.KeyField, Value: key})
	bag = append(bag, fields...)
	r.out.take(result(&wire.Arg{Name: what, Value: bag.Block()}))
	return nil
}

// terminator is what ends the window, in the spelling the wire ends one with.
func terminator(done source.Complete) string {
	args := []*wire.Arg{{Name: wire.ResultComplete, Flag: wire.FlagTrue}}
	switch {
	case done.Exhausted:
		args = append(args, &wire.Arg{Name: "exhausted", Flag: wire.FlagTrue})
	case len(done.Watermark) > 0:
		args = append(args, &wire.Arg{Name: "watermark", Value: done.Watermark.Block()})
	}
	return result(args...)
}

// result builds one result statement. Everything here is one query, so it is
// addressed as the first one an application would have named.
func result(extra ...*wire.Arg) string {
	args := append([]*wire.Arg{{Value: wire.NewInt(1)}}, extra...)
	return wire.EncodeStatement(&wire.Statement{Verb: wire.ResultVerb, Args: args})
}

// afterTarget drops the object a statement is addressed to, leaving what it
// says about it. There is one query here, so which one it names does not have
// to be resolved -- only skipped.
func afterTarget(stmt *wire.Statement) []*wire.Arg {
	if len(stmt.Args) == 0 {
		return nil
	}
	a := stmt.Args[0]
	if a.Value != nil && a.Name == "" && a.Value.Kind == wire.NumberValue {
		return stmt.Args[1:]
	}
	if a.Value == nil && a.Flag == wire.FlagTrue {
		return stmt.Args[1:]
	}
	return stmt.Args
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
