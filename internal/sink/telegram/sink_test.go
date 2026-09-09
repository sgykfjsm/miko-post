package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// sentinelToken is the credential every test in this file configures.
//
// It is deliberately distinctive so the leak sweep can look for it as a
// substring and so a failure names it unambiguously. research R-010 makes a
// sentinel-token leak test a required gate rather than a review habit; this is
// this package's half of it.
const sentinelToken = "7654321:AA-miko-post-SENTINEL-token-must-never-be-logged"

// chatID is the destination every test configures. Negative and long, like a
// real supergroup id, so a builder that parsed it as a number would have to
// work rather than accidentally succeed on "1".
const chatID = "-1001234567890"

// okBody is what Telegram answers when it accepted the message.
const okBody = `{"ok":true,"result":{"message_id":8,"date":1757000000}}`

// baseSettings are enabled settings with no thread configured.
//
// HTTPTimeoutSeconds is the documented default (FR-040); the tests that care
// about the bound override it explicitly, so a test that does not mention a
// timeout is not silently relying on one.
func baseSettings() config.TelegramSettings {
	return config.TelegramSettings{
		Enabled:            true,
		BotToken:           config.NewSecret(sentinelToken),
		ChatID:             chatID,
		ParseMode:          config.ParseModeMarkdownV2,
		HTTPTimeoutSeconds: 30,
	}
}

// withThread returns settings naming a forum topic.
func withThread(settings config.TelegramSettings, thread int64) config.TelegramSettings {
	settings.ThreadID = &thread

	return settings
}

// mustMessage builds a Message that has passed validation, as the orchestrator
// guarantees before a sink is reached.
func mustMessage(t *testing.T, text string) post.Message {
	t.Helper()

	message := post.Message{Original: text}
	if err := message.Validate(); err != nil {
		t.Fatalf("post.Message{%q}.Validate: %v", text, err)
	}

	return message
}

// captured is everything the server saw of one request.
//
// The body is kept raw as well as parsed. The parsed form answers "what did the
// sink send"; the raw string answers "and nothing else" — a misspelled wire key
// is invisible in the parsed map of the key it was supposed to be.
type captured struct {
	method      string
	path        string
	contentType string
	rawBody     string
	form        url.Values
	formErr     error
}

// recorder collects every request a server received.
//
// A slice rather than a single value, because "exactly one request was sent" is
// an assertion this batch has to make: FR-019 and FR-041 forbid any automatic
// re-send, and Batch 9's rescue (T062) is the change that will make the count
// two for one specific failure. A test that only inspected the last request
// would pass either way.
type recorder struct {
	mu   sync.Mutex
	seen []captured
}

func (r *recorder) record(request *http.Request) {
	raw, readErr := io.ReadAll(request.Body)

	entry := captured{
		method:      request.Method,
		path:        request.URL.Path,
		contentType: request.Header.Get("Content-Type"),
		rawBody:     string(raw),
	}

	if readErr != nil {
		entry.formErr = readErr
	} else {
		// url.ParseQuery on the body rather than request.ParseForm, which
		// merges the URL query string in. The claim under test is about the
		// body the sink wrote.
		entry.form, entry.formErr = url.ParseQuery(entry.rawBody)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.seen = append(r.seen, entry)
}

func (r *recorder) requests() []captured {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.seen)
}

// only returns the single request the server received, failing otherwise.
func (r *recorder) only(t *testing.T) captured {
	t.Helper()

	seen := r.requests()
	if len(seen) != 1 {
		t.Fatalf("requests received = %d, want exactly 1", len(seen))
	}

	return seen[0]
}

// newRecordingServer starts a server that records every request and then
// delegates to handler.
func newRecordingServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *recorder) {
	t.Helper()

	rec := &recorder{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return server, rec
}

// reply answers with a fixed status and body.
func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// stall waits for the client to give up, and gives up itself after limit so a
// mutation that removes the bound produces a failing test rather than a hung
// one.
//
// The limit is what makes the timeout tests mutation-sensitive in the right
// direction. With the bound in place the handler returns as soon as the client
// cancels; with the bound removed it answers 200 after cap and the test fails
// on "want an error" instead of hanging until the package timeout.
func stall(limit time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(limit):
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okBody)
	}
}

// formKeys is the sorted key set of a parsed body.
func formKeys(form url.Values) []string {
	keys := make([]string, 0, len(form))
	for key := range form {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// unreachableAddress is a host:port nothing is listening on.
//
// A listener is opened to get a port the operating system agrees is free and
// then closed, which is the only portable way to get a refusal. An
// httptest.Server cannot produce one: it can only answer.
func unreachableAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open a listener: %v", err)
	}

	address := listener.Addr().String()

	if err := listener.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}

	return address
}

// TestSendPostsTheConfiguredFieldsToTelegram is T028's central assertion
// (FR-031, FR-032, FR-033).
//
// The key-set assertion is the load-bearing one, and it is exact rather than a
// series of individual lookups. "message_thread_id is absent when no thread is
// configured" is the classic unfailable assertion: written as a single lookup
// it passes when the key is misspelled, when the form was never populated, and
// when no request was sent at all. Comparing the whole sorted key set against a
// literal kills all three, and the positive case pins the exact wire spelling
// so the two cases cannot agree on a wrong one.
func TestSendPostsTheConfiguredFieldsToTelegram(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		settings   config.TelegramSettings
		wantKeys   []string
		wantThread string
	}{
		{
			name:     "no thread configured posts to the chat directly",
			settings: baseSettings(),
			wantKeys: []string{telegram.FieldChatID, telegram.FieldParse, telegram.FieldText},
		},
		{
			name:     "a configured thread names the forum topic",
			settings: withThread(baseSettings(), 4242),
			wantKeys: []string{
				telegram.FieldChatID,
				telegram.FieldThreadID,
				telegram.FieldParse,
				telegram.FieldText,
			},
			wantThread: "4242",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, rec := newRecordingServer(t, reply(http.StatusOK, okBody))
			sink := telegram.NewWithBaseURL(test.settings, server.URL)

			text := "今日も美琴が可愛い♡"
			if err := sink.Send(context.Background(), mustMessage(t, text)); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := rec.only(t)

			if got.formErr != nil {
				t.Fatalf("parse the request body %q: %v", got.rawBody, got.formErr)
			}

			if got.method != telegram.Method {
				t.Errorf("method = %q, want %q", got.method, telegram.Method)
			}

			if want := telegram.SendMessagePath(test.settings); got.path != want {
				t.Errorf("path = %q, want %q", got.path, want)
			}

			if got.contentType != telegram.ContentTypeHeader {
				t.Errorf("Content-Type = %q, want %q", got.contentType, telegram.ContentTypeHeader)
			}

			wantKeys := slices.Clone(test.wantKeys)
			slices.Sort(wantKeys)

			if keys := formKeys(got.form); !slices.Equal(keys, wantKeys) {
				t.Errorf("wire keys = %q, want exactly %q (raw body %q)", keys, wantKeys, got.rawBody)
			}

			if value := got.form.Get(telegram.FieldChatID); value != chatID {
				t.Errorf("%s = %q, want %q", telegram.FieldChatID, value, chatID)
			}

			// FR-034: the first attempt is always MarkdownV2 as fixed
			// application behaviour, never whatever parse_mode says.
			if value := got.form.Get(telegram.FieldParse); value != config.ParseModeMarkdownV2 {
				t.Errorf("%s = %q, want %q", telegram.FieldParse, value, config.ParseModeMarkdownV2)
			}

			if value := got.form.Get(telegram.FieldText); value != text {
				t.Errorf("%s = %q, want %q", telegram.FieldText, value, text)
			}

			if test.wantThread == "" {
				// Paired with the key-set assertion above, and kept because it
				// names the requirement: absence is the wire signal for "post
				// to the chat directly" (FR-032, A-006).
				if _, present := got.form[telegram.FieldThreadID]; present {
					t.Errorf("%s present with no thread configured: %q",
						telegram.FieldThreadID, got.rawBody)
				}

				// A misspelled key would satisfy both assertions above while
				// still putting a thread field on the wire.
				if strings.Contains(got.rawBody, "thread") {
					t.Errorf("the body mentions a thread with none configured: %q", got.rawBody)
				}

				return
			}

			if value := got.form.Get(telegram.FieldThreadID); value != test.wantThread {
				t.Errorf("%s = %q, want %q", telegram.FieldThreadID, value, test.wantThread)
			}
		})
	}
}

// TestSendAlwaysUsesMarkdownV2WhateverParseModeSays is FR-034.
//
// The setting is accepted and validated but must not alter delivery in v0.1. A
// builder reading settings.ParseMode would pass every other test in this file,
// because every other test configures MarkdownV2.
func TestSendAlwaysUsesMarkdownV2WhateverParseModeSays(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{config.ParseModeHTML, config.ParseModeMarkdown, ""} {
		t.Run("parse_mode="+strconv.Quote(mode), func(t *testing.T) {
			t.Parallel()

			settings := baseSettings()
			settings.ParseMode = mode

			server, rec := newRecordingServer(t, reply(http.StatusOK, okBody))
			sink := telegram.NewWithBaseURL(settings, server.URL)

			if err := sink.Send(context.Background(), mustMessage(t, "miko")); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := rec.only(t)

			if value := got.form.Get(telegram.FieldParse); value != config.ParseModeMarkdownV2 {
				t.Errorf("%s = %q, want %q", telegram.FieldParse, value, config.ParseModeMarkdownV2)
			}
		})
	}
}

// TestSendDeliversTheMessageVerbatim is FR-033 with FR-011 and FR-012.
//
// Every expectation is the literal the test sent, never a value re-derived by
// anything the sink also uses: comparing against a second call to the sink's own
// encoder would agree with any transformation it applied.
//
// The cases are chosen for what they would catch. MarkdownV2 reserves '.', '-',
// '!' and the bracket family, so a well-meant escaper shows up as backslashes.
// Form encoding reserves '+', '&', '=', '%' and ';', so a hand-rolled body
// builder shows up as a split or swallowed field. Whitespace at both ends is
// FR-011's untrimmed guarantee, which the Message type goes out of its way to
// make structurally true and a sink can still undo.
func TestSendDeliversTheMessageVerbatim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{
			name: "every character MarkdownV2 reserves",
			text: `_*[]()~` + "`" + `>#+-=|{}.! and a trailing dot.`,
		},
		{name: "form encoding metacharacters", text: "a+b&c=d%e;f?g"},
		{name: "leading and trailing whitespace", text: "  \t miko had a thought \n "},
		{name: "japanese and an emoji", text: "今日も美琴が可愛い♡ 🎀"},
		{name: "an interior line break", text: "first line\nsecond line"},
		{name: "a carriage return and line feed", text: "first\r\nsecond"},
		{name: "an ideographic space", text: "美琴　可愛い"},
		// Issue #113: bytes a command-line argument may carry on Unix and the
		// obsidian sink writes verbatim.
		{name: "a byte that is not valid utf-8", text: "a\xffb"},
		{name: "a truncated utf-8 sequence", text: "a\xe3\x81b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, rec := newRecordingServer(t, reply(http.StatusOK, okBody))
			sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

			if err := sink.Send(context.Background(), mustMessage(t, test.text)); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := rec.only(t)

			if got.formErr != nil {
				t.Fatalf("parse the request body %q: %v", got.rawBody, got.formErr)
			}

			if value := got.form.Get(telegram.FieldText); value != test.text {
				t.Errorf("delivered text\n got %q\nwant %q\n(raw body %q)",
					value, test.text, got.rawBody)
			}
		})
	}
}

// TestSendDoesNotSubstituteReplacementCharactersForInvalidUTF8 makes issue
// #113's rejected option executable.
//
// A JSON body would have been the obvious choice and is the reason this test
// exists: encoding/json replaces every byte sequence that is not valid UTF-8
// with U+FFFD and returns a nil error while doing it. Telegram would then hold
// U+FFFD where the obsidian note holds the raw byte — #113's option 3, the two
// sinks storing different text for one post, which was rejected in advance.
//
// The test computes what JSON would have delivered rather than asserting
// against a hard-coded rune, so it keeps meaning what it says if the encoder's
// behaviour ever changes.
func TestSendDoesNotSubstituteReplacementCharactersForInvalidUTF8(t *testing.T) {
	t.Parallel()

	const raw = "a\xffb"

	encoded, err := json.Marshal(map[string]string{"text": raw})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var viaJSON map[string]string
	if err := json.Unmarshal(encoded, &viaJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if viaJSON["text"] == raw {
		t.Fatalf("the premise no longer holds: encoding/json round-tripped %q unchanged", raw)
	}

	server, rec := newRecordingServer(t, reply(http.StatusOK, okBody))
	sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

	if err := sink.Send(context.Background(), mustMessage(t, raw)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	delivered := rec.only(t).form.Get(telegram.FieldText)

	if delivered != raw {
		t.Errorf("delivered text = %q, want the original bytes %q", delivered, raw)
	}

	if delivered == viaJSON["text"] {
		t.Errorf("delivered text = %q, which is what a JSON body would have sent (issue #113)", delivered)
	}

	// strings.Contains with the encoded rune, not strings.ContainsRune: the
	// latter documents U+FFFD as matching any invalid byte sequence, so it
	// answers true for the raw 0xFF this test is asserting arrived intact.
	if strings.Contains(delivered, string(utf8.RuneError)) {
		t.Errorf("delivered text = %q contains an encoded U+FFFD", delivered)
	}
}

// TestSendSucceedsOnAnAcceptedMessage is the one path that must return nil.
func TestSendSucceedsOnAnAcceptedMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "the documented reply", body: okBody},
		{name: "ok alone", body: `{"ok":true}`},
		// Unknown fields must not fail: the Bot API adds them, and a decoder
		// that refused would turn a delivered message into a reported failure.
		{name: "unknown fields", body: `{"ok":true,"result":{},"something_new":[1,2]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, rec := newRecordingServer(t, reply(http.StatusOK, test.body))
			sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

			if err := sink.Send(context.Background(), mustMessage(t, "miko")); err != nil {
				t.Fatalf("Send: %v", err)
			}

			rec.only(t)
		})
	}
}

// TestSendFailsClosedOnEveryUntrustworthyResponse is T034's requirement
// (FR-035, FR-066).
//
// Every case here is a reply that does not establish that the message arrived,
// and every one of them must be a failure. The shape being ruled out is the one
// Batch 6a hit three times in review: reported delivered, nothing stored.
//
// Each case also asserts that exactly one request was sent, because a decoder
// that failed and a client that retried would both produce "an error" and only
// one of them is allowed (FR-041).
func TestSendFailsClosedOnEveryUntrustworthyResponse(t *testing.T) {
	t.Parallel()

	// Longer than the read cap, so the JSON arrives truncated and cannot be
	// decoded.
	//
	// The size is a literal, deliberately, and the guard below is why. Sizing
	// it from telegram.MaxResponseBytes read better and was worthless: the body
	// then grew with the constant, so raising the cap kept the body "oversized"
	// and the truncation was never exercised. Mutation testing caught exactly
	// that — a cap raised to 1 GiB left this case passing. A literal plus a
	// guard fails loudly if the cap ever overtakes it instead.
	const oversizedBytes = 2 << 20

	if oversizedBytes <= telegram.MaxResponseBytes {
		t.Fatalf("the oversized body is %d bytes, no longer past the %d-byte read cap",
			oversizedBytes, telegram.MaxResponseBytes)
	}

	oversized := `{"ok":true,"description":"` + strings.Repeat("a", oversizedBytes) + `"}`

	tests := []struct {
		name            string
		status          int
		body            string
		wantCode        int
		wantDescription string
	}{
		{
			name:            "200 with ok false",
			status:          http.StatusOK,
			body:            `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`,
			wantCode:        400,
			wantDescription: "Bad Request: chat not found",
		},
		{
			name:            "400 chat not found",
			status:          http.StatusBadRequest,
			body:            `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`,
			wantCode:        400,
			wantDescription: "Bad Request: chat not found",
		},
		{
			name:            "401 an invalid token",
			status:          http.StatusUnauthorized,
			body:            `{"ok":false,"error_code":401,"description":"Unauthorized"}`,
			wantCode:        401,
			wantDescription: "Unauthorized",
		},
		{
			name:            "429 rate limited",
			status:          http.StatusTooManyRequests,
			body:            `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 30"}`,
			wantCode:        429,
			wantDescription: "Too Many Requests: retry after 30",
		},
		{
			name:   "200 with a body that is not json",
			status: http.StatusOK,
			body:   "delivered, honest",
		},
		{
			name:   "200 with an empty body",
			status: http.StatusOK,
			body:   "",
		},
		{
			name:   "500 serving an html error page",
			status: http.StatusInternalServerError,
			body:   "<html><head><title>502 Bad Gateway</title></head><body>nope</body></html>",
		},
		{
			// The partially decoded payload is discarded on purpose, so no
			// description survives to be matched on.
			name:   "error_code arriving as a string",
			status: http.StatusBadRequest,
			body:   `{"ok":false,"error_code":"400","description":"Bad Request: chat not found"}`,
		},
		{
			name:   "a json null body",
			status: http.StatusOK,
			body:   "null",
		},
		{
			name:   "500 claiming ok",
			status: http.StatusInternalServerError,
			body:   `{"ok":true,"result":{}}`,
		},
		{
			name:   "a body larger than the read cap",
			status: http.StatusOK,
			body:   oversized,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, rec := newRecordingServer(t, reply(test.status, test.body))
			sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

			err := sink.Send(context.Background(), mustMessage(t, "miko"))
			if err == nil {
				t.Fatalf("Send returned nil for %s %q", test.name, test.body)
			}

			rec.only(t)

			var apiErr *telegram.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Send error = %v, want an *telegram.APIError so T040 can log http_status", err)
			}

			if apiErr.HTTPStatus != test.status {
				t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, test.status)
			}

			if apiErr.Code != test.wantCode {
				t.Errorf("Code = %d, want %d", apiErr.Code, test.wantCode)
			}

			if apiErr.Description != test.wantDescription {
				t.Errorf("Description = %q, want %q", apiErr.Description, test.wantDescription)
			}
		})
	}
}

// TestFormattingRejectionKeepsTheFieldsTheRescuePredicateNeeds is the hook
// Batch 9 (T061) reads.
//
// Research R-008 fixes the predicate as
// `ok == false && error_code == 400 && description contains "can't parse
// entities"`. It is asserted here, one batch early and without the rescue,
// because the reason APIError keeps three separate fields instead of one
// formatted string is precisely that predicate — and a flattening refactor
// would otherwise not fail anything until Batch 9.
//
// This test does not assert a retry and must not acquire one: T035 is a single
// attempt (FR-019, FR-041).
func TestFormattingRejectionKeepsTheFieldsTheRescuePredicateNeeds(t *testing.T) {
	t.Parallel()

	const body = `{"ok":false,"error_code":400,` +
		`"description":"Bad Request: can't parse entities: Character '.' is reserved"}`

	server, rec := newRecordingServer(t, reply(http.StatusBadRequest, body))
	sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

	err := sink.Send(context.Background(), mustMessage(t, "a reserved dot."))
	if err == nil {
		t.Fatal("Send returned nil for a formatting rejection")
	}

	// FR-019 and FR-041 until T062 lands. When Batch 9 makes this two, it is
	// this line that must be updated deliberately rather than deleted.
	rec.only(t)

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Send error = %v, want an *telegram.APIError", err)
	}

	if apiErr.Code != 400 {
		t.Errorf("Code = %d, want 400", apiErr.Code)
	}

	if !strings.Contains(apiErr.Description, "can't parse entities") {
		t.Errorf("Description = %q, want it to contain %q", apiErr.Description, "can't parse entities")
	}
}

// TestSendMakesExactlyOneAttempt is FR-019 and FR-041.
//
// No queue, no automatic re-send, for any failure class. The formatting
// rejection is in the table on purpose: it is the one failure Batch 9 will be
// allowed to retry, and until T062 lands it must behave exactly like the
// others.
func TestSendMakesExactlyOneAttempt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "a formatting rejection",
			status: http.StatusBadRequest,
			body:   `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: x"}`,
		},
		{
			name:   "rate limited",
			status: http.StatusTooManyRequests,
			body:   `{"ok":false,"error_code":429,"description":"Too Many Requests"}`,
		},
		{
			name:   "a server error",
			status: http.StatusInternalServerError,
			body:   `{"ok":false,"error_code":500,"description":"Internal Server Error"}`,
		},
		{
			name:   "an undecodable reply",
			status: http.StatusBadGateway,
			body:   "<html>nope</html>",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, rec := newRecordingServer(t, reply(test.status, test.body))
			sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

			if err := sink.Send(context.Background(), mustMessage(t, "miko")); err == nil {
				t.Fatal("Send returned nil")
			}

			if seen := rec.requests(); len(seen) != 1 {
				t.Fatalf("requests sent = %d, want exactly 1 (FR-019, FR-041)", len(seen))
			}
		})
	}
}

// TestSendFailsWhenTheServerCannotBeReached is the transport case an
// httptest.Server cannot produce.
//
// A live handler can only answer, so a "transport failure" test written against
// one asserts nothing. This dials a port whose listener has been closed.
//
// The error must not be an *APIError: contracts/log-events.md puts http_status
// on "Telegram failures with a response", and there was no response.
func TestSendFailsWhenTheServerCannotBeReached(t *testing.T) {
	t.Parallel()

	sink := telegram.NewWithBaseURL(baseSettings(), "http://"+unreachableAddress(t))

	err := sink.Send(context.Background(), mustMessage(t, "miko"))
	if err == nil {
		t.Fatal("Send returned nil against a closed port")
	}

	var apiErr *telegram.APIError
	if errors.As(err, &apiErr) {
		t.Errorf("Send error = %v, an *APIError with HTTPStatus %d, but no response arrived",
			err, apiErr.HTTPStatus)
	}
}

// TestSendFailsWhenTheBodyIsShorterThanItsContentLength covers a reply that
// starts and does not finish.
//
// The connection is hijacked so the response can promise a Content-Length it
// then does not deliver — an httptest handler cannot short its own body. The
// client accepts the headers, so this fails during the body read rather than
// during the round trip, which is a distinct code path from the refusal above.
//
// The bytes that do arrive are deliberately a complete, valid, successful
// reply. That is what makes this test mean something: if the read error were
// swallowed, the truncated bytes would decode cleanly as `ok: true` and the
// post would be reported as delivered on the strength of a response nobody
// finished receiving. A body cut mid-token would fail closed anyway and would
// prove nothing about the read-error arm.
//
// It is an *APIError because a response did arrive and its status is real, so
// T040 has an http_status to log. Its Code is zero: nothing was decoded, and a
// half-read body must not be allowed to look like a decoded refusal.
func TestSendFailsWhenTheBodyIsShorterThanItsContentLength(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("the test server's ResponseWriter is not an http.Hijacker")

			return
		}

		conn, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)

			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = buffered.WriteString("HTTP/1.1 200 OK\r\n" +
			"Content-Type: application/json\r\n" +
			"Content-Length: 100\r\n\r\n")
		_, _ = buffered.WriteString(okBody)
		_ = buffered.Flush()
	}

	server, _ := newRecordingServer(t, handler)
	sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

	err := sink.Send(context.Background(), mustMessage(t, "miko"))
	if err == nil {
		t.Fatal("Send returned nil for a body that never finished")
	}

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Send error = %v, want an *telegram.APIError carrying the status", err)
	}

	if apiErr.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, http.StatusOK)
	}

	if apiErr.Code != 0 {
		t.Errorf("Code = %d, want 0: nothing was decoded", apiErr.Code)
	}
}

// TestSendIsBoundedByItsOwnRequestTimeout is FR-040.
//
// Driven through settings, not through a seam, because the property under test
// is that http_timeout_seconds reaches the http.Client at all. One second is
// the smallest bound the settings can express, so that is what this costs.
//
// The handler answers after five seconds if nobody stops it, which is what
// makes the assertion mutation-sensitive: remove the bound and this returns a
// success at five seconds rather than hanging.
func TestSendIsBoundedByItsOwnRequestTimeout(t *testing.T) {
	t.Parallel()

	settings := baseSettings()
	settings.HTTPTimeoutSeconds = 1

	server, _ := newRecordingServer(t, stall(5*time.Second))
	sink := telegram.NewWithBaseURL(settings, server.URL)

	started := time.Now()
	err := sink.Send(context.Background(), mustMessage(t, "miko"))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("Send returned nil after %s against a stalled server", elapsed)
	}

	// post.reasonFor keys off this to report "request timed out" rather than
	// the generic failure reason, so it is part of the contract and not an
	// implementation detail of net/http.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send error = %v, want it to satisfy errors.Is(context.DeadlineExceeded)", err)
	}

	if elapsed > 4*time.Second {
		t.Errorf("Send took %s, want it bounded near the configured 1s", elapsed)
	}
}

// TestSendHonoursTheOrchestratorsPerSinkDeadline is FR-015, and it is a
// different bound from the test above.
//
// The context carries post.Service's per-sink deadline; s.client.Timeout
// carries FR-040's per-request one. Here the request timeout is left at its
// generous default and the context is the only thing that can stop the call, so
// a builder that dropped the context — passing context.Background(), which
// compiles and passes every other test in this file — fails here.
func TestSendHonoursTheOrchestratorsPerSinkDeadline(t *testing.T) {
	t.Parallel()

	server, _ := newRecordingServer(t, stall(5*time.Second))
	sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	err := sink.Send(ctx, mustMessage(t, "miko"))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("Send returned nil after %s with an expired context", elapsed)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send error = %v, want it to satisfy errors.Is(context.DeadlineExceeded)", err)
	}

	if elapsed > 3*time.Second {
		t.Errorf("Send took %s, want it bounded near the context's 100ms", elapsed)
	}
}

// TestSendNeverLeaksTheBotTokenInAnyError is FR-043 at the only boundary that
// can enforce it today.
//
// The token is in the request URL — "/bot<token>/sendMessage" is the Bot API's
// shape — so net/http's *url.Error carries it verbatim in both Error() and
// %#v. That error would become post.SinkResult.Err, which
// internal/post/result.go deliberately leaves unredacted for whatever assembles
// the log record, and Options.Redact (issue #41) cannot help because nothing on
// this path constructs a logger at all.
//
// Every failure class is swept, and every render is checked: an error's Error()
// is not the only way its contents reach a string. %#v in particular ignores
// the Error method and reflects over the fields, which is how a surviving
// *url.Error would leak past a message-only guard — so the structural
// assertion (no *url.Error anywhere in the chain) is here too.
func TestSendNeverLeaksTheBotTokenInAnyError(t *testing.T) {
	t.Parallel()

	// A distinctive fragment, checked separately, so a guard that mangled the
	// token slightly rather than removing it still fails.
	const fragment = "SENTINEL"

	slowSettings := baseSettings()
	slowSettings.HTTPTimeoutSeconds = 1

	tests := []struct {
		name string
		// send performs one failing Send and returns its error.
		send func(t *testing.T) error
	}{
		{
			name: "the base url cannot be parsed",
			send: func(t *testing.T) error {
				t.Helper()

				return telegram.NewWithBaseURL(baseSettings(), "://not-a-url").
					Send(context.Background(), mustMessage(t, "miko"))
			},
		},
		{
			name: "the connection is refused",
			send: func(t *testing.T) error {
				t.Helper()

				return telegram.NewWithBaseURL(baseSettings(), "http://"+unreachableAddress(t)).
					Send(context.Background(), mustMessage(t, "miko"))
			},
		},
		{
			name: "the request timeout expires",
			send: func(t *testing.T) error {
				t.Helper()

				server, _ := newRecordingServer(t, stall(5*time.Second))

				return telegram.NewWithBaseURL(slowSettings, server.URL).
					Send(context.Background(), mustMessage(t, "miko"))
			},
		},
		{
			name: "the per-sink deadline expires",
			send: func(t *testing.T) error {
				t.Helper()

				server, _ := newRecordingServer(t, stall(5*time.Second))

				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()

				return telegram.NewWithBaseURL(baseSettings(), server.URL).
					Send(ctx, mustMessage(t, "miko"))
			},
		},
		{
			name: "telegram refuses the message",
			send: func(t *testing.T) error {
				t.Helper()

				server, _ := newRecordingServer(t, reply(http.StatusUnauthorized,
					`{"ok":false,"error_code":401,"description":"Unauthorized"}`))

				return telegram.NewWithBaseURL(baseSettings(), server.URL).
					Send(context.Background(), mustMessage(t, "miko"))
			},
		},
		{
			name: "the reply cannot be decoded",
			send: func(t *testing.T) error {
				t.Helper()

				server, _ := newRecordingServer(t, reply(http.StatusBadGateway, "<html>nope</html>"))

				return telegram.NewWithBaseURL(baseSettings(), server.URL).
					Send(context.Background(), mustMessage(t, "miko"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.send(t)
			if err == nil {
				t.Fatal("Send returned nil; this case must fail for the sweep to mean anything")
			}

			renders := map[string]string{
				"Error()": err.Error(),
				"%v":      fmt.Sprintf("%v", err),
				"%s":      fmt.Sprintf("%s", err),
				"%q":      fmt.Sprintf("%q", err),
				"%+v":     fmt.Sprintf("%+v", err),
				"%#v":     fmt.Sprintf("%#v", err),
			}

			for verb, rendered := range renders {
				if strings.Contains(rendered, sentinelToken) {
					t.Errorf("%s rendered the bot token: %s", verb, rendered)
				}

				if strings.Contains(rendered, fragment) {
					t.Errorf("%s rendered a fragment of the bot token: %s", verb, rendered)
				}
			}

			// The message-level checks above cannot see a field reflection
			// would reach, so the carrier itself must be gone from the chain.
			var urlErr *url.Error
			if errors.As(err, &urlErr) {
				t.Errorf("a *url.Error survived in the chain, carrying %q", urlErr.URL)
			}
		})
	}
}

// TestNewAppliesTheConfiguredRequestTimeoutToItsClient is FR-040 and issue
// #109.
//
// Two halves that fail differently. RequestTimeout checks the arithmetic,
// including the overflow residue no test can reach through a real request; the
// ClientTimeout check makes sure the result is wired to the field that bounds
// anything, because a conversion that is correct and unused looks identical
// from the arithmetic side.
func TestNewAppliesTheConfiguredRequestTimeoutToItsClient(t *testing.T) {
	t.Parallel()

	// 18446744074 seconds is the value issue #109 names: multiplied out it
	// wraps int64 to a *positive* 290.448384ms, so it survives validation, a
	// non-positive floor, and any near-MaxInt64 saturation check.
	const residue = 290448384 * time.Nanosecond

	var overflow64 int64 = 18446744074

	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "the documented default", seconds: 30, want: 30 * time.Second},
		{name: "one second", seconds: 1, want: time.Second},
		{name: "zero becomes the default rather than no limit", seconds: 0, want: 30 * time.Second},
		{name: "negative becomes the default", seconds: -17, want: 30 * time.Second},
		{
			name:    "the overflow residue saturates",
			seconds: int(overflow64),
			want:    time.Duration(telegram.MaxRequestTimeoutSeconds) * time.Second,
		},
		{
			name:    "the largest int saturates",
			seconds: int(^uint(0) >> 1),
			want:    time.Duration(telegram.MaxRequestTimeoutSeconds) * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if strconv.IntSize < 64 && int64(test.seconds) != overflow64 && test.seconds > 1 {
				t.Skip("the overflow cases need a 64-bit int")
			}

			if got := telegram.RequestTimeout(test.seconds); got != test.want {
				t.Errorf("RequestTimeout(%d) = %s, want %s", test.seconds, got, test.want)
			}

			settings := baseSettings()
			settings.HTTPTimeoutSeconds = test.seconds

			if got := telegram.ClientTimeout(telegram.New(settings)); got != test.want {
				t.Errorf("New(...).client.Timeout = %s, want %s", got, test.want)
			}
		})
	}

	// Named separately because it is the specific defect, not a boundary: a
	// user asking for ~584 years must not silently get a third of a second.
	if got := telegram.RequestTimeout(int(overflow64)); got == residue {
		t.Errorf("RequestTimeout(%d) = %s, which is issue #109's overflow residue", overflow64, got)
	}

	// http.Client reads a zero Timeout as "no limit", so the floor is not
	// cosmetic: without it an unvalidated settings struct produces a sink with
	// no FR-040 bound at all.
	if got := telegram.RequestTimeout(0); got == 0 {
		t.Error("RequestTimeout(0) = 0, which http.Client reads as no limit at all")
	}
}

// TestNewTargetsTheTelegramAPI pins the production origin.
//
// Worth its own assertion because the substitution seam is what every other
// test in this file uses, so nothing else would notice a New that defaulted to
// an httptest URL or to the empty string.
func TestNewTargetsTheTelegramAPI(t *testing.T) {
	t.Parallel()

	if got := telegram.BaseURL(telegram.New(baseSettings())); got != telegram.DefaultBaseURL {
		t.Errorf("New(...).baseURL = %q, want %q", got, telegram.DefaultBaseURL)
	}

	if telegram.DefaultBaseURL != "https://api.telegram.org" {
		t.Errorf("DefaultBaseURL = %q, want the documented Bot API origin", telegram.DefaultBaseURL)
	}
}

// TestWireNamesMatchTheBotAPI pins the spellings against literals.
//
// Every other assertion in this file reads the same constants the builder
// writes, which makes them blind to a typo in a constant: the key set would be
// consistently wrong and consistently expected. These are the Bot API's own
// names, written out here and nowhere else in the tests.
func TestWireNamesMatchTheBotAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "chat id field", got: telegram.FieldChatID, want: "chat_id"},
		{name: "text field", got: telegram.FieldText, want: "text"},
		{name: "parse mode field", got: telegram.FieldParse, want: "parse_mode"},
		{name: "thread id field", got: telegram.FieldThreadID, want: "message_thread_id"},
		{name: "content type", got: telegram.ContentTypeHeader, want: "application/x-www-form-urlencoded"},
		{name: "method", got: telegram.Method, want: "POST"},
		{
			name: "endpoint",
			got:  telegram.SendMessagePath(baseSettings()),
			want: "/bot" + sentinelToken + "/sendMessage",
		},
	}

	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
}

// TestNameIsTheContractIdentifier pins the string log-events.md and the
// user-facing results use.
func TestNameIsTheContractIdentifier(t *testing.T) {
	t.Parallel()

	if got := telegram.New(baseSettings()).Name(); got != "telegram" {
		t.Errorf("Name() = %q, want %q", got, "telegram")
	}

	if telegram.SinkName != "telegram" {
		t.Errorf("SinkName = %q, want %q", telegram.SinkName, "telegram")
	}
}

// TestSinkImplementsSinkAndNotTargeter is issue #98's box 3.
//
// The chat destination has no path. contracts/log-events.md gives `path` to the
// three obsidian events and to nothing else, and post.Targeter's own comment
// says the telegram sink is the one that does not implement it. A Target()
// returning a chat id or an API URL would invent a field the contract does not
// have — and, given what the URL contains, would be a credential in a log line.
//
// This is a test rather than a compile-time assertion because Go cannot express
// "does not implement".
func TestSinkImplementsSinkAndNotTargeter(t *testing.T) {
	t.Parallel()

	var sink any = telegram.New(baseSettings())

	if _, ok := sink.(post.Sink); !ok {
		t.Error("*telegram.Sink does not satisfy post.Sink")
	}

	if _, ok := sink.(post.Targeter); ok {
		t.Error("*telegram.Sink satisfies post.Targeter; chat events carry no path (issue #98)")
	}
}

// TestOneSinkServesConcurrentPosts backs the claim in Sink's comment that this
// type needs no lock.
//
// A GUI window outlives its submission (FR-028), so one Sink serves more than
// one post over its life and two can overlap. Under -race this fails if the
// type ever acquires shared mutable state — which is exactly what implementing
// post.Targeter would have required.
func TestOneSinkServesConcurrentPosts(t *testing.T) {
	t.Parallel()

	server, rec := newRecordingServer(t, reply(http.StatusOK, okBody))
	sink := telegram.NewWithBaseURL(baseSettings(), server.URL)

	const posts = 8

	var wait sync.WaitGroup

	errs := make([]error, posts)

	for i := range posts {
		wait.Add(1)

		go func() {
			defer wait.Done()

			errs[i] = sink.Send(context.Background(), mustMessage(t, "post "+strconv.Itoa(i)))
		}()
	}

	wait.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Send %d: %v", i, err)
		}
	}

	if seen := rec.requests(); len(seen) != posts {
		t.Errorf("requests received = %d, want %d", len(seen), posts)
	}
}

// TestWithoutRequestURLRemovesEveryCarrierOfTheToken covers the structural half
// of the credential guard directly.
//
// Two of these inputs cannot be produced by net/http and are the reason this is
// exposed at all: a *url.Error nested inside another, which a redirect failure
// can create, and a *url.Error with no cause, whose arm must not return nil —
// an error path answering nil reports a failed post as delivered.
func TestWithoutRequestURLRemovesEveryCarrierOfTheToken(t *testing.T) {
	t.Parallel()

	leaky := "https://api.telegram.org/bot" + sentinelToken + "/sendMessage"
	inner := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")

	tests := []struct {
		name string
		err  error
		// want is the exact error expected back, when the identity matters.
		want error
	}{
		{
			name: "a plain error is returned unchanged",
			err:  inner,
			want: inner,
		},
		{
			name: "one layer is stripped to its cause",
			err:  &url.Error{Op: "Post", URL: leaky, Err: inner},
			want: inner,
		},
		{
			name: "a nested layer is stripped too",
			err: &url.Error{Op: "Get", URL: leaky, Err: &url.Error{
				Op: "Post", URL: leaky, Err: inner,
			}},
			want: inner,
		},
		{
			name: "a wrapped layer is stripped",
			err:  fmt.Errorf("outer: %w", &url.Error{Op: "Post", URL: leaky, Err: inner}),
			want: inner,
		},
		{
			name: "a layer with no cause becomes a non-nil error",
			err:  &url.Error{Op: "Post", URL: leaky, Err: nil},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := telegram.WithoutRequestURL(test.err)

			if got == nil {
				t.Fatal("WithoutRequestURL returned nil; a nil error reports a failed post as delivered")
			}

			if test.want != nil && !errors.Is(got, test.want) {
				t.Errorf("WithoutRequestURL(...) = %v, want %v", got, test.want)
			}

			for _, rendered := range []string{got.Error(), fmt.Sprintf("%#v", got)} {
				if strings.Contains(rendered, sentinelToken) {
					t.Errorf("the token survived: %s", rendered)
				}
			}

			var urlErr *url.Error
			if errors.As(got, &urlErr) {
				t.Errorf("a *url.Error survived, carrying %q", urlErr.URL)
			}
		})
	}
}

// TestSafeReplacesACredentialThatSurvived covers the textual half of the
// credential guard.
//
// It is unreachable through Send by design — WithoutRequestURL removes the only
// shape that carries the token — so without this the guard's firing branch
// would never be executed by anything, which is the same as not having checked
// it.
func TestSafeReplacesACredentialThatSurvived(t *testing.T) {
	t.Parallel()

	sink := telegram.New(baseSettings())

	t.Run("nil stays nil", func(t *testing.T) {
		t.Parallel()

		if got := telegram.Safe(sink, nil); got != nil {
			t.Errorf("Safe(nil) = %v, want nil", got)
		}
	})

	t.Run("an error without the token is returned unchanged", func(t *testing.T) {
		t.Parallel()

		clean := errors.New("dial tcp: connection refused")

		if got := telegram.Safe(sink, clean); got != clean {
			t.Errorf("Safe(...) = %v, want the same error back", got)
		}
	})

	t.Run("an error carrying the token is rewritten", func(t *testing.T) {
		t.Parallel()

		cause := context.DeadlineExceeded
		leaky := fmt.Errorf("posting to /bot%s/sendMessage: %w", sentinelToken, cause)

		got := telegram.Safe(sink, leaky)

		if strings.Contains(got.Error(), sentinelToken) {
			t.Errorf("Safe(...) = %v, which still carries the token", got)
		}

		if !strings.Contains(got.Error(), "[redacted]") {
			t.Errorf("Safe(...) = %v, want the credential marker in its place", got)
		}

		// The chain has to survive: post.reasonFor asks errors.Is whether the
		// sink timed out, and a guard that flattened the error would turn every
		// redacted timeout into the generic failure reason.
		if !errors.Is(got, cause) {
			t.Errorf("Safe(...) lost its cause; errors.Is(%v, %v) is false", got, cause)
		}

		if strings.Contains(fmt.Sprintf("%#v", got), sentinelToken) {
			t.Errorf("%%#v rendered the token: %#v", got)
		}
	})

	t.Run("an empty credential does not turn every error into markers", func(t *testing.T) {
		t.Parallel()

		settings := baseSettings()
		settings.BotToken = config.NewSecret("")

		plain := errors.New("boom")

		if got := telegram.Safe(telegram.New(settings), plain); got != plain {
			t.Errorf("Safe(...) = %q, want the error back unchanged", got)
		}
	})
}

// TestDecodeResponseIsNotFooledByAPartialDecode is the decoder's own unit,
// stated where a round trip would obscure it.
//
// encoding/json fills the fields it managed before it failed, so a body whose
// error_code is a string still leaves description set. Acting on that would let
// Batch 9's rescue predicate match a description nobody successfully decoded.
func TestDecodeResponseIsNotFooledByAPartialDecode(t *testing.T) {
	t.Parallel()

	const body = `{"ok":false,"error_code":"400","description":"Bad Request: can't parse entities"}`

	err := telegram.DecodeResponse(http.StatusBadRequest, []byte(body))
	if err == nil {
		t.Fatal("DecodeResponse returned nil for a body it could not decode")
	}

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("DecodeResponse error = %v, want an *APIError", err)
	}

	if apiErr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, http.StatusBadRequest)
	}

	if apiErr.Code != 0 || apiErr.Description != "" {
		t.Errorf("decoded fields = (%d, %q), want them discarded", apiErr.Code, apiErr.Description)
	}
}

// TestSendMessageFormOmitsTheThreadKeyRatherThanEmptyingIt is the builder's own
// unit, and it is here because the wire-level negative can be satisfied by a
// builder that writes the key with an empty value on a server that then drops
// it.
func TestSendMessageFormOmitsTheThreadKeyRatherThanEmptyingIt(t *testing.T) {
	t.Parallel()

	without := telegram.SendMessageForm(baseSettings(), "miko")
	if _, present := without[telegram.FieldThreadID]; present {
		t.Errorf("%s present with no thread configured: %v", telegram.FieldThreadID, without)
	}

	with := telegram.SendMessageForm(withThread(baseSettings(), 7), "miko")
	if got := with.Get(telegram.FieldThreadID); got != "7" {
		t.Errorf("%s = %q, want %q", telegram.FieldThreadID, got, "7")
	}
}
