// Command mp posts one short message to two independent destinations: a
// Telegram chat and an Obsidian daily note.
//
// Running mp with message arguments posts from the command line; running it
// with none opens a small window.
//
// One binary providing both front doors is what satisfies FR-001, and it is why
// this file exists at all: the dispatch between them is the only thing neither
// front door can do for itself.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sgykfjsm/miko-post/internal/cli"
	"github.com/sgykfjsm/miko-post/internal/gui"
)

// main is the program's only os.Exit, and the only statement that reaches it is
// the one below (FR-059, FR-060).
//
// os.Exit runs no deferred function. Every other package therefore returns a
// status instead of exiting, so that each one's own cleanup — closing the log,
// above all — has already happened by the time control arrives here. A second
// os.Exit anywhere would reintroduce that hazard at a point nobody was looking
// at, which is why "exactly one call site" is the requirement rather than a
// preference.
//
// It also means nothing in this program may write through a buffered writer
// that is flushed by a defer: the flush would not run. Nothing does — os.Stdout
// is passed down unwrapped — and this is the note that has to be re-read before
// anyone wraps it.
func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch parses the command line and hands it to the front door it asked for
// (FR-001 – FR-003).
//
// Split out of main so that everything except the exit call itself is an
// ordinary function: main's body is one statement, and the status it exits with
// is computed by code a test can call. That covers the composition. What it
// cannot cover is whether os.Exit is reached with the value dispatch returned —
// a test asserting the returned int would pass an os.Exit(1) hard-coded above —
// so cmd/mp's tests build this binary and run it.
func dispatch(argv []string, out, errOut io.Writer) int {
	invocation, err := cli.Parse(argv)
	if err != nil {
		fmt.Fprintf(errOut, "mp: %v\n", err)

		return cli.ExitFailure
	}

	if invocation.Mode == cli.ModeWindow {
		return gui.Run(errOut)
	}

	return cli.Run(invocation, out, errOut)
}
