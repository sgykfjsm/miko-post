package post_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/cli"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// leakSentinel is the credential the gate hunts for. It is long enough to pass
// config.MinBotTokenLength, so the logger arms it as a redaction pattern
// rather than skipping it, and distinctive enough that no field name, path or
// status line can contain it by accident.
const leakSentinel = "7654321:AA-secret-leak-gate-sentinel"

// telegramReply is what the fake transport answers for one request.
type telegramReply struct {
	status int
	body   string

	// err, when non-nil, is returned instead of a response. http.Client wraps
	// it in a *url.Error whose URL is the request URL — the one that carries
	// the token in its path. That wrapping is the realistic leak, and it is
	// produced by net/http itself, not constructed by the test.
	err error

	// hang holds the request until its context ends, so the client's own
	// timeout produces the error.
	hang bool
}

// fakeTelegram is an http.RoundTripper that answers every request in-process.
//
// It replaces http.DefaultTransport, which the Telegram sink's client uses
// because internal/sink/telegram sets no Transport of its own. Nothing is
// dialled: there is no fallthrough to a real transport, so this gate cannot
// reach api.telegram.org as long as the sink uses the default transport —
// pinned by internal/sink/telegram's
// TestNewLeavesTheClientOnTheDefaultTransport.
type fakeTelegram struct {
	mu       sync.Mutex
	replies  []telegramReply
	requests []string
}

func (f *fakeTelegram) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
	}

	f.mu.Lock()
	f.requests = append(f.requests, request.URL.String())
	reply := telegramReply{status: http.StatusInternalServerError, body: `{"ok":false}`}
	if len(f.replies) > 0 {
		reply, f.replies = f.replies[0], f.replies[1:]
	}
	f.mu.Unlock()

	if reply.hang {
		<-request.Context().Done()

		return nil, request.Context().Err()
	}

	if reply.err != nil {
		return nil, reply.err
	}

	return &http.Response{
		StatusCode: reply.status,
		Status:     http.StatusText(reply.status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(reply.body)),
		Request:    request,
	}, nil
}

func (f *fakeTelegram) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.requests...)
}

// installFakeTelegram swaps http.DefaultTransport for the duration of a test.
// Tests using it must not run in parallel with each other, because the
// transport is process-global.
func installFakeTelegram(t *testing.T, replies ...telegramReply) *fakeTelegram {
	t.Helper()

	fake := &fakeTelegram{replies: replies}
	previous := http.DefaultTransport
	http.DefaultTransport = fake
	t.Cleanup(func() { http.DefaultTransport = previous })

	return fake
}

// withoutTokenEnvironment removes MIKO_POST_TELEGRAM_BOT_TOKEN for the test.
//
// It overrides the file token (FR-042), so a developer who has it exported
// would otherwise run this gate against their real credential instead of the
// sentinel — and the vacuity witness below would be the only thing to notice.
func withoutTokenEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(config.TelegramBotTokenEnv, "")

	if err := os.Unsetenv(config.TelegramBotTokenEnv); err != nil {
		t.Fatalf("os.Unsetenv: %v", err)
	}
}

// leakFixture is one fully wired run's settings file and the directories it
// points at.
type leakFixture struct {
	root       string
	configPath string
	logPath    string
	noteDir    string
}

// newLeakFixture writes settings that turn every diagnostic on: bodies on
// every record, stack traces, version and commit. The gate should look at the
// most that can be written, not the least. token is the bot_token the settings
// file carries.
func newLeakFixture(t *testing.T, token string, createNote bool) leakFixture {
	t.Helper()

	root := t.TempDir()
	fixture := leakFixture{
		root:       root,
		configPath: filepath.Join(root, "config.toml"),
		logPath:    filepath.Join(root, "state", "app.jsonl"),
		noteDir:    filepath.Join(root, "vault", "daily"),
	}

	if err := os.MkdirAll(fixture.noteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	document := fmt.Sprintf(`[sink.telegram]
enabled = true
bot_token = %q
chat_id = "-1001234567890"
http_timeout_seconds = 1

[sink.obsidian]
enabled = true
daily_note_dir = %q
create_if_missing = %t

[posting]
sink_timeout_seconds = 5

[logging]
path = %q
message_on_error_only = false
stack_trace = true
include_version = true
include_git_commit = true
`, token, fixture.noteDir, createNote, fixture.logPath)

	if err := os.WriteFile(fixture.configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	return fixture
}

// writtenFiles returns every regular file under the fixture root except the
// settings file, which is the one place the sentinel is supposed to be. That
// covers the log, any rotated archive, the daily note, and anything a future
// change writes that nobody thought to list.
func (f leakFixture) writtenFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.Type().IsRegular() || path == f.configPath {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files[path] = string(content)

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", f.root, err)
	}

	return files
}

// assertWrittenFilesClean scans everything the run wrote: the log must exist,
// hold only JSON records, carry every witness, and neither it nor any other
// file may contain the sentinel. skipNote exempts the daily note.
func (f leakFixture) assertWrittenFilesClean(t *testing.T, skipNote bool, wantInLog []string) {
	t.Helper()

	files := f.writtenFiles(t)

	log, ok := files[f.logPath]
	if !ok || log == "" {
		t.Fatalf("the run wrote no log at %s; nothing was checked", f.logPath)
	}

	for path, content := range files {
		if skipNote && strings.HasPrefix(path, f.noteDir) {
			continue
		}

		assertNoSentinel(t, path, content)
	}

	for i, line := range strings.Split(strings.TrimRight(log, "\n"), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("log line %d is not JSON, so the scan above read it as something other than a record:\n%s", i+1, line)
		}
	}

	for _, want := range wantInLog {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not contain %q, so this case is not exercising the path it names:\n%s", want, log)
		}
	}
}

// TestSecretLeakGate is T084: the constitution's secret-leak quality gate
// (FR-043, FR-069, SC-006).
//
// It posts through the whole of what `mp` runs — cli.Run, app.LoadSettings, the
// real logger, the real Obsidian and Telegram sinks, and the orchestrator —
// with a sentinel token. It then asserts the sentinel appears on neither of the
// user's streams, in no file the run wrote, and in none of the strings the
// window builds its result text from.
//
// Several layers keep the credential out, and they overlap on purpose:
//
//   - withoutRequestURL (internal/sink/telegram) drops the *url.Error that
//     net/http wraps a transport failure in, taking the token-bearing URL
//     with it — "the transport fails", "the request times out".
//   - decodeResponse scrubs a description the Bot API echoes back — "the Bot
//     API refuses and echoes the credential".
//   - (*telegram.Sink).safe is the net behind both: any error still carrying
//     the token is rewritten before it leaves the sink.
//   - the logger's Redact patterns (internal/logging) scrub every record, and
//     are the only guard for a credential that arrives as message text, since
//     that is posted rather than errored — "the message itself contains the
//     credential".
//
// For a token that is exactly what the credential is, the first three overlap,
// so removing any one of them alone is masked by the others, and this gate —
// which asserts the outcome, not the mechanism — does not fail. Each is pinned
// individually by internal/sink/telegram's own tests. What this gate adds is
// the property those unit tests cannot see: that the wired stack, end to end,
// still holds it.
//
// The overlap does not hold for a token with trailing whitespace, which
// validation accepts today (#137). url.JoinPath escapes the space to %20, so
// the URL no longer contains the token as configured, and neither safe nor the
// logger's Redact — both searching for the exact token — recognise it. Only
// withoutRequestURL keeps the core credential out of the URL / error path, and
// the whitespace case below pins that single remaining layer end to end. The
// message-body path has no layer for such a token: a message containing the
// core credential reaches the log verbatim, because Redact searches for the
// padded token. That exposure is tracked on #137 and is not covered here.
func TestSecretLeakGate(t *testing.T) {
	cases := []struct {
		name    string
		message string
		replies []telegramReply

		// token is the settings file's bot_token. Empty means leakSentinel;
		// either way the core sentinel is what every absence check hunts for.
		token string

		// createNote false with no existing note makes the Obsidian sink fail
		// too, so a both-destinations-failing run is covered.
		createNote bool

		wantExit int

		// wantInLog is text the log must contain, proving the path this case
		// names was actually taken and recorded. Without it, a case whose
		// error was never logged would pass for the wrong reason: the sentinel
		// is absent because nothing was written. Each entry should be specific
		// to the case's path, not something a neighbouring path also writes.
		wantInLog []string

		// skipNote excludes the daily note from the file scan. Only for the
		// case whose message is the sentinel: the user typed it, and FR-011
		// requires the note to carry the original text.
		skipNote bool
	}{
		{
			name:       "both destinations succeed",
			message:    "hello from the leak gate",
			replies:    []telegramReply{{status: http.StatusOK, body: `{"ok":true,"result":{"message_id":8}}`}},
			createNote: true,
			wantExit:   cli.ExitSuccess,
			wantInLog:  []string{"telegram_send_succeeded"},
		},
		{
			name:       "the transport fails",
			message:    "hello from the leak gate",
			replies:    []telegramReply{{err: errors.New("connection refused")}},
			createNote: true,
			wantExit:   cli.ExitFailure,
			wantInLog:  []string{"connection refused"},
		},
		{
			name:       "the token has trailing whitespace and the transport fails",
			message:    "hello from the leak gate",
			token:      leakSentinel + " ",
			replies:    []telegramReply{{err: errors.New("connection refused")}},
			createNote: true,
			wantExit:   cli.ExitFailure,
			wantInLog:  []string{"connection refused"},
		},
		{
			name:       "the request times out",
			message:    "hello from the leak gate",
			replies:    []telegramReply{{hang: true}},
			createNote: true,
			wantExit:   cli.ExitFailure,
			// The classifier's verdict, not net/http's wording: whether the
			// error reads "Client.Timeout exceeded" or only "context deadline
			// exceeded" depends on which of the client's timer and the
			// transport's return wins a race, so that text is not a witness.
			wantInLog: []string{`"error_type":"timeout"`},
		},
		{
			name:    "the Bot API refuses and echoes the credential",
			message: "hello from the leak gate",
			replies: []telegramReply{{
				status: http.StatusUnauthorized,
				body: `{"ok":false,"error_code":401,"description":"Unauthorized: bot` +
					leakSentinel + ` is not valid"}`,
			}},
			createNote: true,
			wantExit:   cli.ExitFailure,
			wantInLog:  []string{"[redacted]"},
		},
		{
			name:    "the formatting rescue runs and its retry fails",
			message: "a message with *unbalanced markup",
			replies: []telegramReply{
				{
					status: http.StatusBadRequest,
					body:   `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: bot` + leakSentinel + `"}`,
				},
				{err: errors.New("connection reset by peer")},
			},
			createNote: true,
			wantExit:   cli.ExitFailure,
			// "[redacted]" is the scrubbed echo in the first reply, which is
			// written whether or not the retry runs. The other two exist only
			// once the plain-text retry has been sent and has failed.
			wantInLog: []string{"[redacted]", "telegram_plaintext_failed", "connection reset by peer"},
		},
		{
			name:       "both destinations fail",
			message:    "hello from the leak gate",
			replies:    []telegramReply{{err: errors.New("no route to host")}},
			createNote: false,
			wantExit:   cli.ExitFailure,
			wantInLog:  []string{"no route to host", "obsidian_append_failed"},
		},
		{
			name:       "the message itself contains the credential",
			message:    "pasted by mistake: " + leakSentinel,
			replies:    []telegramReply{{status: http.StatusOK, body: `{"ok":true,"result":{"message_id":9}}`}},
			createNote: true,
			wantExit:   cli.ExitSuccess,
			wantInLog:  []string{"[redacted]"},
			skipNote:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.wantInLog) == 0 {
				t.Fatal("every case needs a log witness; an empty wantInLog matches any log")
			}

			for _, want := range tc.wantInLog {
				if want == "" {
					t.Fatal("an empty wantInLog entry matches any log")
				}
			}

			token := tc.token
			if token == "" {
				token = leakSentinel
			}

			withoutTokenEnvironment(t)

			t.Run("front door", func(t *testing.T) {
				fixture := newLeakFixture(t, token, tc.createNote)
				fake := installFakeTelegram(t, tc.replies...)

				var out, errOut bytes.Buffer
				exit := cli.Run(cli.Invocation{
					Mode:       cli.ModePost,
					Message:    tc.message,
					ConfigPath: fixture.configPath,
				}, &out, &errOut)

				requireSentinelOnTheWire(t, fake, token)

				if exit != tc.wantExit {
					t.Errorf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", exit, tc.wantExit, out.String(), errOut.String())
				}

				assertNoSentinel(t, "stdout", out.String())
				assertNoSentinel(t, "stderr", errOut.String())

				fixture.assertWrittenFilesClean(t, tc.skipNote, tc.wantInLog)
			})

			t.Run("window strings", func(t *testing.T) {
				fixture := newLeakFixture(t, token, tc.createNote)
				fake := installFakeTelegram(t, tc.replies...)

				_, settings, err := app.LoadSettings(fixture.configPath)
				if err != nil {
					t.Fatalf("LoadSettings: %v", err)
				}

				logger := app.OpenLogger(settings, logging.SourceGUI)
				outcome := app.NewService(settings, app.NewRecording(logger, settings.Logging)).
					Post(post.Message{Original: tc.message})

				if err := logger.Close(); err != nil {
					t.Fatalf("close the log: %v", err)
				}

				requireSentinelOnTheWire(t, fake, token)

				// The window's run writes the same log through the GUI-source
				// logger, so it gets the same scan as the front door's.
				fixture.assertWrittenFilesClean(t, tc.skipNote, tc.wantInLog)

				// internal/gui's resultText reads Name and Reason from each
				// result, plus the log path and a warning neither of which is
				// derived from sink output. Every guarded rendering of the
				// outcome is checked as well, since those are one careless
				// Sprintf away from a label.
				for _, result := range outcome.Results {
					assertNoSentinel(t, result.Name+" Reason", result.Reason)

					if result.Err != nil {
						assertNoSentinel(t, result.Name+" Err.Error()", result.Err.Error())
					}

					for _, verb := range []string{"%v", "%+v", "%s"} {
						assertNoSentinel(t, result.Name+" "+verb, fmt.Sprintf(verb, result))
					}

					encoded, err := json.Marshal(result)
					if err != nil {
						t.Fatalf("marshal %s: %v", result.Name, err)
					}

					assertNoSentinel(t, result.Name+" JSON", string(encoded))
				}
			})
		})
	}
}

// requireSentinelOnTheWire is the vacuity witness. If no request carried the
// configured token, the run never used it — a token override from the
// environment, a disabled sink, a changed fixture — and every absence
// assertion after this one would hold for the wrong reason.
//
// The token is matched in its path-escaped form, which is how it travels: an
// exact sentinel is unchanged by escaping, and one with trailing whitespace
// arrives as "/bot<sentinel>%20/". Both forms contain the core sentinel.
func requireSentinelOnTheWire(t *testing.T, fake *fakeTelegram, token string) {
	t.Helper()

	onTheWire := "/bot" + url.PathEscape(token) + "/"
	if !strings.Contains(onTheWire, leakSentinel) {
		t.Fatalf("the wire form %q does not contain the core sentinel, so the witness proves nothing", onTheWire)
	}

	for _, request := range fake.seen() {
		if strings.Contains(request, onTheWire) {
			return
		}
	}

	t.Fatalf("no request to the Bot API carried the sentinel token, so the gate checked nothing; requests: %q", fake.seen())
}

func assertNoSentinel(t *testing.T, where, text string) {
	t.Helper()

	if strings.Contains(text, leakSentinel) {
		t.Errorf("the credential leaked into %s:\n%s", where, text)
	}
}
