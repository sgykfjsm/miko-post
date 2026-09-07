package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// sentinel is a value no legitimate render can contain, so any appearance of it
// in output is a leak and nothing else.
const sentinel = "SENTINEL-BOT-TOKEN-3f9a1c"

// TestSecretRedactsUnderEveryRender is the FR-043/FR-069 gate for the type.
//
// It sweeps every fmt verb rather than checking the handful a reasonable
// programmer would use, because the leak this guards against is by definition
// the render nobody meant to write. A bare String implementation passes %v and
// %s and leaks under %d, %t and %#v.
func TestSecretRedactsUnderEveryRender(t *testing.T) {
	t.Parallel()

	secret := config.NewSecret(sentinel)

	t.Run("fmt verbs", func(t *testing.T) {
		t.Parallel()

		// Every ASCII letter, in both cases, with and without the # and +
		// flags — including %p and %w, which fmt answers before it consults
		// Formatter and which therefore dump the struct's fields directly.
		// They are held safe by the field being a pointer rather than by any
		// method, so they belong in the sweep exactly like the rest: this is
		// the assertion that would fail if the field became a plain string.
		for _, letter := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
			for _, flags := range []string{"", "#", "+", "#+"} {
				format := "%" + flags + string(letter)
				rendered := fmt.Sprintf(format, secret)

				if strings.Contains(rendered, sentinel) {
					t.Errorf("%s leaked the credential: %s", format, rendered)
				}
			}
		}
	})

	t.Run("String", func(t *testing.T) {
		t.Parallel()

		if got := secret.String(); got != "[redacted]" {
			t.Errorf("String() = %q, want %q", got, "[redacted]")
		}
	})

	t.Run("GoString", func(t *testing.T) {
		t.Parallel()

		if got := secret.GoString(); strings.Contains(got, sentinel) {
			t.Errorf("GoString() leaked the credential: %s", got)
		}
	})

	t.Run("encoding/json", func(t *testing.T) {
		t.Parallel()

		encoded, err := json.Marshal(secret)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		if bytes.Contains(encoded, []byte(sentinel)) {
			t.Errorf("json.Marshal leaked the credential: %s", encoded)
		}

		if string(encoded) != `"[redacted]"` {
			t.Errorf("json.Marshal = %s, want %q", encoded, `"[redacted]"`)
		}
	})

	t.Run("slog", func(t *testing.T) {
		t.Parallel()

		var buffer bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buffer, nil))
		logger.Info("post", slog.Any("token", secret))
		logger.Info("post", "token", secret)

		if strings.Contains(buffer.String(), sentinel) {
			t.Errorf("slog leaked the credential: %s", buffer.String())
		}
	})
}

// TestSecretInsideSettingsRedacts is the case the type actually exists for.
//
// A Secret is never printed on its own in this program; it is printed because
// someone printed the Settings that contains it. fmt walks a struct's fields
// and applies method dispatch to each, so this should hold — but "should" is
// what the test is for, and it would stop holding the moment the credential
// were copied into a plain string field anywhere in the struct.
func TestSecretInsideSettingsRedacts(t *testing.T) {
	t.Parallel()

	settings := config.Defaults()
	settings.Sink.Telegram.BotToken = config.NewSecret(sentinel)

	for _, letter := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		for _, flags := range []string{"", "#", "+", "#+"} {
			format := "%" + flags + string(letter)

			if rendered := fmt.Sprintf(format, settings); strings.Contains(rendered, sentinel) {
				t.Errorf("Settings rendered with %s leaked the credential: %s", format, rendered)
			}
		}
	}

	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if bytes.Contains(encoded, []byte(sentinel)) {
		t.Errorf("json.Marshal(Settings) leaked the credential: %s", encoded)
	}

	var buffer bytes.Buffer

	slog.New(slog.NewJSONHandler(&buffer, nil)).Info("settings", slog.Any("settings", settings))

	if strings.Contains(buffer.String(), sentinel) {
		t.Errorf("slog leaked the credential from Settings: %s", buffer.String())
	}
}

// TestSecretReveal covers the one deliberate way out, and the emptiness test
// validation depends on.
func TestSecretReveal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		isEmpty bool
	}{
		{name: "a credential", value: sentinel, isEmpty: false},
		{name: "empty", value: "", isEmpty: true},
		{name: "whitespace is not empty", value: " ", isEmpty: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			secret := config.NewSecret(test.value)

			if got := secret.Reveal(); got != test.value {
				t.Errorf("Reveal() = %q, want %q", got, test.value)
			}

			if got := secret.IsEmpty(); got != test.isEmpty {
				t.Errorf("IsEmpty() = %t, want %t", got, test.isEmpty)
			}
		})
	}
}

// TestSecretZeroValueIsEmpty pins the state a Settings has before decoding: no
// credential, and safe to render.
func TestSecretZeroValueIsEmpty(t *testing.T) {
	t.Parallel()

	var secret config.Secret

	if !secret.IsEmpty() {
		t.Error("the zero Secret should be empty")
	}

	if got := fmt.Sprintf("%v", secret); got != "[redacted]" {
		t.Errorf("the zero Secret rendered as %q, want %q", got, "[redacted]")
	}
}
