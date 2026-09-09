package telegram

import (
	"net/http"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// NewWithBaseURL builds a Sink talking to baseURL, for the external test
// package and only for it.
//
// This is the seam research R-008 chose the whole client design around: with a
// substitutable origin, every path in FR-031 – FR-041 is assertable against an
// httptest server and no test needs a live bot token. Following
// internal/sink/obsidian/export_test.go, it lives here so the shipped API does
// not grow a parameter that exists for tests.
func NewWithBaseURL(settings config.TelegramSettings, baseURL string) *Sink {
	sink := New(settings)
	sink.baseURL = baseURL

	return sink
}

// BaseURL reports the origin a Sink was built with, so a test can assert that
// New defaults to DefaultBaseURL. The field is unexported precisely so nothing
// in production can read or change it, which leaves a test no other way to see
// what New chose.
func BaseURL(s *Sink) string { return s.baseURL }

// ClientTimeout reports the http.Client bound New installed.
//
// Without it, requestTimeout could be perfectly correct and simply never wired
// to anything — the shape of defect a unit test of the conversion alone cannot
// see.
func ClientTimeout(s *Sink) time.Duration { return s.client.Timeout }

// RequestTimeout exposes the seconds-to-Duration conversion (issue #109).
//
// Exposed rather than driven through New because the interesting inputs are the
// ones that must never reach an http.Client at all — the overflow residue
// #109 documents — and asserting on the resulting Duration directly says what
// the guard is for. ClientTimeout covers the wiring.
var RequestTimeout = requestTimeout

// WithoutRequestURL exposes the structural half of the credential guard.
//
// Its two hardest inputs cannot be produced by net/http: a *url.Error nested
// inside another, and a *url.Error with a nil cause. The second is the arm that
// must not return nil — an error path answering nil reports a failed post as
// delivered — so leaving it to inspection is not an option.
var WithoutRequestURL = withoutRequestURL

// Safe exposes the textual half of the credential guard.
//
// Unreachable through Send by design: WithoutRequestURL removes the only error
// shape that carries the token, so this net never fires in practice. It exists
// for the shapes nobody has enumerated, and a guard whose firing branch is
// never executed by any test is a guard nobody has checked.
func Safe(s *Sink, err error) error { return s.safe(err) }

// DecodeResponse exposes response decoding for the assertions that are about
// the decoder rather than about a round trip.
var DecodeResponse = decodeResponse

// SendMessageForm exposes the wire fields the builder produces, so a mutation
// of the builder can be checked against the exact key set rather than against a
// re-derivation of it.
var SendMessageForm = sendMessageForm

// MaxResponseBytes is the read cap, so a test can size a body against the real
// value instead of a copy that can drift from it.
const MaxResponseBytes = maxResponseBytes

// MaxRequestTimeoutSeconds is the saturation point requestTimeout clamps to.
const MaxRequestTimeoutSeconds = maxRequestTimeoutSeconds

// ContentTypeHeader is the Content-Type the builder sets, so the assertion and
// the builder read one constant.
const ContentTypeHeader = formContentType

// Wire field names, exported for the same reason: a test asserting that
// message_thread_id is absent must be looking for the key the builder would
// have written.
const (
	FieldChatID   = fieldChatID
	FieldText     = fieldText
	FieldParse    = fieldParse
	FieldThreadID = fieldThreadID
)

// SendMessagePath is the request path a Sink built from these settings will
// use, so a test can assert the endpoint without rebuilding it by hand.
func SendMessagePath(settings config.TelegramSettings) string {
	return "/bot" + settings.BotToken.Reveal() + "/" + sendMessageMethod
}

// Method is the verb Send uses.
const Method = http.MethodPost
