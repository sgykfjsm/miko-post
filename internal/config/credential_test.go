package config_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/config"
)

const (
	fileToken = "FILE-TOKEN-a1b2c3"
	envToken  = "ENV-TOKEN-d4e5f6"
)

// TestResolveCredentialPrecedence is FR-042's four-case matrix.
//
// The cases are not independent: "both set" is the only one that proves a
// precedence rather than a fallback, and it is the one an implementation that
// checks the file first still passes three quarters of. The tests set a
// process-wide variable, so none of them is parallel.
func TestResolveCredentialPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		fileToken string
		envToken  string
		unsetEnv  bool
		want      string
	}{
		{
			name:      "environment only",
			fileToken: "",
			envToken:  envToken,
			want:      envToken,
		},
		{
			name:      "file only",
			fileToken: fileToken,
			unsetEnv:  true,
			want:      fileToken,
		},
		{
			name:      "both set: the environment wins",
			fileToken: fileToken,
			envToken:  envToken,
			want:      envToken,
		},
		{
			name:      "neither set",
			fileToken: "",
			unsetEnv:  true,
			want:      "",
		},
		{
			// Not one of the four, and the reason it is here is that the
			// obvious implementation gets it wrong in the damaging direction:
			// an env check written as os.Getenv(...) != "" is correct, but one
			// written with LookupEnv's ok alone overrides a perfectly good
			// file token with nothing, and the user sees an authentication
			// failure with no indication that their file was ignored.
			name:      "environment set but empty: the file survives",
			fileToken: fileToken,
			envToken:  "",
			want:      fileToken,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(config.TelegramBotTokenEnv, test.envToken)

			if test.unsetEnv {
				if err := os.Unsetenv(config.TelegramBotTokenEnv); err != nil {
					t.Fatalf("os.Unsetenv: %v", err)
				}
			}

			settings := config.Defaults()
			if test.fileToken != "" {
				settings.Sink.Telegram.BotToken = config.NewSecret(test.fileToken)
			}

			config.ResolveCredential(&settings)

			if got := settings.Sink.Telegram.BotToken.Reveal(); got != test.want {
				t.Errorf("resolved credential = %q, want %q", got, test.want)
			}

			if got := settings.Sink.Telegram.BotToken.IsEmpty(); got != (test.want == "") {
				t.Errorf("IsEmpty() = %t, want %t", got, test.want == "")
			}
		})
	}
}

// TestMissingCredentialIsAValidationError closes the fourth case: an absent
// credential is not merely absent, it fails validation once the sink is enabled
// (FR-055). Without this, "neither set" above would pass while the program went
// on to make an unauthenticated request.
func TestMissingCredentialIsAValidationError(t *testing.T) {
	unsetToken(t)

	settings := config.Defaults()
	settings.Sink.Telegram.Enabled = true
	settings.Sink.Telegram.ChatID = "-100123"

	config.ResolveCredential(&settings)

	err := settings.Validate()
	if err == nil {
		t.Fatal("an enabled telegram sink with no credential should not validate")
	}

	var validationErr *config.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is %T, want *config.ValidationError", err)
	}

	if !strings.Contains(err.Error(), "bot_token") {
		t.Errorf("the error should name the missing key, got: %v", err)
	}

	// The message must tell the user about the override, or the environment
	// variable is a feature nobody can discover from the failure it causes.
	if !strings.Contains(err.Error(), config.TelegramBotTokenEnv) {
		t.Errorf("the error should name %s, got: %v", config.TelegramBotTokenEnv, err)
	}
}

// TestDisabledSinkNeedsNoCredential is the other half of "required when
// enabled": a user who has not configured a sink they do not use must not be
// blocked by it.
func TestDisabledSinkNeedsNoCredential(t *testing.T) {
	unsetToken(t)

	settings := config.Defaults()
	settings.Sink.Obsidian.Enabled = true
	settings.Sink.Obsidian.DailyNoteDir = t.TempDir()

	if err := settings.Validate(); err != nil {
		t.Errorf("a disabled telegram sink should not require a credential: %v", err)
	}
}

// unsetToken removes the credential variable for the duration of one test.
//
// t.Setenv comes first even though the value is discarded: it is what records
// the original value and registers the cleanup that restores it, so the
// Unsetenv that follows is undone when the test ends. Calling Unsetenv alone
// would leak the change into every test that ran afterwards.
func unsetToken(t *testing.T) {
	t.Helper()
	t.Setenv(config.TelegramBotTokenEnv, "")

	if err := os.Unsetenv(config.TelegramBotTokenEnv); err != nil {
		t.Fatalf("os.Unsetenv: %v", err)
	}
}
