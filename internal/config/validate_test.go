package config_test

import (
	"errors"
	"strings"
	"testing"

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
			name:           "other real layouts",
			filenameFormat: "2006/01/02-Mon.md",
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
