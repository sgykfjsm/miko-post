package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/cli"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// -----------------------------------------------------------------------------
// T077, T081, T083: settings that must not be posted with
// -----------------------------------------------------------------------------

// writeSettings replaces the vault's settings document with document, which is
// formatted with the vault's note directory and log path in that order.
func (v vault) writeSettings(t *testing.T, document string) {
	t.Helper()

	rendered := fmt.Sprintf(document, v.noteDir, v.logPath)
	if err := os.WriteFile(v.configPath, []byte(rendered), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}
}

// runRefused parses `-c <vault settings> hello` and runs it through a service
// constructor that fails the test if it is ever called.
//
// Every caller expects a refusal before a service exists, and that is what the
// constructor asserts. It is also what keeps these tests off the network: some
// of their settings enable the chat destination with a well-formed token, and
// under the very regression each test exists to catch — the refusal not
// happening — the production constructor would build the real chat sink and
// send to the Bot API before any assertion ran.
func (v vault) runRefused(t *testing.T) (status int, out, errOut string) {
	t.Helper()

	invocation, err := cli.Parse([]string{"-c", v.configPath, "hello"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var stdout, stderr bytes.Buffer

	status = cli.RunWith(invocation, &stdout, &stderr, func(config.Settings, post.Recording) *post.Service {
		t.Fatal("a posting service was constructed, so the settings were not refused")

		return nil
	})

	return status, stdout.String(), stderr.String()
}

// TestNoDestinationEnabledIsAStartupError is FR-018 through the CLI front door
// (T081, T083).
//
// The refusal must come before a post is attempted, and "before" is asserted
// three ways rather than inferred from the status: the report is empty, so no
// result was rendered; the log file does not exist, so the logger — which is
// opened only once a post is going ahead — never was; and the stderr text is
// the actionable one, naming the file and the keys, not the after-the-fact
// "nothing was posted" line Render prints for an empty report.
func TestNoDestinationEnabledIsAStartupError(t *testing.T) {
	t.Setenv("MIKO_POST_TELEGRAM_BOT_TOKEN", "")

	v := newVault(t)
	v.writeSettings(t, `
[sink.obsidian]
enabled = false
daily_note_dir = %q

[sink.telegram]
enabled = false

[logging]
path = %q
`)

	status, out, errOut := v.runRefused(t)

	if status != cli.ExitFailure {
		t.Errorf("status = %d, want %d", status, cli.ExitFailure)
	}

	if out != "" {
		t.Errorf("a startup error printed a report: %q", out)
	}

	for _, want := range []string{config.ErrNoDestinationEnabled.Error(), v.configPath} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want it to contain %q", errOut, want)
		}
	}

	if strings.Contains(errOut, "nothing was posted") {
		t.Errorf("stderr carries the after-the-fact report line, not the startup error: %q", errOut)
	}

	if _, err := os.Stat(v.logPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the log exists (stat: %v), so a post was set up before the refusal", err)
	}

	if names := v.notes(t); len(names) != 0 {
		t.Errorf("a startup error still wrote %v", names)
	}
}

// TestPartiallyValidSettingsStartNoDestination is FR-058 and T077: one valid,
// enabled destination beside one that is enabled but misconfigured must not
// post to the valid one. Posting to half is the outcome the requirement names.
//
// The obsidian section here is the fixture's own, which the posting tests prove
// can write a note, so the empty vault is a statement about the refusal.
func TestPartiallyValidSettingsStartNoDestination(t *testing.T) {
	t.Setenv("MIKO_POST_TELEGRAM_BOT_TOKEN", "")

	v := newVault(t)
	v.writeSettings(t, `
[sink.obsidian]
enabled = true
daily_note_dir = %q

[sink.telegram]
enabled = true
bot_token = "0123456789:abcdefghijklmnop"

[logging]
path = %q
`)

	status, out, errOut := v.runRefused(t)

	if status != cli.ExitFailure {
		t.Errorf("status = %d, want %d", status, cli.ExitFailure)
	}

	if !strings.Contains(errOut, "chat_id") {
		t.Errorf("stderr = %q, want it to name the missing chat_id", errOut)
	}

	if out != "" {
		t.Errorf("a settings failure printed a report: %q", out)
	}

	if names := v.notes(t); len(names) != 0 {
		t.Errorf("the valid destination was posted to: %v", names)
	}
}

// -----------------------------------------------------------------------------
// Issue #119: two destinations both succeeding, through this front door
// -----------------------------------------------------------------------------

// succeedingChat stands in for the chat destination and succeeds.
//
// It takes the real chat sink's name, so it occupies the real sink's slot in
// the report. Everything else in the run is real: the settings load, the sink
// construction in app.Sinks, the orchestrator, the note written to disk, the
// report and the status.
type succeedingChat struct {
	name  string
	calls *atomic.Int32
}

func (s succeedingChat) Name() string { return s.name }

func (s succeedingChat) Send(context.Context, post.Message) error {
	s.calls.Add(1)

	return nil
}

// TestBothDestinationsSucceedingExitsZero is issue #119's missing case: argv
// driving two destinations that both succeed, reported as two successes, exit 0.
//
// The seam replaces the service constructor and nothing upstream of it. The
// sinks come from app.Sinks and the service from post.New, so a truncation of
// the sink list in either — the `sinks[:1]` mutant the issue names — drops a
// result this test counts. The substitute replaces the whole chat sink with a
// succeeding stand-in carrying its name — found by name in the list app.Sinks
// built, so none of the real chat sink's own code runs here — and the test
// fails, before anything is sent, if it was not there to replace.
//
// Process-level, through a built binary, this remains unreachable: the chat
// sink's origin is unexported and no settings key may redirect it (FR-057).
// contracts/cli-interface.md records that.
func TestBothDestinationsSucceedingExitsZero(t *testing.T) {
	t.Setenv("MIKO_POST_TELEGRAM_BOT_TOKEN", "")

	v := newVault(t)
	v.writeSettings(t, `
[sink.obsidian]
enabled = true
daily_note_dir = %q

[sink.telegram]
enabled = true
bot_token = "0123456789:abcdefghijklmnop"
chat_id = "42"

[logging]
path = %q
`)

	invocation, err := cli.Parse([]string{"-c", v.configPath, "hello", "both"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var chatCalls atomic.Int32

	substituted := false

	newService := func(settings config.Settings, recording post.Recording) *post.Service {
		sinks := app.Sinks(settings)

		for i, sink := range sinks {
			if sink.Name() == telegram.SinkName {
				sinks[i] = succeedingChat{name: sink.Name(), calls: &chatCalls}
				substituted = true
			}
		}

		// Checked here, before post.New, and not after the run: an
		// unsubstituted chat sink is a real one, and letting the post go ahead
		// to find that out afterwards would send to the Bot API first.
		if !substituted {
			t.Fatal("app.Sinks built no chat sink to substitute, so this is not a two-destination run")
		}

		return post.New(sinks, app.SinkTimeout(settings), recording)
	}

	var out, errOut bytes.Buffer

	status := cli.RunWith(invocation, &out, &errOut, newService)

	if !substituted {
		t.Fatal("the service constructor was never called")
	}

	if status != cli.ExitSuccess {
		t.Errorf("status = %d, want %d\nstdout: %s\nstderr: %s",
			status, cli.ExitSuccess, out.String(), errOut.String())
	}

	if want := "Obsidian: success\nTelegram: success\n"; out.String() != want {
		t.Errorf("report = %q, want %q", out.String(), want)
	}

	if got := chatCalls.Load(); got != 1 {
		t.Errorf("the chat destination was sent %d times, want 1", got)
	}

	if got := v.content(t); !strings.Contains(got, "hello both") {
		t.Errorf("the note does not carry the post: %q", got)
	}

	if errOut.Len() != 0 {
		t.Errorf("a clean two-destination success wrote to stderr: %q", errOut.String())
	}

	if _, err := os.Stat(filepath.Clean(v.logPath)); err != nil {
		t.Errorf("the post left no log: %v", err)
	}
}
