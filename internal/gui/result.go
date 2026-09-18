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
// warning is FR-076's degradation warning, or "" when diagnostics reached
// disk. It goes last, after the per-destination lines and the log path, because
// it is about the log rather than about the post: a reader's first question is
// what happened to their message, and the answer must not be pushed down the
// label by a paragraph about diagnostics.
func resultText(outcome post.Outcome, logPath string, warning string) string {
	var lines []string
	for _, result := range outcome.Results {
		if result.Success {
			lines = append(lines, result.Name+": sent")
		} else {
			lines = append(lines, result.Name+": "+result.Reason)
		}
	}
	if !outcome.Succeeded() {
		// Suppressed when diagnostics failed, because the path would be
		// pointing at a file that does not have the details. The warning
		// below names the same path and says why, which is the honest
		// version of the same sentence — this is the reasoning
		// internal/cli/render.go already applies to its own pairing.
		if warning == "" {
			lines = append(lines, "Details: "+logPath)
		}
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

// next returns the warning the first time diagnostics are found to have
// failed, and "" on every call after that.
func (d *degradationWarning) next() string {
	if d == nil || d.warned || d.degraded == nil {
		return ""
	}

	degraded := d.degraded()
	if degraded == nil {
		return ""
	}

	d.warned = true

	// The same type the CLI renders, so both front doors produce one shape of
	// sentence naming the path and the reason rather than two.
	return degraded.Warning()
}
