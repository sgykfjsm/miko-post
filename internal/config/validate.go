package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Accepted values for the two keys that are validated but inert in v0.1
// (FR-034).
const (
	// ParseModeMarkdownV2 is what v0.1 always sends on the first attempt,
	// regardless of what the key says.
	ParseModeMarkdownV2 = "MarkdownV2"
	ParseModeMarkdown   = "Markdown"
	ParseModeHTML       = "HTML"

	// LogFormatJSONL is the only accepted log format in v0.1.
	LogFormatJSONL = "jsonl"
)

// acceptedParseModes is the set ParseMode is validated against.
//
// The three are the modes the chat service itself documents, not the one mode
// v0.1 sends. That combination is the point of FR-034: the key exists as a
// forward-compatibility affordance, so rejecting "HTML" today would defeat it,
// while accepting anything at all would make "validated" meaningless and let a
// typo such as "MarkdownV3" sit in a file being silently ignored — twice over,
// since the key is inert as well. Validating against the documented set catches
// the typo and preserves the affordance.
var acceptedParseModes = []string{ParseModeMarkdownV2, ParseModeMarkdown, ParseModeHTML}

// layoutProbeTime is the instant used to decide whether a string is a Go time
// layout at all.
//
// Every component differs from the corresponding element of Go's reference time
// (2006-01-02 15:04:05, Mon Jan, MST, .000000000), and deliberately so: the
// check below asks whether formatting changed the string, so a probe instant
// that happened to render as its own layout — 15:04 formatted at 15:04, or a
// zero nanosecond count under ".000" — would report a real layout as literal
// text. 2026-09-03 21:47:53.123456789 UTC is a Thursday in September, which
// collides with none of them.
var layoutProbeTime = time.Date(2026, 9, 3, 21, 47, 53, 123456789, time.UTC)

// ValidationError reports every problem found in one settings document.
//
// It exists as a type rather than a joined string so a front door can tell a
// validation failure from a read or decode failure without matching on message
// text, and so the problems stay individually addressable if a future front
// door wants to render them differently (FR-058 requires the failure to surface
// through the front door that was used, and the two front doors have very
// different amounts of room).
type ValidationError struct {
	// Problems is every problem found, in document order. Never empty: a
	// ValidationError is only constructed when there is at least one.
	Problems []string
}

// Error renders every problem at once.
//
// FR-055 and FR-058 call for accumulating validation, and accumulation only
// pays off if the output shows all of it. Returning at the first problem would
// make fixing a settings file an iterative guessing game, one restart per typo.
func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid settings: " + e.Problems[0]
	}

	return fmt.Sprintf("invalid settings (%d problems):\n  - %s",
		len(e.Problems), strings.Join(e.Problems, "\n  - "))
}

// problems accumulates validation failures.
//
// A named slice type with an addf method, rather than a bare []string appended
// to at each site, so that every problem is phrased through one call and the
// "keep going" behaviour is not something each check has to remember not to
// break with an early return.
type problems []string

func (p *problems) addf(format string, args ...any) {
	*p = append(*p, fmt.Sprintf(format, args...))
}

// Validate checks the whole document and reports every problem together
// (FR-055, FR-058).
//
// Two kinds of rule are mixed here, and the difference is deliberate:
//
//   - Keys the contract marks "required when enabled" — the credential, the
//     chat id, the note directory — are checked only when their sink is
//     enabled. A user who has not configured a sink they are not using should
//     not be blocked by it.
//   - Keys with a default and a stated range or set — the timeouts, the close
//     delays, the rotation thresholds, the formats — are checked
//     unconditionally. They always hold a value, so there is no state in which
//     an out-of-range one is meaningless, and a nonsense value sitting in a
//     disabled section is a latent failure waiting for the day the sink is
//     enabled.
//
// Whether every sink is disabled is not checked here. That is FR-018's startup
// error and belongs to the front door (T081): such a document is entirely valid
// as a document, and the actionable message the requirement calls for is a
// front-door concern.
func (s Settings) Validate() error {
	var found problems

	s.Sink.Telegram.validate(&found)
	s.Sink.Obsidian.validate(&found)
	s.Posting.validate(&found)
	s.GUI.validate(&found)
	s.Logging.validate(&found)

	if len(found) == 0 {
		return nil
	}

	return &ValidationError{Problems: found}
}

func (t TelegramSettings) validate(found *problems) {
	if t.Enabled {
		if t.BotToken.IsEmpty() {
			found.addf("sink.telegram.bot_token is required when the sink is enabled; "+
				"set it in the file or in %s", TelegramBotTokenEnv)
		}

		if strings.TrimSpace(t.ChatID) == "" {
			found.addf("sink.telegram.chat_id is required when the sink is enabled")
		}
	}

	// An explicit thread_id = 0 is rejected rather than passed through. The
	// contract makes absence, not a sentinel, the way to post to the chat
	// directly, so 0 is a user reaching for a sentinel that does not exist;
	// forwarding it would produce a request naming a topic that cannot be one.
	// Absence is nil here and never reaches this branch.
	if t.ThreadID != nil && *t.ThreadID <= 0 {
		found.addf("sink.telegram.thread_id must be positive when set (got %d); "+
			"omit the key entirely to post to the chat directly", *t.ThreadID)
	}

	if !slices.Contains(acceptedParseModes, t.ParseMode) {
		found.addf("sink.telegram.parse_mode must be one of %s (got %q); "+
			"the key is accepted and validated but does not change delivery in v0.1",
			strings.Join(acceptedParseModes, ", "), t.ParseMode)
	}

	if t.HTTPTimeoutSeconds <= 0 {
		found.addf("sink.telegram.http_timeout_seconds must be greater than 0 (got %d)",
			t.HTTPTimeoutSeconds)
	}
}

func (o ObsidianSettings) validate(found *problems) {
	if o.Enabled {
		switch {
		case strings.TrimSpace(o.DailyNoteDir) == "":
			found.addf("sink.obsidian.daily_note_dir is required when the sink is enabled")
		case !filepath.IsAbs(o.DailyNoteDir):
			found.addf("sink.obsidian.daily_note_dir must be an absolute path (got %q)",
				o.DailyNoteDir)
		}
	}

	validateLayout(found, "sink.obsidian.filename_format", o.FilenameFormat)

	// The suffix check is separate from the layout check so a format that is a
	// valid layout but not a Markdown filename reports the actual problem.
	if o.FilenameFormat != "" && !strings.HasSuffix(o.FilenameFormat, ".md") {
		found.addf("sink.obsidian.filename_format must end in .md (got %q)", o.FilenameFormat)
	}

	validateLayout(found, "sink.obsidian.time_format", o.TimeFormat)
}

func (p PostingSettings) validate(found *problems) {
	if p.SinkTimeoutSeconds <= 0 {
		found.addf("posting.sink_timeout_seconds must be greater than 0 (got %d)",
			p.SinkTimeoutSeconds)
	}
}

func (g GUISettings) validate(found *problems) {
	// Zero is allowed for both: it means close as soon as the result is
	// rendered, which is a legitimate preference, unlike a negative delay.
	if g.SuccessCloseSeconds < 0 {
		found.addf("gui.success_close_seconds must not be negative (got %d)",
			g.SuccessCloseSeconds)
	}

	if g.ErrorCloseSeconds < 0 {
		found.addf("gui.error_close_seconds must not be negative (got %d)",
			g.ErrorCloseSeconds)
	}
}

func (l LoggingSettings) validate(found *problems) {
	if l.Format != LogFormatJSONL {
		found.addf("logging.format must be %q (got %q)", LogFormatJSONL, l.Format)
	}

	if l.RotateSizeMiB <= 0 {
		found.addf("logging.rotate_size_mib must be greater than 0 (got %d)", l.RotateSizeMiB)
	}

	if l.RotateAfterDays <= 0 {
		found.addf("logging.rotate_after_days must be greater than 0 (got %d)", l.RotateAfterDays)
	}

	// logging.path is deliberately unvalidated. Empty means the default state
	// path (FR-056), and any non-empty value is a path whose writability can
	// only be discovered by trying — which the logger does, failing soft
	// (FR-075). Rejecting an unwritable path here would turn a degraded-
	// diagnostics condition into a refusal to post, inverting FR-075.
}

// validateLayout reports whether value is usable as a Go time layout.
//
// There is no API that answers this, and the obvious substitute does not work:
// a format/parse round trip succeeds for any string at all, because a layout
// containing no reference elements is pure literal text that formats to itself
// and parses back unchanged. So "YYYY-MM-DD.md" — the single most likely thing
// a user reaching for a date format will write, since it is what nearly every
// other language uses — would round-trip cleanly and then silently append every
// note in history to one file named YYYY-MM-DD.md.
//
// What actually distinguishes a layout from literal text is whether formatting
// an instant through it changes anything. That is the check.
func validateLayout(found *problems, key, value string) {
	if value == "" {
		found.addf("%s must not be empty", key)

		return
	}

	if layoutProbeTime.Format(value) == value {
		found.addf("%s is not a Go time layout: %q contains no time reference elements "+
			"and would render the same text every day; use the reference date, "+
			"for example 2006-01-02 for the date and 15:04 for the time", key, value)
	}
}
