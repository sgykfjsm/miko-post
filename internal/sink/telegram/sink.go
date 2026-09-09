package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// SinkName is this sink's stable identifier. It appears in results shown to the
// user and in every log record for this destination, so it is contract text
// rather than a debugging label (contracts/log-events.md).
const SinkName = "telegram"

// defaultRequestTimeout is FR-040's documented default, used when this package
// is handed a request timeout it cannot use.
//
// It is a real value rather than zero, and that is the point: http.Client reads
// a zero Timeout as "no limit at all", so the naive conversion of an
// unconfigured settings struct would produce a sink with no bound on FR-040's
// request. config.Validate rejects a non-positive http_timeout_seconds, but New
// is exported and a caller can construct settings without going through Load —
// the tests do — so the floor lives here too.
const defaultRequestTimeout = 30 * time.Second

// maxRequestTimeoutSeconds is the largest whole second a time.Duration can
// hold, about 292 years.
//
// This constant exists because of issue #109, which is about
// posting.sink_timeout_seconds and applies word for word to
// sink.telegram.http_timeout_seconds: internal/config/validate.go bounds both
// only from below. T035 is the first code in the repository to convert a
// settings seconds value into a time.Duration, so it is the first place the
// gap is reachable.
//
// The residue #109 documents is not the obvious overflow. A value that wraps
// negative or to zero is caught by any sane floor. `18446744074` wraps to a
// *positive* 290.448384ms — it passes validation, passes the floor, and gives a
// user who asked for ~584 years a request that times out in under a third of a
// second, tighter than the default they were trying to raise.
//
// Clamped here rather than fixed in internal/config, which keeps this batch to
// its own package. #109 should still be amended to cover http_timeout_seconds
// as well as sink_timeout_seconds and to reject the value at load time, where
// the user can be told; saturating here means the misconfiguration behaves
// sanely but silently.
const maxRequestTimeoutSeconds = int64(math.MaxInt64 / time.Second)

// credentialMarker stands in for the bot token if one ever survives as far as
// the credential net. Deliberately the same text config.Secret renders, so a
// reader who greps for it finds both.
const credentialMarker = "[redacted]"

// errRequestFailed is the fallback for a *url.Error with no cause of its own —
// a shape net/http does not produce today, and the one branch of
// withoutRequestURL that must not return nil. An error path that answers nil
// reports a failed post as delivered.
var errRequestFailed = errors.New("the request failed without a reported cause")

// Sink delivers a message to a Telegram chat over the Bot API (FR-031 – FR-041).
//
// One attempt, and only one. FR-019 forbids queueing or automatically
// re-sending a failed chat message, and FR-041 forbids retrying transport
// errors, timeouts, error statuses, or service-reported errors. The single
// unformatted rescue FR-035 permits is narrower than any of those and is Batch
// 9's work (T059 – T064); nothing here re-sends anything, and the test asserting
// exactly one request per Send is what keeps it that way.
//
// Unlike the obsidian sink this type holds no mutable state and needs no mutex:
// it does not implement post.Targeter, so there is nothing to record between
// Send and a later read. That absence is deliberate and is issue #98's box 3 —
// a chat has no path, so chat events carry none, and a Target() returning a URL
// or a chat id would invent a field the contract does not have. http.Client is
// safe for concurrent use, so one Sink serves the overlapping posts a GUI
// window's lifetime produces (FR-028).
type Sink struct {
	settings config.TelegramSettings

	// baseURL is the Bot API origin, substitutable by tests only; see
	// DefaultBaseURL and export_test.go.
	baseURL string

	// client carries FR-040's per-request bound as its Timeout. That bound is
	// not the same as the orchestrator's per-sink deadline (FR-015), which
	// arrives as the context: the first limits one HTTP round trip, the second
	// limits everything this sink does for one post, and in Batch 9 it will
	// have to cover two attempts. Both are honoured here, and a failure from
	// either wraps context.DeadlineExceeded so the orchestrator's classifier
	// reports "request timed out" for both.
	client *http.Client
}

// Sink must satisfy post.Sink, and must not accidentally satisfy post.Targeter.
// The first half is checked here; the second cannot be — an interface a type
// does not implement is not expressible as a compile-time assertion — so it is
// a test (issue #98).
var _ post.Sink = (*Sink)(nil)

// New builds the sink from its settings.
//
// The settings are copied by value, so a later reload cannot change where a
// post in flight is going. Mirrors obsidian.New.
func New(settings config.TelegramSettings) *Sink {
	return &Sink{
		settings: settings,
		baseURL:  DefaultBaseURL,
		client:   &http.Client{Timeout: requestTimeout(settings.HTTPTimeoutSeconds)},
	}
}

// Name identifies this sink (post.Sink).
func (s *Sink) Name() string { return SinkName }

// Send performs the single MarkdownV2 attempt (FR-019, FR-033, FR-040, FR-041).
//
// The shape is four steps and one exit rule: every error leaves through
// s.safe, and every transport error is stripped of its request URL before it is
// wrapped. That is not tidiness. The URL is "/bot<token>/sendMessage", so a
// *url.Error escaping this function puts the bot token in SinkResult.Err, which
// internal/post/result.go deliberately leaves unredacted for whatever assembles
// the log record. Verified, before this guard existed:
//
//	Post "http://127.0.0.1:1/bot123456:AA-SECRET/sendMessage": dial tcp …:
//	connect: connection refused
//
// FR-043 says the credential must never appear in logs, on-screen errors,
// command-line errors, or settings dumps. Options.Redact (T040, issue #41)
// cannot help: nothing on this path constructs a logger at all, and the error
// is a value handed upward, not a line written down. The sink boundary is the
// only layer that exists today, so the redaction has to be complete here.
//
// The wrapping must happen after the stripping, in that order. fmt.Errorf bakes
// the wrapped error's rendering into its own message, so wrapping first would
// copy the URL into a string no later unwrapping could reach.
//
// Both time bounds are live. The context is the orchestrator's per-sink
// deadline (FR-015) and rides on the request; s.client.Timeout is FR-040's
// per-request bound. Whichever expires first produces an error that satisfies
// errors.Is(err, context.DeadlineExceeded) — net/http's Client.Timeout wraps it
// too — which is what post.reasonFor keys off to report "request timed out".
// The context is not checked before the request is built: net/http checks it
// itself and answers with the same error, so a pre-check would only add a
// second code path producing a different one.
func (s *Sink) Send(ctx context.Context, message post.Message) error {
	request, err := newSendRequest(ctx, s.baseURL, s.settings, message.Original)
	if err != nil {
		return s.safe(fmt.Errorf("building the telegram request: %w", withoutRequestURL(err)))
	}

	response, err := s.client.Do(request)
	if err != nil {
		return s.safe(fmt.Errorf("sending the telegram request: %w", withoutRequestURL(err)))
	}

	defer func() {
		// The body is closed and its error discarded: we already have
		// everything we are going to read, and a close failure on a response
		// cannot change whether Telegram accepted the message.
		_ = response.Body.Close()
	}()

	// A read error here — a connection cut mid-body, a body shorter than its
	// Content-Length — must not reach decodeResponse, which would diagnose the
	// truncated bytes as Telegram sending something malformed. It is not that:
	// we simply did not receive what Telegram sent.
	//
	// It is still an *APIError, because a response line did arrive and its
	// status is real, and contracts/log-events.md wants http_status on
	// "Telegram failures with a response". Code and Description stay zero, so a
	// half-read body cannot be mistaken for a decoded refusal — including by
	// Batch 9's rescue predicate.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return s.safe(&APIError{HTTPStatus: response.StatusCode, cause: withoutRequestURL(err)})
	}

	return s.safe(decodeResponse(response.StatusCode, body))
}

// requestTimeout converts the configured seconds into FR-040's per-request
// bound, saturating rather than overflowing (issue #109).
//
// Both arms are reachable from a settings file config.Validate accepts today,
// which is the whole reason this is a function and not an expression:
//
//   - seconds <= 0 is rejected by validation, but New does not require
//     validated settings, and http.Client reads Timeout == 0 as "unbounded".
//     The floor turns a zero-value settings struct into FR-040's default rather
//     than into no limit at all.
//   - seconds > maxRequestTimeoutSeconds passes validation and wraps. See the
//     constant: 18446744074 becomes 290ms.
//
// Saturating is the right answer for the upper arm only because the value is
// absurd either way — nobody who wrote 18446744074 wanted 292 years any more
// than they wanted 290 milliseconds. The honest fix is to reject it at load
// time with a message, which is #109's, not this batch's.
func requestTimeout(seconds int) time.Duration {
	switch {
	case seconds <= 0:
		return defaultRequestTimeout
	case int64(seconds) > maxRequestTimeoutSeconds:
		return time.Duration(maxRequestTimeoutSeconds) * time.Second
	default:
		return time.Duration(seconds) * time.Second
	}
}

// withoutRequestURL removes every *url.Error layer from a transport failure,
// and with them the request URL that carries the bot token.
//
// Structural rather than textual, and that matters: %#v on a *url.Error renders
// its exported URL field whatever its Error() says, so replacing the URL inside
// the message would leave the credential one verb away. Dropping the wrapper
// keeps the cause — a *net.OpError, a timeout — which carries the host and port
// but never the path, and preserves errors.Is against context.DeadlineExceeded.
//
// The loop handles nesting because net/http can wrap a redirect failure in a
// second *url.Error. It terminates for any acyclic chain, since each pass
// descends strictly below the layer errors.As just found; a cyclic error chain
// would already hang errors.As itself, so no extra bound is added for a shape
// the standard library does not survive either.
func withoutRequestURL(err error) error {
	for {
		var wrapper *url.Error
		if !errors.As(err, &wrapper) {
			return err
		}

		if wrapper.Err == nil {
			return errRequestFailed
		}

		err = wrapper.Err
	}
}

// safe is the second net under withoutRequestURL: it returns err unless the bot
// token appears in its rendering, and replaces it if it does.
//
// withoutRequestURL is the mechanism; this is the guarantee. It exists because
// the claim being made is "no error out of Send carries the credential", and
// that claim should not depend on this package having enumerated every error
// shape net/http can produce. The obsidian sink has no equivalent because it has
// no credential in its error paths.
//
// It is honest about its limits. It reads err.Error(), so it catches a token in
// a rendered message and not one sitting in an exported field of some type
// reflection would reach — that case is what withoutRequestURL handles
// structurally, and the leak sweep in sink_test.go checks %#v as well as %v to
// keep both honest.
//
// The cause is retained through Unwrap rather than discarded, so
// errors.Is(err, context.DeadlineExceeded) still answers and the orchestrator
// still reports a timeout as a timeout. Retaining it is safe precisely because
// withoutRequestURL has already run: the chain below this point holds no
// *url.Error to reach into.
//
// This is this package's second config.Secret.Reveal call site. That type's
// comment says Reveal is meant for exactly one — building the request URL — and
// two is the honest count now: a guard looking for a string has to be given the
// string. Nothing else changes, in particular nothing stores the revealed value
// on the Sink: an unexported plain-string field would be dumped by fmt's
// reflection on any print of the Sink, which is the exposure config.Secret
// exists to remove.
func (s *Sink) safe(err error) error {
	if err == nil {
		return nil
	}

	token := s.settings.BotToken.Reveal()

	// An empty credential is the unconfigured sink, and strings.Contains
	// answers true for an empty needle — without this arm every error would be
	// rewritten into a marker soup.
	if token == "" {
		return err
	}

	rendered := err.Error()
	if !strings.Contains(rendered, token) {
		return err
	}

	return &redactedError{
		message: strings.ReplaceAll(rendered, token, credentialMarker),
		cause:   err,
	}
}

// redactedError carries a rewritten message over an intact cause.
//
// Both fields are unexported so that %#v renders the cause as a pointer address
// instead of walking into whatever leaked — the mechanism config.Secret's field
// comment describes. Error() is what every fmt verb for an error consults, so
// the rewritten message is what gets printed; Unwrap keeps errors.Is and
// errors.As working for the callers that ask deliberately.
type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.cause }
