package gui

import (
	"errors"
	"strings"

	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

func correction(err error) string {
	switch {
	case errors.Is(err, post.ErrEmptyMessage):
		return "Type a message before sending."
	case errors.Is(err, post.ErrInvalidUTF8):
		return "The message contains invalid UTF-8. Replace it with valid text and try again."
	default:
		return "The message could not be submitted."
	}
}

// Only the core's display fields belong here; Err is exclusively diagnostic.
//
// warning is FR-076's degradation warning, or "" when there is none to show
// *on this post*. lost says whether diagnostics are unusable at all, which is a
// different question: the warning is spent once per session and `lost` stays
// true for the rest of it. Passing one flag for both jobs is what made a failed
// post point at an unwritten log once the warning had been spent on an earlier
// successful one.
//
// The warning goes last, after the per-destination lines and the log path,
// because it is about the log rather than about the post: a reader's first
// question is what happened to their message, and the answer must not be pushed
// down the label by a paragraph about diagnostics.
func resultText(outcome post.Outcome, logPath string, warning string, lost bool) string {
	var lines []string
	for _, result := range outcome.Results {
		if result.Success {
			lines = append(lines, result.Name+": sent")
		} else {
			lines = append(lines, result.Name+": "+result.Reason)
		}
	}
	if !outcome.Succeeded() && !lost {
		// Suppressed whenever diagnostics are unusable, because the path
		// would be pointing at a file that does not have the details —
		// keyed on `lost`, not on whether a warning is being printed right
		// now. Those differ for the whole rest of a session: the warning is
		// spent once, and if it was spent on a *successful* post then every
		// later failed post would otherwise get the stale invitation back,
		// which is the one post the user actually needs to diagnose. The
		// warning below names the same path and says why, which is the
		// honest version of the same sentence — the reasoning
		// internal/cli/render.go already applies to its own pairing.
		lines = append(lines, "Details: "+logPath)
	}
	if warning != "" {
		lines = append(lines, warning)
	}
	return strings.Join(lines, "\n")
}

// degradationWarning yields FR-076's warning once per session.
//
// FR-076 allows exactly one warning and SC-013 says the user is told exactly
// once, and in the GUI those two are the same sentence only if something
// remembers. logging.Logger latches its degradation — once diagnostics have
// failed, Degraded() keeps reporting it — and the window outlives every post
// in the session, so asking on each post and rendering the answer would put the
// warning on the second post's result, and the third's, for as long as the
// window stays open.
//
// Once per *session* rather than once per post is the reading this takes, and
// the latch is why: a second warning would not be telling the user about a
// second failure, it would be repeating the first. The GUI's own consequence is
// that a degradation which begins mid-session is still reported on the next
// post, which is the first post that could have been affected by it.
//
// No lock. next is called only from finished, which runs on the Fyne main
// goroutine through window.dispatch; the posting goroutine never touches it.
type degradationWarning struct {
	degraded func() *logging.Degradation
	warned   bool
}

// check reports the warning to show on this post — the first time diagnostics
// are found to have failed, and "" afterwards — and whether diagnostics are
// unusable at all.
//
// Two returns rather than one because the two outlive each other. The warning
// is spent once per session under FR-076; `lost` stays true for every post
// after it, and it is `lost` that must decide whether a failed post is still
// invited to read the log.
//
// The degradation is consulted on every post even after the warning is spent,
// because `lost` has to stay current — and it is cheap and side-effect-free to
// ask, which is the whole reason this holds Logger.DegradedSoFar rather than
// Logger.Degraded. See that method: the flushing one can kill the queue.
func (d *degradationWarning) check() (warning string, lost bool) {
	if d == nil || d.degraded == nil {
		return "", false
	}

	degraded := d.degraded()
	if degraded == nil {
		return "", false
	}

	if d.warned {
		return "", true
	}

	d.warned = true

	// The same type the CLI renders, so both front doors produce one shape of
	// sentence naming the path and the reason rather than two.
	return degraded.Warning(), true
}
