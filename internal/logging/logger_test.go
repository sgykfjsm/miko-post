package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
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

	logger := logging.Open(opts)
	if degraded := logger.Degraded(); degraded != nil {
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

// ownedKeys are the record keys this package writes. A caller attribute must
// not be able to put a second copy of any of them into a record, in any shape.
var ownedKeys = []string{"ts", "level", "event", "source", "message_id", "app_version", "git_commit"}

// forgedArgs are attributes that each try to shadow one owned key, plus one
// legitimate attribute that must survive.
//
// "time" and "msg" are in the list because they are not near misses: the
// handler manufactures ts out of slog.TimeKey and event out of slog.MessageKey,
// so an attribute under either name is *renamed into* an identity key. "msg" in
// particular is one character from the contract's own message field and is the
// habitual slog spelling, so a call site will reach for it.
//
// Returned as []any because slog.Group takes args that way; forgedAttrs is the
// same list for the call sites that need []slog.Attr.
func forgedArgs() []any {
	return []any{
		slog.String("ts", "FORGED-TS"),
		slog.String("level", "FORGED-LEVEL"),
		slog.String("event", "FORGED-EVENT"),
		slog.String("source", "FORGED-SOURCE"),
		slog.String("message_id", "FORGED-ID"),
		slog.String("app_version", "FORGED-VERSION"),
		slog.String("git_commit", "FORGED-COMMIT"),
		slog.String("time", "FORGED-TS"),
		slog.String("msg", "FORGED-EVENT"),
		slog.String("sink", "obsidian"),
	}
}

func forgedAttrs() []slog.Attr {
	args := forgedArgs()
	attrs := make([]slog.Attr, 0, len(args))

	for _, arg := range args {
		attrs = append(attrs, arg.(slog.Attr))
	}

	return attrs
}

// forgedValuer resolves to an empty-key group, which is the shape a LogValuer
// reaches the handler as. post.SinkResult.LogValue returns a group already, so
// this is not a hypothetical construction.
type forgedValuer struct{}

func (forgedValuer) LogValue() slog.Value {
	return slog.GroupValue(forgedAttrs()...)
}

// shiftingValuer resolves to a plain string the first time and to an empty-key
// group of forged identity keys after that.
//
// It exists to pin one property: whatever this package resolves in order to
// judge an attribute is what it must forward, so the handler cannot be shown a
// different value than the filter inspected. Not safe to share between records
// — each test that uses it constructs its own.
type shiftingValuer struct {
	resolved int
}

func (s *shiftingValuer) LogValue() slog.Value {
	s.resolved++

	if s.resolved == 1 {
		return slog.StringValue("harmless")
	}

	return slog.GroupValue(forgedAttrs()...)
}

// TestNoAttributeShapeCanDuplicateAnOwnedKey covers the identity fields against
// every nesting shape a caller attribute can arrive in.
//
// TestReservedKeysCannotBeShadowed above covers the flat shape. It is not
// enough, because slog does not open a group for an *empty* key: the members of
// slog.Group("", …) are emitted at the top level, and ReplaceAttr sees
// len(groups) == 0 for each of them. A filter that inspects only attr.Key sees
// one attribute named "" — not a reserved name — and passes the whole group,
// putting a second event, level, source, message_id and ts into the record with
// the forged values *last*, which is the copy most decoders keep.
//
// The same shape arrives through slog.Any("", v) when v.LogValue() returns a
// group, so the value has to be resolved before it can be judged.
func TestNoAttributeShapeCanDuplicateAnOwnedKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		attrs []slog.Attr
	}{
		{
			name:  "flat attributes",
			attrs: forgedAttrs(),
		},
		{
			name:  "an empty-key group",
			attrs: []slog.Attr{slog.Group("", forgedArgs()...)},
		},
		{
			// A chain of empty-key groups all inlines to the top level, so one
			// level of unwrapping is not enough.
			name:  "nested empty-key groups",
			attrs: []slog.Attr{slog.Group("", slog.Group("", forgedArgs()...))},
		},
		{
			name:  "a LogValuer resolving to an empty-key group",
			attrs: []slog.Attr{slog.Any("", forgedValuer{})},
		},
		{
			// The shape that defeats resolving a value for the check and then
			// forwarding the original attr. forgedValuer above answers the
			// same way every time, so that mistake still passes it: the
			// handler resolves a second time and gets the same group. This one
			// answers differently, so the check sees a plain string and the
			// handler sees a group full of forged identity keys.
			//
			// A LogValuer is documented as cheap and expected to be pure, so
			// this is a call-site bug rather than an attack. It belongs here
			// anyway: the filter's whole job is to keep the record's identity
			// fields from depending on a call site being correct.
			name:  "a LogValuer that answers differently the second time",
			attrs: []slog.Attr{slog.Any("", &shiftingValuer{})},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logger, buffer := newBufferLogger(t, logging.Options{
				Source:     logging.SourceCLI,
				AppVersion: "0.1.0",
				GitCommit:  "abc1234",
			})

			logger.Post("real-id").Info(logging.EventMessageReceived, test.attrs...)

			raw := buffer.Bytes()

			records := decodeRecords(t, raw)
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			// A decoded map cannot show a duplicate key, so the raw line is
			// what the duplicate has to be ruled out against. Counting over
			// the whole line is only sound because none of the shapes above
			// produces a nested object: an owned key inside a *named* group is
			// a different field and is required to survive, which
			// TestANamedGroupIsNotTraversed covers.
			for _, key := range ownedKeys {
				if count := bytes.Count(raw, []byte(`"`+key+`":`)); count != 1 {
					t.Errorf("key %q appears %d times in the record, want 1\nline: %s", key, count, raw)
				}
			}

			want := map[string]string{
				"ts":          "",
				"level":       "info",
				"event":       string(logging.EventMessageReceived),
				"source":      string(logging.SourceCLI),
				"message_id":  "real-id",
				"app_version": "0.1.0",
				"git_commit":  "abc1234",
			}

			for key, wantValue := range want {
				got := requireString(t, records[0], key)
				if wantValue != "" && got != wantValue {
					t.Errorf("%s = %q, want %q", key, got, wantValue)
				}
			}
		})
	}
}

// TestAForgedAttributeIsDroppedRatherThanRenamed is the other half of the
// filter: nothing forged survives anywhere in the record, and the legitimate
// attribute travelling with it does.
//
// Separate from the test above because "appears once" and "holds the right
// value" would both pass if the filter dropped the record's own key and kept
// the caller's, and because a filter that simply discarded every attribute
// would pass a duplicate-count assertion perfectly.
func TestAForgedAttributeIsDroppedRatherThanRenamed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		attrs []slog.Attr
	}{
		{name: "flat attributes", attrs: forgedAttrs()},
		{name: "an empty-key group", attrs: []slog.Attr{slog.Group("", forgedArgs()...)}},
		{
			name:  "nested empty-key groups",
			attrs: []slog.Attr{slog.Group("", slog.Group("", forgedArgs()...))},
		},
		{
			name:  "a LogValuer resolving to an empty-key group",
			attrs: []slog.Attr{slog.Any("", forgedValuer{})},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})
			logger.Post("real-id").Info(logging.EventMessageReceived, test.attrs...)

			raw := buffer.Bytes()

			if bytes.Contains(raw, []byte("FORGED")) {
				t.Errorf("a forged value reached the record: %s", raw)
			}

			records := decodeRecords(t, raw)
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			if got := requireString(t, records[0], "sink"); got != "obsidian" {
				t.Errorf("the legitimate attribute travelling with the forged ones was lost: %v", records[0])
			}
		})
	}
}

// TestANamedGroupIsNotTraversed is the boundary of the filter above.
//
// An empty-key group is unwrapped because slog inlines its members where the
// record's own keys live. A *named* group must not be, and neither must an
// empty-key group nested inside one: those members inline into the named group,
// so upstream.event is upstream's own field and dropping it would silently
// destroy a caller's data to fix a collision that does not exist.
//
// This is what stops the fix for the empty-key case from being applied one
// level too far.
//
// The named group deliberately travels alongside a reserved-key attribute, and
// that detail is the test rather than incidental to it. The filter returns its
// input untouched unless some attribute has a reserved or empty key, so a call
// carrying only slog.Group("upstream", …) never enters the filtering loop at
// all — the boundary would be enforced by that early return instead of by the
// condition this test is named for, and a change that filtered *named* groups
// too would leave the test passing while silently deleting the whole upstream
// object. The reserved sibling is what makes the loop run.
func TestANamedGroupIsNotTraversed(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})

	logger.Post("real-id").Error(logging.EventTelegramSendFailed,
		// Dropped, and present so that the filtering loop is entered.
		slog.String("event", "forged"),
		slog.Group("upstream",
			slog.Group("",
				slog.String("event", "upstreams-own-event"),
				slog.String("level", "upstreams-own-level"),
				slog.String("message_id", "upstreams-own-id"),
			),
		),
	)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	// The record's own preamble is untouched.
	if got := requireString(t, records[0], "event"); got != string(logging.EventTelegramSendFailed) {
		t.Errorf("record event = %q, want %q", got, logging.EventTelegramSendFailed)
	}

	if got := requireString(t, records[0], "message_id"); got != "real-id" {
		t.Errorf("record message_id = %q, want %q", got, "real-id")
	}

	group, ok := records[0]["upstream"].(map[string]any)
	if !ok {
		t.Fatalf("upstream is %T, want a nested object: %v", records[0]["upstream"], records[0])
	}

	for key, want := range map[string]string{
		"event":      "upstreams-own-event",
		"level":      "upstreams-own-level",
		"message_id": "upstreams-own-id",
	} {
		if got := requireString(t, group, key); got != want {
			t.Errorf("upstream.%s = %q, want %q; the filter reached into a named group", key, got, want)
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

	logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
	if degraded := logger.Degraded(); degraded != nil {
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

	logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
	if degraded := logger.Degraded(); degraded != nil {
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

			logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})

			degraded := logger.Degraded()
			if degraded == nil {
				t.Fatal("Degraded reported nothing for a log that cannot be written")
			}

			if degraded.Err == nil {
				t.Error("the degradation carries no reason")
			}

			if degraded.Path != path {
				t.Errorf("degradation path = %q, want %q", degraded.Path, path)
			}

			warning := degraded.Warning()

			// The no-path case gets its own expected wording rather than
			// dropping the assertion. Guarding this check with `path != ""`
			// made it disappear in exactly the case worth checking, and what
			// it was hiding was "could not be written to : no log path was
			// resolved" — a message with a hole where the path would be.
			if path == "" {
				const wantNoPath = "warning: diagnostics could not be written: "
				if !strings.HasPrefix(warning, wantNoPath) {
					t.Errorf("the no-path warning is %q, want it to start %q", warning, wantNoPath)
				}

				if strings.Contains(warning, "written to") {
					t.Errorf("the warning still names a path it does not have: %q", warning)
				}
			} else if !strings.Contains(warning, path) {
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

	logger := logging.Open(logging.Options{
		Source: logging.SourceCLI,
		Writer: writer,
	})
	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("Degraded is non-nil before any write failed: %v", degraded.Err)
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

// shortWriter writes half of its first record and then behaves normally.
//
// The existing failingWriter returns (0, err), which loses a record cleanly.
// This is the other half of io.Writer's failure space and the destructive one:
// a partial write leaves a fragment on the line, and whatever is appended next
// joins it.
type shortWriter struct {
	mu       sync.Mutex
	written  bytes.Buffer
	truncate bool
	err      error
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.truncate {
		w.truncate = false

		half := len(p) / 2
		n, _ := w.written.Write(p[:half])

		return n, w.err
	}

	return w.written.Write(p)
}

func (w *shortWriter) contents() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.written.String()
}

// TestAPartialWriteCostsOneRecordAndNotTwo covers the short-write case
// (FR-064, FR-076).
//
// slog hands the writer one whole record, trailing newline included, in a
// single Write, so a short count always truncates mid-object: that record is
// lost whatever happens. What must not also be lost is the *next* one — with
// the fragment left unterminated, the following record is appended onto it and
// one failed write costs two unparseable records instead of one.
// Both shapes of short write are covered. A writer returning (n, err) is the
// filesystem case FR-076 names. A writer returning (n, nil) violates
// io.Writer's contract, which is exactly why it has to be tested: nothing
// downstream reports it, so if safeWriter did not treat a short count as a
// failure in its own right, a wrapper with that bug would corrupt the line
// stream in complete silence. The declared Options.Writer seam (T068-T070) is
// where such a wrapper would come from.
func TestAPartialWriteCostsOneRecordAndNotTwo(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "reported as an error", err: errors.New("no space left on device")},
		{name: "reported as a success", err: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			partialWriteCostsOneRecord(t, test.err)
		})
	}
}

func partialWriteCostsOneRecord(t *testing.T, writeErr error) {
	t.Helper()

	writer := &shortWriter{truncate: true, err: writeErr}

	logger := logging.Open(logging.Options{Source: logging.SourceCLI, Writer: writer})

	post := logger.Post("id")
	post.Info(logging.EventMessageReceived)
	post.Info(logging.EventObsidianAppendStarted)
	post.Error(logging.EventRequestCompletedWithError)

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A short count is a failure in its own right, whether or not the writer
	// admitted one, so the one warning FR-076 allows still gets emitted.
	degraded := logger.Degraded()
	if degraded == nil {
		t.Fatal("Degraded is nil after a write that stored only half a record")
	}

	lines := strings.Split(strings.TrimSuffix(writer.contents(), "\n"), "\n")

	// The truncated record is line one and is expected to be unreadable. Every
	// later line must decode on its own.
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (the truncated record, then two whole ones):\n%s",
			len(lines), writer.contents())
	}

	var fragment map[string]any
	if json.Unmarshal([]byte(lines[0]), &fragment) == nil {
		t.Errorf("line 1 decoded; the test no longer exercises a truncated record: %s", lines[0])
	}

	wantEvents := []logging.Event{
		logging.EventObsidianAppendStarted,
		logging.EventRequestCompletedWithError,
	}

	for i, line := range lines[1:] {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d did not survive the truncated record before it: %v\nline: %s",
				i+2, err, line)

			continue
		}

		for _, key := range alwaysPresent {
			requireString(t, record, key)
		}

		if got := requireString(t, record, "event"); got != string(wantEvents[i]) {
			t.Errorf("line %d event = %q, want %q", i+2, got, wantEvents[i])
		}
	}
}

// TestDegradedIsSafeToReadWhileRecordsAreBeingWritten is what makes
// safeWriter's mutex load-bearing (the -race gate in the constitution).
//
// Degraded's own comment promises it is safe to call while sinks are still
// logging, and the mutex's comment says that is the reason it exists. Reading
// Degraded after a WaitGroup has drained — as the test below does — orders the
// read strictly after every write, so it would stay green with the lock
// removed. The overlap has to be real, and the writes have to actually *fail*:
// the first error is the only field Write ever stores, so a run in which every
// write succeeds has nothing for the read to race.
func TestDegradedIsSafeToReadWhileRecordsAreBeingWritten(t *testing.T) {
	t.Parallel()

	const (
		posts          = 16
		recordsPerPost = 8
		succeedFirst   = 4
	)

	failure := errors.New("no space left on device")
	logger := logging.Open(logging.Options{
		Source: logging.SourceCLI,
		Writer: &failingWriter{allow: succeedFirst, err: failure},
	})

	var (
		emitting sync.WaitGroup
		reading  sync.WaitGroup
	)

	for p := range posts {
		emitting.Add(1)

		go func(p int) {
			defer emitting.Done()

			post := logger.Post(fmt.Sprintf("post-%02d", p))

			for r := range recordsPerPost {
				post.Info(logging.EventTelegramSendStarted, slog.Int("seq", r))
			}
		}(p)
	}

	stop := make(chan struct{})

	reading.Add(1)

	go func() {
		defer reading.Done()

		// Once degraded, always degraded: the first error is kept and never
		// cleared, so a nil after a non-nil would mean a torn read.
		seen := false

		for {
			degraded := logger.Degraded()

			switch {
			case degraded != nil:
				seen = true

				if degraded.Err == nil {
					t.Error("a degradation was reported with no reason")

					return
				}
			case seen:
				t.Error("Degraded went back to nil after reporting a failure")

				return
			}

			select {
			case <-stop:
				return
			default:
			}
		}
	}()

	emitting.Wait()
	close(stop)
	reading.Wait()

	got := logger.Degraded()
	if got == nil {
		t.Fatal("Degraded is nil after every write past the first few failed")
	}

	if !errors.Is(got.Err, failure) {
		t.Errorf("degradation reason = %v, want the first write failure %v", got.Err, failure)
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

	logger := logging.Open(logging.Options{Source: logging.SourceCLI, Writer: writer})
	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("Open reported degradation: %v", degraded.Err)
	}

	logger.Post("id").Info(logging.EventMessageReceived)

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if writer.closes != 0 {
		t.Errorf("Close closed a writer it did not open (%d times)", writer.closes)
	}

	// A second Close must not reach the writer either. This half of the double
	// Close is about ownership only; that Close is idempotent is asserted
	// against a file-backed logger below, which is the path that can actually
	// fail it.
	if err := logger.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	if writer.closes != 0 {
		t.Errorf("a second Close closed a writer it did not open (%d times)", writer.closes)
	}
}

// TestCloseIsIdempotentOnAFileBackedLogger covers the claim both Close doc
// comments make, on the only path that can break it.
//
// A deferred Close plus an explicit one is the ordinary shape, and the second
// call must not become an error a front door has to special-case. Asserting
// this through Options.Writer — as the test above used to — cannot fail: with
// no file to close, Close returns nil unconditionally for any implementation,
// which is how a real "file already closed" on the second call survived full
// statement coverage.
func TestCloseIsIdempotentOnAFileBackedLogger(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.jsonl")

	logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("Open reported degradation: %v", degraded.Err)
	}

	logger.Post("id").Info(logging.EventMessageReceived)

	if err := logger.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	for attempt := 2; attempt <= 3; attempt++ {
		if err := logger.Close(); err != nil {
			t.Errorf("Close attempt %d: %v", attempt, err)
		}
	}

	// Idempotence must not have been bought by never closing at all: the
	// record written before the first Close is on disk and complete.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if records := decodeRecords(t, raw); len(records) != 1 {
		t.Fatalf("got %d records in the file, want 1", len(records))
	}

	// A record emitted after Close is discarded, not reported. The handle is
	// gone either way, and turning the shutdown into a degradation would spend
	// FR-076's single warning on it instead of on a real problem.
	logger.Post("late").Info(logging.EventRequestCompleted)

	if got := logger.Degraded(); got != nil {
		t.Errorf("a write after Close produced a degradation: %v", got.Err)
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

	// A literal built from the environment this test sets, not a second call
	// to config.DefaultLogPath. ResolvePath("") *is* a call to that function,
	// so comparing the two asserted nothing: it passed for every possible
	// value the resolver could return, including one that ignored
	// XDG_STATE_HOME entirely, which is the seam the t.Setenv above exists to
	// pin. config.DefaultLogPath owns whether this layout is right
	// (paths_test.go); this owns that ResolvePath("") reaches it.
	want := filepath.Join("/xdg", "state", "miko-post", "app.jsonl")

	if got != want {
		t.Errorf("ResolvePath(\"\") = %q, want the resolved default %q", got, want)
	}
}

// TestFileKindNamesEveryRejectedType covers the phrases the one FR-076 warning
// uses when something that is not a regular file sits at the log path.
//
// The point of naming the type is that "the log path is a named pipe" is
// immediately actionable where a bare errno is not, so each branch has to
// actually produce its phrase. Driven by mode rather than by real files: see
// export_test.go.
func TestFileKindNamesEveryRejectedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{name: "directory", mode: os.ModeDir | 0o755, want: "a directory"},
		{name: "named pipe", mode: os.ModeNamedPipe | 0o600, want: "a named pipe"},
		{name: "socket", mode: os.ModeSocket | 0o600, want: "a socket"},
		// Not a symlink: os.Stat follows links, so a symlink mode never
		// reaches fileKind. This is the fallback arm, driven by the one
		// mode that names no specific kind.
		{name: "an irregular file", mode: os.ModeIrregular | 0o600, want: "unsupported type"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := logging.FileKind(test.mode)
			if !strings.Contains(got, test.want) {
				t.Errorf("FileKind(%s) = %q, want it to mention %q", test.mode, got, test.want)
			}
		})
	}
}

// TestUsableAsLogAllowsWhatCanBeAppendedTo covers which shapes at the log path
// are refused and which are not.
//
// The device rows are the point. A device node is allowed even though it is not
// a regular file, because logging.path has no companion "disabled" setting, so
// /dev/null is how a user turns diagnostics off — and refusing it produced an
// unsilenceable warning on every post for a path that opens instantly and
// discards exactly as asked. What stays refused is what cannot be appended to:
// a FIFO blocks in open(2) until a reader attaches and would stop the post, a
// socket cannot be opened this way, and a directory fails.
func TestUsableAsLogAllowsWhatCanBeAppendedTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{name: "regular file", mode: 0o600, want: true},
		{name: "character device, as /dev/null is", mode: os.ModeDevice | os.ModeCharDevice | 0o666, want: true},
		{name: "block device", mode: os.ModeDevice | 0o660, want: true},
		{name: "directory", mode: os.ModeDir | 0o755, want: false},
		{name: "named pipe", mode: os.ModeNamedPipe | 0o600, want: false},
		{name: "socket", mode: os.ModeSocket | 0o600, want: false},
		{name: "irregular", mode: os.ModeIrregular | 0o600, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := logging.UsableAsLog(test.mode); got != test.want {
				t.Errorf("UsableAsLog(%s) = %t, want %t", test.mode, got, test.want)
			}
		})
	}
}

// TestOpenAcceptsADeviceAtTheLogPath is the end-to-end half of the row above:
// /dev/null must open, discard, and report no degradation, because that is how
// a user opts out of diagnostics.
func TestOpenAcceptsADeviceAtTheLogPath(t *testing.T) {
	t.Parallel()

	info, err := os.Stat(os.DevNull)
	if err != nil {
		t.Skipf("cannot stat %s: %v", os.DevNull, err)
	}

	if info.Mode().IsRegular() {
		t.Skipf("%s is a regular file on this platform; the device case is not reachable", os.DevNull)
	}

	logger := logging.Open(logging.Options{Path: os.DevNull, Source: logging.SourceCLI})

	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("%s was refused: %s", os.DevNull, degraded.Warning())
	}

	logger.Post("id").Info(logging.EventMessageReceived)

	if degraded := logger.Degraded(); degraded != nil {
		t.Errorf("writing to %s degraded: %s", os.DevNull, degraded.Warning())
	}

	if err := logger.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestOpenDegradesOnAnUnwritableRegularFile keeps the OpenFile failure path
// exercised.
//
// It used to be reached by the "log path is a directory" case, but the
// non-regular-file check now rejects a directory before OpenFile is called, so
// that test no longer proves anything about OpenFile's error handling. A
// read-only log file is the realistic remaining route — and a plausible one,
// since a user who wants to stop diagnostics being written may well chmod the
// file rather than change the setting.
func TestOpenDegradesOnAnUnwritableRegularFile(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission bits this test relies on")
	}

	path := filepath.Join(t.TempDir(), "app.jsonl")
	if err := os.WriteFile(path, nil, 0o400); err != nil {
		t.Fatalf("create the read-only log: %v", err)
	}

	logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})

	degraded := logger.Degraded()
	if degraded == nil {
		t.Fatal("Open reported no degradation for a log file it cannot write")
	}

	// It must read as a failure to open the file, not as a failure to identify
	// it: the non-regular-file check must have passed this through rather than
	// claimed a read-only regular file is the wrong type.
	if warning := degraded.Warning(); !strings.Contains(warning, "open the log file") {
		t.Errorf("the warning does not attribute the failure to opening the file: %q", warning)
	}

	// FR-076 in full.
	logger.Post("id").Error(logging.EventRequestCompletedWithError)

	if err := logger.Close(); err != nil {
		t.Errorf("Close on a degraded logger: %v", err)
	}
}

// TestTheConfiguredCredentialNeverReachesTheLog covers the chokepoint scrub
// (FR-069, FR-043).
//
// The shapes here are not hypothetical. contracts/log-events.md makes `error`
// required on a failure, and its only natural source is post.SinkResult.Err,
// which for a Telegram transport failure is a *url.Error whose exported URL
// field is the bot endpoint with the token in the path. The value types cannot
// defend that: an error has no LogValue, and slog hands an unrecognised value
// to json.Marshal, which prints the URL verbatim. So every one of these is a
// route a real emitting task (T040, T063) would take.
func TestTheConfiguredCredentialNeverReachesTheLog(t *testing.T) {
	t.Parallel()

	const token = "1234567890:SENTINEL-BOT-TOKEN-MUST-NOT-APPEAR"

	endpoint := "https://api.telegram.org/bot" + token + "/sendMessage"
	transport := &url.Error{Op: "Post", URL: endpoint, Err: errors.New("dial tcp: i/o timeout")}

	tests := []struct {
		name string
		attr slog.Attr
	}{
		{name: "the error value itself", attr: slog.Any("error", transport)},
		{name: "the error's message as a string", attr: slog.String("error", transport.Error())},
		{name: "a wrapped error", attr: slog.Any("error", fmt.Errorf("sending failed: %w", transport))},
		{name: "the raw endpoint in a string field", attr: slog.String("url", endpoint)},
		{name: "a Stringer carrying it", attr: slog.Any("target", stringerCarrying{endpoint})},
		{name: "nested inside a named group", attr: slog.Group("upstream", slog.Any("error", transport))},
		{name: "nested inside an empty-key group", attr: slog.Group("", slog.Any("error", transport))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logger, buffer := newBufferLogger(t, logging.Options{
				Source: logging.SourceCLI,
				Redact: []config.Secret{config.NewSecret(token)},
			})

			logger.Post("id").Error(logging.EventTelegramSendFailed, test.attr)

			if bytes.Contains(buffer.Bytes(), []byte(token)) {
				t.Fatalf("the credential reached the log:\n%s", buffer.Bytes())
			}

			// The record must still be usable: redaction that destroyed the
			// diagnostic would trade one FR for another.
			records := decodeRecords(t, buffer.Bytes())
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			if !bytes.Contains(buffer.Bytes(), []byte("[redacted]")) {
				t.Errorf("nothing was marked as redacted; the value may have been dropped instead:\n%s",
					buffer.Bytes())
			}
		})
	}
}

// stringerCarrying is a value whose only rendering is through fmt.Stringer.
type stringerCarrying struct {
	text string
}

func (s stringerCarrying) String() string { return s.text }

// TestRedactionLeavesOrdinaryDiagnosticsIntact is the control for the test
// above.
//
// A scrub that quietly rewrote or dropped legitimate text would pass every
// assertion there while making the log useless. Exact-substring replacement of
// one known credential is what makes that impossible, and this pins it.
func TestRedactionLeavesOrdinaryDiagnosticsIntact(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{
		Source: logging.SourceCLI,
		Redact: []config.Secret{config.NewSecret("the-secret")},
	})

	logger.Post("id").Error(logging.EventTelegramSendFailed,
		slog.String("error_type", "timeout"),
		slog.String("message", "今日も美琴が可愛い♡"),
		slog.Int64("duration_ms", 10012),
		slog.Int("http_status", 502),
		slog.Bool("retried", false),
	)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	if got := requireString(t, records[0], "error_type"); got != "timeout" {
		t.Errorf("error_type = %q, want %q", got, "timeout")
	}

	if got := requireString(t, records[0], "message"); got != "今日も美琴が可愛い♡" {
		t.Errorf("message = %q, want it unchanged", got)
	}

	// The numeric and boolean fields must keep their JSON types: a scrub that
	// stringified every value would break every consumer that reads
	// duration_ms as a number.
	for key, want := range map[string]float64{"duration_ms": 10012, "http_status": 502} {
		value, ok := records[0][key].(float64)
		if !ok {
			t.Errorf("%s is %T, want a JSON number", key, records[0][key])

			continue
		}

		if value != want {
			t.Errorf("%s = %v, want %v", key, value, want)
		}
	}

	if _, ok := records[0]["retried"].(bool); !ok {
		t.Errorf("retried is %T, want a JSON bool", records[0]["retried"])
	}
}

// TestNoRedactionConfiguredIsTheUnscrubbedPath covers the cost-free default,
// and documents that the scrub is opt-in: a caller that supplies no credential
// gets exactly what it logged.
func TestNoRedactionConfiguredIsTheUnscrubbedPath(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{Source: logging.SourceCLI})

	logger.Post("id").Error(logging.EventTelegramSendFailed,
		slog.Any("error", errors.New("dial tcp: i/o timeout")),
	)

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	// Still flattened to a message, because that is the shape
	// contracts/log-events.md specifies for the field — but not because it was
	// scrubbed.
	if got := requireString(t, records[0], "error"); got != "dial tcp: i/o timeout" {
		t.Errorf("error = %q, want the message unchanged", got)
	}
}

// TestAnEmptySecretIsIgnored guards the realistic wiring mistake: a front door
// that passes the credential whether or not Telegram is enabled.
//
// An empty Secret must not become an empty search string, which would match
// everywhere and replace nothing usefully.
func TestAnEmptySecretIsIgnored(t *testing.T) {
	t.Parallel()

	logger, buffer := newBufferLogger(t, logging.Options{
		Source: logging.SourceCLI,
		Redact: []config.Secret{config.NewSecret(""), {}},
	})

	logger.Post("id").Info(logging.EventMessageReceived, slog.String("message", "unchanged"))

	records := decodeRecords(t, buffer.Bytes())
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	if got := requireString(t, records[0], "message"); got != "unchanged" {
		t.Errorf("message = %q, want %q", got, "unchanged")
	}
}

// panickingWriter panics on its first write.
type panickingWriter struct{}

func (panickingWriter) Write([]byte) (int, error) { panic("writer exploded") }

// TestAPanickingWriterCannotTakeDownThePost covers the emission path's
// recover (FR-076, T023's "never panicking, never blocking a post").
//
// Options.Writer is the seam the rotating writer plugs into (T068-T070), and
// rename/stat/reopen logic is where a nil dereference lives. Without the
// recover, that panic unwinds through the handler into the sink's goroutine:
// the sink never reports, and the exit status describes a post that did not
// finish — diagnostics changing the outcome, which is the one thing FR-076
// forbids.
func TestAPanickingWriterCannotTakeDownThePost(t *testing.T) {
	t.Parallel()

	logger := logging.Open(logging.Options{
		Source: logging.SourceCLI,
		Writer: panickingWriter{},
	})

	post := logger.Post("id")

	// Both levels, and more than one record: the recover must not be a
	// one-shot that leaves the second call unprotected.
	post.Info(logging.EventMessageReceived)
	post.Error(logging.EventTelegramSendFailed, slog.String("error_type", "timeout"))
	post.Error(logging.EventRequestCompletedWithError)

	// Reaching here at all is the assertion. The panic is not swallowed
	// silently, though: it comes back as the one warning FR-076 allows.
	degraded := logger.Degraded()
	if degraded == nil {
		t.Fatal("a panicking writer produced no degradation; the panic was swallowed silently")
	}

	if warning := degraded.Warning(); !strings.Contains(warning, "panicked") {
		t.Errorf("the warning does not say the writer panicked: %q", warning)
	}

	if err := logger.Close(); err != nil {
		t.Errorf("Close after a panicking writer: %v", err)
	}
}
