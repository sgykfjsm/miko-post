package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sgykfjsm/miko-post/internal/post"
)

// Report is everything one post's user-facing output is made of.
//
// A struct rather than four parameters because the three fields are produced at
// different moments — the results by the orchestrator, the path and the warning
// by the logger — and a positional call would let two strings be swapped with
// nothing to catch it.
//
// It carries strings rather than a *logging.Logger deliberately. Rendering is
// the one thing in this program that must be assertable byte for byte, and a
// renderer that reached into a logger would need one to exist before any of
// FR-062's cases could be checked.
type Report struct {
	// Results is one entry per enabled destination, in the order the sinks were
	// built. Empty when no destination was enabled.
	Results []post.SinkResult

	// LogPath is the diagnostic log FR-063 requires failure output to name, or
	// "" when none could be resolved — in which case Warning explains why and
	// there is no path worth printing.
	LogPath string

	// Warning is FR-076's single degradation warning, or "" when diagnostics
	// are reaching disk. One string and not a slice: the requirement is exactly
	// one warning, and a slice would make "exactly one" a thing the caller has
	// to arrange rather than a thing the type says.
	Warning string
}

// The rendered vocabulary. Assembled from constants so that a test asserting
// the output and the code producing it read the same strings, and so that a
// change to the wording is one edit rather than a search.
const (
	successWord = "success"
	failureWord = "failed"

	// The separator between "failed" and the reason, matching the sample in
	// contracts/cli-interface.md.
	reasonSeparator = " — "

	logPathPrefix = "See log for details: "

	// Printed when the post ran with no destinations at all.
	//
	// Deliberately a statement of fact and not FR-018's startup error: that
	// requirement wants an actionable message *before* a post is attempted, and
	// it is T081's, in batch 11. Printing nothing at all was the alternative and
	// is worse — a bare "See log for details" under an empty report reads as a
	// crash.
	noDestinationsLine = "No destination is enabled, so nothing was posted."
)

// Render writes one post's report (FR-062, FR-063, FR-076).
//
// The results go to out and the warning to errOut. The split is not specified —
// contracts/cli-interface.md calls it an implementation detail and requires
// only completeness, brevity and the log path — and this is the split that lets
// `mp … > captured.txt` keep the report while the user still sees a diagnostics
// problem on their terminal.
//
// Every destination is named, successes included, because FR-062's "partial
// success is visible" is not satisfied by listing only failures: a report
// showing one failed sink and nothing else cannot be distinguished from a
// single-sink configuration.
//
// Nothing here decides the exit status, and nothing here reads Err. SinkResult
// splits Reason from Err precisely so that a front door has one safe half to
// print (FR-017, FR-029) — Err can carry a URL, a header or a credential, and
// this function must have no route to it.
func Render(out, errOut io.Writer, report Report) {
	var rendered strings.Builder

	if len(report.Results) == 0 {
		rendered.WriteString(noDestinationsLine)
		rendered.WriteString("\n")
	}

	for _, result := range report.Results {
		rendered.WriteString(line(result))
		rendered.WriteString("\n")
	}

	// FR-063: failure output names the log. Keyed off the same predicate that
	// decides the exit status, so the line cannot appear on a run that exits 0
	// or go missing on one that exits 1. Suppressed when nothing resolved,
	// because "See log for details: " with nothing after it is an instruction
	// the user cannot follow; Warning says why in that case.
	if !post.AllSucceeded(report.Results) && report.LogPath != "" {
		rendered.WriteString(logPathPrefix)
		rendered.WriteString(report.LogPath)
		rendered.WriteString("\n")
	}

	// One Write for the whole report, so a reader never sees half of it
	// interleaved with the warning below.
	fmt.Fprint(out, rendered.String())

	// FR-076's exactly-one warning. There is one Fprintln and one guard, so
	// "exactly one" is a property of the code's shape; a second warning would
	// have to be a second statement.
	if report.Warning != "" {
		fmt.Fprintln(errOut, report.Warning)
	}
}

// line renders one destination's outcome.
//
// Reason is printed only on failure and only when it is non-empty. An empty
// Reason on a failure is an orchestrator bug — every failure path there sets
// one — and rendering "Telegram: failed — " for it would trail a separator into
// a message that is already telling the user something went wrong; "Telegram:
// failed" alone is the honest rendering of a failure whose reason was lost.
func line(result post.SinkResult) string {
	name := displayName(result.Name)

	if result.Success {
		return name + ": " + successWord
	}

	if result.Reason == "" {
		return name + ": " + failureWord
	}

	return name + ": " + failureWord + reasonSeparator + result.Reason
}

// displayName capitalises a sink's name for the report.
//
// The names are contract text — "obsidian" and "telegram" appear in every log
// record and are matched by saved queries — so they are lowercase everywhere
// they matter, while contracts/cli-interface.md's sample report shows them
// capitalised. This is the one place the two meet, and doing it here keeps the
// display convention out of the sinks.
//
// ASCII only, and by design: applying Unicode case rules to a value that is an
// identifier makes the rendering depend on the case-mapping table rather than on
// the name, and a name beginning with a rune whose uppercase form is more than
// one rune would change length. A name not beginning with a lowercase ASCII
// letter is passed through unchanged rather than mangled, which is also what
// makes this safe for a name this package did not choose: the orchestrator
// substitutes "unknown" for a sink whose Name() panicked, and that renders as
// "Unknown" — recognisably the sentinel, which is the point of having one.
func displayName(name string) string {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return name
	}

	return string(name[0]-'a'+'A') + name[1:]
}
