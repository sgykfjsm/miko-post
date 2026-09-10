package app

import (
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
	"github.com/sgykfjsm/miko-post/internal/version"
)

// Sinks builds the enabled destinations and only the enabled ones (FR-016,
// T036).
//
// A disabled destination is not constructed, which is a stronger guarantee than
// constructing it and declining to call it: post.Service holds the sinks it was
// given and has no flag to consult, so "disabled sinks are not invoked and do
// not count as failures" becomes a property of what exists rather than of a
// branch someone has to keep writing. The orchestrator's own comment says the
// same thing from the other side.
//
// The order is fixed and is the order results are rendered in, because
// post.Outcome.Results preserves it deliberately: a report whose lines reorder
// between runs is harder to read and harder to test than one that does not.
// Obsidian first matches the sample in contracts/cli-interface.md.
//
// The returned slice is nil when both destinations are disabled. That is
// FR-018's startup error, and it belongs to the front door (T081) rather than
// here: the same settings are a perfectly valid document, and the actionable
// message the requirement calls for is front-door text. What this function must
// not do is invent a sink to avoid the empty case — post.AllSucceeded already
// answers false for no results, so an empty set exits 1 rather than reporting a
// vacuous success.
func Sinks(settings config.Settings) []post.Sink {
	// Capacity for both, so the common case of two enabled destinations does
	// not reallocate. Length zero, so the nil-versus-empty distinction above
	// still holds when neither is enabled.
	sinks := make([]post.Sink, 0, 2)

	if settings.Sink.Obsidian.Enabled {
		sinks = append(sinks, obsidian.New(settings.Sink.Obsidian))
	}

	if settings.Sink.Telegram.Enabled {
		sinks = append(sinks, telegram.New(settings.Sink.Telegram))
	}

	if len(sinks) == 0 {
		return nil
	}

	return sinks
}

// SinkTimeout converts posting.sink_timeout_seconds into the per-sink bound
// post.Service applies (FR-015).
//
// The conversion lives here and not in internal/post because that package
// imports no other internal package: post.Service takes a time.Duration rather
// than a config.Settings precisely so the settings tree does not sit behind the
// posting core, and its own comment records that as the reason. Moving the
// conversion inward would undo a shipped design decision for the sake of one
// integer.
//
// It is a plain multiplication, and that is now safe rather than lucky.
// config.Validate bounds the key at config.MaxTimeoutSeconds (issue #109), which
// is the largest whole second a time.Duration holds, so the product cannot
// overflow for any value that reached a validated Settings. Nothing is clamped
// here on top of that: post.New still floors a non-positive duration to FR-015's
// default for the caller who builds a Settings without going through Load, and
// two clamps in series would mean neither one's test could tell which was doing
// the work.
func SinkTimeout(settings config.Settings) time.Duration {
	return time.Duration(settings.Posting.SinkTimeoutSeconds) * time.Second
}

// NewService builds the posting service for one run from settings, reporting
// what happens to recording (T040).
//
// Sinks and SinkTimeout stay exported alongside it because they are separately
// testable — "only the enabled sinks were built" is an assertion about names,
// and a *post.Service does not expose the set it holds.
//
// recording is a parameter rather than built from settings here, because the
// logger it wraps has a lifetime the service does not: a front door opens it,
// posts, then closes it and asks whether diagnostics reached disk, and that
// close has to happen before the results are rendered so a failure at close can
// still be reported (FR-076). Passing nil records nothing and changes nothing
// else about the post, which is the property FR-076 requires.
func NewService(settings config.Settings, recording post.Recording) *post.Service {
	return post.New(Sinks(settings), SinkTimeout(settings), recording)
}

// OpenLogger builds the diagnostic logger for one run (FR-064 – FR-066,
// FR-075, FR-076).
//
// Never returns an error and never returns nil, because logging.Open does not:
// a logger that discards is still a logger, and FR-076 requires that a
// diagnostics failure change nothing about the post. The caller consults
// Logger.Degraded once, when it renders results, for FR-076's single warning.
//
// Three settings are translated rather than passed through, and each translation
// is where a wiring bug would be silent:
//
//   - logging.path is handed over as-is. Empty means the resolved default and
//     logging.Open applies that rule itself (issue #107); resolving it here
//     would put the trap back for the second front door.
//   - include_version and include_git_commit are expressed by supplying the
//     value or the empty string, which is the contract logging.Options states.
//     A caller that passed the values unconditionally would stamp every record
//     whatever the user configured, and nothing in the record would say so.
//   - Options.Redact carries the resolved bot token. It is the chokepoint that
//     keeps the credential out of every record whatever shape it arrives in, and
//     it is inert unless it is populated — so populating it is a wiring
//     obligation, not an optimisation (issue #41, FR-043, FR-069). The token is
//     passed as a config.Secret rather than a revealed string so this function
//     never holds a second unguarded copy.
//
// The token is supplied whether or not the chat destination is enabled. A
// disabled sink cannot produce a record carrying it, so the scrub costs nothing
// there; conditioning it on Enabled would make the redaction depend on a second
// setting, and a redaction that is only sometimes armed is the kind of guard
// that is discovered to have been off.
func OpenLogger(settings config.Settings, source logging.Source) *logging.Logger {
	return logging.Open(logging.Options{
		Path:       settings.Logging.Path,
		Source:     source,
		Redact:     []config.Secret{settings.Sink.Telegram.BotToken},
		AppVersion: stampedIf(settings.Logging.IncludeVersion, version.Version),
		GitCommit:  stampedIf(settings.Logging.IncludeGitCommit, version.Commit),
	})
}

// stampedIf returns the build-identity value when the setting asks for it and
// "" when it does not, which is how logging.Options expresses "omit this key".
//
// It takes the accessor rather than the value so that a disabled setting does
// not resolve one. That matters less for cost than for honesty: version.Commit
// reads build info, and evaluating it to throw the answer away invites the next
// reader to assume the field is always populated.
func stampedIf(include bool, value func() string) string {
	if !include {
		return ""
	}

	return value()
}
