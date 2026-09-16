package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

// The adapter's tests drive a whole post through a real logging.Logger and read
// the bytes back off disk.
//
// That is the decision DEC-D2 made explicit rather than a preference. Routing
// the orchestrator's events through an interface buys the package boundary and
// costs fidelity, and every trap on issue #41's list is a record-shape bug
// rather than an orchestrator bug: duration_ms in nanoseconds instead of
// milliseconds, a reserved key silently dropped, a record that is not one
// self-contained object per line, Options.Redact never armed. A counting spy
// sees none of them. So the fidelity is bought back here, where the records
// actually become bytes.

// noteSink stands in for the obsidian sink: it reports a destination and
// declares that it does.
type noteSink struct {
	note    string
	sendErr error
}

func (s *noteSink) Name() string   { return obsidian.SinkName }
func (s *noteSink) ReportsTarget() {}

func (s *noteSink) Send(ctx context.Context, _ post.Message) error {
	post.ReportTarget(ctx, s.note)

	return s.sendErr
}

// chatSink stands in for the telegram sink: no destination to report.
type chatSink struct{ sendErr error }

func (s *chatSink) Name() string { return telegram.SinkName }

func (s *chatSink) Send(context.Context, post.Message) error { return s.sendErr }

// namelessSink panics when asked its name, which resolves it to a sentinel the
// event vocabulary does not cover (issue #110).
type namelessSink struct{ sendErr error }

func (s *namelessSink) Name() string { panic("Name exploded") }

func (s *namelessSink) Send(context.Context, post.Message) error { return s.sendErr }

// strangerSink reports a name no vocabulary knows, which is what a third sink
// added without extending the adapter would look like.
type strangerSink struct{}

func (s *strangerSink) Name() string { return "carrier-pigeon" }

func (s *strangerSink) Send(context.Context, post.Message) error { return nil }

// loggedSettings returns settings whose diagnostics land in a temp file, plus
// that path.
func loggedSettings(t *testing.T) (config.Settings, string) {
	t.Helper()

	settings := validSettings(t)
	path := filepath.Join(t.TempDir(), "app.jsonl")
	settings.Logging.Path = path

	return settings, path
}

// postThrough runs one post with the adapter wired in and returns the records
// it wrote, in order.
func postThrough(t *testing.T, settings config.Settings, path string, sinks ...post.Sink) []map[string]any {
	t.Helper()

	logger := app.OpenLogger(settings, logging.SourceCLI)

	post.New(sinks, app.SinkTimeout(settings), app.NewRecording(logger)).
		Post(post.Message{Original: "記録される投稿"})

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return readRecords(t, path)
}

// readRecords decodes the log, asserting FR-064's one-independently-valid-
// object-per-line shape as it goes.
func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if len(raw) == 0 {
		t.Fatal("the log is empty; the recording was not wired to the orchestrator")
	}

	if !strings.HasSuffix(string(raw), "\n") {
		t.Error("the log does not end with a newline, so its last record is not a whole line")
	}

	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	records := make([]map[string]any, 0, len(lines))

	for i, line := range lines {
		var record map[string]any

		// Decoded one line at a time on purpose: SC-007 and FR-064 require each
		// line to stand alone, so a record split across two lines has to fail
		// here rather than be reassembled by a lenient reader.
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not one self-contained JSON object: %v\n%s", i+1, err, line)
		}

		records = append(records, record)
	}

	return records
}

// eventsOf returns the event names of the records, in order.
func eventsOf(records []map[string]any) []string {
	names := make([]string, 0, len(records))

	for _, record := range records {
		name, _ := record["event"].(string)
		names = append(names, name)
	}

	return names
}

// recordFor returns the single record with the given event name.
func recordFor(t *testing.T, records []map[string]any, event logging.Event) map[string]any {
	t.Helper()

	var found []map[string]any

	for _, record := range records {
		if record["event"] == string(event) {
			found = append(found, record)
		}
	}

	if len(found) != 1 {
		t.Fatalf("found %d records for %s, want exactly one; the log holds %v",
			len(found), event, eventsOf(records))
	}

	return found[0]
}

// number reads a JSON number field, which decodes as float64.
func number(t *testing.T, record map[string]any, key string) float64 {
	t.Helper()

	value, ok := record[key]
	if !ok {
		t.Fatalf("the %v record has no %s field; it holds %v", record["event"], key, keysOf(record))
	}

	asFloat, ok := value.(float64)
	if !ok {
		t.Fatalf("%s = %#v, want a number", key, value)
	}

	return asFloat
}

func text(t *testing.T, record map[string]any, key string) string {
	t.Helper()

	value, ok := record[key]
	if !ok {
		t.Fatalf("the %v record has no %s field; it holds %v", record["event"], key, keysOf(record))
	}

	asString, ok := value.(string)
	if !ok {
		t.Fatalf("%s = %#v, want a string", key, value)
	}

	return asString
}

func keysOf(record map[string]any) []string {
	keys := make([]string, 0, len(record))

	for key := range record {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// TestASuccessfulPostWritesTheContractsRecords covers the whole happy path in
// one pass: the sequence, the levels, the always-present preamble, and the
// fields FR-066 puts on each event.
func TestASuccessfulPostWritesTheContractsRecords(t *testing.T) {
	settings, path := loggedSettings(t)

	const note = "/vault/2026-09-10.md"

	records := postThrough(t, settings, path, &noteSink{note: note}, &chatSink{})

	want := []string{
		string(logging.EventMessageReceived),
		string(logging.EventObsidianAppendStarted),
		string(logging.EventObsidianAppendSucceeded),
		string(logging.EventTelegramSendStarted),
		string(logging.EventTelegramSendSucceeded),
		string(logging.EventRequestCompleted),
	}

	got := eventsOf(records)

	// The two sinks run concurrently, so their records interleave freely; what
	// is fixed is the multiset and each sink's own internal order.
	sortedGot := slices.Clone(got)
	sortedWant := slices.Clone(want)
	slices.Sort(sortedGot)
	slices.Sort(sortedWant)

	if !slices.Equal(sortedGot, sortedWant) {
		t.Fatalf("the log holds %v, want exactly %v", got, want)
	}

	if got[0] != string(logging.EventMessageReceived) {
		t.Errorf("the first record is %s, want the post's arrival", got[0])
	}

	if last := got[len(got)-1]; last != string(logging.EventRequestCompleted) {
		t.Errorf("the last record is %s, want the terminal record", last)
	}

	assertStartPrecedesFinish(t, got,
		logging.EventObsidianAppendStarted, logging.EventObsidianAppendSucceeded)
	assertStartPrecedesFinish(t, got,
		logging.EventTelegramSendStarted, logging.EventTelegramSendSucceeded)

	// The preamble internal/logging owns is on every record, and message_id is
	// the same on all of them: that is what makes the log correlatable at all.
	messageID := text(t, records[0], "message_id")
	if messageID == "" || messageID == "unknown" {
		t.Errorf("message_id = %q, want the post's ULID", messageID)
	}

	for _, record := range records {
		for _, key := range []string{"ts", "level", "event", "source", "message_id"} {
			if _, ok := record[key]; !ok {
				t.Errorf("the %v record has no %s field; it holds %v",
					record["event"], key, keysOf(record))
			}
		}

		if got := record["message_id"]; got != messageID {
			t.Errorf("the %v record carries message_id %v, want %q",
				record["event"], got, messageID)
		}

		if got := record["source"]; got != string(logging.SourceCLI) {
			t.Errorf("the %v record carries source %v, want %q",
				record["event"], got, logging.SourceCLI)
		}

		if got := record["level"]; got != "info" {
			t.Errorf("the %v record is at level %v, want info for a post that succeeded",
				record["event"], got)
		}
	}

	// message_received carries the two counts and deliberately no body.
	arrival := recordFor(t, records, logging.EventMessageReceived)

	if got, want := number(t, arrival, "message_len"), float64(7); got != want {
		t.Errorf("message_len = %v, want %v runes", got, want)
	}

	if got, want := number(t, arrival, "message_bytes"), float64(21); got != want {
		t.Errorf("message_bytes = %v, want %v bytes", got, want)
	}

	if _, present := arrival["message"]; present {
		t.Error("message_received carries the message body; FR-068's capture rule is T071's, " +
			"and recording the body on a successful post is what that rule forbids")
	}

	// The note events carry the path the sink reported; the chat events carry
	// none (issue #98).
	for _, event := range []logging.Event{
		logging.EventObsidianAppendStarted,
		logging.EventObsidianAppendSucceeded,
	} {
		record := recordFor(t, records, event)

		if got := text(t, record, "sink"); got != obsidian.SinkName {
			t.Errorf("%s carries sink %q, want %q", event, got, obsidian.SinkName)
		}

		if got := text(t, record, "path"); got != note {
			t.Errorf("%s carries path %q, want the note the sink reported (%q)", event, got, note)
		}
	}

	for _, event := range []logging.Event{
		logging.EventTelegramSendStarted,
		logging.EventTelegramSendSucceeded,
	} {
		record := recordFor(t, records, event)

		if got := text(t, record, "sink"); got != telegram.SinkName {
			t.Errorf("%s carries sink %q, want %q", event, got, telegram.SinkName)
		}

		if _, present := record["path"]; present {
			t.Errorf("%s carries a path field; chat events have no destination path (issue #98)", event)
		}
	}

	// duration_ms is on the completion events and not on the starts.
	for _, event := range []logging.Event{
		logging.EventObsidianAppendSucceeded,
		logging.EventTelegramSendSucceeded,
		logging.EventRequestCompleted,
	} {
		if _, present := recordFor(t, records, event)["duration_ms"]; !present {
			t.Errorf("%s has no duration_ms field, which FR-066 requires on a completion event", event)
		}
	}

	// A successful post carries no failure fields anywhere.
	for _, record := range records {
		for _, key := range []string{"error", "error_type", "http_status"} {
			if _, present := record[key]; present {
				t.Errorf("the %v record carries %s on a post where nothing failed",
					record["event"], key)
			}
		}
	}
}

// assertStartPrecedesFinish checks one sink's own ordering within the log.
func assertStartPrecedesFinish(t *testing.T, events []string, start, finish logging.Event) {
	t.Helper()

	startAt := slices.Index(events, string(start))
	finishAt := slices.Index(events, string(finish))

	if startAt == -1 || finishAt == -1 {
		t.Fatalf("expected both %s and %s in %v", start, finish, events)
	}

	if startAt > finishAt {
		t.Errorf("%s was recorded after %s; a sink's outcome cannot precede its start",
			start, finish)
	}
}

// TestAFailedPostWritesTheFailureFields covers the fields
// contracts/log-events.md puts on a failure, and FR-070's requirement that
// every failure is logged rather than only the first.
func TestAFailedPostWritesTheFailureFields(t *testing.T) {
	settings, path := loggedSettings(t)

	const note = "/vault/2026-09-10.md"

	noteFailure := &os.PathError{Op: "open", Path: note, Err: os.ErrPermission}
	chatFailure := &telegram.APIError{HTTPStatus: 429, Code: 429, Description: "Too Many Requests"}

	records := postThrough(t, settings, path,
		&noteSink{note: note, sendErr: noteFailure},
		&chatSink{sendErr: chatFailure},
	)

	// Both failures, not just the first (FR-070).
	noteFailed := recordFor(t, records, logging.EventObsidianAppendFailed)
	chatFailed := recordFor(t, records, logging.EventTelegramSendFailed)
	terminal := recordFor(t, records, logging.EventRequestCompletedWithError)

	if _, present := findRecord(records, logging.EventRequestCompleted); present {
		t.Error("the log carries request_completed for a post that failed")
	}

	for _, record := range []map[string]any{noteFailed, chatFailed, terminal} {
		if got := record["level"]; got != "error" {
			t.Errorf("the %v record is at level %v, want error", record["event"], got)
		}
	}

	// The note failure names its file, so the user can find what could not be
	// written (issue #98).
	if got := text(t, noteFailed, "path"); got != note {
		t.Errorf("obsidian_append_failed carries path %q, want %q", got, note)
	}

	if got, want := text(t, noteFailed, "error"), noteFailure.Error(); got != want {
		t.Errorf("obsidian_append_failed carries error %q, want the sink's own diagnostic %q", got, want)
	}

	if got := text(t, noteFailed, "error_type"); got != "permission_denied" {
		t.Errorf("obsidian_append_failed carries error_type %q, want %q", got, "permission_denied")
	}

	if _, present := noteFailed["http_status"]; present {
		t.Error("obsidian_append_failed carries http_status; only a chat reply has one")
	}

	// The chat failure carries the status off its own error type, which is the
	// only field that comes from a sink's error rather than from the result.
	if got, want := number(t, chatFailed, "http_status"), float64(429); got != want {
		t.Errorf("telegram_send_failed carries http_status %v, want %v", got, want)
	}

	if got := text(t, chatFailed, "error"); !strings.Contains(got, "429") {
		t.Errorf("telegram_send_failed carries error %q, want the API error's own text", got)
	}

	if got := text(t, chatFailed, "error_type"); got != "rate_limited" {
		t.Errorf("telegram_send_failed carries error_type %q, want rate_limited", got)
	}

	// The terminal record does not restate the sink failures: each is already
	// recorded once, and repeating them would make FR-070's guarantee
	// ambiguous about how many failures there were.
	for _, key := range []string{"error", "error_type"} {
		if _, present := terminal[key]; present {
			t.Errorf("request_completed_with_error carries aggregate %s: %v", key, terminal[key])
		}
	}
}

// findRecord reports whether a record with the event exists.
func findRecord(records []map[string]any, event logging.Event) (map[string]any, bool) {
	for _, record := range records {
		if record["event"] == string(event) {
			return record, true
		}
	}

	return nil, false
}

// TestDurationIsRecordedInMilliseconds is issue #41's second rider, and it is a
// numeric assertion rather than a presence check for the reason the rider
// exists.
//
// slog's JSON handler renders a time.Duration as its nanosecond count, so
// slog.Duration("duration_ms", d) emits nanoseconds under a key promising
// milliseconds — a factor of a million, with nothing in the record to make it
// look wrong. A test asserting only that the field is present passes for both.
func TestDurationIsRecordedInMilliseconds(t *testing.T) {
	settings, path := loggedSettings(t)

	// Long enough to be unambiguous in milliseconds and to be a nine-digit
	// number in nanoseconds.
	const held = 120 * time.Millisecond

	slow := &noteSink{note: "/vault/slow.md"}
	records := postThrough(t, settings, path, &slowSink{inner: slow, hold: held})

	succeeded := recordFor(t, records, logging.EventObsidianAppendSucceeded)
	sinkMS := number(t, succeeded, "duration_ms")

	if sinkMS < 100 || sinkMS > 10_000 {
		t.Errorf("duration_ms = %v for a sink held %s; a value near %d is nanoseconds "+
			"under a millisecond key", sinkMS, held, held.Nanoseconds())
	}

	postMS := number(t, recordFor(t, records, logging.EventRequestCompleted), "duration_ms")

	if postMS < sinkMS {
		t.Errorf("the post's duration_ms (%v) is less than its only sink's (%v)", postMS, sinkMS)
	}

	if postMS > 10_000 {
		t.Errorf("the post's duration_ms = %v for a post that took about %s", postMS, held)
	}
}

// slowSink holds inside Send after delegating, so a measurable duration reaches
// the record.
type slowSink struct {
	inner post.Sink
	hold  time.Duration
}

func (s *slowSink) Name() string   { return s.inner.Name() }
func (s *slowSink) ReportsTarget() {}

func (s *slowSink) Send(ctx context.Context, message post.Message) error {
	err := s.inner.Send(ctx, message)
	time.Sleep(s.hold)

	return err
}

// TestTheBotTokenNeverReachesARecordThroughSinkResultErr is issue #41's first
// rider.
//
// contracts/log-events.md makes error required on a failure, and its only source
// is SinkResult.Err — which for a Telegram transport failure is a *url.Error
// whose exported URL field is the full API URL, bot token included. The scrub in
// internal/logging is a chokepoint that is inert unless OpenLogger arms it, and
// this asserts the whole path rather than the wiring alone.
func TestTheBotTokenNeverReachesARecordThroughSinkResultErr(t *testing.T) {
	const token = "7654321:AA-the-sentinel-token"

	settings, path := loggedSettings(t)
	settings.Sink.Telegram.BotToken = config.NewSecret(token)

	leaky := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot" + token + "/sendMessage",
		Err: errors.New("dial tcp: connection refused"),
	}

	// The fixture has to be capable of leaking before "it did not leak" means
	// anything. Batch 6b lost exactly this: an unrelated fix made a redaction
	// fixture clean, and its mutant silently stopped being killed.
	if !strings.Contains(leaky.Error(), token) {
		t.Fatalf("the fixture does not carry the token in its own rendering (%q), "+
			"so this test would pass with the scrub removed", leaky.Error())
	}

	records := postThrough(t, settings, path, &chatSink{sendErr: leaky})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if strings.Contains(string(raw), token) {
		t.Fatalf("the bot token reached the log:\n%s", raw)
	}

	// And the record still says something useful, so the scrub is not being
	// satisfied by an absent field.
	failed := recordFor(t, records, logging.EventTelegramSendFailed)

	if got := text(t, failed, "error"); !strings.Contains(got, "connection refused") {
		t.Errorf("telegram_send_failed carries error %q, want the transport failure's own text", got)
	}
}

// TestTheAdapterProducesEveryOrchestratorReachableEvent is decision DEC-D2's
// mandatory obligation.
//
// internal/logging's registration test scans that package's own source, so it
// cannot see this adapter: an event name defined there and never produced here
// would be a saved log query that silently returns nothing, forever, and nothing
// in either package would fail. This closes that from the other side.
func TestTheAdapterProducesEveryOrchestratorReachableEvent(t *testing.T) {
	t.Parallel()

	notYetProduced := map[logging.Event]string{}

	produced := app.ProducibleEvents()

	for _, event := range produced {
		if owner, deferred := notYetProduced[event]; deferred {
			t.Errorf("the adapter produces %s, which is listed as deferred to %s", event, owner)
		}

		if !slices.Contains(logging.AllEvents(), event) {
			t.Errorf("the adapter produces %s, which is not a registered event name", event)
		}
	}

	for _, event := range logging.AllEvents() {
		if _, deferred := notYetProduced[event]; deferred {
			continue
		}

		if !slices.Contains(produced, event) {
			t.Errorf("no code path produces %s, so a query for it would silently return nothing; "+
				"either map it in the adapter or record which task owns it", event)
		}
	}

	// Every produced name distinct, so a copy-paste in the lifecycle table
	// cannot leave one stage emitting another's name.
	seen := make(map[logging.Event]bool, len(produced))

	for _, event := range produced {
		if seen[event] {
			t.Errorf("%s is produced by two different lifecycle stages", event)
		}

		seen[event] = true
	}
}

// TestEachSinkHasItsOwnLifecycleNames guards the lookup itself.
func TestEachSinkHasItsOwnLifecycleNames(t *testing.T) {
	t.Parallel()

	obsStarted, obsSucceeded, obsFailed, ok := app.LifecycleFor(obsidian.SinkName)
	if !ok {
		t.Fatalf("no events are registered for %q", obsidian.SinkName)
	}

	if obsStarted != logging.EventObsidianAppendStarted ||
		obsSucceeded != logging.EventObsidianAppendSucceeded ||
		obsFailed != logging.EventObsidianAppendFailed {
		t.Errorf("the obsidian lifecycle is %s/%s/%s, want the three obsidian_append_* names",
			obsStarted, obsSucceeded, obsFailed)
	}

	chatStarted, chatSucceeded, chatFailed, ok := app.LifecycleFor(telegram.SinkName)
	if !ok {
		t.Fatalf("no events are registered for %q", telegram.SinkName)
	}

	if chatStarted != logging.EventTelegramSendStarted ||
		chatSucceeded != logging.EventTelegramSendSucceeded ||
		chatFailed != logging.EventTelegramSendFailed {
		t.Errorf("the telegram lifecycle is %s/%s/%s, want the three telegram_send_* names",
			chatStarted, chatSucceeded, chatFailed)
	}

	if _, _, _, ok := app.LifecycleFor("carrier-pigeon"); ok {
		t.Error("an unregistered sink name was mapped to events")
	}

	if _, _, _, ok := app.LifecycleFor("unknown"); ok {
		t.Error("the unattributable-sink sentinel was mapped to events; " +
			"a panicking Name would then produce records naming a real destination")
	}
}

// TestAnUnattributableSinkIsReportedRatherThanDropped covers what the adapter
// does with a sink name the vocabulary does not cover.
//
// A panicking Name resolves to a sentinel, and the event names are keyed by sink
// name, so there is no lifecycle record such a sink can produce. Emitting
// nothing and saying nothing would be a post whose records claim one
// destination while two ran — the silent-drop shape this repository keeps
// finding. The terminal record is the one record that is always written, so the
// diagnostic goes there (issue #110).
func TestAnUnattributableSinkIsReportedRatherThanDropped(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &namelessSink{}, &chatSink{})

	// The post succeeded — both sends returned nil — so the terminal record is
	// the success one, and its level says so. A sink's Name misbehaving must
	// not change what the post reports (FR-019, issue #110).
	terminal := recordFor(t, records, logging.EventRequestCompleted)

	if got := terminal["level"]; got != "info" {
		t.Errorf("the terminal record is at level %v, want info: both sends succeeded", got)
	}

	diagnostic := text(t, terminal, "error")

	if !strings.Contains(diagnostic, "Name exploded") {
		t.Errorf("the terminal record's diagnostic is %q, want the recovered panic value", diagnostic)
	}

	if !strings.Contains(diagnostic, "no diagnostic events are registered") {
		t.Errorf("the terminal record's diagnostic is %q, want it to say the sink's records "+
			"could not be written", diagnostic)
	}

	// Each fact once. The start and the finish both hit the unmapped path, and
	// the panic diagnostic is collected at the start only, so a note added
	// from both would repeat every sentence.
	if got := strings.Count(diagnostic, "Name exploded"); got != 1 {
		t.Errorf("the diagnostic reports the panic %d times (%q), want once", got, diagnostic)
	}

	if got := strings.Count(diagnostic, "no diagnostic events are registered"); got != 1 {
		t.Errorf("the diagnostic reports the missing vocabulary %d times (%q), want once",
			got, diagnostic)
	}

	// The sink that *is* attributable still gets its full lifecycle, so one
	// broken sink does not cost its sibling its records.
	for _, event := range []logging.Event{
		logging.EventTelegramSendStarted,
		logging.EventTelegramSendSucceeded,
	} {
		recordFor(t, records, event)
	}

	// And no record was written under a name the sentinel was mapped onto.
	for _, record := range records {
		if got, present := record["sink"]; present && got == "unknown" {
			t.Errorf("the %v record names the sentinel sink, which no vocabulary covers",
				record["event"])
		}
	}
}

// TestAnUnregisteredSinkNameIsReportedOnce covers the same path for a sink whose
// Name works and is simply unknown here — a third sink added without extending
// the adapter.
func TestAnUnregisteredSinkNameIsReportedOnce(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &strangerSink{})

	terminal := recordFor(t, records, logging.EventRequestCompleted)
	diagnostic := text(t, terminal, "error")

	if !strings.Contains(diagnostic, "carrier-pigeon") {
		t.Errorf("the diagnostic is %q, want it to name the unregistered sink", diagnostic)
	}

	// Once, not twice: the start and the finish both hit the unmapped path, and
	// a note from each would put the same sentence into the record twice.
	if got := strings.Count(diagnostic, "carrier-pigeon"); got != 1 {
		t.Errorf("the diagnostic names the sink %d times (%q), want once", got, diagnostic)
	}

	// The post itself is unaffected.
	if got := terminal["level"]; got != "info" {
		t.Errorf("the terminal record is at level %v, want info: the send succeeded", got)
	}
}

// TestNoRecordKeyCollidesWithAReservedOne covers a failure mode that does not
// look like one.
//
// internal/logging owns seven record keys plus slog's own time and msg, and its
// emission path *drops* a caller attribute that collides with any of them. A
// collision would therefore not produce a duplicate key or an error — it would
// make a field FR-066 requires quietly absent.
func TestNoRecordKeyCollidesWithAReservedOne(t *testing.T) {
	settings, path := loggedSettings(t)

	failure := &telegram.APIError{HTTPStatus: 401, Code: 401, Description: "Unauthorized"}

	records := postThrough(t, settings, path,
		&noteSink{note: "/vault/2026-09-10.md", sendErr: errors.New("read-only file system")},
		&chatSink{sendErr: failure},
	)

	// Collect every key this adapter writes, from the records that carry the
	// widest set: a note failure and a chat failure between them cover all
	// eight.
	written := map[string]bool{}

	for _, record := range records {
		for key := range record {
			written[key] = true
		}
	}

	for _, key := range []string{
		"sink", "path", "duration_ms", "error", "error_type", "http_status",
		"message_len", "message_bytes",
	} {
		if !written[key] {
			t.Errorf("no record carries %s, so this test is not checking it against the "+
				"reserved set", key)
		}
	}

	// The reserved keys are all present too, which is what proves none of the
	// above displaced one.
	for _, key := range []string{"ts", "level", "event", "source", "message_id"} {
		if !written[key] {
			t.Errorf("no record carries the reserved key %s", key)
		}
	}
}

// TestAHostileErrorCannotTakeDownThePost covers errorMessage's two shapes.
//
// Err.Error() is the deliberate access FR-017 reserves for the log, on a value
// this program did not write. A typed nil stored in the interface is non-nil as
// an interface, so a sink returning one produces a failure result whose Error()
// dereferences nothing — internal/post's own comment on SinkResult records that
// this panics at exactly this call.
func TestAHostileErrorCannotTakeDownThePost(t *testing.T) {
	t.Parallel()

	t.Run("a typed nil", func(t *testing.T) {
		t.Parallel()

		var typedNil *url.Error

		// Proof the fixture is dangerous: without the guard this call panics.
		func() {
			defer func() {
				if recover() == nil {
					t.Error("the fixture's Error() did not panic, so the guard is untested here")
				}
			}()

			_ = error(typedNil).Error()
		}()

		got := app.ErrorMessage(typedNil)

		if !strings.Contains(got, "whose own rendering panicked") {
			t.Errorf("ErrorMessage = %q, want it to report the failed render", got)
		}

		if !strings.Contains(got, "url.Error") {
			t.Errorf("ErrorMessage = %q, want it to name the type", got)
		}
	})

	t.Run("an error whose render panics", func(t *testing.T) {
		t.Parallel()

		got := app.ErrorMessage(hostileError{})

		if !strings.Contains(got, "whose own rendering panicked") {
			t.Errorf("ErrorMessage = %q, want it to report the failed render", got)
		}
	})

	t.Run("no error at all", func(t *testing.T) {
		t.Parallel()

		got := app.ErrorMessage(nil)

		if got == "" {
			t.Error("ErrorMessage for a nil error is empty; a required field would be absent")
		}
	})

	t.Run("through a whole post", func(t *testing.T) {
		settings, path := loggedSettings(t)

		records := postThrough(t, settings, path, &chatSink{sendErr: hostileError{}})

		failed := recordFor(t, records, logging.EventTelegramSendFailed)

		if got := text(t, failed, "error"); !strings.Contains(got, "whose own rendering panicked") {
			t.Errorf("telegram_send_failed carries error %q, want the guarded description", got)
		}

		// The record survived rather than being lost to a degradation, which
		// is the whole reason the guard is here rather than in slog.
		recordFor(t, records, logging.EventRequestCompletedWithError)
	})
}

// hostileError panics when rendered, which is what a faulty sink error type
// does.
type hostileError struct{}

func (hostileError) Error() string { panic("Error() exploded") }

// TestHTTPStatusOnlyComesFromAChatReply pins the one field read off a sink's own
// error type.
func TestHTTPStatusOnlyComesFromAChatReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantOK     bool
	}{
		{
			name:       "an API error",
			err:        &telegram.APIError{HTTPStatus: 400},
			wantStatus: 400,
			wantOK:     true,
		},
		{
			name:       "an API error behind a wrapper",
			err:        fmt.Errorf("sending failed: %w", &telegram.APIError{HTTPStatus: 429}),
			wantStatus: 429,
			wantOK:     true,
		},
		{
			name: "a transport failure, which never reached a reply",
			err:  &url.Error{Op: "Post", URL: "https://example.invalid", Err: errors.New("refused")},
		},
		{name: "no error"},
		{
			name: "a typed nil API error, which errors.As matches and leaves nil",
			err:  (*telegram.APIError)(nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			status, ok := app.HTTPStatus(test.err)

			if ok != test.wantOK {
				t.Errorf("HTTPStatus reported ok=%t, want %t", ok, test.wantOK)
			}

			if status != test.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", status, test.wantStatus)
			}
		})
	}
}

// TestARecordingWithNoLoggerRecordsNothing covers the nil arm, which is what a
// front door has when diagnostics could not be opened (FR-076).
func TestARecordingWithNoLoggerRecordsNothing(t *testing.T) {
	t.Parallel()

	if got := app.NewRecording(nil).Post("01ABC"); got != nil {
		t.Errorf("Post returned %#v for a Recording with no logger, want a nil Recorder", got)
	}

	// And a post through it still succeeds, which is the property that matters.
	outcome := post.New([]post.Sink{&chatSink{}}, time.Second, app.NewRecording(nil)).
		Post(post.Message{Original: "診断は無い"})

	if !outcome.Succeeded() {
		t.Errorf("the post failed with no diagnostics: %v", outcome.Results)
	}
}

// TestTheRecordingIsWiredIntoTheServiceItBuilds covers NewService's new
// parameter.
//
// A service built without one posts identically and records nothing, so the
// wiring is invisible from the outcome and only observable from the log.
func TestTheRecordingIsWiredIntoTheServiceItBuilds(t *testing.T) {
	settings, path := loggedSettings(t)
	settings.Sink.Telegram.Enabled = false

	logger := app.OpenLogger(settings, logging.SourceCLI)

	app.NewService(settings, app.NewRecording(logger)).
		Post(post.Message{Original: "設定から組み立てる"})

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := readRecords(t, path)

	for _, event := range []logging.Event{
		logging.EventMessageReceived,
		logging.EventObsidianAppendStarted,
		logging.EventObsidianAppendSucceeded,
		logging.EventRequestCompleted,
	} {
		recordFor(t, records, event)
	}

	// The real obsidian sink reports a real note, so the path is the file it
	// wrote rather than a fixture's string.
	started := recordFor(t, records, logging.EventObsidianAppendStarted)

	if got, want := filepath.Dir(text(t, started, "path")), settings.Sink.Obsidian.DailyNoteDir; got != want {
		t.Errorf("obsidian_append_started carries a path under %q, want the configured vault %q",
			got, want)
	}
}

type formattingSink struct{ err error }

func (s formattingSink) Name() string { return "telegram" }
func (s formattingSink) Send(ctx context.Context, _ post.Message) error {
	post.ReportFormatting(ctx, post.FormattingAttempt{Err: &telegram.APIError{HTTPStatus: 400, Code: 400, Description: "can't parse entities"}, Duration: 12 * time.Millisecond})
	post.ReportFormatting(ctx, post.FormattingAttempt{Plain: true, Err: s.err, Duration: 23 * time.Millisecond})
	return s.err
}
func TestFormattingEventsAreCorrelatedAndOrdered(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			settings, path := loggedSettings(t)
			var err error
			if fail {
				err = &telegram.APIError{HTTPStatus: 403, Code: 403, Description: "Forbidden " + settings.Sink.Telegram.BotToken.Reveal()}
			}
			records := postThrough(t, settings, path, formattingSink{err: err})
			final, plain, terminal := "telegram_send_succeeded", "telegram_plaintext_succeeded", "request_completed"
			if fail {
				final, plain, terminal = "telegram_send_failed", "telegram_plaintext_failed", "request_completed_with_error"
			}
			want := []string{"message_received", "telegram_send_started", "telegram_markdown_failed", plain, final, terminal}
			if !slices.Equal(eventsOf(records), want) {
				t.Fatalf("events=%v", eventsOf(records))
			}
			for _, r := range records {
				if r["message_id"] != records[0]["message_id"] || r["source"] != "cli" {
					t.Fatalf("correlation=%v", r)
				}
			}
			if records[2]["duration_ms"] != float64(12) || records[3]["duration_ms"] != float64(23) || records[2]["http_status"] != float64(400) || records[2]["level"] != "error" {
				t.Fatalf("attempt fields=%v", records)
			}
			if fail {
				if records[3]["error_type"] != "permission_denied" || records[3]["http_status"] != float64(403) {
					t.Fatalf("failure fields=%v", records[3])
				}
			} else if records[3]["level"] != "info" {
				t.Fatal("success level")
			}
			raw, _ := os.ReadFile(path)
			if strings.Contains(string(raw), settings.Sink.Telegram.BotToken.Reveal()) {
				t.Fatal("token leaked")
			}
		})
	}
}
