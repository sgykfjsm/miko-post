package config

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
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
//
// It is fixed, and layout *detection* must keep using it rather than the wall
// clock: "is this a layout?" has one right answer for a given string, and an
// answer that moved with the clock would make the rule flaky — "15:04" is
// indistinguishable from literal text for exactly the one minute a day at which
// it renders as itself.
var layoutProbeTime = time.Date(2026, 9, 3, 21, 47, 53, 123456789, time.UTC)

// pathSeparators are the characters a rendered filename may never contain.
//
// Both spellings are rejected on every platform rather than deferring to
// filepath.Separator, deliberately. A settings file is portable — the same
// document is expected to work wherever the user carries their vault — so a
// rule that changed with GOOS would accept "2006\01\02.md" on Unix as a
// filename containing two literal backslashes and then silently turn it into a
// directory hierarchy the moment the same file is read on Windows. Rejecting
// both everywhere makes the accepted set the intersection, which is the only
// set that means the same thing wherever the document is opened.
const pathSeparators = `/\`

// MaxTimeoutSeconds is the largest whole second any timeout key may hold: the
// largest that still converts to a time.Duration without wrapping, about 292
// years.
//
// It exists because bounding a timeout only from below is not a bound (issues
// #109 and #114). Every consumer of these keys has to evaluate
// `time.Duration(seconds) * time.Second`, and that multiplication overflows
// int64 in silence. The dangerous residue is not the obvious one:
//
//	9223372037  -> -2562047h47m…   negative, and any floor catches it
//	1 << 62     -> 0s              zero, and any floor catches it
//	18446744074 -> 290.448384ms    positive, small, and nothing downstream can tell
//	18446744075 -> 1.290448384s    the same
//
// The third and fourth rows are why this is a validation rule rather than a
// clamp at each conversion. A user who wrote 18446744074 asked for ~584 years
// and would have received a deadline three times tighter than the default they
// were trying to raise, with every post then failing for a reason their own
// settings file appears to contradict. Rejecting at load time is the only place
// the user can still be told.
//
// Exported because the number is user-facing — it appears in the problem
// message and in contracts/config-schema.md — and because a test that recomputed
// it would be asserting its own arithmetic rather than the code's.
//
// On a platform where int is 32 bits the conversion cannot overflow at all, so
// the upper check simply never fires; the constant is still correct there, it is
// merely unreachable.
const MaxTimeoutSeconds = int64(math.MaxInt64 / int64(time.Second))

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
	// time.Now() carries time.Local, and that Location is the point: FR-051
	// makes the Obsidian sink render its date and time at local wall-clock time,
	// so a problem message that quoted any other rendering would be showing the
	// user a filename their sink is never going to write.
	//
	// Message fidelity is all it buys, and that is deliberate. The rendered-value
	// *rules* are instant-independent once the zone name is refused — the
	// argument is on rendersZoneName — so a wrong clock here could not turn a
	// rejection into an acceptance, only make the rejection harder to act on.
	// The rules must not be made to depend on this instant again: a check that
	// consulted the wall clock for its verdict would be asking a question whose
	// answer the attacker schedules.
	return s.validateAt(time.Now())
}

// validateAt is Validate with the wall clock supplied.
//
// The seam exists so the instant-dependent parts can be pinned at a stated
// instant: that layout detection ignores this argument, and that the accepted
// set does not move across a real zone transition. Reaching for $TZ instead
// would not work — the time.Local *variable* is resolved once per process, so a
// test that set it would be racing the rest of the binary — and assigning to
// time.Local would make the whole package's tests order-dependent. Passing the
// instant keeps every case hermetic and parallel.
//
// Note what resolving time.Local once does not give you, because a previous fix
// rested on the opposite and was wrong: it fixes the Location, and a Location is
// not a zone. It selects among arbitrarily many by instant, so nothing about
// this argument makes the zone at validation the zone at the write. See
// rendersZoneName for the rule that removes the zone from the question instead.
func (s Settings) validateAt(now time.Time) error {
	var found problems

	s.Sink.Telegram.validate(&found)
	s.Sink.Obsidian.validate(&found, now)
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

	validateTimeoutSeconds(found, "sink.telegram.http_timeout_seconds", t.HTTPTimeoutSeconds)
}

// validateTimeoutSeconds is the shared bound for every settings key holding a
// number of seconds that a consumer converts to a time.Duration.
//
// One function rather than two copies because #109 and #114 are one defect
// found twice, in posting.sink_timeout_seconds and in
// sink.telegram.http_timeout_seconds, and the way that happened is that the
// lower bound was written twice and the upper bound was forgotten in both. A
// third timeout key added later gets the whole rule by calling this, or it gets
// neither half and the omission is visible in the diff.
//
// The two arms are separate problems with separate messages on purpose. "Too
// small" and "too large" need different corrections, and FR-055's accumulating
// validation is only useful if what it accumulates is actionable.
//
// The upper message names the mechanism rather than only the limit. A bound of
// 9223372036 looks arbitrary next to a value the user chose deliberately, and a
// user told merely "must be at most 9223372036" would reasonably conclude the
// program has an opinion about long timeouts; told that a larger value silently
// becomes a fraction of a second, they can see it is arithmetic and not policy.
func validateTimeoutSeconds(found *problems, key string, seconds int) {
	switch {
	case seconds <= 0:
		found.addf("%s must be greater than 0 (got %d)", key, seconds)
	case int64(seconds) > MaxTimeoutSeconds:
		found.addf("%s must be at most %d (got %d); a larger value overflows the internal "+
			"duration and would silently become a fraction of a second rather than the long "+
			"timeout it reads as", key, MaxTimeoutSeconds, seconds)
	}
}

func (o ObsidianSettings) validate(found *problems, now time.Time) {
	if o.Enabled {
		switch {
		case strings.TrimSpace(o.DailyNoteDir) == "":
			found.addf("sink.obsidian.daily_note_dir is required when the sink is enabled")
		case !filepath.IsAbs(o.DailyNoteDir):
			found.addf("sink.obsidian.daily_note_dir must be an absolute path (got %q)",
				o.DailyNoteDir)
		}
	}

	// Being a layout and rendering something usable are independent questions,
	// so the rendered checks run on their own rather than behind the layout
	// check: "../daily.md" is both literal text and an escape from the vault,
	// and a user who fixes only the problem they were told about would just
	// rediscover the other one on the next run.
	if rendered, ok := validateLayout(found, "sink.obsidian.filename_format", o.FilenameFormat, now); ok {
		validateRenderedFilename(found, "sink.obsidian.filename_format", o.FilenameFormat, rendered)
	}

	// The suffix check is separate from the layout check so a format that is a
	// valid layout but not a Markdown filename reports the actual problem.
	if o.FilenameFormat != "" && !strings.HasSuffix(o.FilenameFormat, ".md") {
		found.addf("sink.obsidian.filename_format must end in .md (got %q)",
			elide(o.FilenameFormat))
	}

	if rendered, ok := validateLayout(found, "sink.obsidian.time_format", o.TimeFormat, now); ok {
		validateRenderedTimePrefix(found, "sink.obsidian.time_format", o.TimeFormat, rendered)
	}
}

func (p PostingSettings) validate(found *problems) {
	validateTimeoutSeconds(found, "posting.sink_timeout_seconds", p.SinkTimeoutSeconds)
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
// an instant through it changes anything. That is the check, and it runs on the
// fixed probe rather than on now: detection has one right answer per string, so
// deciding it against the wall clock would make the rule flaky.
//
// The rendering is returned rather than discarded because being a layout is
// only half of what these two keys have to satisfy: what a layout *renders*
// ends up in a filesystem path and in a note's text, and nothing else in this
// package would otherwise look at it. ok is false when there is nothing worth
// rendering, so a caller can chain a rendered-value rule without repeating the
// empty case.
//
// One rendering is enough, and that is a consequence of the zone-name rule
// rather than an assumption. See rendersZoneName: with the MST element refused,
// every remaining reference element renders out of a fixed alphabet — digits,
// Go's own English month and day names, and " +-,.:" — none of which contains a
// path separator, a control character, or a lone dot. So every character the
// rendered-value rules can object to comes from the layout's own literal text,
// which is the same at every instant. Checking a second instant would therefore
// reach the same verdict by construction, and checking *which* second instant
// was never a question the code could win: an attacker who can supply a zone can
// also supply its transition schedule, so any fixed number of extra samples is
// one sample short.
func validateLayout(found *problems, key, value string, now time.Time) (rendered string, ok bool) {
	if value == "" {
		found.addf("%s must not be empty", key)

		return "", false
	}

	if rendersZoneName(value) {
		found.addf("%s must not render the zone name: %q contains Go's MST element, and the "+
			"text that element emits comes from the machine's zone database rather than from "+
			"this document — $TZ may name an arbitrary TZif file, and nothing constrains the "+
			"abbreviation inside it to look like a zone name, so the rendered value can be a "+
			"path traversal or a line break; write the offset instead, for example Z07:00 "+
			"or -0700, which render digits and sign characters only", key, elide(value))

		// The rendered-value rules are skipped rather than run on this value.
		// What it renders is not a property of the document — it is a property
		// of the machine — and reporting a problem about a rendering that is
		// going to change the moment the required fix is applied would be worse
		// than not reporting it. Quoting that text would also put an arbitrary
		// environment-supplied string into the user's error and the JSONL log.
		//
		// Layout detection is skipped with it, and loses nothing: a value
		// carrying the MST element always formats to something other than
		// itself, because the element emits the probe's zone name in place of
		// the three characters "MST", so the detection below could never have
		// fired for it.
		return "", false
	}

	if layoutProbeTime.Format(value) == value {
		found.addf("%s is not a Go time layout: %q contains no time reference elements "+
			"and would render the same text every day; use the reference date, "+
			"for example 2006-01-02 for the date and 15:04 for the time", key, elide(value))
	}

	return now.Format(value), true
}

// zoneNameProbeA and zoneNameProbeB are one instant read twice, differing in
// nothing but the name of the zone it is read in.
var (
	zoneNameProbeA = layoutProbeTime.In(time.FixedZone("AAA", 0))
	zoneNameProbeB = layoutProbeTime.In(time.FixedZone("BBB", 0))
)

// rendersZoneName reports whether a layout emits the zone abbreviation.
//
// That element is refused in both format keys because it is the one element
// whose output is neither the document's text nor a digit: it copies the zone
// abbreviation through verbatim, and that string is outside the trust boundary.
// Go loads an arbitrary TZif when $TZ is an absolute path and places no
// character constraint on the abbreviation it finds there, so a crafted file
// turns the entirely contract-legal "2006-01-02MST.md" into an escape from the
// vault, or defeats FR-048's one-entry-one-line promise with a newline.
//
// Refusing the element, rather than inspecting what it happens to render right
// now, is the only rule that holds. A Location is not a zone: it holds
// arbitrarily many, and picks one per instant. America/New_York — an ordinary
// zone, no crafted file — renders "2026-01-15EST.md" in January and
// "2026-07-15EDT.md" in July through one Location. So a check that formatted an
// instant and approved the result would be approving one of the zones a
// Location can produce, while the write happens at another instant with another
// zone. A hostile TZif can carry a POSIX footer giving it recurring transitions,
// which makes the attacker, not the validator, the one who chooses how many
// instants would have had to be sampled.
//
// The comparison is the detection, so that the question is answered by Go's own
// formatter rather than by a second implementation of its layout grammar. The
// two probes are the same instant at the same offset, so the abbreviation is the
// only thing that can differ between the outputs; they differ exactly when the
// layout contains the element.
//
// strings.Contains(value, "MST") would be wrong, and in the surprising
// direction. Go's scanner consumes a layout left to right, so the M in
// "03:04PMST.md" is taken by the PM element and "ST" is ordinary literal text —
// a substring test would reject that safe layout. There is nothing to miss in
// the other direction, because the grammar has no literal "MST": any MST the
// scanner reaches at a chunk boundary *is* the element, so "2006-MST-01.md",
// which reads exactly like a user typing the three letters on purpose, formats
// to "2026-UTC-09.md". A user cannot ask for those characters literally in the
// first place, so refusing the element takes nothing away from them.
func rendersZoneName(layout string) bool {
	return zoneNameProbeA.Format(layout) != zoneNameProbeB.Format(layout)
}

// messageValueRunes is how much of a layout or of its rendering a problem
// message quotes.
const messageValueRunes = 120

// elide bounds a document value before it is interpolated into a problem
// message.
//
// Both values these messages quote — the layout and what it renders — come
// straight from the settings file and have no length rule of their own, and the
// message multiplies them: one problem interpolates both, and a document can
// trip several problems at once. FR-058 puts the result in front of the user,
// where the GUI has a small fixed window to render it in, and FR-064 puts it in
// a JSONL log line, where a multi-megabyte field is charged against rotation
// thresholds measured in MiB. Without a bound a 155-byte layout produced a
// 50 MB error.
//
// The budget is counted in runes and cut on a rune boundary because the result
// is about to be %q-quoted: slicing bytes could split a multi-byte rune and put
// U+FFFD in a message whose whole job is to show the user what they typed.
// 120 is chosen to be several times any real date layout, so an honest document
// is never elided and the elision is itself a signal.
//
// This bounds the *message* only. Whether the keys should carry a length rule
// of their own — a rendered name over NAME_MAX fails at the write, not here —
// is a separate question and is deliberately not answered by this function.
func elide(value string) string {
	// Fast path on bytes: a rune count can never exceed the byte count, so a
	// value this short is under budget without decoding it.
	if len(value) <= messageValueRunes {
		return value
	}

	runes := []rune(value)
	if len(runes) <= messageValueRunes {
		return value
	}

	return string(runes[:messageValueRunes]) + "…"
}

// validateRenderedFilename rejects a filename layout that renders something
// other than a plain name.
//
// Neither existing rule constrains this. The layout check asks only whether the
// string is a layout, and the .md suffix check is satisfied by any string
// ending in those three characters — a traversal prefix included. The gap
// matters because data-model.md fixes the composition as
// filepath.Join(daily_note_dir, now.Format(filename_format)), so every
// character the layout renders lands in the path:
//
//   - "2006/01/02.md" is a plausible typo for a nested date hierarchy, and it
//     is the reason this check cannot live in the sink. It renders
//     "2026/09/03.md", and the sink opens its target with O_APPEND|O_RDWR|
//     O_NOFOLLOW (plus O_CREATE when permitted) and no MkdirAll, so the
//     subdirectory is never created and that destination fails with ENOENT
//     every day until someone edits the file.
//   - "../../../../etc/cron.d/2006-01-02.md" renders an escape from the vault.
//     The sink appends with the user's privileges, so accepting it turns a
//     settings file into an append-anywhere primitive.
//   - A NUL truncates the path at the syscall boundary, and any other control
//     character produces a name that cannot be typed back or read out of the
//     error messages and log lines this value later appears in.
//
// FR-055 and FR-058 want every settings problem surfaced before any sink
// starts, and validation is the only moment at which the user is still looking
// at the file they typed the layout into — so the guard belongs here rather
// than at the point of the first failed write.
func validateRenderedFilename(found *problems, key, value, rendered string) {
	// A ".." path *element* can only be the whole rendered name once every
	// separator is refused: anything longer needs a separator to make ".." an
	// element, and that separator is rejected on the same line. A leading
	// separator falls out of the same rule.
	if strings.ContainsAny(rendered, pathSeparators) || rendered == ".." {
		found.addf("%s must render a plain filename, but %q renders %q; the rendered value is "+
			"joined onto sink.obsidian.daily_note_dir, so a path separator or a .. element "+
			"would write into a directory the sink never creates or outside the vault entirely",
			key, elide(value), elide(rendered))
	}

	// Reported through %q so the message stays readable and stays on one line:
	// writing the offending character out raw would put a NUL or a newline into
	// the user-facing error and into the log line that records it.
	//
	// This runs even when the separator rule already fired, because the two are
	// separate defects with separate fixes; the loop stops at the first control
	// character because naming every one of them would say nothing more.
	for _, r := range rendered {
		if unicode.IsControl(r) {
			found.addf("%s must not render control characters, but %q renders %q, "+
				"which contains U+%04X", key, elide(value), elide(rendered), r)

			break
		}
	}
}

// validateRenderedTimePrefix rejects a time layout that renders a line break.
//
// FR-048 requires one entry to be exactly one physical UTF-8 line, and FR-047's
// <br> normalization applies to the message body only — nothing downstream
// touches the time prefix. So a layout of "15:04\n" splits every entry across
// two physical lines with nothing to rejoin them, corrupting the note silently
// and identically on every later post. Only CR and LF are refused: any other
// control character in a time prefix is ugly, but it does not break the one
// structural promise the note format makes.
func validateRenderedTimePrefix(found *problems, key, value, rendered string) {
	if strings.ContainsAny(rendered, "\r\n") {
		found.addf("%s must render a single line, but %q renders %q; a daily-note entry is "+
			"exactly one physical line and nothing rejoins a time prefix that was split",
			key, elide(value), elide(rendered))
	}
}
