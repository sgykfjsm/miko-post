package cli_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/cli"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// -----------------------------------------------------------------------------
// T037: argument parsing, joining, and dispatch
// -----------------------------------------------------------------------------

// TestParseJoinsMessageArgumentsWithOneASCIISpace is FR-004, driven from a real
// argument vector through the real flag set.
//
// Testing the join function alone would prove nothing about any of the four
// things that actually go wrong between argv and a message: the flag parser
// consuming a bare word that was meant as text, `--` not being removed, an
// argument that contains a space being re-split, and an empty argument being
// dropped instead of contributing its separators. Every row below is one of
// those, and none of them is reachable from a []string handed straight to
// strings.Join.
func TestParseJoinsMessageArgumentsWithOneASCIISpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "a single quoted argument",
			argv: []string{"hello world"},
			want: "hello world",
		},
		{
			name: "bare words",
			argv: []string{"hello", "world"},
			want: "hello world",
		},
		{
			name: "three bare words",
			argv: []string{"a", "b", "c"},
			want: "a b c",
		},
		{
			name: "an argument that already contains spaces keeps them",
			argv: []string{"two  spaces", "and", "one"},
			want: "two  spaces and one",
		},
		{
			name: "an argument containing a tab is not re-split",
			argv: []string{"a\tb", "c"},
			want: "a\tb c",
		},
		{
			name: "an argument containing a newline is not re-split",
			argv: []string{"first\nsecond"},
			want: "first\nsecond",
		},
		{
			name: "an empty argument contributes its separators",
			argv: []string{"a", "", "b"},
			want: "a  b",
		},
		{
			name: "a trailing empty argument",
			argv: []string{"a", ""},
			want: "a ",
		},
		{
			name: "a word that looks like a flag after the message has started",
			argv: []string{"hello", "-x"},
			want: "hello -x",
		},
		{
			name: "a word that looks like this program's own flag",
			argv: []string{"hello", "-c", "not-a-path"},
			want: "hello -c not-a-path",
		},
		{
			name: "everything after -- is message text",
			argv: []string{"--", "-x"},
			want: "-x",
		},
		{
			name: "-- followed by something that looks like --config",
			argv: []string{"--", "--config", "x"},
			want: "--config x",
		},
		{
			name: "a message that is only a dash",
			argv: []string{"-"},
			want: "-",
		},
		{
			name: "the config flag is consumed and the rest is the message",
			argv: []string{"-c", "/tmp/x.toml", "hello", "world"},
			want: "hello world",
		},
		{
			name: "the long config flag is consumed too",
			argv: []string{"--config", "/tmp/x.toml", "hello"},
			want: "hello",
		},
		{
			name: "the config flag in its joined form",
			argv: []string{"--config=/tmp/x.toml", "hello"},
			want: "hello",
		},
		{
			name: "full-width spaces are not separators and are not touched",
			argv: []string{"日本語", "メモ"},
			want: "日本語 メモ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			invocation, err := cli.Parse(tt.argv)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tt.argv, err)
			}

			if invocation.Mode != cli.ModePost {
				t.Fatalf("Parse(%q) chose %s, want post", tt.argv, invocation.Mode)
			}

			if invocation.Message != tt.want {
				t.Errorf("Parse(%q) message = %q, want %q", tt.argv, invocation.Message, tt.want)
			}
		})
	}
}

// TestParseDispatchesToTheWindowOnlyWithNoMessage is FR-002 and FR-003.
//
// The `-c` rows are FR-005: the settings-file override applies to command-line
// posting only. Dropping it at the parse boundary is what makes that structural,
// and the assertion is that the window invocation carries no path at all rather
// than that some later code ignores one.
func TestParseDispatchesToTheWindowOnlyWithNoMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		argv       []string
		wantMode   cli.Mode
		wantConfig string
	}{
		{name: "no arguments", argv: nil, wantMode: cli.ModeWindow},
		{name: "an empty argument vector", argv: []string{}, wantMode: cli.ModeWindow},
		{
			name:     "the config flag alone",
			argv:     []string{"-c", "/tmp/x.toml"},
			wantMode: cli.ModeWindow,
		},
		{
			name:     "the long config flag alone",
			argv:     []string{"--config", "/tmp/x.toml"},
			wantMode: cli.ModeWindow,
		},
		{
			name:     "a bare -- with nothing after it",
			argv:     []string{"--"},
			wantMode: cli.ModeWindow,
		},
		{
			name:       "one empty message argument still posts",
			argv:       []string{""},
			wantMode:   cli.ModePost,
			wantConfig: "",
		},
		{
			name:       "a message with the config flag",
			argv:       []string{"-c", "/tmp/x.toml", "hello"},
			wantMode:   cli.ModePost,
			wantConfig: "/tmp/x.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			invocation, err := cli.Parse(tt.argv)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tt.argv, err)
			}

			if invocation.Mode != tt.wantMode {
				t.Errorf("Parse(%q) chose %s, want %s", tt.argv, invocation.Mode, tt.wantMode)
			}

			if invocation.ConfigPath != tt.wantConfig {
				t.Errorf("Parse(%q) config = %q, want %q",
					tt.argv, invocation.ConfigPath, tt.wantConfig)
			}
		})
	}
}

// TestParseRejectsWhatItCannotUnderstand covers the two error shapes, and the
// help flag, which is neither.
func TestParseRejectsWhatItCannotUnderstand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		// wantIs, when non-nil, is the sentinel the error must match.
		wantIs error
	}{
		{name: "an unknown flag before the message", argv: []string{"-x", "hello"}},
		{name: "an unknown long flag", argv: []string{"--nope"}},
		{name: "the config flag with no value", argv: []string{"-c"}},
		{name: "short help", argv: []string{"-h"}, wantIs: cli.ErrHelpNotAvailable},
		{name: "long help", argv: []string{"--help"}, wantIs: cli.ErrHelpNotAvailable},
		{
			name:   "help after a message is still help, because flag parsing stops at the message",
			argv:   []string{"hello", "-h"},
			wantIs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			invocation, err := cli.Parse(tt.argv)

			if tt.name == "help after a message is still help, because flag parsing stops at the message" {
				// Documented above as a consequence of the flag package's rule,
				// asserted here so the consequence is visible rather than
				// surprising: -h after a message is message text.
				if err != nil {
					t.Fatalf("Parse(%q) = %v, want the message", tt.argv, err)
				}

				if invocation.Message != "hello -h" {
					t.Errorf("Parse(%q) message = %q, want %q", tt.argv, invocation.Message, "hello -h")
				}

				return
			}

			if err == nil {
				t.Fatalf("Parse(%q) accepted it as %+v", tt.argv, invocation)
			}

			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("Parse(%q) = %v, want it to match %v", tt.argv, err, tt.wantIs)
			}

			// A failed parse must not leave a usable invocation behind: a caller
			// that logged the error and carried on would otherwise post an empty
			// message or open the window.
			if invocation != (cli.Invocation{}) {
				t.Errorf("Parse(%q) failed but returned %+v", tt.argv, invocation)
			}
		})
	}
}

// TestHelpIsRecognisedRatherThanTreatedAsATypo pins the deliberate gap.
//
// Help output is T080's (batch 11) and half of it is worse than none, so the
// flag is refused with a message that says it is not implemented. This test
// exists so that T080 has to change a test rather than discover the behaviour,
// and so the reason is written down next to the assertion.
func TestHelpIsRecognisedRatherThanTreatedAsATypo(t *testing.T) {
	t.Parallel()

	_, err := cli.Parse([]string{"--help"})
	if err == nil {
		t.Fatal("--help was accepted, so help is implemented and this test is stale")
	}

	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help produced %v, which does not match flag.ErrHelp — it is being "+
			"reported as an unknown flag, which reads to a user as their own typo", err)
	}

	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("the message does not say help is unimplemented: %q", err)
	}
}

// -----------------------------------------------------------------------------
// T038: result rendering
// -----------------------------------------------------------------------------

// TestRenderNamesEveryDestination is FR-062 and FR-063.
//
// Every case asserts the whole of stdout, not a substring. A Contains check
// passes on a report that also printed something it should not have — a raw
// error, a second copy of a line, a sink nobody enabled — and "completeness and
// brevity" is exactly what contracts/cli-interface.md requires of this output.
func TestRenderNamesEveryDestination(t *testing.T) {
	t.Parallel()

	const logPath = "/state/miko-post/app.jsonl"

	tests := []struct {
		name    string
		results []post.SinkResult
		logPath string
		want    string
	}{
		{
			name: "both succeeded",
			results: []post.SinkResult{
				{Name: "obsidian", Success: true},
				{Name: "telegram", Success: true},
			},
			logPath: logPath,
			want:    "Obsidian: success\nTelegram: success\n",
		},
		{
			name: "partial success names the failure and the log",
			results: []post.SinkResult{
				{Name: "obsidian", Success: true},
				{Name: "telegram", Reason: "request timed out"},
			},
			logPath: logPath,
			want: "Obsidian: success\nTelegram: failed — request timed out\n" +
				"See log for details: " + logPath + "\n",
		},
		{
			name: "the failure can be the first destination",
			results: []post.SinkResult{
				{Name: "obsidian", Reason: "delivery failed"},
				{Name: "telegram", Success: true},
			},
			logPath: logPath,
			want: "Obsidian: failed — delivery failed\nTelegram: success\n" +
				"See log for details: " + logPath + "\n",
		},
		{
			name: "both failed",
			results: []post.SinkResult{
				{Name: "obsidian", Reason: "delivery failed"},
				{Name: "telegram", Reason: "request timed out"},
			},
			logPath: logPath,
			want: "Obsidian: failed — delivery failed\nTelegram: failed — request timed out\n" +
				"See log for details: " + logPath + "\n",
		},
		{
			name:    "one enabled destination that succeeded",
			results: []post.SinkResult{{Name: "obsidian", Success: true}},
			logPath: logPath,
			want:    "Obsidian: success\n",
		},
		{
			name:    "no destination was enabled",
			results: nil,
			logPath: logPath,
			want:    "No destination is enabled, so nothing was posted.\nSee log for details: " + logPath + "\n",
		},
		{
			name:    "a failure with no resolvable log path omits the line",
			results: []post.SinkResult{{Name: "telegram", Reason: "delivery failed"}},
			logPath: "",
			want:    "Telegram: failed — delivery failed\n",
		},
		{
			name:    "a failure whose reason was lost does not trail a separator",
			results: []post.SinkResult{{Name: "telegram"}},
			logPath: logPath,
			want:    "Telegram: failed\nSee log for details: " + logPath + "\n",
		},
		{
			name:    "the orchestrator's unknown-name sentinel is recognisable",
			results: []post.SinkResult{{Name: "unknown", Reason: "delivery failed"}},
			logPath: logPath,
			want:    "Unknown: failed — delivery failed\nSee log for details: " + logPath + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer

			cli.Render(&out, &errOut, cli.Report{Results: tt.results, LogPath: tt.logPath})

			if got := out.String(); got != tt.want {
				t.Errorf("report is\n%q\nwant\n%q", got, tt.want)
			}

			if errOut.Len() != 0 {
				t.Errorf("a report with no degradation wrote to stderr: %q", errOut.String())
			}
		})
	}
}

// TestRenderNeverPrintsTheDiagnosticError is FR-017, FR-029 and FR-043 at the
// point where they are actually at risk.
//
// SinkResult.Err carries the bot token in a Telegram transport failure, and
// internal/post deliberately leaves it unredacted for the diagnostic logger. The
// display half is Reason. A renderer that reached for Err — or that printed the
// whole result with %v, which is guarded, or with a verb that is not — would put
// the credential on the user's terminal.
func TestRenderNeverPrintsTheDiagnosticError(t *testing.T) {
	t.Parallel()

	const sentinel = "7654321:AA-the-sentinel-token"

	var out, errOut bytes.Buffer

	cli.Render(&out, &errOut, cli.Report{
		Results: []post.SinkResult{{
			Name:   "telegram",
			Reason: "delivery failed",
			Err: fmt.Errorf("Post %q: connection refused",
				"https://api.telegram.org/bot"+sentinel+"/sendMessage"),
			Duration: 3 * time.Second,
		}},
		LogPath: "/state/app.jsonl",
	})

	combined := out.String() + errOut.String()

	if strings.Contains(combined, sentinel) {
		t.Fatalf("the report printed the credential:\n%s", combined)
	}

	if strings.Contains(combined, "connection refused") {
		t.Errorf("the report printed the diagnostic error text:\n%s", combined)
	}
}

// TestRenderEmitsExactlyOneDegradationWarning is FR-076's counting requirement.
//
// Occurrences, not presence. A Contains check passes when the warning is printed
// twice, and printing it twice is the specific mistake FR-076 was written to
// forbid — the logger's own comments record that an earlier design invited it by
// offering the degradation from two places.
func TestRenderEmitsExactlyOneDegradationWarning(t *testing.T) {
	t.Parallel()

	const warning = "warning: diagnostics could not be written to /state/app.jsonl: no space left"

	tests := []struct {
		name    string
		results []post.SinkResult
	}{
		{name: "with every destination succeeding", results: []post.SinkResult{{Name: "obsidian", Success: true}}},
		{name: "with a failure", results: []post.SinkResult{{Name: "obsidian", Reason: "delivery failed"}}},
		{name: "with no destinations"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer

			cli.Render(&out, &errOut, cli.Report{
				Results: tt.results,
				LogPath: "/state/app.jsonl",
				Warning: warning,
			})

			combined := out.String() + errOut.String()

			if got := strings.Count(combined, warning); got != 1 {
				t.Errorf("the warning appears %d times, want exactly 1:\n%s", got, combined)
			}

			if got := strings.Count(combined, "warning:"); got != 1 {
				t.Errorf("output carries %d warnings, want exactly 1:\n%s", got, combined)
			}
		})
	}
}

// TestDisplayNamePassesThroughWhatItCannotCapitalise covers the arms Render
// cannot reach, since the only names it ever sees come from the two sinks.
func TestDisplayNamePassesThroughWhatItCannotCapitalise(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{in: "obsidian", want: "Obsidian"},
		{in: "telegram", want: "Telegram"},
		{in: "unknown", want: "Unknown"},
		{in: "", want: ""},
		{in: "Already", want: "Already"},
		{in: "3rd-party", want: "3rd-party"},
		{in: "日本語", want: "日本語"},
		{in: "a", want: "A"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := cli.DisplayName(tt.in); got != tt.want {
				t.Errorf("DisplayName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCorrectionPromptHasAnAnswerForAnUnknownRejection drives the default arm,
// which is unreachable through Message.Validate and is the one that decides
// whether a rejection reason added later is explained or silent.
func TestCorrectionPromptHasAnAnswerForAnUnknownRejection(t *testing.T) {
	t.Parallel()

	unknown := errors.New("some future rejection rule")

	prompt := cli.CorrectionPrompt(unknown)

	if !strings.Contains(prompt, unknown.Error()) {
		t.Errorf("the prompt for an unknown rejection does not repeat it: %q", prompt)
	}

	// And neither written prompt may be reachable from an unrelated error, which
	// is what a selection made by anything other than errors.Is would do.
	if strings.Contains(prompt, blankPromptMarker) || strings.Contains(prompt, encodingPromptMarker) {
		t.Errorf("an unrelated error selected a written prompt: %q", prompt)
	}
}

// -----------------------------------------------------------------------------
// T029: the end-to-end CLI test
// -----------------------------------------------------------------------------

// The two written correction prompts, identified by a phrase that appears in the
// prompt and nowhere else.
//
// Matching on "whitespace" or on "UTF-8" was the obvious spelling and it is
// unfailable: correctionPrompt's default arm repeats the sentinel's own text,
// and both sentinels contain those words — so a switch that selected the wrong
// arm, or no arm at all, produced output that still matched. A mutant
// duplicating the ErrEmptyMessage case survived exactly that way. These phrases
// are written by the prompt and cannot come from the error.
const (
	blankPromptMarker    = "Type a message and try again."
	encodingPromptMarker = "switch it to UTF-8 and try again."
)

// vault is one test's throwaway configuration: a settings file with the
// obsidian destination enabled and pointed at an empty directory, and the chat
// destination disabled.
//
// The vault directory is the recorder. Every case below runs through this same
// fixture, so "no destination was contacted" is asserted against a directory
// that other rows in the same table demonstrably do write into — the point of
// trap the earlier batches fell into, where an emptiness assertion was satisfied
// by a fixture that could never have recorded anything.
type vault struct {
	configPath string
	noteDir    string
	logPath    string
}

func newVault(t *testing.T) vault {
	t.Helper()

	root := t.TempDir()

	v := vault{
		configPath: filepath.Join(root, "config.toml"),
		noteDir:    filepath.Join(root, "notes"),
		logPath:    filepath.Join(root, "state", "app.jsonl"),
	}

	if err := os.MkdirAll(v.noteDir, 0o700); err != nil {
		t.Fatalf("create the vault: %v", err)
	}

	document := fmt.Sprintf(`
[sink.obsidian]
enabled = true
daily_note_dir = %q

[sink.telegram]
enabled = false

[logging]
path = %q
`, v.noteDir, v.logPath)

	if err := os.WriteFile(v.configPath, []byte(document), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}

	return v
}

// notes returns every file the obsidian destination left behind.
func (v vault) notes(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(v.noteDir)
	if err != nil {
		t.Fatalf("read the vault: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// content returns the single note in the vault.
func (v vault) content(t *testing.T) string {
	t.Helper()

	names := v.notes(t)
	if len(names) != 1 {
		t.Fatalf("the vault holds %v, want exactly one note", names)
	}

	raw, err := os.ReadFile(filepath.Join(v.noteDir, names[0]))
	if err != nil {
		t.Fatalf("read the note: %v", err)
	}

	return string(raw)
}

// TestTheCommandLinePostsWhatWasTypedAndRejectsWhatItMustNotSend is T029.
//
// It runs the real parser, the real settings loader, the real sink construction
// and the real orchestrator; the only thing it does not do is exit the process,
// which is cmd/mp's test.
//
// The four rows are FR-003, FR-004 and FR-010's two rejection reasons. The two
// posting rows come first for a reason that is the whole design of this table:
// they prove the vault can record a post, so the emptiness assertion in the two
// rejection rows is a claim about behaviour rather than about a fixture that was
// never capable of anything.
func TestTheCommandLinePostsWhatWasTypedAndRejectsWhatItMustNotSend(t *testing.T) {
	tests := []struct {
		name string
		// message is appended to the argv after -c, exactly as a shell would
		// hand it over.
		message []string
		want    int
		// posted, when non-empty, is the text the note must contain.
		posted string
		// rejection is the written prompt that must appear on stderr; the
		// other prompt must not.
		rejection string
	}{
		{
			name:    "a single quoted message",
			message: []string{"hello world"},
			want:    cli.ExitSuccess,
			posted:  "hello world",
		},
		{
			name:    "bare words joined with one ASCII space",
			message: []string{"hello", "world", "again"},
			want:    cli.ExitSuccess,
			posted:  "hello world again",
		},
		{
			name:      "a whitespace-only message is rejected",
			message:   []string{"   \t　"},
			want:      cli.ExitFailure,
			rejection: blankPromptMarker,
		},
		{
			name:      "bare words that are all whitespace are rejected after joining",
			message:   []string{" ", "\t"},
			want:      cli.ExitFailure,
			rejection: blankPromptMarker,
		},
		{
			name:      "an empty argument is rejected",
			message:   []string{""},
			want:      cli.ExitFailure,
			rejection: blankPromptMarker,
		},
		{
			name:      "a message containing invalid UTF-8 is rejected",
			message:   []string{"a\xffb"},
			want:      cli.ExitFailure,
			rejection: encodingPromptMarker,
		},
		{
			name:      "invalid UTF-8 arriving as one of several words",
			message:   []string{"note:", "\x93\xfa\x96\x7b\x8c\xea"},
			want:      cli.ExitFailure,
			rejection: encodingPromptMarker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVault(t)

			argv := append([]string{"-c", v.configPath}, tt.message...)

			invocation, err := cli.Parse(argv)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", argv, err)
			}

			var out, errOut bytes.Buffer

			status := cli.Run(invocation, &out, &errOut)

			if status != tt.want {
				t.Fatalf("status = %d, want %d\nstdout: %s\nstderr: %s",
					status, tt.want, out.String(), errOut.String())
			}

			if tt.posted != "" {
				if got := v.content(t); !strings.HasSuffix(got, tt.posted+"\n") {
					t.Errorf("the note holds %q, want it to end with %q", got, tt.posted+"\n")
				}

				if !strings.Contains(out.String(), "Obsidian: success") {
					t.Errorf("the report does not name the successful destination: %q", out.String())
				}

				return
			}

			// FR-010: no destination contacted. The same vault records a post
			// in the rows above, so this is a real negative.
			if names := v.notes(t); len(names) != 0 {
				t.Errorf("a rejected message reached the vault: %v", names)
			}

			// And nothing was reported as a destination outcome either, which
			// is the other half of "no destination was contacted": a rejection
			// is not a post whose sinks all failed.
			if strings.Contains(out.String(), "Obsidian") || strings.Contains(out.String(), "Telegram") {
				t.Errorf("a rejected message produced a destination report: %q", out.String())
			}

			if !strings.Contains(errOut.String(), tt.rejection) {
				t.Errorf("stderr = %q, want it to explain %q", errOut.String(), tt.rejection)
			}

			// The other reason must not be reported. Without this, a prompt
			// selection that fell through to the default arm — which repeats the
			// sentinel's own words — would satisfy the check above.
			other := blankPromptMarker
			if tt.rejection == blankPromptMarker {
				other = encodingPromptMarker
			}

			if strings.Contains(errOut.String(), other) {
				t.Errorf("stderr = %q, which also reports the other rejection reason",
					errOut.String())
			}

			// A rejection is not a diagnostics problem, and FR-076 allows one
			// warning for a real one; a correction prompt must not be counted
			// as it.
			if strings.Contains(errOut.String(), "warning:") {
				t.Errorf("a rejection produced a diagnostics warning: %q", errOut.String())
			}
		})
	}
}

// TestARejectedMessageOpensNoLog is the other observable half of FR-010.
//
// The vault proves no sink ran. This proves nothing else did either: validation
// happens before the settings are read, so a rejected message must not create
// the log directory, must not read the settings file, and therefore cannot fail
// on a settings problem instead of on the message.
func TestARejectedMessageOpensNoLog(t *testing.T) {
	v := newVault(t)

	// A settings file that would fail to load. If validation did not come
	// first, the user would be told about their configuration rather than about
	// the message they can still fix.
	if err := os.WriteFile(v.configPath, []byte("this is not toml"), 0o600); err != nil {
		t.Fatalf("rewrite the settings: %v", err)
	}

	invocation, err := cli.Parse([]string{"-c", v.configPath, "  "})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var out, errOut bytes.Buffer

	if status := cli.Run(invocation, &out, &errOut); status != cli.ExitFailure {
		t.Fatalf("status = %d, want %d", status, cli.ExitFailure)
	}

	if !strings.Contains(errOut.String(), blankPromptMarker) {
		t.Errorf("the user was told about something other than their message: %q", errOut.String())
	}

	if _, err := os.Stat(filepath.Dir(v.logPath)); err == nil {
		t.Error("a rejected message created the log directory")
	}
}

// TestASettingsFailureIsReportedAndPostsNothing is FR-058 and FR-060: a settings
// error exits 1 through the front door that was used, and no destination runs.
func TestASettingsFailureIsReportedAndPostsNothing(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{name: "not TOML at all", document: "this is not toml", want: "config.toml"},
		{
			name:     "a value out of range",
			document: "[posting]\nsink_timeout_seconds = 18446744074\n",
			want:     "sink_timeout_seconds",
		},
		{
			name:     "a key that does not exist",
			document: "[sink.obsidian]\nenabledd = true\n",
			want:     "enabledd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVault(t)

			if err := os.WriteFile(v.configPath, []byte(tt.document), 0o600); err != nil {
				t.Fatalf("rewrite the settings: %v", err)
			}

			invocation, err := cli.Parse([]string{"-c", v.configPath, "hello"})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			var out, errOut bytes.Buffer

			if status := cli.Run(invocation, &out, &errOut); status != cli.ExitFailure {
				t.Fatalf("status = %d, want %d", status, cli.ExitFailure)
			}

			if !strings.Contains(errOut.String(), tt.want) {
				t.Errorf("stderr = %q, want it to mention %q", errOut.String(), tt.want)
			}

			if names := v.notes(t); len(names) != 0 {
				t.Errorf("a settings failure still posted: %v", names)
			}

			if out.Len() != 0 {
				t.Errorf("a settings failure printed a report: %q", out.String())
			}
		})
	}
}

// TestASinkFailureExitsOneAndNamesTheLog is FR-060, FR-062 and FR-063 through
// the whole front door.
//
// The failure is arranged by removing the vault directory, which the obsidian
// sink reports as a missing vault rather than as a create refusal.
func TestASinkFailureExitsOneAndNamesTheLog(t *testing.T) {
	v := newVault(t)

	if err := os.Remove(v.noteDir); err != nil {
		t.Fatalf("remove the vault: %v", err)
	}

	invocation, err := cli.Parse([]string{"-c", v.configPath, "into the void"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var out, errOut bytes.Buffer

	if status := cli.Run(invocation, &out, &errOut); status != cli.ExitFailure {
		t.Fatalf("status = %d, want %d\nstdout: %s", status, cli.ExitFailure, out.String())
	}

	report := out.String()

	if !strings.Contains(report, "Obsidian: failed") {
		t.Errorf("the report does not name the failed destination: %q", report)
	}

	if !strings.Contains(report, "See log for details: "+v.logPath) {
		t.Errorf("the report does not name the log (FR-063): %q", report)
	}

	if errOut.Len() != 0 {
		t.Errorf("a sink failure with working diagnostics wrote to stderr: %q", errOut.String())
	}
}

// TestDegradedDiagnosticsChangeNeitherTheOutcomeNorTheStatus is FR-076.
//
// The log path is made unopenable while the post itself is perfectly fine, so
// the run must still exit 0, still report the destination as succeeded, still
// have written the note, and add exactly one warning. Every one of those four is
// a separate way to get FR-076 wrong, and the exit status is the one that would
// otherwise be invisible: a degradation that returned 1 would look like an
// ordinary failure to anyone reading the report.
func TestDegradedDiagnosticsChangeNeitherTheOutcomeNorTheStatus(t *testing.T) {
	v := newVault(t)

	// A directory where the log file should be. logging.Open refuses it by name
	// rather than blocking, which is the case its own tests cover; here it is
	// only a reliable way to degrade.
	if err := os.MkdirAll(v.logPath, 0o700); err != nil {
		t.Fatalf("put a directory at the log path: %v", err)
	}

	invocation, err := cli.Parse([]string{"-c", v.configPath, "diagnostics are broken"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var out, errOut bytes.Buffer

	status := cli.Run(invocation, &out, &errOut)

	if status != cli.ExitSuccess {
		t.Fatalf("status = %d, want %d — FR-076 says a diagnostics failure does not change "+
			"the exit status\nstdout: %s\nstderr: %s",
			status, cli.ExitSuccess, out.String(), errOut.String())
	}

	if got := v.content(t); !strings.Contains(got, "diagnostics are broken") {
		t.Errorf("the post did not reach the vault: %q", got)
	}

	if !strings.Contains(out.String(), "Obsidian: success") {
		t.Errorf("the reported outcome changed: %q", out.String())
	}

	if got := strings.Count(errOut.String(), "warning:"); got != 1 {
		t.Errorf("got %d warnings, want exactly 1:\n%s", got, errOut.String())
	}

	if !strings.Contains(errOut.String(), v.logPath) {
		t.Errorf("the warning does not name the log path: %q", errOut.String())
	}
}

// TestWarningForPicksExactlyOneReason covers FR-076's selection, including the
// close-failure arm that has no portable trigger through a real logger.
//
// The precedence row is the one that matters: both conditions can hold at once —
// a log that could not be opened is also a log whose close does nothing useful —
// and a caller checking each in turn would print two warnings, which is the
// specific thing FR-076 forbids.
func TestWarningForPicksExactlyOneReason(t *testing.T) {
	t.Parallel()

	var (
		openFailure  = errors.New("the log path is a directory, which cannot be appended to")
		closeFailure = errors.New("no space left on device")
	)

	tests := []struct {
		name      string
		degraded  *logging.Degradation
		closeErr  error
		path      string
		wantEmpty bool
		wantHas   []string
		wantNot   []string
	}{
		{
			name:      "nothing went wrong",
			path:      "/state/app.jsonl",
			wantEmpty: true,
		},
		{
			name:     "the log could not be opened",
			degraded: &logging.Degradation{Path: "/state/app.jsonl", Err: openFailure},
			path:     "/state/app.jsonl",
			wantHas:  []string{"warning:", "/state/app.jsonl", openFailure.Error()},
		},
		{
			name:     "only the close failed",
			closeErr: closeFailure,
			path:     "/state/app.jsonl",
			wantHas:  []string{"warning:", "/state/app.jsonl", closeFailure.Error()},
		},
		{
			name:     "both failed, and only the open is reported",
			degraded: &logging.Degradation{Path: "/state/app.jsonl", Err: openFailure},
			closeErr: closeFailure,
			path:     "/state/app.jsonl",
			wantHas:  []string{openFailure.Error()},
			wantNot:  []string{closeFailure.Error()},
		},
		{
			name:     "a close failure with no path names no path",
			closeErr: closeFailure,
			wantHas:  []string{"warning: diagnostics could not be written: "},
			wantNot:  []string{"written to"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cli.WarningFor(tt.degraded, tt.closeErr, tt.path)

			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("WarningFor = %q, want no warning", got)
				}

				return
			}

			if got == "" {
				t.Fatal("WarningFor returned no warning for a real failure")
			}

			// One warning, whatever combination produced it.
			if count := strings.Count(got, "warning:"); count != 1 {
				t.Errorf("the result carries %d warnings, want 1: %q", count, got)
			}

			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("WarningFor = %q, want it to contain %q", got, want)
				}
			}

			for _, unwanted := range tt.wantNot {
				if strings.Contains(got, unwanted) {
					t.Errorf("WarningFor = %q, want it not to contain %q", got, unwanted)
				}
			}
		})
	}
}

// TestModeStringNamesEveryValue keeps the dispatch decision legible in a failure
// message.
//
// Reached only from a failing assertion elsewhere, so without this it is a
// formatter nobody has ever run — and its default arm is the one that fires
// precisely when a third Mode has been added and a test is trying to say which
// one it got.
func TestModeStringNamesEveryValue(t *testing.T) {
	t.Parallel()

	if got := cli.ModePost.String(); got != "post" {
		t.Errorf("ModePost = %q, want %q", got, "post")
	}

	if got := cli.ModeWindow.String(); got != "window" {
		t.Errorf("ModeWindow = %q, want %q", got, "window")
	}

	if got := cli.Mode(7).String(); got != "Mode(7)" {
		t.Errorf("an unnamed Mode rendered %q, want it to name its value", got)
	}
}

// TestRunResolvesTheDefaultSettingsPathWhenNoneWasGiven is FR-053 through the
// front door: no -c means the XDG-resolved default.
//
// The process-level test covers the same path, but only for the case where the
// file is there. This is where the failure is legible: a run whose default
// settings file does not exist must say which path it looked at, since "no such
// file" without a path is the least actionable message this program can produce.
func TestRunResolvesTheDefaultSettingsPathWhenNoneWasGiven(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	v := newVault(t)

	resolved := filepath.Join(configHome, "miko-post", "config.toml")

	// First, with nothing there: the error must name the resolved path.
	invocation, err := cli.Parse([]string{"hello"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var out, errOut bytes.Buffer

	if status := cli.Run(invocation, &out, &errOut); status != cli.ExitFailure {
		t.Fatalf("status = %d, want %d", status, cli.ExitFailure)
	}

	if !strings.Contains(errOut.String(), resolved) {
		t.Errorf("stderr = %q, want it to name the resolved default %q", errOut.String(), resolved)
	}

	// Then with a real document there, so the negative above is not satisfied
	// by a resolution that never happens.
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		t.Fatalf("create the config directory: %v", err)
	}

	document, err := os.ReadFile(v.configPath)
	if err != nil {
		t.Fatalf("read the fixture settings: %v", err)
	}

	if err := os.WriteFile(resolved, document, 0o600); err != nil {
		t.Fatalf("write the default settings: %v", err)
	}

	out.Reset()
	errOut.Reset()

	if status := cli.Run(invocation, &out, &errOut); status != cli.ExitSuccess {
		t.Fatalf("status = %d, want %d\nstdout: %s\nstderr: %s",
			status, cli.ExitSuccess, out.String(), errOut.String())
	}

	if got := v.content(t); !strings.Contains(got, "hello") {
		t.Errorf("the post did not reach the vault: %q", got)
	}
}

// TestRunReportsASettingsPathItCannotResolve covers the arm a user with no home
// directory reaches.
//
// It is a real state — a daemon context, a container with no passwd entry, a
// cleared environment — and the only one in which this program has no settings
// path to name at all. The requirement is that it says so and exits 1, rather
// than reading a relative path out of the working directory.
func TestRunReportsASettingsPathItCannotResolve(t *testing.T) {
	// Both, because DefaultConfigPath falls back to $HOME/.config when
	// XDG_CONFIG_HOME is unset or empty.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := config.DefaultConfigPath(); err == nil {
		t.Skip("this platform resolves a config path with no HOME; the arm is unreachable here")
	}

	invocation, err := cli.Parse([]string{"hello"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var out, errOut bytes.Buffer

	if status := cli.Run(invocation, &out, &errOut); status != cli.ExitFailure {
		t.Fatalf("status = %d, want %d", status, cli.ExitFailure)
	}

	if out.Len() != 0 {
		t.Errorf("an unresolvable settings path printed a report: %q", out.String())
	}

	if !strings.Contains(errOut.String(), "home directory") {
		t.Errorf("stderr = %q, want it to name the reason", errOut.String())
	}
}
