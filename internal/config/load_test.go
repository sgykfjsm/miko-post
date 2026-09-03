package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// fixture returns the path to a committed settings fixture.
//
// The fixtures live at the repository root rather than in a package-local
// testdata directory because tasks.md places them there (T020), which keeps one
// directory of example documents that a future contract test, the quickstart,
// and the example settings file (T087) can all point at.
func fixture(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join("..", "..", "testdata", "config", name)
}

// TestLoadValidFixture asserts that every key in the schema decodes to the
// value the document names.
//
// The fixture sets every key to something other than its default on purpose. A
// document written with default values would pass this test even if a struct
// tag were missing or misspelled, because the field would keep the default the
// assertion expects — the test would be checking Defaults, not decoding.
func TestLoadValidFixture(t *testing.T) {
	unsetToken(t)

	settings, err := config.Load(fixture(t, "valid.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	telegram := settings.Sink.Telegram
	if !telegram.Enabled {
		t.Error("sink.telegram.enabled should be true")
	}

	if got := telegram.BotToken.Reveal(); got != "fixture-token-not-a-real-credential" {
		t.Errorf("bot_token = %q", got)
	}

	if got := telegram.ChatID; got != "-1001234567890" {
		t.Errorf("chat_id = %q", got)
	}

	if telegram.ThreadID == nil {
		t.Error("thread_id should be present")
	} else if *telegram.ThreadID != 42 {
		t.Errorf("thread_id = %d, want 42", *telegram.ThreadID)
	}

	if got := telegram.HTTPTimeoutSeconds; got != 15 {
		t.Errorf("http_timeout_seconds = %d, want 15", got)
	}

	obsidian := settings.Sink.Obsidian
	if !obsidian.Enabled {
		t.Error("sink.obsidian.enabled should be true")
	}

	if got := obsidian.DailyNoteDir; got != "/tmp/miko-post-fixture-vault/daily" {
		t.Errorf("daily_note_dir = %q", got)
	}

	if got := settings.Posting.SinkTimeoutSeconds; got != 45 {
		t.Errorf("sink_timeout_seconds = %d, want 45", got)
	}

	if got := settings.GUI.SuccessCloseSeconds; got != 5 {
		t.Errorf("success_close_seconds = %d, want 5", got)
	}

	if got := settings.Logging.RotateSizeMiB; got != 5 {
		t.Errorf("rotate_size_mib = %d, want 5", got)
	}

	// The four booleans the fixture sets to false are the ones that default to
	// true. They are the reason decoding happens into Defaults() rather than a
	// zero Settings, and the reason an explicit false has to survive it.
	for _, check := range []struct {
		key   string
		value bool
	}{
		{"logging.message_on_error_only", settings.Logging.MessageOnErrorOnly},
		{"logging.stack_trace", settings.Logging.StackTrace},
		{"logging.include_version", settings.Logging.IncludeVersion},
		{"logging.include_git_commit", settings.Logging.IncludeGitCommit},
	} {
		if check.value {
			t.Errorf("%s = true, but the document says false", check.key)
		}
	}
}

// TestLoadAppliesDefaultsToAbsentKeys is the other half of the same mechanism:
// a nearly empty document must come back fully populated.
//
// all-disabled.toml also proves that a document with every sink disabled is a
// valid document. FR-018 makes that a startup error, but the error belongs to
// the front door (T081); Load rejecting it here would put the same rule in two
// places and give the user the wrong message from the wrong layer.
func TestLoadAppliesDefaultsToAbsentKeys(t *testing.T) {
	unsetToken(t)

	settings, err := config.Load(fixture(t, "all-disabled.toml"))
	if err != nil {
		t.Fatalf("a document with every sink disabled should load: %v", err)
	}

	defaults := config.Defaults()

	if settings != defaults {
		t.Errorf("loaded settings differ from the defaults:\n got %+v\nwant %+v", settings, defaults)
	}

	if settings.Sink.Telegram.ThreadID != nil {
		t.Error("an absent thread_id should stay nil, not become a zero value")
	}
}

// TestLoadRejectsUnknownAndMisplacedKeys covers FR-054 and strict decoding.
//
// The non-namespaced case is the one that matters most: without
// DisallowUnknownFields it is not an error at all. A top-level [telegram] table
// decodes into nothing, both sinks stay disabled, and the user is told to
// enable a sink they can see they have already enabled.
func TestLoadRejectsUnknownAndMisplacedKeys(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantInError []string
	}{
		{
			name:        "unknown key",
			file:        "unknown-key.toml",
			wantInError: []string{"unknown-key.toml", "sink.telegram.retry_count", "unknown field"},
		},
		{
			name:        "non-namespaced table",
			file:        "non-namespaced.toml",
			wantInError: []string{"non-namespaced.toml", "telegram", "missing table"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unsetToken(t)

			_, err := config.Load(fixture(t, test.file))
			if err == nil {
				t.Fatal("expected a load error")
			}

			for _, want := range test.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should mention %q, got: %v", want, err)
				}
			}
		})
	}
}

// TestLoadNeverEchoesTheDocument is the FR-043 gate on the load path.
//
// go-toml's own error rendering prints the offending line together with the
// lines around it, which for any problem inside [sink.telegram] reproduces
// bot_token. That output is exactly what a first implementation reaches for,
// because it is genuinely the most helpful presentation available — so this
// test exists to make the tempting version fail.
//
// Each case puts the sentinel token above a different class of defect, and one
// puts the defect on the credential's own line.
func TestLoadNeverEchoesTheDocument(t *testing.T) {
	const credential = "bot_token = \"" + sentinel + "\"\n"

	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "unknown key below the credential",
			document: "[sink.telegram]\nenabled = true\n" + credential + "surprise = 1\n",
		},
		{
			name:     "unknown key several lines below the credential",
			document: "[sink.telegram]\n" + credential + "chat_id = \"x\"\nenabled = true\nsurprise = 1\n",
		},
		{
			name:     "type mismatch below the credential",
			document: "[sink.telegram]\n" + credential + "enabled = \"yes\"\n",
		},
		{
			name:     "type mismatch on the credential's own line",
			document: "[sink.telegram]\nbot_token = " + sentinel + "\n",
		},
		{
			name:     "syntax error below the credential",
			document: "[sink.telegram]\n" + credential + "this is not toml\n",
		},
		{
			name:     "duplicate key",
			document: "[sink.telegram]\n" + credential + credential,
		},
		{
			name:     "misplaced table with the credential inside",
			document: "[telegram]\n" + credential,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unsetToken(t)

			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(test.document), 0o600); err != nil {
				t.Fatalf("write document: %v", err)
			}

			_, err := config.Load(path)
			if err == nil {
				t.Fatal("expected a load error")
			}

			if strings.Contains(err.Error(), sentinel) {
				t.Errorf("the load error echoed the credential: %v", err)
			}
		})
	}
}

// TestLoadAccumulatesValidationProblems covers FR-055 and FR-058 through the
// public entry point: one load, every problem.
//
// missing-required.toml holds four independent defects across three tables. An
// implementation that returns at the first one passes a test that only checks
// that loading failed, which is why the assertion is on the count and on each
// key by name.
func TestLoadAccumulatesValidationProblems(t *testing.T) {
	unsetToken(t)

	_, err := config.Load(fixture(t, "missing-required.toml"))
	if err == nil {
		t.Fatal("expected a validation error")
	}

	var validationErr *config.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is %T, want *config.ValidationError", err)
	}

	wantKeys := []string{
		"sink.telegram.bot_token",
		"sink.telegram.chat_id",
		"sink.obsidian.daily_note_dir",
		"posting.sink_timeout_seconds",
		"gui.error_close_seconds",
	}

	if len(validationErr.Problems) != len(wantKeys) {
		t.Errorf("reported %d problems, want %d:\n%v",
			len(validationErr.Problems), len(wantKeys), validationErr.Problems)
	}

	for _, key := range wantKeys {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the error should name %s, got:\n%v", key, err)
		}
	}
}

// TestLoadReturnsZeroSettingsOnFailure is FR-058 as a property of the return
// convention: a caller that ignores the error cannot get a half-configured
// sink out of this package.
func TestLoadReturnsZeroSettingsOnFailure(t *testing.T) {
	unsetToken(t)

	for _, name := range []string{"unknown-key.toml", "non-namespaced.toml", "missing-required.toml"} {
		t.Run(name, func(t *testing.T) {
			settings, err := config.Load(fixture(t, name))
			if err == nil {
				t.Fatal("expected a load error")
			}

			if settings != (config.Settings{}) {
				t.Errorf("Load returned populated settings alongside an error: %+v", settings)
			}

			if settings.Sink.Telegram.Enabled || settings.Sink.Obsidian.Enabled {
				t.Error("the settings returned with an error have a sink enabled")
			}
		})
	}
}

// TestLoadMissingFile pins the choice that an absent file is an error rather
// than "use the defaults", and that the error names the path.
//
// The defaults disable every sink, so accepting a missing file would surface as
// FR-018's "enable at least one destination" — which sends the user to their
// sink configuration when the actual problem is that the file they edited is
// not the file being read.
func TestLoadMissingFile(t *testing.T) {
	unsetToken(t)

	path := filepath.Join(t.TempDir(), "absent.toml")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error should wrap fs.ErrNotExist, got %v", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the path %q, got: %v", path, err)
	}
}

// TestLoadResolvesTheCredentialFromTheEnvironment proves the environment
// override is wired into Load and not only into ResolveCredential — the two
// can pass separately while nothing calls one from the other.
func TestLoadResolvesTheCredentialFromTheEnvironment(t *testing.T) {
	t.Setenv(config.TelegramBotTokenEnv, envToken)

	settings, err := config.Load(fixture(t, "valid.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := settings.Sink.Telegram.BotToken.Reveal(); got != envToken {
		t.Errorf("credential = %q, want the environment value %q", got, envToken)
	}
}

// TestLoadWithOnlyTheEnvironmentCredential covers the deployment the override
// exists for: a settings file with no credential in it at all.
func TestLoadWithOnlyTheEnvironmentCredential(t *testing.T) {
	t.Setenv(config.TelegramBotTokenEnv, envToken)

	document := "[sink.telegram]\nenabled = true\nchat_id = \"-100123\"\n"

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("a file-less credential should validate: %v", err)
	}

	if got := settings.Sink.Telegram.BotToken.Reveal(); got != envToken {
		t.Errorf("credential = %q, want %q", got, envToken)
	}
}

// TestLoadUnreadableFile covers the other read failure, which needs different
// words from a missing file: the path is right and something else is wrong, so
// telling the user the file does not exist would send them looking for a file
// that is sitting there.
//
// A directory is used rather than a chmod-ed file because the latter is not a
// failure when the tests run as root, which is a normal way for CI to run.
func TestLoadUnreadableFile(t *testing.T) {
	unsetToken(t)

	path := t.TempDir()

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected an error when the settings path is not a readable file")
	}

	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an unreadable path should not be reported as missing: %v", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the path %q, got: %v", path, err)
	}
}
