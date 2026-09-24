package gui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestTheWindowNeverAsksTheFlushingDegradedQuery pins which of the two health
// queries this package may call.
//
// A structural test, and it earns its keep because the behavioural one is not
// available here. Every test in this package supplies `degraded` as a closure
// that answers instantly, so swapping the wiring back to logging.Degraded — the
// one that submits a flush barrier and permanently fails the queue on timeout —
// changes nothing any of them can observe. That mutant was built and it survived
// the whole suite, which is why this exists.
//
// The consequence being guarded is not subtle: internal/gui consults this after
// every post for the life of the window, on the Fyne event goroutine. With the
// flushing query, one write slower than the flush timeout freezes the window for
// a quarter of a second and then discards every record of every later post in
// the session — the code added to satisfy FR-076 causing the outage FR-076
// exists to report. See logging.DegradedSoFar.
//
// The repository already takes this route for the same reason in cmd/mp, where
// T039's single os.Exit call site is asserted by an AST scan. An AST scan rather
// than a grep, because the comments in run.go legitimately name Degraded while
// explaining why it is not called.
func TestTheWindowNeverAsksTheFlushingDegradedQuery(t *testing.T) {
	fset := token.NewFileSet()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/gui: %v", err)
	}

	var files []*ast.File

	var paths []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		files = append(files, parsed)
		paths = append(paths, name)
	}

	if len(files) == 0 {
		t.Fatal("no source files parsed, so this test would pass vacuously")
	}

	sawTheOtherOne := false

	for i, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			switch selector.Sel.Name {
			case "Degraded":
				// Exactly "Degraded", never "DegradedSoFar".
				t.Errorf("%s:%d calls the flushing Degraded query; "+
					"internal/gui must use DegradedSoFar, which cannot fail the record queue",
					paths[i], fset.Position(selector.Pos()).Line)
			case "DegradedSoFar":
				sawTheOtherOne = true
			}

			return true
		})
	}

	if !sawTheOtherOne {
		t.Error("the scan found no DegradedSoFar call either, so it is not looking where the wiring lives " +
			"and its silence about Degraded proves nothing")
	}
}
