package config_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	// The zone-transition case needs a Location that actually transitions.
	// Embedding the database rather than reading the system one keeps that case
	// from degrading into a skip on a machine — a slim container, or Windows —
	// that has no zoneinfo, which is precisely where a missing check would go
	// unnoticed. It costs the test binary only.
	_ "time/tzdata"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// enabledSettings returns a document that validates cleanly, so each case below
// can introduce exactly one defect and attribute the resulting problem to it.
func enabledSettings(t *testing.T) config.Settings {
	t.Helper()

	settings := config.Defaults()
	settings.Sink.Telegram.Enabled = true
	settings.Sink.Telegram.BotToken = config.NewSecret("token")
	settings.Sink.Telegram.ChatID = "-100123"
	settings.Sink.Obsidian.Enabled = true
	settings.Sink.Obsidian.DailyNoteDir = t.TempDir()

	return settings
}

// TestValidateAcceptsTheDefaults is the baseline every other case rests on. If
// Defaults() did not validate, the defaults would be a document no user could
// write.
func TestValidateAcceptsTheDefaults(t *testing.T) {
	t.Parallel()

	if err := config.Defaults().Validate(); err != nil {
		t.Fatalf("the defaults should validate: %v", err)
	}
}

func TestValidateRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*config.Settings)
		wantKey string
	}{
		{
			name:    "absolute daily_note_dir is required",
			mutate:  func(s *config.Settings) { s.Sink.Obsidian.DailyNoteDir = "vault/daily" },
			wantKey: "sink.obsidian.daily_note_dir",
		},
		{
			name:    "parse_mode must be a known mode",
			mutate:  func(s *config.Settings) { s.Sink.Telegram.ParseMode = "MarkdownV3" },
			wantKey: "sink.telegram.parse_mode",
		},
		{
			name:    "http_timeout_seconds must be positive",
			mutate:  func(s *config.Settings) { s.Sink.Telegram.HTTPTimeoutSeconds = 0 },
			wantKey: "sink.telegram.http_timeout_seconds",
		},
		{
			name:    "sink_timeout_seconds must be positive",
			mutate:  func(s *config.Settings) { s.Posting.SinkTimeoutSeconds = -1 },
			wantKey: "posting.sink_timeout_seconds",
		},
		{
			name:    "success_close_seconds must not be negative",
			mutate:  func(s *config.Settings) { s.GUI.SuccessCloseSeconds = -1 },
			wantKey: "gui.success_close_seconds",
		},
		{
			name:    "logging.format must be jsonl",
			mutate:  func(s *config.Settings) { s.Logging.Format = "text" },
			wantKey: "logging.format",
		},
		{
			name:    "rotate_size_mib must be positive",
			mutate:  func(s *config.Settings) { s.Logging.RotateSizeMiB = 0 },
			wantKey: "logging.rotate_size_mib",
		},
		{
			name:    "rotate_after_days must be positive",
			mutate:  func(s *config.Settings) { s.Logging.RotateAfterDays = 0 },
			wantKey: "logging.rotate_after_days",
		},
		{
			name:    "filename_format must end in .md",
			mutate:  func(s *config.Settings) { s.Sink.Obsidian.FilenameFormat = "2006-01-02" },
			wantKey: "sink.obsidian.filename_format",
		},
		{
			name:    "filename_format must not be empty",
			mutate:  func(s *config.Settings) { s.Sink.Obsidian.FilenameFormat = "" },
			wantKey: "sink.obsidian.filename_format",
		},
		{
			name:    "time_format must not be empty",
			mutate:  func(s *config.Settings) { s.Sink.Obsidian.TimeFormat = "" },
			wantKey: "sink.obsidian.time_format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			test.mutate(&settings)

			err := settings.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}

			if !strings.Contains(err.Error(), test.wantKey) {
				t.Errorf("error should name %s, got: %v", test.wantKey, err)
			}
		})
	}
}

// TestValidateRejectsNonLayouts is the case a format/parse round trip cannot
// catch and a user is most likely to hit.
//
// "YYYY-MM-DD.md" is what nearly every other language's date formatting looks
// like, so it is the first thing someone writes. It contains no Go reference
// elements, so it formats to itself and parses back unchanged — a round-trip
// check accepts it, and every note in the vault's history then goes into one
// file literally named YYYY-MM-DD.md.
func TestValidateRejectsNonLayouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		filenameFormat string
		timeFormat     string
		wantValid      bool
	}{
		{
			name:           "the defaults are layouts",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04",
			wantValid:      true,
		},
		{
			// Separator-free on purpose. "2006/01/02-Mon.md" is a real layout
			// and was accepted here before, but what it *renders* is a nested
			// path, which TestValidateRejectsUnsafeRenderedValues now refuses;
			// this case is about layout detection, so it uses a spelling that
			// isolates that question.
			name:           "other real layouts",
			filenameFormat: "2006-01-02-Mon.md",
			timeFormat:     "3:04PM",
			wantValid:      true,
		},
		{
			name:           "strftime-style placeholders are not Go layouts",
			filenameFormat: "%Y-%m-%d.md",
			timeFormat:     "15:04",
			wantValid:      false,
		},
		{
			name:           "YYYY-MM-DD is literal text, not a layout",
			filenameFormat: "YYYY-MM-DD.md",
			timeFormat:     "15:04",
			wantValid:      false,
		},
		{
			name:           "a constant filename is literal text",
			filenameFormat: "daily.md",
			timeFormat:     "15:04",
			wantValid:      false,
		},
		{
			name:           "HH:MM is literal text, not a layout",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "HH:MM",
			wantValid:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Obsidian.FilenameFormat = test.filenameFormat
			settings.Sink.Obsidian.TimeFormat = test.timeFormat

			err := settings.Validate()
			if test.wantValid && err != nil {
				t.Fatalf("should validate: %v", err)
			}

			if !test.wantValid && err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

// TestValidateRejectsUnsafeRenderedValues covers the other half of what these
// two keys have to satisfy: not just being a layout, but rendering something
// the destination can hold.
//
// Every case here passed validation with a nil error before the rendered-value
// checks existed, because "is this a Go layout?" and "does it end in .md?" are
// both satisfied by a traversal prefix, a nested path, and an embedded newline
// alike. The two that matter most are at opposite ends of intent:
// "2006/01/02.md" is an honest typo that would make the Obsidian sink fail with
// ENOENT forever, and the cron.d case is a settings file being used as an
// append-anywhere primitive.
func TestValidateRejectsUnsafeRenderedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		filenameFormat string
		timeFormat     string
		wantValid      bool
		wantKey        string
	}{
		{
			name:           "the defaults render safe values",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04",
			wantValid:      true,
		},
		{
			name:           "a nested date hierarchy renders a subdirectory the sink never creates",
			filenameFormat: "2006/01/02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a traversal prefix escapes the vault",
			filenameFormat: "../../../../../../../../etc/cron.d/2006-01-02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a single parent element escapes the vault",
			filenameFormat: "../2006-01-02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a leading separator renders an absolute path",
			filenameFormat: "/etc/2006-01-02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			// Rejected on Unix too, where it is merely two literal
			// backslashes, because the same document has to mean the same
			// thing on a platform where it is not.
			name:           "a backslash separator is rejected on every platform",
			filenameFormat: `2006\01\02.md`,
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a newline in the filename",
			filenameFormat: "2006\n-01-02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a NUL truncates the path at the syscall boundary",
			filenameFormat: "2006-01-02\x00.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a trailing newline in the time prefix splits the entry",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04\n",
			wantKey:        "sink.obsidian.time_format",
		},
		{
			name:           "an embedded newline in the time prefix splits the entry",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15\n:04",
			wantKey:        "sink.obsidian.time_format",
		},
		{
			name:           "a carriage return in the time prefix",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04\r",
			wantKey:        "sink.obsidian.time_format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Obsidian.FilenameFormat = test.filenameFormat
			settings.Sink.Obsidian.TimeFormat = test.timeFormat

			err := settings.Validate()

			if test.wantValid {
				if err != nil {
					t.Fatalf("should validate: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected a validation error")
			}

			if !strings.Contains(err.Error(), test.wantKey) {
				t.Errorf("error should name %s, got: %v", test.wantKey, err)
			}
		})
	}
}

// TestValidateRenderedChecksDoNotShortCircuit keeps the rendered-value rules
// inside the accumulator rather than in front of it, and independent of the
// layout rule rather than behind it. Those are two different guarantees, so
// each has its own case.
//
// The second case is the one the production comment names and the one that
// actually pins the ordering. "../daily.md" is *both* literal text and an
// escape from the vault: gate the rendered checks on the layout check having
// passed, and the traversal stops being reported at all, so the user fixes the
// layout they were told about, reruns, and only then discovers that the value
// also writes outside the vault. The first case cannot catch that — both of its
// formats are real layouts, so the gate it is meant to guard never closes.
func TestValidateRenderedChecksDoNotShortCircuit(t *testing.T) {
	t.Parallel()

	t.Run("one bad format does not hide the rest of the document", func(t *testing.T) {
		t.Parallel()

		settings := enabledSettings(t)
		settings.Sink.Obsidian.FilenameFormat = "../2006-01-02.md"
		settings.Sink.Obsidian.TimeFormat = "15:04\n"
		settings.Logging.Format = "text"
		settings.Posting.SinkTimeoutSeconds = 0

		err := settings.Validate()
		if err == nil {
			t.Fatal("expected a validation error")
		}

		var validationErr *config.ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("error is %T, want *config.ValidationError", err)
		}

		for _, key := range []string{
			"sink.obsidian.filename_format",
			"sink.obsidian.time_format",
			"logging.format",
			"posting.sink_timeout_seconds",
		} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("the error should still name %s, got:\n%v", key, err)
			}
		}
	})

	t.Run("literal text that also renders unsafely reports both problems", func(t *testing.T) {
		t.Parallel()

		settings := enabledSettings(t)
		settings.Sink.Obsidian.FilenameFormat = "../daily.md"

		err := settings.Validate()
		if err == nil {
			t.Fatal("expected a validation error")
		}

		var validationErr *config.ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("error is %T, want *config.ValidationError", err)
		}

		// Exactly two: the document is otherwise clean, so any other count means
		// a rule stopped running or started running twice.
		if len(validationErr.Problems) != 2 {
			t.Fatalf("reported %d problems, want 2:\n  %s",
				len(validationErr.Problems), strings.Join(validationErr.Problems, "\n  "))
		}

		for _, want := range []string{
			"is not a Go time layout",
			"must render a plain filename",
		} {
			found := false

			for _, problem := range validationErr.Problems {
				if strings.Contains(problem, "sink.obsidian.filename_format") &&
					strings.Contains(problem, want) {
					found = true
				}
			}

			if !found {
				t.Errorf("no problem names the key and says %q; got:\n  %s",
					want, strings.Join(validationErr.Problems, "\n  "))
			}
		}
	})
}

// TestValidateRejectsTheZoneNameElement pins the rule that makes the
// rendered-value checks decidable at all.
//
// Go's MST element is the only one that copies text out of the environment: it
// emits the zone abbreviation verbatim, and $TZ may point at an arbitrary TZif
// whose abbreviation is under no character constraint. So the contract-legal
// "2006-01-02MST.md" can render an escape from the vault, and "15:04 MST" can
// render a line break that splits a daily-note entry. Refusing the element is
// what removes the environment from the rendered value; inspecting what it
// happens to render is not an option, for the reason
// TestValidateAcceptedLayoutsAreInstantIndependent demonstrates.
//
// The numeric offsets are the other half of the rule and matter just as much:
// they render digits, "+", "-" and ":" only, so they are structurally safe and
// a user who genuinely needs the zone in the name keeps a way to say so.
func TestValidateRejectsTheZoneNameElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		filenameFormat string
		timeFormat     string
		wantKey        string
	}{
		{
			name:           "the zone name in the filename",
			filenameFormat: "2006-01-02MST.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "the zone name in the time prefix",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04 MST",
			wantKey:        "sink.obsidian.time_format",
		},
		{
			// Go's scanner finds the element wherever it starts a chunk, not
			// only at the end, and so must the rule.
			name:           "the zone name in the middle of a layout",
			filenameFormat: "2006-MST-01-02.md",
			timeFormat:     "15:04",
			wantKey:        "sink.obsidian.filename_format",
		},
		{
			name:           "a layout that is nothing but the zone name",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "MST",
			wantKey:        "sink.obsidian.time_format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Obsidian.FilenameFormat = test.filenameFormat
			settings.Sink.Obsidian.TimeFormat = test.timeFormat

			err := settings.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}

			if !strings.Contains(err.Error(), test.wantKey) {
				t.Errorf("error should name %s, got: %v", test.wantKey, err)
			}

			// The message has to say where the text comes from, because the
			// layout looks entirely reasonable and the reason it is refused is
			// not visible in the document.
			for _, want := range []string{"must not render the zone name", "$TZ"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should say %q, got: %v", want, err)
				}
			}
		})
	}
}

// TestValidateAcceptsZoneOffsetsAndLiteralLookalikes guards the two ways the
// zone-name rule could be too broad.
//
// Rejecting every layout mentioning a zone would be the easy rule and the wrong
// one: the numeric offsets carry the same information and render from a fixed
// alphabet of digits and punctuation, so nothing an attacker controls reaches
// the path. And detection has to be Go's, not a substring search — the M in
// "03:04PMST.md" is consumed by the PM element, leaving "ST" as ordinary
// literal text, so strings.Contains(value, "MST") would reject a layout that
// never renders a zone at all.
func TestValidateAcceptsZoneOffsetsAndLiteralLookalikes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		filenameFormat string
		timeFormat     string
	}{
		{
			name:           "the colon-separated offset",
			filenameFormat: "2006-01-02Z07:00.md",
			timeFormat:     "15:04Z07:00",
		},
		{
			name:           "the compact offset",
			filenameFormat: "2006-01-02Z0700.md",
			timeFormat:     "15:04Z0700",
		},
		{
			name:           "the hour-only offset",
			filenameFormat: "2006-01-02Z07.md",
			timeFormat:     "15:04Z07",
		},
		{
			name:           "the always-numeric offset",
			filenameFormat: "2006-01-02-0700.md",
			timeFormat:     "15:04 -07:00",
		},
		{
			name:           "the offset with seconds",
			filenameFormat: "2006-01-02Z070000.md",
			timeFormat:     "15:04:05-07:00:00",
		},
		{
			// "MST" as a substring, but not as an element.
			name:           "PM followed by literal ST",
			filenameFormat: "2006-01-02-03:04PMST.md",
			timeFormat:     "03:04PMST",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Obsidian.FilenameFormat = test.filenameFormat
			settings.Sink.Obsidian.TimeFormat = test.timeFormat

			if err := settings.Validate(); err != nil {
				t.Fatalf("should validate: %v", err)
			}
		})
	}
}

// TestValidateAcceptedLayoutsAreInstantIndependent is the case that says why
// the zone-name rule had to be a rule about the layout rather than a check on
// what the layout renders right now.
//
// Fixing a Location does not fix a zone. A Location holds arbitrarily many and
// picks one per instant, so America/New_York — an ordinary zone, no crafted
// TZif needed — renders EST in January and EDT in July through one and the same
// Location. Any validator that formatted an instant and approved the result
// would therefore be approving one of the zones the Location can produce while
// the sink writes at some other instant under another one, and a hostile TZif
// with a POSIX footer makes that schedule the attacker's to choose. The test
// asserts the property that replaced it: with the zone name refused, the
// verdict does not move with the instant.
//
// time/tzdata is imported so this holds on a machine with no system zone
// database, which would otherwise turn the case into a silent skip.
func TestValidateAcceptedLayoutsAreInstantIndependent(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load a zone with real transitions: %v", err)
	}

	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, newYork)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, newYork)

	// The premise of the whole case. If these ever matched, the instants below
	// would no longer straddle a transition and everything after would pass
	// without testing anything.
	if winter.Format("MST") == summer.Format("MST") {
		t.Fatalf("both instants render the zone as %q; the case needs a transition between them",
			winter.Format("MST"))
	}

	tests := []struct {
		name           string
		filenameFormat string
		timeFormat     string
		wantValid      bool
	}{
		{
			name:           "the defaults",
			filenameFormat: "2006-01-02.md",
			timeFormat:     "15:04",
			wantValid:      true,
		},
		{
			// The offset differs between the two instants — -05:00 against
			// -04:00 — and stays safe in both, which is the point: what varies
			// with the instant is digits, not attacker text.
			name:           "a numeric offset that differs across the transition",
			filenameFormat: "2006-01-02Z07:00.md",
			timeFormat:     "15:04Z07:00",
			wantValid:      true,
		},
		{
			name:           "the zone name, refused at both instants",
			filenameFormat: "2006-01-02MST.md",
			timeFormat:     "15:04",
		},
		{
			name:           "a traversal, refused at both instants",
			filenameFormat: "../2006-01-02.md",
			timeFormat:     "15:04",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Obsidian.FilenameFormat = test.filenameFormat
			settings.Sink.Obsidian.TimeFormat = test.timeFormat

			for _, instant := range []time.Time{winter, summer} {
				err := settings.ValidateAt(instant)

				if test.wantValid && err != nil {
					t.Errorf("should validate at %s (zone %s): %v",
						instant.Format(time.RFC3339), instant.Format("MST"), err)
				}

				if !test.wantValid && err == nil {
					t.Errorf("should not validate at %s (zone %s)",
						instant.Format(time.RFC3339), instant.Format("MST"))
				}
			}
		})
	}
}

// TestValidateDetectsLayoutsAgainstTheProbeNotTheClock makes an invariant that
// the production comment argues at length actually executable.
//
// Layout detection asks whether formatting changed the string, and it has to
// ask that of the fixed probe. Ask it of the supplied instant instead and the
// shipped defaults become self-rejecting for exactly as long as they render as
// themselves: "15:04" is indistinguishable from literal text at 15:04, and
// "2006-01-02.md" was indistinguishable from it for one day in 2006. Load then
// returns zero Settings and the app refuses to post — a once-a-day, one-minute
// startup failure that no scheduled test run would ever be awake for.
//
// The instant is chosen so both default layouts render as themselves at once,
// which is what makes the case a single assertion rather than two.
func TestValidateDetectsLayoutsAgainstTheProbeNotTheClock(t *testing.T) {
	t.Parallel()

	// 2006-01-02 15:04 — Go's own reference time, the one instant at which
	// every reference element is its own rendering.
	selfRendering := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)

	settings := enabledSettings(t)

	// Stated rather than assumed: if the defaults ever stop rendering as
	// themselves here, the case is no longer testing what it claims to.
	for _, layout := range []string{
		settings.Sink.Obsidian.FilenameFormat,
		settings.Sink.Obsidian.TimeFormat,
	} {
		if got := selfRendering.Format(layout); got != layout {
			t.Fatalf("%q renders %q at the reference time; the case needs it to render as itself",
				layout, got)
		}
	}

	if err := settings.ValidateAt(selfRendering); err != nil {
		t.Fatalf("the defaults must stay layouts at the instant they render as themselves: %v", err)
	}
}

// TestValidateUsesTheWallClock pins Validate's own behaviour rather than
// ValidateAt's.
//
// Every zone and rendering assertion in this file goes through the ValidateAt
// seam, which leaves the one line that supplies the instant — and therefore
// Validate itself — asserted by nothing: replacing time.Now() with the fixed
// probe kept the entire suite green while restoring the defect the seam was
// introduced to fix. These two cases are what that mutation now has to survive.
func TestValidateUsesTheWallClock(t *testing.T) {
	t.Parallel()

	t.Run("the instant advances between calls", func(t *testing.T) {
		t.Parallel()

		settings := enabledSettings(t)

		// Renders the nanosecond, so two calls differ unless the instant is
		// frozen, and renders a separator, so the problem message quotes the
		// rendering in the first place.
		settings.Sink.Obsidian.FilenameFormat = "2006-01-02T15:04:05.000000000/x.md"

		first := settings.Validate()
		if first == nil {
			t.Fatal("expected a validation error")
		}

		// Any positive delay well under a second forces a different rendering:
		// the element is the nanosecond within the second, so a collision would
		// need the two calls to be a whole number of seconds apart.
		time.Sleep(2 * time.Millisecond)

		second := settings.Validate()
		if second == nil {
			t.Fatal("expected a validation error")
		}

		if first.Error() == second.Error() {
			t.Fatalf("two calls quoted the same rendering, so Validate is not reading the clock: %v",
				first)
		}
	})

	t.Run("the instant is local", func(t *testing.T) {
		t.Parallel()

		settings := enabledSettings(t)

		// The offset is the part of the rendering that encodes the Location, and
		// unlike the date it does not change under the test's feet.
		const layout = "2006-01-02Z07:00/x.md"

		settings.Sink.Obsidian.FilenameFormat = layout

		// Bracketing the call covers the one rendering change that can happen
		// during it — a date rollover — without making the assertion fuzzy.
		before := time.Now().Format(layout)

		err := settings.Validate()
		if err == nil {
			t.Fatal("expected a validation error")
		}

		after := time.Now().Format(layout)

		if !strings.Contains(err.Error(), fmt.Sprintf("%q", before)) &&
			!strings.Contains(err.Error(), fmt.Sprintf("%q", after)) {
			t.Errorf("the problem should quote the local rendering %q, got: %v", before, err)
		}
	})
}

// TestValidateBoundsTheQuotedValues keeps a problem message a message.
//
// Both values a rendered-value problem interpolates come straight from the
// document and have no length rule of their own, so without a bound the message
// is a multiple of whatever the file contains: a 155-byte layout once produced a
// 50 MB error. That error is not a string that stays in memory — FR-058 renders
// it to the user, and FR-064 writes it into a JSONL log line whose rotation
// threshold is measured in MiB.
func TestValidateBoundsTheQuotedValues(t *testing.T) {
	t.Parallel()

	// A megabyte of literal text in front of a real layout: a valid layout, an
	// unsafe rendering, and both of the quoted values enormous.
	huge := strings.Repeat("x", 1<<20) + "/2006-01-02.md"

	settings := enabledSettings(t)
	settings.Sink.Obsidian.FilenameFormat = huge

	err := settings.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}

	// Generous enough that rewording the message never breaks the case, and
	// four orders of magnitude below the unbounded size it is guarding against.
	const budget = 4096

	if got := len(err.Error()); got > budget {
		t.Errorf("the error is %d bytes, want at most %d; the quoted values are not bounded", got, budget)
	}

	if !strings.Contains(err.Error(), "…") {
		t.Errorf("an elided message should say so, got: %v", err)
	}

	// Bounding must not cost the message its point: the reason for the
	// rejection still has to be in there.
	if !strings.Contains(err.Error(), "must render a plain filename") {
		t.Errorf("the bounded message should still name the rule, got: %v", err)
	}
}

// TestValidateQuotesTheLocalRendering keeps the actionable half of the message
// honest: the value quoted is the one the sink is going to hand to
// filepath.Join, not a rendering of some other instant.
func TestValidateQuotesTheLocalRendering(t *testing.T) {
	t.Parallel()

	settings := enabledSettings(t)
	settings.Sink.Obsidian.FilenameFormat = "2006/01/02.md"

	// 21:47 UTC is already the next day in Tokyo, so a rendering taken at any
	// other Location would be visibly the wrong one here.
	now := time.Date(2026, 9, 3, 21, 47, 53, 0, time.UTC).In(time.FixedZone("JST", 9*60*60))

	err := settings.ValidateAt(now)
	if err == nil {
		t.Fatal("expected a validation error")
	}

	var validationErr *config.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is %T, want *config.ValidationError", err)
	}

	// Exactly one: the document is otherwise clean, and a second copy of the
	// same problem is what checking a second instant used to produce.
	if len(validationErr.Problems) != 1 {
		t.Fatalf("reported %d problems, want 1:\n  %s",
			len(validationErr.Problems), strings.Join(validationErr.Problems, "\n  "))
	}

	if !strings.Contains(validationErr.Problems[0], "2026/09/04.md") {
		t.Errorf("the problem should quote the local rendering, got: %s", validationErr.Problems[0])
	}
}

// TestValidateThreadID covers the distinction the pointer exists for: absent
// and zero are different, and only one of them is legal.
func TestValidateThreadID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threadID  *int64
		wantValid bool
	}{
		{name: "absent", threadID: nil, wantValid: true},
		{name: "positive", threadID: pointerTo(int64(42)), wantValid: true},
		{name: "zero is not a sentinel for absent", threadID: pointerTo(int64(0)), wantValid: false},
		{name: "negative", threadID: pointerTo(int64(-1)), wantValid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := enabledSettings(t)
			settings.Sink.Telegram.ThreadID = test.threadID

			err := settings.Validate()
			if test.wantValid && err != nil {
				t.Fatalf("should validate: %v", err)
			}

			if !test.wantValid {
				if err == nil {
					t.Fatal("expected a validation error")
				}

				if !strings.Contains(err.Error(), "sink.telegram.thread_id") {
					t.Errorf("error should name the key, got: %v", err)
				}
			}
		})
	}
}

// TestValidateReportsEveryProblem is the accumulation guarantee at the unit
// level: eight independent defects, eight reported problems, in one call.
func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	settings := config.Defaults()
	settings.Sink.Telegram.Enabled = true
	settings.Sink.Telegram.ParseMode = "nope"
	settings.Sink.Telegram.HTTPTimeoutSeconds = 0
	settings.Sink.Obsidian.Enabled = true
	settings.Sink.Obsidian.FilenameFormat = "notes.md"
	settings.Posting.SinkTimeoutSeconds = 0
	settings.GUI.ErrorCloseSeconds = -5
	settings.Logging.Format = "text"

	err := settings.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}

	var validationErr *config.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is %T, want *config.ValidationError", err)
	}

	// bot_token, chat_id, parse_mode, http_timeout_seconds, daily_note_dir,
	// filename_format, sink_timeout_seconds, error_close_seconds, format.
	const want = 9

	if len(validationErr.Problems) != want {
		t.Errorf("reported %d problems, want %d:\n  %s",
			len(validationErr.Problems), want, strings.Join(validationErr.Problems, "\n  "))
	}

	// A single-problem error reads as a sentence and a multi-problem error as a
	// list; both shapes are user-facing, so both are asserted rather than left
	// to whatever Join happens to produce.
	if !strings.Contains(err.Error(), "9 problems") {
		t.Errorf("the aggregated message should count the problems, got: %v", err)
	}
}

// TestValidationErrorSingularForm covers the other rendering branch.
func TestValidationErrorSingularForm(t *testing.T) {
	t.Parallel()

	settings := config.Defaults()
	settings.Logging.Format = "text"

	err := settings.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}

	if strings.Contains(err.Error(), "problems") {
		t.Errorf("a single problem should not be rendered as a list, got: %v", err)
	}

	if !strings.HasPrefix(err.Error(), "invalid settings: ") {
		t.Errorf("unexpected message shape: %v", err)
	}
}

func pointerTo[T any](value T) *T {
	return &value
}

// TestValidateBoundsTimeoutSecondsFromAbove is issues #109 and #114.
//
// Both keys were bounded only from below, and the residue that leaves is not the
// obvious overflow. A value that wraps negative or to zero is caught by any
// floor downstream — post.New has one, telegram.requestTimeout has one — but
// 18446744074 wraps to a *positive* 290.448384ms: it passed validation, passed
// both floors, and handed a user who asked for ~584 years a deadline three times
// tighter than the default they were trying to raise, with nothing anywhere
// saying so.
//
// The table pins the exact value each issue names, plus the one above it, plus
// the two rows that were already caught so a fix that only moved the floor is
// visible. The boundary pair is the part that fails a bound written with the
// wrong comparison: MaxTimeoutSeconds itself must be accepted and one more
// refused.
func TestValidateBoundsTimeoutSecondsFromAbove(t *testing.T) {
	t.Parallel()

	// Both keys, so a fix applied to one of them fails here rather than leaving
	// its sibling open — which is exactly how #109 and #114 came to be two
	// issues describing one defect.
	keys := []struct {
		key string
		set func(*config.Settings, int)
	}{
		{
			key: "posting.sink_timeout_seconds",
			set: func(s *config.Settings, v int) { s.Posting.SinkTimeoutSeconds = v },
		},
		{
			key: "sink.telegram.http_timeout_seconds",
			set: func(s *config.Settings, v int) { s.Sink.Telegram.HTTPTimeoutSeconds = v },
		},
	}

	cases := []struct {
		name     string
		seconds  int
		accepted bool
	}{
		{name: "the default-shaped value", seconds: 60, accepted: true},
		{name: "one second", seconds: 1, accepted: true},
		{name: "zero", seconds: 0},
		{name: "negative", seconds: -1},
		{name: "the largest whole second a duration holds", seconds: int(config.MaxTimeoutSeconds), accepted: true},
		{name: "one past the largest", seconds: int(config.MaxTimeoutSeconds) + 1},
		{name: "the value that wraps negative", seconds: 9223372037},
		{name: "the value that wraps to 290ms", seconds: 18446744074},
		{name: "the value that wraps to 1.29s", seconds: 18446744075},
	}

	for _, key := range keys {
		for _, tc := range cases {
			t.Run(key.key+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				settings := enabledSettings(t)
				key.set(&settings, tc.seconds)

				err := settings.Validate()

				if tc.accepted {
					if err != nil {
						t.Fatalf("Validate() rejected %s = %d: %v", key.key, tc.seconds, err)
					}

					return
				}

				if err == nil {
					t.Fatalf("Validate() accepted %s = %d, which converts to %s",
						key.key, tc.seconds, time.Duration(tc.seconds)*time.Second)
				}

				// The problem must name the key, or an accumulated report of
				// several problems tells the user to fix the wrong line.
				if !strings.Contains(err.Error(), key.key) {
					t.Errorf("the problem does not name %s: %v", key.key, err)
				}

				// And it must not name the other key, which is what a shared
				// guard called with the wrong label would do.
				for _, other := range keys {
					if other.key != key.key && strings.Contains(err.Error(), other.key) {
						t.Errorf("the problem for %s also names %s: %v", key.key, other.key, err)
					}
				}
			})
		}
	}
}

// TestTheTimeoutBoundIsTheOneThatCannotOverflow pins what MaxTimeoutSeconds is,
// rather than trusting that whatever it happens to be is right.
//
// A bound is only a bound if it is below the wrap point. This asserts that the
// largest accepted value still converts to a positive duration of the size it
// reads as, and that one more second does not — which is the arithmetic the
// whole rule exists for, and the thing a hand-typed constant gets wrong by one.
func TestTheTimeoutBoundIsTheOneThatCannotOverflow(t *testing.T) {
	t.Parallel()

	// Every conversion here routes the seconds through a variable rather than
	// using config.MaxTimeoutSeconds directly. It is a typed constant, so the
	// direct form is a constant expression the compiler evaluates — and a
	// mutant that raised the bound past the wrap point would then fail to
	// compile this file instead of failing this test, which is a red suite for
	// the wrong reason and tells the next reader nothing about the bound.
	bound := config.MaxTimeoutSeconds

	atBound := time.Duration(bound) * time.Second
	if atBound <= 0 {
		t.Fatalf("MaxTimeoutSeconds = %d converts to %s, which is not a usable timeout",
			config.MaxTimeoutSeconds, atBound)
	}

	if want := int64(atBound / time.Second); want != config.MaxTimeoutSeconds {
		t.Errorf("converting MaxTimeoutSeconds and back gives %d, want %d",
			want, config.MaxTimeoutSeconds)
	}

	// One second more must wrap. Routed through a variable so the overflow is
	// the runtime's: as a constant expression the compiler refuses to build it,
	// which is the same fact stated at compile time and no use as an assertion.
	beyond := bound + 1
	past := time.Duration(beyond) * time.Second
	if past > 0 {
		t.Errorf("one second past the bound converts to %s, which is still positive — "+
			"the bound is lower than it needs to be, or the wrap point moved", past)
	}
}
