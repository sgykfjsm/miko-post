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
		// DEC-I2 (#137): both sources are trimmed. A padded token used to pass
		// validation and then escape redaction, because the redaction pattern
		// was the padded value and a message carrying the bare token did not
		// contain it.
		{
			name:      "file token padded: trimmed",
			fileToken: " \t" + fileToken + " \n",
			unsetEnv:  true,
			want:      fileToken,
		},
		{
			name:      "environment token padded: trimmed, and it still wins",
			fileToken: fileToken,
			envToken:  "\n" + envToken + "\r\n",
			want:      envToken,
		},
		{
			// The whitespace-only variable is the same user intent as the
			// empty one above, so it must not override the file with nothing.
			name:      "environment only whitespace: absent, and the trimmed file survives",
			fileToken: fileToken + " ",
			envToken:  " \t\n",
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

// cleanCoreToken is a token of valid length with nothing wrong with it. The
// refusal cases below each spoil it in one way, so a refusal can only be the
// rule under test.
const cleanCoreToken = "1234567:AA-inner-space-token"

// TestANonPrintableASCIICredentialIsRefused is DEC-I2's other half and DEC-J9
// (#137): after trimming, a token must be printable ASCII other than space,
// 0x21 to 0x7E. Whitespace left after the trim is inside the value, and an
// invisible or non-ASCII character can be anywhere, because trimming does not
// remove it. No Bot API token contains any of these, and all are refused
// rather than repaired.
//
// Every case runs against both sources, because the file and the environment
// reach Validate through different branches of ResolveCredential, and with the
// sink enabled and disabled, because the token arms the redaction pattern
// either way. The message must not quote the value, because the value is the
// credential.
func TestANonPrintableASCIICredentialIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		token string
		// fragment is a piece of the token that no message may carry. The
		// whole token is checked too; the fragment catches a message that
		// quotes only part of it.
		fragment string
	}{
		// A plain space is 0x20, one below the range, so this is the case
		// that pins the lower bound.
		{name: "interior space", token: "1234567:AA-inner space-token", fragment: "inner space"},
		// A tab and a newline are what a paste or `$(cat file)` leaves. An
		// implementation that looked for the ASCII space alone would pass
		// the case above and let these through.
		{name: "interior tab", token: "1234567:AA-inner\tspace-token", fragment: "inner\tspace"},
		{name: "interior newline", token: "1234567:AA-inner\nspace-token", fragment: "inner\nspace"},
		// A zero-width space and a byte-order mark are not whitespace, so
		// trimming keeps them. At the end and at the start is where a copy from
		// a web page or a BOM-writing editor puts them, and where a trim that
		// knew them would have removed them.
		{name: "zero-width space suffix", token: cleanCoreToken + "\u200b", fragment: "\u200b"},
		{name: "byte-order mark prefix", token: "\ufeff" + cleanCoreToken, fragment: "\ufeff"},
		// A control that is not whitespace.
		{name: "interior control", token: "1234567:AA-inner\aspace-token", fragment: "inner\aspace"},
		// DEL is 0x7F, one above the range, so this is the case that pins
		// the upper bound.
		{name: "interior DEL", token: "1234567:AA-inner\x7fspace-token", fragment: "inner\x7fspace"},
		// These three are graphic runes that render as nothing, so DEC-J6's
		// category rule let them load and the cycle-1 review reproduced the
		// leak through them. Only a rule about the byte range catches them.
		{name: "variation selector suffix", token: cleanCoreToken + "\ufe0f", fragment: "\ufe0f"},
		{name: "Hangul filler suffix", token: cleanCoreToken + "\u3164", fragment: "\u3164"},
		{name: "braille blank suffix", token: cleanCoreToken + "\u2800", fragment: "\u2800"},
		// Visible non-ASCII lookalikes: a token copied out of a document that
		// curled its quotes, and an IME that typed the colon full width.
		{name: "smart quotes around", token: "\u201c" + cleanCoreToken + "\u201d", fragment: "\u201c"},
		{name: "fullwidth colon", token: "1234567\uff1aAA-inner-space-token", fragment: "1234567\uff1a"},
		// A byte that is not valid UTF-8. 0xA0 is a no-break space in
		// Latin-1, so it is what a token padded in that encoding carries, and
		// the Unicode trim keeps it, because alone it decodes as U+FFFD. The
		// rule reads bytes and refuses it as itself; a rune reading would see
		// U+FFFD and refuse it too (see Secret.IsPrintableASCII).
		{name: "invalid UTF-8 byte suffix", token: cleanCoreToken + "\xa0", fragment: "\xa0"},
	}

	for _, test := range tests {
		for _, source := range []string{"file", "environment"} {
			for _, enabled := range []bool{true, false} {
				state := map[bool]string{true: "enabled", false: "disabled"}[enabled]

				t.Run(test.name+"/"+source+"/"+state, func(t *testing.T) {
					settings := credentialSettings(t, enabled, source, test.token)

					err := settings.Validate()
					if err == nil {
						t.Fatal("a token that is not printable ASCII should not validate")
					}

					if !strings.Contains(err.Error(), "must contain only printable ASCII characters") {
						t.Errorf("the error should say why, got: %v", err)
					}

					if strings.Contains(err.Error(), test.token) || strings.Contains(err.Error(), test.fragment) ||
						strings.Contains(err.Error(), cleanCoreToken) {
						t.Errorf("the error quoted the credential: %q", err)
					}
				})
			}
		}
	}
}

// TestAnNBSPPaddedCredentialIsTrimmedAndValidates is the other direction of
// the rule above. A no-break space (U+00A0) is whitespace to unicode.IsSpace but
// not to a trim written for ASCII, so this pins that the trim is the Unicode
// one: an ASCII-only trim would leave the pad in place, and the refusal would
// then reject a token the user did nothing wrong with beyond copying it from a
// web page.
func TestAnNBSPPaddedCredentialIsTrimmedAndValidates(t *testing.T) {
	const padded = "\u00a0" + cleanCoreToken + "\u00a0"

	for _, source := range []string{"file", "environment"} {
		for _, enabled := range []bool{true, false} {
			state := map[bool]string{true: "enabled", false: "disabled"}[enabled]

			t.Run(source+"/"+state, func(t *testing.T) {
				settings := credentialSettings(t, enabled, source, padded)

				if got := settings.Sink.Telegram.BotToken.Reveal(); got != cleanCoreToken {
					t.Errorf("resolved credential = %q, want %q", got, cleanCoreToken)
				}

				if err := settings.Validate(); err != nil {
					t.Errorf("an NBSP-padded token should be trimmed and validate: %v", err)
				}
			})
		}
	}
}

// credentialSettings returns resolved settings whose token came from source
// ("file" or "environment"), with the chat sink enabled or disabled; when it is
// disabled the notes sink is enabled so that a destination exists.
//
// The environment case leaves the file empty, so the value validated is the
// environment's and nothing else. The variable is set with t.Setenv to a fake
// value and restored when the test ends.
func credentialSettings(t *testing.T, enabled bool, source, token string) config.Settings {
	t.Helper()

	settings := config.Defaults()
	settings.Sink.Telegram.Enabled = enabled
	settings.Sink.Telegram.ChatID = "-100123"
	settings.Sink.Obsidian.Enabled = !enabled
	settings.Sink.Obsidian.DailyNoteDir = "/tmp/miko-post-test-vault"

	switch source {
	case "file":
		unsetToken(t)
		settings.Sink.Telegram.BotToken = config.NewSecret(token)
	case "environment":
		t.Setenv(config.TelegramBotTokenEnv, token)
	default:
		t.Fatalf("unknown source %q", source)
	}

	config.ResolveCredential(&settings)

	return settings
}

// TestACleanCredentialValidates is the control for the refusal tests: the
// same shape with nothing spoiled validates from either source and in either
// sink state, so each failure above is the rule under test and nothing else.
//
// The punctuation case is the control for DEC-J9's range: every byte of it is
// printable ASCII, including the first and last of the range ('!' is 0x21 and
// '~' is 0x7E), so a rule narrowed to Telegram's alphabet, or a bound moved one
// step inward, refuses it and fails here.
func TestACleanCredentialValidates(t *testing.T) {
	tokens := map[string]string{
		"clean":       cleanCoreToken,
		"punctuation": "1234567:AA-_.~!token",
	}

	for name, token := range tokens {
		for _, source := range []string{"file", "environment"} {
			for _, enabled := range []bool{true, false} {
				state := map[bool]string{true: "enabled", false: "disabled"}[enabled]

				t.Run(name+"/"+source+"/"+state, func(t *testing.T) {
					settings := credentialSettings(t, enabled, source, token)

					if err := settings.Validate(); err != nil {
						t.Errorf("a printable-ASCII token should validate: %v", err)
					}
				})
			}
		}
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
