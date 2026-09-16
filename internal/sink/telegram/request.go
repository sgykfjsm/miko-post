package telegram

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// DefaultBaseURL is the Bot API origin every request is built against
// (research R-008).
//
// Exported so a reader can see what the sink talks to; the field holding it is
// not, so nothing outside this package can point production traffic elsewhere.
// The substitution seam is in export_test.go and exists only for httptest,
// which is what lets every path in FR-031 – FR-041 be asserted without a live
// token — the constitution's "sinks MUST be testable without contacting live
// external services".
const DefaultBaseURL = "https://api.telegram.org"

// sendMessageMethod is the only Bot API method this package uses. R-008 chose
// no SDK precisely because the whole surface is this one endpoint and four
// form fields.
const sendMessageMethod = "sendMessage"

// formContentType is the encoding of the request body, and it is a decision
// rather than a default (decision DEC-C1). See newSendRequest.
const formContentType = "application/x-www-form-urlencoded"

// Wire field names, written once so a test can assert against the same strings
// the builder emits rather than against a second copy that can drift.
//
// message_thread_id is the one that matters here: "the key is absent when no
// thread is configured" is an assertion that passes just as happily when the
// key is misspelled, so the negative test and the builder have to be reading
// the same constant.
const (
	fieldChatID   = "chat_id"
	fieldText     = "text"
	fieldParse    = "parse_mode"
	fieldThreadID = "message_thread_id"
)

// sendMessageForm builds the wire fields of one sendMessage call
// (FR-031, FR-032, FR-033).
//
// Three things here are contract rather than preference.
//
// text is message.Original, copied and not touched. FR-033 forbids escaping,
// transforming, or normalizing the characters MarkdownV2 reserves, which means
// this sink must not "helpfully" backslash a '.' or a '-'. If the user's text
// is not valid MarkdownV2, Telegram rejects it and Batch 9's rescue re-sends it
// unformatted; that is the design, and pre-escaping here would silently replace
// the user's punctuation with the tool's idea of it in every message that did
// parse.
//
// parse_mode is the constant, never settings.ParseMode. FR-034 says the key is
// accepted and validated but must not alter delivery in v0.1: the first attempt
// is always MarkdownV2 as fixed application behaviour. Reading the setting here
// would make the key live, which is the requirement inverted — and config's own
// comment on the field says as much.
//
// message_thread_id is set only when a thread is configured and is otherwise
// absent from the map entirely (FR-032, A-006). Absence is the wire signal for
// "post to the chat directly"; config.TelegramSettings.ThreadID is a pointer
// for exactly this reason, and validation already rejects a non-positive value,
// so a nil pointer here means the user omitted the key rather than that the
// value was lost.
func sendMessageForm(settings config.TelegramSettings, text string) url.Values {
	form := url.Values{}
	form.Set(fieldChatID, settings.ChatID)
	form.Set(fieldText, text)
	form.Set(fieldParse, config.ParseModeMarkdownV2)

	if settings.ThreadID != nil {
		form.Set(fieldThreadID, strconv.FormatInt(*settings.ThreadID, 10))
	}

	return form
}

// newSendRequest builds the POST that delivers one message.
//
// # Why the body is form-encoded and not JSON (decision DEC-C1, issue #113)
//
// Telegram accepts either. Form encoding is chosen because JSON cannot carry
// the user's bytes unchanged, and this sink is not allowed to change them
// (FR-011, FR-012, FR-033).
//
// encoding/json substitutes U+FFFD for any byte sequence that is not valid
// UTF-8 and reports no error while doing it — verified:
//
//	json.Marshal(map[string]string{"text": "a\xffb"})
//	    → {"text":"a�b"}, err == nil
//
// On Unix a command-line argument may contain such bytes, and issue #113
// records that they really do reach a destination: the obsidian sink writes
// them verbatim. With a JSON body Telegram would receive U+FFFD where the note
// holds the raw 0xFF, which is #113's option 3 — the two sinks storing
// different text for one post — and option 3 was rejected in advance as the
// divergence constitution principle II exists to prevent. Reaching a rejected
// option by accident, through a library's silent substitution, is the failure
// worth guarding against here.
//
// url.Values.Encode percent-encodes byte by byte instead, so 0xFF travels as
// %FF and arrives as 0xFF. What Telegram's server then does with a non-UTF-8
// sequence is outside our control and is not the claim being made. The claim is
// narrower and is the only one a sink can honour: we do not silently rewrite
// the user's bytes on the way out.
//
// #113 is still open and is a Message-layer question. If it is answered by
// rejecting invalid UTF-8 in Message.Validate, this comment stays true and
// simply stops mattering; if it is answered the other way, this is the encoding
// that keeps the two sinks agreeing.
//
// # Why url.JoinPath and not concatenation
//
// The token goes in the path — "/bot<token>/sendMessage" is the Bot API's
// shape, and it is the reason every error out of this package has to be
// scrubbed (see Sink.Send). JoinPath parses the base URL and then edits only
// its path, so no token can move the request to another host; that is the
// property worth having here and the one concatenation does not offer. It also
// absorbs a trailing slash on the base URL, which httptest never has and a
// hand-written settings value might.
//
// What it does not do is escape the token. An earlier version of this comment
// said it did, which is backwards: JoinPath treats each element as already
// escaped and unescapes it. config.Validate only checks that the token is
// non-empty, so both consequences are reachable from a settings file it
// accepts, and on Go 1.27 they are:
//
//   - a token containing "%zz" makes JoinPath itself fail, before any request
//     exists. The error is a url.EscapeError — not a *url.Error, so
//     withoutRequestURL has nothing to strip — and it leaves Send as the
//     fmt.Errorf wrapper, a *fmt.wrapError. That is the right outcome and the
//     reason this is documented rather than defended against: it fails closed,
//     and the text is `invalid URL escape "%zz"`, which names the offending
//     escape and not the credential.
//   - a token containing "%41" is unescaped to "A" for the path and travels as
//     "%41", so Telegram is asked about a token the user did not configure and
//     refuses it. Mangled, but only ever mangled into another refusal.
//
// The ".." resolution is real and is not the "wrong path at Telegram" the old
// wording promised: a token of "../../X" is resolved away rather than sent, and
// the path becomes "/X/sendMessage". There is no security consequence in any of
// this. The host is fixed by the base URL, traversal stays inside it, and the
// worst reachable outcome is a Bot API path that does not exist. It is recorded
// because a reader who believed the escaping claim would size the validation
// owed by internal/config wrongly, and because a comment that is wrong about
// the standard library is worse than no comment. Pinned by
// TestAMalformedTokenFailsClosedInsideTheAPIHost.
func newSendRequest(
	ctx context.Context,
	baseURL string,
	settings config.TelegramSettings,
	text string,
	plain ...bool,
) (*http.Request, error) {
	// The first of this package's two config.Secret.Reveal call sites, and the
	// one that type's comment names. The second is the credential net in
	// sink.go, which has to know the string it is looking for; see there.
	target, err := url.JoinPath(baseURL, "bot"+settings.BotToken.Reveal(), sendMessageMethod)
	if err != nil {
		return nil, err
	}

	form := sendMessageForm(settings, text)
	if len(plain) > 0 && plain[0] {
		form.Del(fieldParse)
	}
	body := form.Encode()

	// A *strings.Reader rather than an io.Reader of unknown kind, because
	// net/http then fills in ContentLength and GetBody itself. Content-Length
	// matters: a chunked body would be a second thing Telegram has to accept
	// for no benefit.
	//
	// Its error arm is not reachable through Send today and is kept rather than
	// ignored: the three things it rejects are a nil context, a method it
	// cannot parse, and a URL it cannot parse, and here the method is a
	// constant and the URL has already been through url.Parse inside JoinPath.
	// Discarding the error to satisfy a coverage number would be trading a real
	// guard for an unreachable one, so it stays and the package sits just under
	// 100%.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", formContentType)

	return request, nil
}
