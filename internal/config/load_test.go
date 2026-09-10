package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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

// TestLoadAggregatesEveryUnknownKey pins the plural rendering of a strict
// decode failure.
//
// Every committed fixture and every case in TestLoadNeverEchoesTheDocument
// happens to produce exactly one strict error, so the singular branch was the
// only one ever executed and the aggregated shape — the count, the list, and
// the fact that the *last* offender is reported at all — was unpinned. An
// implementation that reported only strict.Errors[0] passed the whole suite.
//
// The document is written to a temporary directory rather than committed as a
// fixture so it can carry the credential sentinel: FR-043 applies no less to
// the plural branch, and this is the branch that builds the longest message out
// of the most pieces of the failure.
func TestLoadAggregatesEveryUnknownKey(t *testing.T) {
	unsetToken(t)

	document := "[sink.telegram]\nenabled = true\nchat_id = \"-100123\"\n" +
		"bot_token = \"" + sentinel + "\"\n" +
		"surprise = 1\nanother = 2\n\n[logging]\nnope = true\n"

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected a load error")
	}

	message := err.Error()

	// The count and the per-key list are both asserted: the count alone would
	// pass for a message that miscounted its own list, and the list alone would
	// pass for a message that dropped the header.
	if !strings.Contains(message, "3 unknown or misplaced keys") {
		t.Errorf("the error should count the offending keys, got:\n%v", err)
	}

	for _, key := range []string{
		"sink.telegram.surprise",
		"sink.telegram.another",
		"logging.nope",
	} {
		if !strings.Contains(message, key) {
			t.Errorf("the error should name %s, got:\n%v", key, err)
		}
	}

	if !strings.Contains(message, path) {
		t.Errorf("the error should name the settings path %q, got:\n%v", path, err)
	}

	if strings.Contains(message, sentinel) {
		t.Errorf("the aggregated load error echoed the credential: %v", err)
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
// failure when the tests run as root, which is a normal way for CI to run. It
// is also the one non-regular kind every platform can produce, so it is where
// the refusal's wording is asserted; the kinds that hang or never end are in
// load_unix_test.go.
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

	// The kind, by name, in the refusal's own sentence and not in EISDIR's.
	//
	// "a directory" alone is unfailable here and was: os.ReadFile's own message
	// is "read <path>: is a directory", so an assertion on that substring
	// passes whether the path was refused before the read or relayed after it.
	// A mutant that disabled the refusal entirely survived exactly that way.
	// The phrase below is written by refuseUnreadable and by nothing else.
	if !strings.Contains(err.Error(), "is a directory, which cannot be read") {
		t.Errorf("error should refuse the path by naming what is at it, got: %v", err)
	}
}

// TestLoadRefusesADocumentTooLargeToBeSettings is the size half of the read
// guard (ADV-001).
//
// The regular-file check does not bound the read: a 500 MB regular document
// decodes nothing and peaks at about 1.5 GB of resident memory finding that
// out. Both sides of the limit are asserted, because only the pair pins the
// comparison — a guard written with >= instead of > refuses a document that is
// exactly the limit, and a test that only oversized would not notice.
//
// The documents are pure comment, which is valid TOML that decodes to the
// defaults, so the accepted case fails only if the size guard refused it and
// not because of anything about its content.
func TestLoadRefusesADocumentTooLargeToBeSettings(t *testing.T) {
	unsetToken(t)

	// "# " plus padding, sized to the byte.
	document := func(size int) []byte {
		return []byte("# " + strings.Repeat("x", size-2))
	}

	dir := t.TempDir()

	atLimit := filepath.Join(dir, "at-limit.toml")
	if err := os.WriteFile(atLimit, document(config.MaxSettingsFileBytes), 0o600); err != nil {
		t.Fatalf("write the document at the limit: %v", err)
	}

	if _, err := config.Load(atLimit); err != nil {
		t.Errorf("a document of exactly %d bytes was refused: %v",
			config.MaxSettingsFileBytes, err)
	}

	overLimit := filepath.Join(dir, "over-limit.toml")
	if err := os.WriteFile(overLimit, document(config.MaxSettingsFileBytes+1), 0o600); err != nil {
		t.Fatalf("write the oversized document: %v", err)
	}

	_, err := config.Load(overLimit)
	if err == nil {
		t.Fatalf("a document of %d bytes was accepted", config.MaxSettingsFileBytes+1)
	}

	if !strings.Contains(err.Error(), overLimit) {
		t.Errorf("error should name the path %q, got: %v", overLimit, err)
	}

	// The limit itself, so the user can tell a size refusal from a parse
	// failure without reading the source.
	if !strings.Contains(err.Error(), strconv.Itoa(config.MaxSettingsFileBytes)) {
		t.Errorf("error should name the %d-byte limit, got: %v",
			config.MaxSettingsFileBytes, err)
	}
}

// TestFileKindNamesEveryRefusedType covers the phrases the settings refusal
// uses when something that is not a regular file sits at the -c path.
//
// The point of naming the type is that "the settings path is a named pipe" is
// immediately actionable where a relayed errno is not, so each branch has to
// actually produce its phrase. Driven by mode rather than by real files: see
// export_test.go. Mirrors TestFileKindNamesEveryRejectedType in
// internal/logging, which owns the same table for the log path.
func TestFileKindNamesEveryRefusedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{name: "directory", mode: os.ModeDir | 0o755, want: "a directory"},
		{name: "named pipe", mode: os.ModeNamedPipe | 0o600, want: "a named pipe"},
		{name: "socket", mode: os.ModeSocket | 0o600, want: "a socket"},
		// A device is refused here where internal/logging allows one, so this
		// row is the one that is not a copy: /dev/null is a way to silence a
		// log and not a way to configure anything.
		{name: "character device", mode: os.ModeDevice | os.ModeCharDevice | 0o666, want: "a device file"},
		// Not a symlink: os.Stat follows links, so a symlink mode never
		// reaches fileKind. This is the fallback arm, driven by the one mode
		// that names no specific kind.
		{name: "an irregular file", mode: os.ModeIrregular | 0o600, want: "unsupported type"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := config.FileKind(test.mode)
			if !strings.Contains(got, test.want) {
				t.Errorf("FileKind(%s) = %q, want it to mention %q", test.mode, got, test.want)
			}
		})
	}
}

// TestLoadRefusesARegularFileItCannotRead keeps the "there and unreadable"
// branch reachable.
//
// It used to be reached by the directory in TestLoadUnreadableFile, which the
// non-regular refusal now stops before the read — so without this the branch
// that distinguishes a permission failure from a missing file would be one no
// test had executed, in the code whose whole job is telling those two apart.
//
// A mode-0 regular file is the only trigger left, and it is not one when the
// tests run as root, which is a normal way for CI to run. Skipped there rather
// than asserted, following internal/logging and internal/sink/obsidian.
func TestLoadRefusesARegularFileItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: an unreadable file is still readable")
	}

	unsetToken(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# nothing to see\n"), 0o000); err != nil {
		t.Fatalf("write the unreadable document: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected an error for a file that cannot be read")
	}

	// Not "missing". The file is sitting there and telling the user it does not
	// exist sends them to create one that already exists.
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an unreadable file was reported as missing: %v", err)
	}

	if !strings.Contains(err.Error(), "read settings file") {
		t.Errorf("error should say the file could not be read, got: %v", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the path %q, got: %v", path, err)
	}
}
