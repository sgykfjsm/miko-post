package telegram

import (
	"context"
	"errors"
	"fmt"
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
// the tests do — so the floor lives here too (decision DEC-C3).
const defaultRequestTimeout = 30 * time.Second

// maxRequestTimeoutSeconds is the largest whole second a time.Duration can
// hold, about 292 years.
//
// This constant is the second of two independent guards on
// sink.telegram.http_timeout_seconds, and the validation layer is the one that
// talks to the user. internal/config/validate.go bounds the key from above as
// well as from below — one shared rule at config.MaxTimeoutSeconds, covering
// this key and posting.sink_timeout_seconds together — so a settings document
// carrying 18446744074 is now refused at load time with a message naming the
// key and the mechanism. That is issue #114's real fix, and #109's; it has
// landed, and #114 is the one scoped to this key.
//
// The residue it guards is not the obvious overflow. A value that wraps
// negative or to zero is caught by any sane floor. `18446744074` wraps to a
// *positive* 290.448384ms — so before the validation bound existed it passed
// validation, passed the floor, and gave a user who asked for ~584 years a
// request that timed out in under a third of a second, tighter than the default
// they were trying to raise.
//
// The clamp is kept, deliberately, which is #114's remaining obligation
// discharged rather than left to rot (decision DEC-C3, and
// contracts/telegram-sink.md records the same choice). New is exported and does
// not require validated settings — this package's own tests construct a
// config.TelegramSettings directly, and so may any future caller — so the
// conversion needs its own floor and ceiling whatever validation does. The
// floor is the part that would actually be missed: http.Client reads
// Timeout: 0 as *unbounded*, so a zero-valued settings struct without it
// produces a request with no limit at all rather than FR-040's default, which
// is a worse failure than the one the ceiling prevents. Neither guard's test
// may be deleted on the grounds that the other exists.
const maxRequestTimeoutSeconds = int64(math.MaxInt64 / time.Second)

// credentialMarker stands in for the bot token wherever one is removed.
// Deliberately the same text config.Secret renders, so a reader who greps for
// it finds every site at once.
const credentialMarker = "[redacted]"

// redactToken is the one substitution both credential guards perform.
//
// Shared rather than written twice because the two sites are a value guard and
// a rendering guard over the same secret: decodeResponse scrubs the description
// a proxy quoted the request into, and Sink.safe scrubs an error's message. Two
// copies could drift on the marker, and the marker is what a reader greps for.
//
// The empty token is the unconfigured sink and returns text untouched.
// strings.Contains answers true for an empty needle and ReplaceAll would splice
// the marker between every rune, so the arm is load-bearing rather than
// defensive.
func redactToken(text, token string) string {
	if token == "" {
		return text
	}

	return strings.ReplaceAll(text, token, credentialMarker)
}

// errRequestFailed is the fallback for a *url.Error with no cause of its own —
// a shape net/http does not produce today, and the one branch of
// withoutRequestURL that must not return nil. An error path that answers nil
// reports a failed post as delivered.
var errRequestFailed = errors.New("the request failed without a reported cause")

// Sink delivers a message to a Telegram chat over the Bot API (FR-031 – FR-041).
//
// One MarkdownV2 attempt, followed only on a trusted formatting rejection by
// one plain-text rescue. Redirects remain refused (DEC-C4).
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
	// limits everything this sink does for one post, and covers both attempts. Both are honoured here, and a failure from
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
// post in flight is going. Mirrors obsidian.New. The client's two fields are
// both decisions and neither is a default: see requestTimeout for the bound and
// refuseRedirect for the redirect policy.
func New(settings config.TelegramSettings) *Sink {
	return &Sink{
		settings: settings,
		baseURL:  DefaultBaseURL,
		client: &http.Client{
			Timeout:       requestTimeout(settings.HTTPTimeoutSeconds),
			CheckRedirect: refuseRedirect,
		},
	}
}

// refuseRedirect is the client's redirect policy: 3xx answers are handed back
// as responses rather than followed (decision DEC-C4, FR-041, FR-043).
//
// Leaving CheckRedirect nil is not "no policy". It selects Go's default, which
// follows up to ten redirects, and both things that then happen are
// disqualifying. Measured against this sink before this policy existed, with a
// single 302 pointing at a second httptest server:
//
//	Send err      = <nil>      ← the post was reported delivered
//	final method  = "GET"      ← net/http converted the POST
//	final body    = ""         ← the user's message was never sent
//	final Referer = "http://127.0.0.1:62700/bot7654321:AA-…-SENTINEL-…/sendMessage"
//
// The first three lines are the failure this whole package is shaped around —
// delivered reported, nothing delivered — arriving without Telegram being
// involved at all, because anything positioned to answer for api.telegram.org
// can answer 302 and decodeResponse then reads the redirect target's reply as
// Telegram's. The fourth is FR-043: net/http's refererForURL strips userinfo
// from a cross-origin Referer but keeps the path, and the path is
// "/bot<token>/sendMessage", so following the hop hands the credential to
// whatever host the Location names. Neither withoutRequestURL nor safe can see
// that one — it is a header on the wire, not an error value.
//
// A 307 or 308 keeps the method and replays the body instead. One Send became
// four wire requests, each carrying the user's message in full: FR-041 allows a
// single attempt and forbids re-sending, FR-019 forbids automatic re-delivery,
// and a redirect chain is a re-send this sink never decided to make.
//
// ErrUseLastResponse rather than an error of this package's own, because the
// 3xx should be judged by the same code as every other status. Returned that
// way it becomes an ordinary response, the body is read and handed to
// decodeResponse, and it fails closed as an *APIError carrying the 3xx.
// Inventing an error here would add a second failure shape for callers to
// recognise and would bypass the reading that makes HTTPStatus available to
// T040.
//
// Which arm it fails on depends on the 3xx's body, and an earlier version of
// this comment asserted the wrong one. A redirect normally carries no body or
// an HTML one, and that reaches the decode arm. A 3xx whose body does parse as
// `{"ok":true,…}` — which anything answering for the origin can send, and which
// is measurably what happens — is a body claiming success under a non-success
// status, so it lands on the contradiction arm and wraps
// errContradictoryStatus. That matters to more than prose: response.go tells
// T061 to exclude errContradictoryStatus, so T061's author needs to know that
// refused redirects are inside that sentinel's set and not, as the old wording
// implied, categorically outside it.
//
// The signature ignores both arguments deliberately: no redirect is acceptable,
// so neither the hop nor the history can change the answer.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// Name identifies this sink (post.Sink).
func (s *Sink) Name() string { return SinkName }

// Send performs MarkdownV2 delivery with one formatting-only rescue (FR-033–FR-041).
//
// The shape is four steps and one exit rule: every error leaves through
// s.safe, and every transport error is stripped of its request URL before it is
// wrapped (decision DEC-C2). That is not tidiness. The URL is
// "/bot<token>/sendMessage", so a *url.Error escaping this function puts the
// bot token in SinkResult.Err, which internal/post/result.go deliberately
// leaves unredacted for whatever assembles the log record. Verified, before
// this guard existed:
//
//	Post "http://127.0.0.1:1/bot123456:AA-SECRET/sendMessage": dial tcp …:
//	connect: connection refused
//
// FR-043 says the credential must never appear in logs, on-screen errors,
// command-line errors, or settings dumps. Options.Redact (T040, issue #41)
// cannot help: nothing on this path constructs a logger at all, and the error
// is a value handed upward, not a line written down. The sink boundary is the
// only layer that exists today, so the redaction has to be complete here.
// Issues #103 and #115 cover the general case; #115 is specifically about this
// invariant now being load-bearing and enforced nowhere else.
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
	started := time.Now()
	first := s.send(ctx, message, false)
	if !isFormattingRejection(first) {
		return first
	}
	post.ReportFormatting(ctx, post.FormattingAttempt{Err: first, Duration: time.Since(started)})
	started = time.Now()
	second := s.send(ctx, message, true)
	post.ReportFormatting(ctx, post.FormattingAttempt{Plain: true, Err: second, Duration: time.Since(started)})
	if second != nil {
		return &RescueError{Markdown: first, Plaintext: second}
	}
	return nil
}

func (s *Sink) send(ctx context.Context, message post.Message, plain bool) error {
	request, err := newSendRequest(ctx, s.baseURL, s.settings, message.Original, plain)
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
	// Content-Length, or a reply past the size cap — must not reach
	// decodeResponse, which would diagnose the bytes it did get as Telegram
	// sending something malformed. It is not that: we simply do not have what
	// Telegram sent.
	//
	// It is still an *APIError, because a response line did arrive and its
	// status is real, and contracts/log-events.md wants http_status on
	// "Telegram failures with a response". Code and Description stay zero, so a
	// half-read body cannot be mistaken for a decoded refusal — including by
	// Batch 9's rescue predicate.
	body, err := readResponseBody(response.Body)
	if err != nil {
		return s.safe(&APIError{HTTPStatus: response.StatusCode, cause: withoutRequestURL(err)})
	}

	return s.safe(decodeResponse(response.StatusCode, body, s.botToken()))
}

// requestTimeout converts the configured seconds into FR-040's per-request
// bound, saturating rather than overflowing (issue #114, decision DEC-C3).
//
// Both arms are reachable from settings this package can be handed, which is
// the whole reason this is a function and not an expression:
//
//   - seconds <= 0 is refused by config.Validate, but New does not require
//     validated settings, and http.Client reads Timeout == 0 as "unbounded".
//     The floor turns a zero-value settings struct into FR-040's default rather
//     than into no limit at all.
//   - seconds > maxRequestTimeoutSeconds is refused by config.Validate too, and
//     wraps if it arrives anyway. See the constant: 18446744074 becomes 290ms.
//
// Neither arm is reachable through config.Load any more. Rejecting the value at
// load time with a message is the honest fix and it is the one that landed
// (#114, #109) — in internal/config, where the user can be told. What is left
// for this function is the unvalidated caller, and saturating is the right
// answer for it because the value is absurd either way: nobody who wrote
// 18446744074 wanted 292 years any more than they wanted 290 milliseconds. See
// maxRequestTimeoutSeconds for why the clamp is kept rather than removed as
// redundant.
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
// and with them the request URL that carries the bot token (decision DEC-C2).
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
// withoutRequestURL is the mechanism; this is the guarantee, and it is the
// textual net decision DEC-C2 puts behind the structural one. It exists because
// the claim being made is "no error out of Send carries the credential", and
// that claim should not depend on this package having enumerated every error
// shape net/http can produce. The obsidian sink has no equivalent because it has
// no credential in its error paths.
//
// It scans two renderings, because fmt reaches an error's contents by two
// routes that do not agree. %v, %s, %q and %+v all call Error(); %#v does not —
// it reflects the value and prints every exported field whatever Error() chose
// to say. Reading Error() alone therefore left a real hole rather than a
// theoretical one, and APIError is the type that opened it: Error() suppresses
// Description whenever a cause is set, so on the contradiction path — a non-2xx
// whose body claimed ok, the proxy-or-captive-portal reply decodeResponse
// exists to refuse — a Description quoting the request URL rendered clean,
// this guard declined to fire, and %#v printed the token verbatim.
//
// Firing is what closes it, and not by rewriting the field. redactedError holds
// its cause unexported, so once this returns, %#v prints a pointer address
// instead of walking into whatever leaked — the same mechanism config.Secret
// relies on. That also means the two arms can disagree harmlessly: when only
// the reflected form carried the token, the substitution on the message is a
// no-op and the wrapping alone does the work.
//
// # What this net is not
//
// It is a net over renderings, and for a while it was mistaken for a net over
// values. Wrapping a leaking *APIError hides the field from fmt; it does not
// take the token out of the field, and errors.As walks straight past the
// wrapper to it. T040 is specified to make exactly that call. So the *APIError
// path is fixed where the string is built instead — see decodeResponse — and
// what remains here is the original job: the error shapes nobody enumerated,
// caught by their rendering because a rendering is all this layer has.
//
// It is honest about the rest of its limits too. %#v does not follow pointers,
// so this is not "everything reflection could reach" — but it is exactly the
// surface the leak sweep in sink_test.go renders, and the sweep now also walks
// the chain for the two typed carriers, so the guarantee and the assertion
// cover the same ground rather than one of them being wider on paper.
//
// The cause is retained through Unwrap rather than discarded, so
// errors.Is(err, context.DeadlineExceeded) still answers and the orchestrator
// still reports a timeout as a timeout. Retaining it is safe precisely because
// withoutRequestURL has already run: the chain below this point holds no
// *url.Error to reach into.
//
// The match is unanchored, which is safe for the value this looks for and
// would not be for an arbitrary one: a one- or two-character token would shred
// every diagnostic into "telegr[redacted]m", and no Bot API token is anything
// like that short. That rule now exists where it belongs, in the validation
// layer where the user can be told: config.Validate rejects a bot_token below
// config.MinBotTokenLength (issue #117), and internal/logging skips any pattern
// shorter than that bound as a second guard for callers that build settings
// without going through Load. Neither guard lives here, because a sink is the
// wrong place to have an opinion about the shape of a credential it was handed. The substitution itself is literal — strings.ReplaceAll interprets
// neither the needle nor the replacement — so a token containing %s, $1 or a
// backslash, or one that happens to contain the marker, behaves like any other.
func (s *Sink) safe(err error) error {
	if err == nil {
		return nil
	}

	token := s.botToken()

	// An empty credential is the unconfigured sink, and strings.Contains
	// answers true for an empty needle — without this arm every error would be
	// rewritten into a marker soup.
	if token == "" {
		return err
	}

	// Error() first because it is the cheap surface and the usual carrier; the
	// reflected form only when the message came back clean, since that is the
	// case where a suppressed exported field can still be one verb away.
	rendered := err.Error()
	if !strings.Contains(rendered, token) && !strings.Contains(fmt.Sprintf("%#v", err), token) {
		return err
	}

	return &redactedError{
		message: redactToken(rendered, token),
		cause:   err,
	}
}

// botToken is this package's second config.Secret.Reveal call site, and its
// last.
//
// That type's comment says Reveal is meant for exactly one — building the
// request URL — and two is the honest count now: the credential guards look for
// a string and remove it, and both halves have to be given the string. Routing
// them through one accessor is what keeps the count at two as the guards grow;
// Sink.safe needs it to search, decodeResponse to scrub.
//
// Nothing stores the revealed value on the Sink. An unexported plain-string
// field would be dumped by fmt's reflection on any print of the Sink, which is
// the exposure config.Secret exists to remove.
func (s *Sink) botToken() string { return s.settings.BotToken.Reveal() }

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
