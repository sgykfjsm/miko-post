package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// errorPrefix marks output as coming from this program rather than from
// whatever it was running under, which matters most for the settings errors
// that quote a decoder's own wording.
const errorPrefix = "mp: "

// Run performs one command-line post and returns the process exit status
// (FR-003, FR-009 – FR-012, FR-059, FR-060).
//
// It returns a status rather than exiting. cmd/mp has the program's only
// os.Exit, and that is not a stylistic rule: os.Exit runs no deferred function,
// so a package that exited from the middle of a sequence would skip its own
// cleanup — here, closing the log — and any test of this front door would have
// to be a subprocess. Returning keeps the whole sequence assertable in-process
// and keeps the one thing a subprocess test is actually needed for, the real
// exit status, in one place where it can be checked directly.
//
// The order of the steps is the requirement. Validation runs before settings
// are read and before any sink exists, because FR-010 and constitution
// principle IV both say no destination may be contacted for a message that was
// going to be rejected — and the surest way to guarantee that is for the
// rejection to return before anything capable of contacting one has been
// constructed. Nothing between the guard and the return can reach a sink,
// because at that point in the function there are none.
func Run(invocation Invocation, out, errOut io.Writer) int {
	message := post.Message{Original: invocation.Message}
	if err := message.Validate(); err != nil {
		fmt.Fprintln(errOut, correctionPrompt(err))

		return ExitFailure
	}

	settings, err := load(invocation.ConfigPath)
	if err != nil {
		fmt.Fprintln(errOut, errorPrefix+err.Error())

		return ExitFailure
	}

	logger := app.OpenLogger(settings, logging.SourceCLI)

	// The recording is built from the logger and handed to the service, which
	// is the whole of this front door's part in T040: the event vocabulary is
	// internal/logging's, the mapping onto it is internal/app's, and the
	// orchestrator emits through an interface that knows about neither. What
	// this line owns is that the two are connected at all — a service built
	// without one posts identically and records nothing, and nothing in the
	// output would say so.
	outcome := app.NewService(settings, app.NewRecording(logger)).Post(message)

	// Closed before the report is assembled, not in a defer. A deferred Close
	// would run after everything had been printed, so a failure to flush and
	// release the log — the one that only shows up at close, on a full disk or a
	// revoked mount — would have nowhere left to be reported and would be
	// discarded by the defer's ignored return value. Doing it here puts it
	// inside the single warning FR-076 allows.
	warning := closeAndWarn(logger)

	Render(out, errOut, Report{
		Results: outcome.Results,
		LogPath: logger.Path(),
		Warning: warning,
	})

	// FR-059, FR-060, FR-076: the status is computed from the sink outcomes and
	// from nothing else. Not from the settings load, which has already returned;
	// not from the warning, which FR-076 explicitly forbids from changing it.
	if outcome.Succeeded() {
		return ExitSuccess
	}

	return ExitFailure
}

// load resolves the settings path and reads it (FR-005, FR-053).
//
// An empty configured path means the default resolved location, and the
// resolution happens here rather than in Parse because a path is only needed by
// the code that opens one: making Parse resolve it would put a filesystem-shaped
// dependency into argument parsing and give the window path a resolved value it
// must not use.
func load(configured string) (config.Settings, error) {
	path := configured

	if path == "" {
		resolved, err := config.DefaultConfigPath()
		if err != nil {
			return config.Settings{}, err
		}

		path = resolved
	}

	return config.Load(path)
}

// correctionPrompt is FR-010's correction prompt: what the user has to change
// before the message can be posted.
//
// One prompt per rejection reason, matched with errors.Is rather than by
// comparing error text, so a sentinel that is later wrapped for context still
// selects the right prompt. The two reasons are deliberately different
// sentences: "you typed nothing" and "your terminal handed us bytes we cannot
// carry" have different fixes, and a shared "invalid message" would tell a user
// with a mis-encoded locale to try typing something.
//
// The UTF-8 prompt names the locale, because that is the realistic cause on the
// one platform this version supports — argv is bytes on macOS, so a terminal in
// a legacy Shift_JIS or EUC-JP locale delivers ordinary Japanese text as invalid
// UTF-8 — and "not valid UTF-8" alone leaves that user with nothing to act on
// (decision DEC-D4, issue #104).
//
// The default arm exists for a rejection reason added later without a prompt
// here. It repeats the error's own text, which is worse than a written prompt
// and much better than silence: a message rejected with no explanation is
// indistinguishable from a program that did nothing.
func correctionPrompt(err error) string {
	switch {
	case errors.Is(err, post.ErrEmptyMessage):
		return errorPrefix + "nothing to post: the message is empty or contains only " +
			"whitespace. Type a message and try again."

	case errors.Is(err, post.ErrInvalidUTF8):
		return errorPrefix + "nothing to post: the message contains bytes that are not valid " +
			"UTF-8. No destination stores them — the chat service rejects them, the note becomes " +
			"replacement characters when it is next saved, and the diagnostic log cannot record " +
			"them — so the message was not sent. If your terminal is in a legacy encoding such " +
			"as Shift_JIS or EUC-JP, switch it to UTF-8 and try again."

	default:
		return errorPrefix + "nothing to post: " + err.Error()
	}
}

// closeAndWarn releases the log and returns FR-076's single warning, or "" when
// diagnostics reached disk.
//
// Exactly one string comes back from exactly one function, which is how
// "exactly one warning" is kept true across the three conditions that can
// produce one: an open that never succeeded, a write that failed after it, and
// a close that failed at the end. Two of those can hold at once — a log that
// could not be opened is also a log whose close does nothing useful — and a
// caller checking each in turn would print two.
//
// Degraded outranks the close error. A logger that was already discarding has
// nothing to say at close that is more useful than why it was discarding, and
// the open failure is the one with a fix attached.
func closeAndWarn(logger *logging.Logger) string {
	// Ordered: close first, then ask. Degraded() reports a write that failed
	// after a successful open, and a record written during the post is only
	// certainly flushed once the file is closed.
	closeErr := logger.Close()

	return warningFor(logger.Degraded(), closeErr, logger.Path())
}

// warningFor picks the one warning, given the two things that can have gone
// wrong and the path to name.
//
// Separated from closeAndWarn so the precedence is testable without a logger
// that can be made to fail its Close. There is no portable way to arrange that
// — closing a *os.File succeeds unless the filesystem is failing underneath it —
// so as an inline branch this arm would be a guard nobody has ever executed, in
// the function that decides whether the user hears about a diagnostics problem
// at all. The repository already takes this route for the same reason in
// internal/sink/obsidian (appendEntry's failing handle) and internal/sink/telegram
// (the credential net's unreachable textual half).
//
// Degraded outranks the close error, and the order is the point rather than an
// accident of which if came first. A logger that was already discarding has
// nothing to say at close that is more useful than why it was discarding, and
// the open failure is the one with a fix attached; reporting both would be two
// warnings, which FR-076 forbids.
func warningFor(degraded *logging.Degradation, closeErr error, path string) string {
	if degraded != nil {
		return degraded.Warning()
	}

	if closeErr != nil {
		// Rendered through the same type as every other degradation so the
		// three conditions produce one shape of sentence, not two.
		return (&logging.Degradation{Path: path, Err: closeErr}).Warning()
	}

	return ""
}
