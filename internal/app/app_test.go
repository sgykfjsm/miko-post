package app_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// validSettings is a document that config.Validate accepts with both
// destinations enabled, so each test below can disable exactly what it is about.
func validSettings(t *testing.T) config.Settings {
	t.Helper()

	settings := config.Defaults()
	settings.Sink.Telegram.Enabled = true
	settings.Sink.Telegram.BotToken = config.NewSecret("7654321:AA-test-token")
	settings.Sink.Telegram.ChatID = "-100123"
	settings.Sink.Obsidian.Enabled = true
	settings.Sink.Obsidian.DailyNoteDir = t.TempDir()

	if err := settings.Validate(); err != nil {
		t.Fatalf("the fixture does not validate: %v", err)
	}

	return settings
}

// TestOnlyEnabledSinksAreBuilt is T036 and FR-016.
//
// The assertion is on the names, over all four combinations, and both of those
// choices are the test rather than decoration.
//
// Asserting a count is what this test would naturally have been, and a count
// cannot see the defect worth fearing: `if settings.Sink.Telegram.Enabled {
// sinks = append(sinks, obsidian.New(...)) }` builds exactly one sink for
// exactly one enabled destination and satisfies every length check that could be
// written. Names are the only observable that distinguishes the sink that was
// asked for from the sink that was built.
//
// All four combinations, because three of them are one-line special cases of the
// implementation and the fourth — neither enabled — is the one with a documented
// contract of its own: it must produce nothing, so that post.AllSucceeded's
// empty-slice rule reports the post as failed rather than vacuously succeeding.
func TestOnlyEnabledSinksAreBuilt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		obsidian bool
		telegram bool
		want     []string
	}{
		{
			name:     "both enabled",
			obsidian: true,
			telegram: true,
			want:     []string{obsidian.SinkName, telegram.SinkName},
		},
		{
			name:     "only obsidian",
			obsidian: true,
			want:     []string{obsidian.SinkName},
		},
		{
			name:     "only telegram",
			telegram: true,
			want:     []string{telegram.SinkName},
		},
		{
			name: "neither enabled",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := validSettings(t)
			settings.Sink.Obsidian.Enabled = tt.obsidian
			settings.Sink.Telegram.Enabled = tt.telegram

			built := app.Sinks(settings)

			var names []string
			for _, sink := range built {
				names = append(names, sink.Name())
			}

			if !slices.Equal(names, tt.want) {
				t.Fatalf("built %v, want %v", names, tt.want)
			}

			// The order is part of the contract: post.Outcome.Results preserves
			// the order sinks were given, and the CLI report is rendered from
			// it. slices.Equal above already enforces it; this says so out loud
			// so a later change to the expectation is a deliberate one.
			if tt.obsidian && tt.telegram && names[0] != obsidian.SinkName {
				t.Errorf("the report order changed: first sink is %q", names[0])
			}
		})
	}
}

// TestNoSinkIsBuiltWhenNeitherIsEnabled pins the consequence of the empty case
// rather than only its shape.
//
// A nil slice is not the interesting part; what matters is that a post through
// it reaches no destination and still exits non-zero, because
// post.AllSucceeded's empty-slice rule is the only thing standing between "no
// destination was enabled" and "everything succeeded".
func TestNoSinkIsBuiltWhenNeitherIsEnabled(t *testing.T) {
	t.Parallel()

	settings := validSettings(t)
	settings.Sink.Obsidian.Enabled = false
	settings.Sink.Telegram.Enabled = false

	outcome := app.NewService(settings, nil).Post(post.Message{Original: "nowhere to go"})

	if len(outcome.Results) != 0 {
		t.Fatalf("a post with no enabled destination produced %d results", len(outcome.Results))
	}

	if outcome.Succeeded() {
		t.Error("a post that reached no destination reported success")
	}
}

// TestSinksAreTheConcreteTypesTheSettingsName guards against the two sinks being
// swapped, which is a defect Name() alone cannot see: nothing stops
// obsidian.Sink from being constructed with the telegram settings, and the name
// would still read "obsidian".
func TestSinksAreTheConcreteTypesTheSettingsName(t *testing.T) {
	t.Parallel()

	built := app.Sinks(validSettings(t))

	if len(built) != 2 {
		t.Fatalf("built %d sinks, want both", len(built))
	}

	if _, ok := built[0].(*obsidian.Sink); !ok {
		t.Errorf("the first sink is %T, want *obsidian.Sink", built[0])
	}

	if _, ok := built[1].(*telegram.Sink); !ok {
		t.Errorf("the second sink is %T, want *telegram.Sink", built[1])
	}
}

// TestTheObsidianSinkIsBuiltFromItsOwnSettings closes the other half of the
// swap: the right type built from the wrong settings table.
//
// It observes the filesystem rather than asking the sink where it went, and the
// change of mechanism is deliberate. This test used to read post.Targeter's
// Target(), which decision DEC-D3 removed — a getter on a sink that serves every
// post can only report the sink's most recent note, which is issue #111. Its
// failure message ("the obsidian sink no longer reports a target") would now be
// actively misleading, because the interface it named is gone by design rather
// than by regression.
//
// Reading the vault is the stronger observation anyway. A sink handed the zero
// ObsidianSettings has an empty DailyNoteDir, and filepath.Join("", …) resolves
// relative to the working directory — which is how a mutation run in this
// repository once left a stray daily note inside a source package. Asserting
// that the note landed *under the configured vault* fails for that sink; asking
// the sink for a string it computed itself could still agree with a wrong
// answer.
func TestTheObsidianSinkIsBuiltFromItsOwnSettings(t *testing.T) {
	t.Parallel()

	settings := validSettings(t)
	settings.Sink.Telegram.Enabled = false

	vault := settings.Sink.Obsidian.DailyNoteDir

	built := app.Sinks(settings)
	if len(built) != 1 {
		t.Fatalf("built %d sinks, want one", len(built))
	}

	// Proof that the assertion below can fail: nothing is in the vault yet, so
	// finding a note there afterwards is this Send's doing and not a fixture's.
	if before, err := os.ReadDir(vault); err == nil && len(before) != 0 {
		t.Fatalf("the vault already holds %d entries before the post; "+
			"the assertion below would pass without the sink writing anything", len(before))
	}

	const body = "into the vault"

	if err := built[0].Send(t.Context(), post.Message{Original: body}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	notes, err := os.ReadDir(vault)
	if err != nil {
		t.Fatalf("reading the configured vault %s: %v", vault, err)
	}

	if len(notes) != 1 {
		t.Fatalf("the configured vault %s holds %d notes, want the one this post wrote; "+
			"a sink built from the wrong settings table writes elsewhere", vault, len(notes))
	}

	written, err := os.ReadFile(filepath.Join(vault, notes[0].Name()))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}

	if !strings.Contains(string(written), body) {
		t.Errorf("the note under %s does not contain the posted message; it holds %q",
			vault, written)
	}
}

// The telegram sink's settings wiring is asserted at the process level instead,
// by TestTheBinaryReportsPartialFailure in cmd/mp: that run configures
// bot_token = "%zz", which makes url.JoinPath fail while the request URL is
// being built, so the chat destination fails before any socket is opened. A
// sink built from a zero config.TelegramSettings would have an empty token, a
// well-formed URL, and would reach the network — which is both the defect and,
// under the constitution's no-live-services rule, a test failure of its own
// kind. There is no in-process equivalent here: everything that observes the
// built client (its timeout, its base URL) is behind internal/sink/telegram's
// own export_test.go and is not visible from this package.

// TestSinkTimeoutConvertsSecondsToADuration covers the conversion DEC-D1 moved
// here out of internal/post.
//
// The wrapping rows are the ones with history: 18446744074 is the value issues
// #109 and #114 name, and it is now unreachable through a validated Settings.
// This asserts both halves of that claim — the conversion is a plain
// multiplication, and config.Validate is what makes the plain multiplication
// safe — so a later relaxation of the validation bound fails here rather than
// silently reintroducing a 290ms deadline.
func TestSinkTimeoutConvertsSecondsToADuration(t *testing.T) {
	t.Parallel()

	boundSeconds := config.MaxTimeoutSeconds

	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "the default", seconds: 60, want: 60 * time.Second},
		{name: "one second", seconds: 1, want: time.Second},
		// Routed through a variable, not the typed constant directly: the
		// direct form is a constant expression, so a raised bound would fail to
		// compile this file rather than fail an assertion.
		{name: "the validation bound", seconds: int(config.MaxTimeoutSeconds),
			want: time.Duration(boundSeconds) * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := validSettings(t)
			settings.Posting.SinkTimeoutSeconds = tt.seconds

			if err := settings.Validate(); err != nil {
				t.Fatalf("the fixture no longer validates: %v", err)
			}

			if got := app.SinkTimeout(settings); got != tt.want {
				t.Errorf("SinkTimeout = %s, want %s", got, tt.want)
			}
		})
	}

	// The overflow value, asserted as unreachable rather than as clamped. If
	// this ever validates, the conversion above becomes wrong and the fix is in
	// internal/config, not here.
	overflowing := validSettings(t)
	overflowing.Posting.SinkTimeoutSeconds = 18446744074

	if err := overflowing.Validate(); err == nil {
		t.Fatalf("sink_timeout_seconds = 18446744074 validated, and SinkTimeout converts it to %s",
			app.SinkTimeout(overflowing))
	}
}

// readOneRecord returns the single JSON object at path, failing if there is not
// exactly one.
func readOneRecord(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the log holds %d lines, want 1:\n%s", len(lines), raw)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("the record is not JSON: %v\n%s", err, lines[0])
	}

	return record
}

// TestOpenLoggerArmsTheCredentialScrub is issue #41's wiring obligation.
//
// Options.Redact is a chokepoint that is completely inert until something
// populates it, and the value it needs — the resolved bot token — is known only
// here. So "the scrub is armed" is a property of this function and of nothing
// else, and the only way to see it is to put the token into a record and read
// the record back.
//
// The record is written through the real logger to a real file rather than
// through a fake, because what is being checked is that a token reaches the
// disk redacted; a fake would be asserting this test's own idea of what
// redaction does.
func TestOpenLoggerArmsTheCredentialScrub(t *testing.T) {
	const token = "7654321:AA-the-sentinel-token"

	settings := validSettings(t)
	settings.Sink.Telegram.BotToken = config.NewSecret(token)
	settings.Logging.Path = filepath.Join(t.TempDir(), "app.jsonl")

	logger := app.OpenLogger(settings, logging.SourceCLI)

	logger.Post("scrub").Error(logging.EventTelegramSendFailed,
		slog.String("error", "Post \"https://api.telegram.org/bot"+token+"/sendMessage\": refused"))

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(settings.Logging.Path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if strings.Contains(string(raw), token) {
		t.Fatalf("the bot token reached the log:\n%s", raw)
	}
}

// TestOpenLoggerStampsTheBuildIdentityOnlyWhenAsked covers the two settings
// whose wiring is expressed by passing a value or the empty string.
//
// A caller that passed the values unconditionally would satisfy every test that
// only checked the enabled case, and the user's include_version = false would
// have no effect anywhere in the program.
func TestOpenLoggerStampsTheBuildIdentityOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name           string
		includeVersion bool
		includeCommit  bool
	}{
		{name: "both included", includeVersion: true, includeCommit: true},
		{name: "neither included"},
		{name: "version only", includeVersion: true},
		{name: "commit only", includeCommit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := validSettings(t)
			settings.Logging.Path = filepath.Join(t.TempDir(), "app.jsonl")
			settings.Logging.IncludeVersion = tt.includeVersion
			settings.Logging.IncludeGitCommit = tt.includeCommit

			logger := app.OpenLogger(settings, logging.SourceCLI)
			logger.Post("stamp").Info(logging.EventMessageReceived)

			if err := logger.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			record := readOneRecord(t, settings.Logging.Path)

			if _, present := record["app_version"]; present != tt.includeVersion {
				t.Errorf("app_version present = %t, want %t (record: %v)",
					present, tt.includeVersion, record)
			}

			if _, present := record["git_commit"]; present != tt.includeCommit {
				t.Errorf("git_commit present = %t, want %t (record: %v)",
					present, tt.includeCommit, record)
			}
		})
	}
}

// TestOpenLoggerRecordsTheSourceItWasGiven pins the parameter that decides
// whether a saved query filtering source = "cli" sees a run at all.
//
// logging.Open substitutes "unknown" for a Source it does not recognise rather
// than refusing, which is right for the logger and means a wiring mistake here
// is silent — the records exist, they are just attributed to nothing.
func TestOpenLoggerRecordsTheSourceItWasGiven(t *testing.T) {
	for _, source := range []logging.Source{logging.SourceCLI, logging.SourceGUI} {
		t.Run(string(source), func(t *testing.T) {
			settings := validSettings(t)
			settings.Logging.Path = filepath.Join(t.TempDir(), "app.jsonl")

			logger := app.OpenLogger(settings, source)
			logger.Post("source").Info(logging.EventMessageReceived)

			if err := logger.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			record := readOneRecord(t, settings.Logging.Path)

			if got := record["source"]; got != string(source) {
				t.Errorf("source = %v, want %q", got, source)
			}
		})
	}
}

// TestOpenLoggerUsesTheConfiguredPathAndTheDefaultWhenUnset is the wiring half
// of issue #107.
//
// The logger applies the "empty means the default" rule itself, so what this
// asserts is that OpenLogger hands the setting over unchanged rather than
// pre-resolving it — pre-resolving is the workaround #107 explicitly rejects,
// because it leaves the trap in place for the second front door.
func TestOpenLoggerUsesTheConfiguredPathAndTheDefaultWhenUnset(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	defaultPath, err := config.DefaultLogPath()
	if err != nil {
		t.Fatalf("resolve the default: %v", err)
	}

	unset := validSettings(t)
	unset.Logging.Path = ""

	logger := app.OpenLogger(unset, logging.SourceCLI)
	if got := logger.Path(); got != defaultPath {
		t.Errorf("an unset logging.path resolved to %q, want %q", got, defaultPath)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	chosen := filepath.Join(t.TempDir(), "elsewhere.jsonl")

	configured := validSettings(t)
	configured.Logging.Path = chosen

	logger = app.OpenLogger(configured, logging.SourceCLI)
	if got := logger.Path(); got != chosen {
		t.Errorf("a configured logging.path resolved to %q, want %q", got, chosen)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
