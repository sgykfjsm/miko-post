package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
)

// alwaysPresent are the keys contracts/log-events.md marks present on every
// record (FR-066), which is exactly what T024 requires this file to assert.
var alwaysPresent = []string{"ts", "level", "event", "source", "message_id"}

// newBufferLogger returns a logger writing into a buffer through the
// Options.Writer seam, so no test touches the filesystem unless it is testing
// the filesystem.
func newBufferLogger(t *testing.T, opts logging.Options) (*logging.Logger, *bytes.Buffer) {
	t.Helper()

	buffer := &bytes.Buffer{}
	opts.Writer = buffer

	logger, degraded := logging.Open(opts)
	if degraded != nil {
		t.Fatalf("Open with a supplied writer reported degradation: %v", degraded.Err)
	}

	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return logger, buffer
}

// decodeRecords splits a log by newline and decodes each line on its own.
//
// Decoding line by line rather than with a streaming json.Decoder over the
// whole buffer is the point of the exercise: a decoder would happily consume a
// record split across two lines, or two records sharing one, and report
// success. FR-064 promises that each *line* is an independently valid,
// self-contained object, and only a per-line decode tests that promise.
func decodeRecords(t *testing.T, raw []byte) []map[string]any {
	t.Helper()

	if len(raw) == 0 {
		t.Fatal("no log output was produced")
	}

	if raw[len(raw)-1] != '\n' {
		t.Error("the log does not end in a newline; the final record is not a complete line")
	}

	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	records := make([]map[string]any, 0, len(lines))

	for i, line := range lines {
		if line == "" {
			t.Errorf("line %d is empty; every line must be one record", i+1)

			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d is not an independently valid JSON object: %v\nline: %s", i+1, err, line)

			continue
		}

		records = append(records, record)
	}

	return records
}

// requireString reads a key that must be present and hold a string.
func requireString(t *testing.T, record map[string]any, key string) string {
	t.Helper()

	value, ok := record[key]
	if !ok {
		t.Errorf("record is missing the %q key: %v", key, record)

		return ""
	}

	text, ok := value.(string)
	if !ok {
		t.Errorf("record key %q is %T, want string", key, value)

		return ""
	}

	return text
}

// TestEveryLineIsAnIndependentlyValidRecord is T024's central assertion
// (FR-064, FR-066, SC-007).
func TestEveryLineIsAnIndependentlyValidRecord(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})
	post := logger.Post("01K5ZXAMPLEULID0000000000")

	post.Info(logging.EventMessageReceived)
	post.Info(logging.EventObsidianAppendStarted, slog.String("sink", "obsidian"))
	post.Error(logging.EventTelegramSendFailed,
		slog.String("sink", "telegram"),
		slog.String("error_type", "timeout"),
		slog.Int64("duration_ms", 10012),
	)
	post.Error(logging.EventRequestCompletedWithError)

	records := decodeRecords(t, buffer.Bytes())

	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}

	wantEvents := []logging.Event{
		logging.EventMessageReceived,
		logging.EventObsidianAppendStarted,
		logging.EventTelegramSendFailed,
		logging.EventRequestCompletedWithError,
	}

	for i, record := range records {
		for _, key := range alwaysPresent {
			requireString(t, record, key)
		}

		if got := requireString(t, record, "event"); got != string(wantEvents[i]) {
			t.Errorf("record %d event = %q, want %q", i, got, wantEvents[i])
		}

		if got := requireString(t, record, "source"); got != string(logging.SourceCLI) {
			t.Errorf("record %d source = %q, want %q", i, got, logging.SourceCLI)
		}

		if got := requireString(t, record, "message_id"); got != "01K5ZXAMPLEULID0000000000" {
			t.Errorf("record %d message_id = %q, want the identifier passed to Post", i, got)
		}

		// slog's own message key must not survive alongside the renamed one.
		// If it did, every record would carry a redundant msg duplicating
		// event, and a rename that only half worked would look correct.
		if _, present := record["msg"]; present {
			t.Errorf("record %d still carries slog's msg key: %v", i, record)
		}

		if _, present := record["time"]; present {
			t.Errorf("record %d still carries slog's time key: %v", i, record)
		}
	}
}

// TestTimestampIsRFC3339WithAZone covers the ts field's stated type (FR-066).
func TestTimestampIsRFC3339WithAZone(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceGUI})
	logger.Post("id").Info(logging.EventMessageReceived)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	raw := requireString(t, records[0], "ts")

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("ts %q does not parse as RFC 3339: %v", raw, err)
	}

	// A zone is required, not merely a parseable instant. time.Parse accepts a
	// trailing Z or a numeric offset and rejects neither-nor, so the check that
	// matters is that the text carries one — a record without it cannot be
	// placed on a timeline by anyone reading the log in another zone.
	if !strings.HasSuffix(raw, "Z") && !strings.Contains(raw[len("2026-01-02T15:04:05"):], "+") &&
		!strings.Contains(raw[len("2026-01-02T15:04:05"):], "-") {
		t.Errorf("ts %q carries no time zone", raw)
	}

	if time.Since(parsed) > time.Minute || time.Since(parsed) < -time.Minute {
		t.Errorf("ts %q is not close to now; the record time is not the event time", raw)
	}
}

// TestLevelIsExactlyInfoOrError covers the level field's two admitted values
// (FR-066, contracts/log-events.md).
func TestLevelIsExactlyInfoOrError(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})
	post := logger.Post("id")

	post.Info(logging.EventRequestCompleted)
	post.Error(logging.EventRequestCompletedWithError)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	// Lowercase, because slog renders its levels as "INFO" and "ERROR" and the
	// contract says "info" and "error". A consumer filtering level == "error"
	// gets nothing from the uppercase form.
	for i, want := range []string{"info", "error"} {
		if got := requireString(t, records[i], "level"); got != want {
			t.Errorf("record %d level = %q, want %q", i, got, want)
		}
	}
}

// TestAMessageBodyCannotSplitARecord is the self-containment case that matters
// in practice (FR-064, SC-007).
//
// The message body is user input and reaches the log verbatim whenever a sink
// failed (FR-068). A multi-line note is completely ordinary, and a logger that
// wrote it unescaped would turn one record into several unparseable fragments —
// while still looking correct for every single-line test.
func TestAMessageBodyCannotSplitARecord(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})

	hostile := "first line\nsecond line\r\n{\"event\":\"forged\"}\n\ttab \"quoted\" \\ backslash"
	logger.Post("id").Error(logging.EventTelegramSendFailed, slog.String("message", hostile))

	records := decodeRecords(t, buffer.Bytes())

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1; the message body split the record", len(records))
	}

	if got := requireString(t, records[0], "message"); got != hostile {
		t.Errorf("message round-tripped as %q, want %q", got, hostile)
	}

	// The forged object embedded in the body must not have become a record.
	if got := requireString(t, records[0], "event"); got != string(logging.EventTelegramSendFailed) {
		t.Errorf("event = %q, want %q", got, logging.EventTelegramSendFailed)
	}
}

// TestBuildIdentityIsPresentOnlyWhenSupplied covers how include_version and
// include_git_commit are expressed (FR-066).
func TestBuildIdentityIsPresentOnlyWhenSupplied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		appVersion    string
		gitCommit     string
		wantPresent   map[string]string
		wantAbsentKey []string
	}{
		{
			name:        "both supplied",
			appVersion:  "0.1.0",
			gitCommit:   "abc1234",
			wantPresent: map[string]string{"app_version": "0.1.0", "git_commit": "abc1234"},
		},
		{
			name:          "neither supplied",
			wantAbsentKey: []string{"app_version", "git_commit"},
		},
		{
			name:          "version only",
			appVersion:    "0.1.0",
			wantPresent:   map[string]string{"app_version": "0.1.0"},
			wantAbsentKey: []string{"git_commit"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logger, buffer := newBufferLogger(t, logging.Options{
				Source:     logging.SourceCLI,
				AppVersion: test.appVersion,
				GitCommit:  test.gitCommit,
			})
			logger.Post("id").Info(logging.EventMessageReceived)

			records := decodeRecords(t, buffer.Bytes())
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			for key, want := range test.wantPresent {
				if got := requireString(t, records[0], key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}

			for _, key := range test.wantAbsentKey {
				if _, present := records[0][key]; present {
					t.Errorf("%s is present but was not supplied: %v", key, records[0])
				}
			}
		})
	}
}

// TestReservedKeysCannotBeShadowed covers the identity fields against a caller
// attribute of the same name.
//
// slog does not deduplicate keys, so without the guard this record would carry
// two event keys and every consumer would read whichever its decoder kept.
func TestReservedKeysCannotBeShadowed(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{
		Source:     logging.SourceCLI,
		AppVersion: "0.1.0",
	})

	logger.Post("real-id").Info(logging.EventMessageReceived,
		slog.String("event", "forged"),
		slog.String("level", "debug"),
		slog.String("ts", "not-a-time"),
		slog.String("source", "forged"),
		slog.String("message_id", "forged"),
		slog.String("app_version", "forged"),
		slog.String("sink", "obsidian"),
	)

	raw := buffer.Bytes()
	records := decodeRecords(t, raw)

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	want := map[string]string{
		"event":       string(logging.EventMessageReceived),
		"level":       "info",
		"source":      string(logging.SourceCLI),
		"message_id":  "real-id",
		"app_version": "0.1.0",
		"sink":        "obsidian",
	}

	for key, wantValue := range want {
		if got := requireString(t, records[0], key); got != wantValue {
			t.Errorf("%s = %q, want %q", key, got, wantValue)
		}
	}

	// A decoded map cannot show a duplicate key, so the raw line is checked
	// too: the map assertion above passes either way, and a duplicate is
	// exactly the defect being ruled out.
	for _, key := range []string{`"event":`, `"level":`, `"source":`, `"message_id":`, `"app_version":`} {
		if count := bytes.Count(raw, []byte(key)); count != 1 {
			t.Errorf("key %s appears %d times in the record, want 1\nline: %s", key, count, raw)
		}
	}
}

// TestGroupedAttributesKeepTheirOwnKeys covers replaceAttr's group guard.
//
// ReplaceAttr is called for attributes nested inside groups as well as for the
// record's own, and slog reuses the built-in key names: post.SinkResult.LogValue
// already emits a group, and a group member named "level" or "msg" is an
// ordinary field name that means something else entirely there. Without the
// guard, such a member is rewritten into the record's identity keys — inside the
// group, where it silently replaces the caller's field and lowercases its value.
func TestGroupedAttributesKeepTheirOwnKeys(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})

	logger.Post("id").Error(logging.EventTelegramSendFailed,
		slog.Group("upstream",
			slog.String("time", "SHOUTING-VALUE"),
			slog.String("level", "SHOUTING-VALUE"),
			slog.String("msg", "SHOUTING-VALUE"),
		),
	)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	// The record's own preamble is untouched.
	if got := requireString(t, records[0], "level"); got != "error" {
		t.Errorf("record level = %q, want %q", got, "error")
	}

	if got := requireString(t, records[0], "event"); got != string(logging.EventTelegramSendFailed) {
		t.Errorf("record event = %q, want %q", got, logging.EventTelegramSendFailed)
	}

	group, ok := records[0]["upstream"].(map[string]any)
	if !ok {
		t.Fatalf("upstream is %T, want a nested object: %v", records[0]["upstream"], records[0])
	}

	// Each member keeps its own key and its own value: no rename to ts or
	// event, and no lowercasing applied to a field that is not a level.
	for _, key := range []string{"time", "level", "msg"} {
		value, present := group[key]
		if !present {
			t.Errorf("group member %q was renamed away: %v", key, group)

			continue
		}

		if value != "SHOUTING-VALUE" {
			t.Errorf("group member %q = %v, want the value as supplied", key, value)
		}
	}

	for _, key := range []string{"ts", "event"} {
		if _, present := group[key]; present {
			t.Errorf("group acquired a record-level key %q: %v", key, group)
		}
	}
}

// TestSourceIsConstrainedToTheContractValues covers the source field, including
// the sentinel for a front door that supplies neither value.
func TestSourceIsConstrainedToTheContractValues(t *testing.T) {
	t.Parallel()

	if logging.SourceCLI != "cli" || logging.SourceGUI != "gui" {
		t.Fatalf("source constants are %q and %q, want \"cli\" and \"gui\"",
			logging.SourceCLI, logging.SourceGUI)
	}

	tests := []struct {
		name   string
		source logging.Source
		want   string
	}{
		{name: "cli", source: logging.SourceCLI, want: "cli"},
		{name: "gui", source: logging.SourceGUI, want: "gui"},
		// A zero Options.Source is the realistic mistake: a front door that
		// forgot the field. Records must still be written — losing diagnostics
		// over it would be the FR-076 failure — and must not claim to come
		// from a front door they did not.
		{name: "unset", source: "", want: "unknown"},
		{name: "invented", source: "daemon", want: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logger, buffer := newBufferLogger(t, logging.Options{Source: test.source})
			logger.Post("id").Info(logging.EventMessageReceived)

			records := decodeRecords(t, buffer.Bytes())
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			if got := requireString(t, records[0], "source"); got != test.want {
				t.Errorf("source = %q, want %q", got, test.want)
			}
		})
	}
}

// TestAnEmptyMessageIDBecomesASentinel covers per-post correlation against the
// one input that would quietly destroy it (R-007).
//
// An empty message_id would satisfy "the key is present" while making two
// concurrent posts correlate to the same empty value, silently interleaving
// them into one apparent post.
func TestAnEmptyMessageIDBecomesASentinel(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})
	logger.Post("").Info(logging.EventMessageReceived)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	if got := requireString(t, records[0], "message_id"); got != "unknown" {
		t.Errorf("message_id = %q, want the sentinel %q", got, "unknown")
	}
}

// TestASlogValuerIsHonoured guards the handler configuration against
// undoing the redaction the value types implement (FR-069, FR-043).
//
// config.Secret and post.SinkResult both redact through slog.LogValuer, which
// only helps if the handler resolves it. This is not the full secret-leak sweep
// — that is T084's sentinel-token gate across every render path — it is the
// narrow check that this handler does not bypass what those types provide.
func TestASlogValuerIsHonoured(t *testing.T) {
	t.Parallel()

	const sentinel = "1234567890:SENTINEL-BOT-TOKEN-MUST-NOT-APPEAR"

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})
	logger.Post("id").Error(logging.EventTelegramSendFailed,
		slog.Any("bot_token", config.NewSecret(sentinel)),
	)

	if bytes.Contains(buffer.Bytes(), []byte(sentinel)) {
		t.Fatalf("the sentinel token reached the log: %s", buffer.Bytes())
	}

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	if got := requireString(t, records[0], "bot_token"); got != "[redacted]" {
		t.Errorf("bot_token = %q, want the redaction marker", got)
	}
}

// TestOpenCreatesTheLogDirectory covers FR-075, including a path several levels
// below anything that exists.
func TestOpenCreatesTheLogDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "miko-post", "app.jsonl")

	logger, degraded := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
	if degraded != nil {
		t.Fatalf("Open reported degradation: %v", degraded.Err)
	}

	logger.Post("id").Info(logging.EventMessageReceived)

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if records := decodeRecords(t, raw); len(records) != 1 {
		t.Fatalf("got %d records in the file, want 1", len(records))
	}

	// The log accumulates message bodies whenever a post fails (FR-068), so
	// the file and its directory are the user's private notes and are not
	// world-readable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the log: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file mode is %#o, want 0600", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat the log directory: %v", err)
	}

	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("log directory mode is %#o, want 0700", perm)
	}
}

// TestOpenAppendsAndNeverTruncates covers FR-074 at the point where it is
// easiest to violate.
//
// O_TRUNC in place of O_APPEND passes every test that writes one record to a
// fresh file, and destroys the whole log on the next post.
func TestOpenAppendsAndNeverTruncates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.jsonl")
	existing := "{\"ts\":\"2026-01-01T00:00:00+09:00\",\"level\":\"info\",\"event\":\"message_received\"," +
		"\"source\":\"cli\",\"message_id\":\"earlier-run\"}\n"

	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	logger, degraded := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
	if degraded != nil {
		t.Fatalf("Open reported degradation: %v", degraded.Err)
	}

	logger.Post("this-run").Info(logging.EventRequestCompleted)

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if !strings.HasPrefix(string(raw), existing) {
		t.Fatalf("the earlier run's records were lost; log is now:\n%s", raw)
	}

	records := decodeRecords(t, raw)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	if got := requireString(t, records[0], "message_id"); got != "earlier-run" {
		t.Errorf("first record message_id = %q, want the seeded one", got)
	}
}

// TestOpenDegradesWhenTheLogCannotBeOpened covers FR-076's first half: the
// logger stays usable and the caller gets one warning naming path and reason.
func TestOpenDegradesWhenTheLogCannotBeOpened(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// path is built from the temp dir, because what makes each case fail is
		// a filesystem shape rather than a string.
		build func(t *testing.T, dir string) string
	}{
		{
			name: "a parent path component is a regular file",
			build: func(t *testing.T, dir string) string {
				t.Helper()

				blocker := filepath.Join(dir, "not-a-dir")
				if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
					t.Fatalf("create the blocking file: %v", err)
				}

				return filepath.Join(blocker, "miko-post", "app.jsonl")
			},
		},
		{
			name: "the log path is itself a directory",
			build: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, "app.jsonl")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("create the directory in the log's place: %v", err)
				}

				return path
			},
		},
		{
			name: "no path was resolved",
			build: func(t *testing.T, _ string) string {
				t.Helper()

				return ""
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := test.build(t, t.TempDir())

			logger, degraded := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
			if degraded == nil {
				t.Fatal("Open reported no degradation for a log it cannot write")
			}

			if degraded.Err == nil {
				t.Error("the degradation carries no reason")
			}

			if degraded.Path != path {
				t.Errorf("degradation path = %q, want %q", degraded.Path, path)
			}

			warning := degraded.Warning()
			if path != "" && !strings.Contains(warning, path) {
				t.Errorf("the warning does not name the log path: %q", warning)
			}

			if !strings.Contains(warning, degraded.Err.Error()) {
				t.Errorf("the warning does not carry the reason: %q", warning)
			}

			// The whole point of FR-076: a post still runs. Logging through the
			// degraded logger must not panic and must not block.
			post := logger.Post("id")
			post.Info(logging.EventMessageReceived)
			post.Error(logging.EventRequestCompletedWithError)

			if err := logger.Close(); err != nil {
				t.Errorf("Close on a degraded logger: %v", err)
			}

			// Degraded stays consistent after the fact, so a front door
			// rendering results at the end sees the same thing Open reported.
			if again := logger.Degraded(); again == nil {
				t.Error("Degraded went back to nil after logging")
			}
		})
	}
}

// failingWriter fails every write after the first n succeed.
type failingWriter struct {
	mu        sync.Mutex
	succeeded int
	allow     int
	err       error
}

func (f *failingWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.succeeded < f.allow {
		f.succeeded++

		return len(p), nil
	}

	return 0, f.err
}

// TestAFailedWriteDegradesWithoutBlockingThePost covers FR-076's second half: a
// write that fails after a successful open.
//
// This is the disk-full and revoked-mount case, and it is invisible to Open —
// which is why Degraded is a method consulted when results are rendered rather
// than only a value Open returns.
func TestAFailedWriteDegradesWithoutBlockingThePost(t *testing.T) {
	t.Parallel()

	first := errors.New("no space left on device")
	writer := &failingWriter{allow: 1, err: first}

	logger, degraded := logging.Open(logging.Options{
		Source: logging.SourceCLI,
		Writer: writer,
	})
	if degraded != nil {
		t.Fatalf("Open reported degradation before any write failed: %v", degraded.Err)
	}

	post := logger.Post("id")

	post.Info(logging.EventMessageReceived)

	if got := logger.Degraded(); got != nil {
		t.Fatalf("Degraded is non-nil after a successful write: %v", got.Err)
	}

	post.Error(logging.EventTelegramSendFailed)
	post.Error(logging.EventObsidianAppendFailed)
	post.Error(logging.EventRequestCompletedWithError)

	got := logger.Degraded()
	if got == nil {
		t.Fatal("Degraded is nil after every write failed")
	}

	// The first failure is the one kept: subsequent writes fail with the same
	// cause and FR-076 allows exactly one warning, so a later error has nothing
	// to add and must not displace the original reason.
	if !errors.Is(got.Err, first) {
		t.Errorf("degradation reason = %v, want the first write failure %v", got.Err, first)
	}
}

// TestConcurrentPostsProduceWellFormedRecords covers the shape the orchestrator
// actually produces (FR-070, and the -race gate in the constitution).
//
// Two sinks log concurrently for one post, and a GUI window can have an earlier
// post still logging while the next begins. Interleaved writes that tore a
// record in half would break FR-064 in exactly the situation nobody tests by
// hand.
func TestConcurrentPostsProduceWellFormedRecords(t *testing.T) {
	t.Parallel()

	const (
		posts           = 16
		recordsPerPost  = 8
		expectedRecords = posts * recordsPerPost
	)

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})

	var wait sync.WaitGroup

	for p := range posts {
		wait.Add(1)

		go func(p int) {
			defer wait.Done()

			post := logger.Post(fmt.Sprintf("post-%02d", p))

			for r := range recordsPerPost {
				post.Info(logging.EventTelegramSendStarted, slog.Int("seq", r))
			}
		}(p)
	}

	wait.Wait()

	if got := logger.Degraded(); got != nil {
		t.Fatalf("Degraded is non-nil: %v", got.Err)
	}

	records := decodeRecords(t, buffer.Bytes())

	if len(records) != expectedRecords {
		t.Fatalf("got %d records, want %d", len(records), expectedRecords)
	}

	// Every record still carries a complete, correctly attributed preamble —
	// a torn or interleaved write would show up as a missing key or a
	// message_id belonging to another post's sequence.
	perPost := make(map[string]int, posts)

	for _, record := range records {
		for _, key := range alwaysPresent {
			requireString(t, record, key)
		}

		perPost[requireString(t, record, "message_id")]++
	}

	if len(perPost) != posts {
		t.Errorf("records correlate to %d posts, want %d", len(perPost), posts)
	}

	for id, count := range perPost {
		if count != recordsPerPost {
			t.Errorf("post %s has %d records, want %d", id, count, recordsPerPost)
		}
	}
}

// TestCloseLeavesASuppliedWriterAlone covers the Options.Writer ownership rule
// that the rotating writer (T068-T070) will depend on.
func TestCloseLeavesASuppliedWriterAlone(t *testing.T) {
	t.Parallel()

	writer := &closeCountingWriter{}

	logger, degraded := logging.Open(logging.Options{Source: logging.SourceCLI, Writer: writer})
	if degraded != nil {
		t.Fatalf("Open reported degradation: %v", degraded.Err)
	}

	logger.Post("id").Info(logging.EventMessageReceived)

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if writer.closes != 0 {
		t.Errorf("Close closed a writer it did not open (%d times)", writer.closes)
	}

	// Closing twice is what a deferred Close plus an explicit one produces, and
	// must not turn into an error a front door has to special-case.
	if err := logger.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// closeCountingWriter records Close calls without acting on them.
type closeCountingWriter struct {
	io.Writer
	closes int
}

func (c *closeCountingWriter) Write(p []byte) (int, error) { return len(p), nil }

func (c *closeCountingWriter) Close() error {
	c.closes++

	return nil
}

// TestResolvePath covers FR-056's rule that an empty logging.path means the
// default rather than a literal empty path (FR-065).
func TestResolvePath(t *testing.T) {
	// Not parallel: t.Setenv is process-wide.
	t.Setenv("XDG_STATE_HOME", filepath.Join("/xdg", "state"))

	configured := filepath.Join("/somewhere", "else", "custom.jsonl")

	got, err := logging.ResolvePath(configured)
	if err != nil {
		t.Fatalf("ResolvePath with a configured path: %v", err)
	}

	if got != configured {
		t.Errorf("ResolvePath(%q) = %q, want the configured path unchanged", configured, got)
	}

	got, err = logging.ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath(\"\"): %v", err)
	}

	want, err := config.DefaultLogPath()
	if err != nil {
		t.Fatalf("config.DefaultLogPath: %v", err)
	}

	if got != want {
		t.Errorf("ResolvePath(\"\") = %q, want the resolved default %q", got, want)
	}
}
