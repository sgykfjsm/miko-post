package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
)

// maxResponseBytes caps how much of a reply is read.
//
// A sendMessage reply is a few hundred bytes. What arrives instead, when
// something between here and Telegram goes wrong, is whatever a proxy or a
// captive portal felt like serving — an HTML error page of no particular size.
// The cap costs nothing on the real path and turns "the reply was enormous"
// into an ordinary decode failure rather than an allocation.
//
// Truncating a large body makes its JSON invalid, which fails closed by the
// same route as any other unparseable reply. That is the intended outcome: a
// 1 MiB sendMessage response is not a response we should be acting on.
const maxResponseBytes = 1 << 20

// errContradictoryStatus reports a body claiming success under an HTTP status
// that says otherwise.
//
// Telegram does not do this; a proxy rewriting a status, or a captive portal
// answering for it, can. The pair cannot both be true, so the sink refuses to
// pick the optimistic half — reporting a post as delivered when it was not is
// the one failure mode this batch's tests exist to rule out.
var errContradictoryStatus = errors.New("the body reported ok with a non-success HTTP status")

// apiResponse is the envelope every Bot API method answers with.
//
// Three fields, because three are what the contract needs: `ok` decides
// success, and `error_code` with `description` are what contracts/log-events.md
// and Batch 9's rescue predicate (T061) read. `result` is deliberately absent —
// nothing in this feature uses the sent message's id, and a field nobody reads
// is a field that can quietly acquire a meaning.
type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// APIError reports a sendMessage call that reached Telegram and was not
// accepted.
//
// It is exported, and typed, for two reasons that outlive this batch.
//
// contracts/log-events.md requires `http_status` on Telegram failures that had
// a response, and T040 (Batch 6c) has to emit it — but post.Sink.Send returns a
// bare error and the orchestrator has no other channel to the sink. That is
// structurally the same gap post.Targeter was invented to close for `path`.
// Here a typed error closes it: the orchestrator already holds the error, so
// errors.As is the whole mechanism and post.Sink stays two methods.
//
// The three fields are kept apart rather than flattened into the message
// because Batch 9's rescue predicate (T061, research R-008) is
// `ok == false && error_code == 400 && description contains "can't parse
// entities"`. A predicate re-parsing that out of a formatted string would be
// matching this package's own prose instead of Telegram's answer.
//
// Nothing on it carries the request URL, and so nothing carries the bot token:
// the URL is the only place the token appears, and Sink.Send strips it from
// every transport error before wrapping (see withoutRequestURL). The fields
// here come from the response body, which Telegram does not echo credentials
// into. That property is asserted rather than assumed — see the leak sweep in
// sink_test.go, which renders every error this package returns through %v, %+v
// and %#v.
type APIError struct {
	// HTTPStatus is the response status. Always set, including when the body
	// could not be decoded, because it is the field T040 must log.
	HTTPStatus int

	// Code is the body's `error_code`. Zero when the body did not decode.
	Code int

	// Description is the body's `description`. Empty when the body did not
	// decode, or when Telegram sent none.
	Description string

	// cause is why the body could not be trusted, when that is the reason this
	// error exists: a decode failure, or errContradictoryStatus.
	//
	// Unexported so that %#v renders it as a pointer address rather than
	// walking into it — the same mechanism config.Secret relies on to keep a
	// nested value out of a reflected dump. Unwrap exposes it deliberately for
	// errors.Is.
	cause error
}

// Error renders the failure for the diagnostic log.
//
// Two shapes, kept distinct on purpose. "Telegram refused the message" is a
// statement about what Telegram decided, and it must not be printed for a reply
// we could not read: a proxy's HTML error page is not Telegram refusing
// anything, and saying so would send the user looking at their chat settings.
func (e *APIError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("telegram answered HTTP %d and the reply could not be trusted: %v",
			e.HTTPStatus, e.cause)
	}

	if e.Description == "" {
		return fmt.Sprintf("telegram refused the message: HTTP %d, error_code %d",
			e.HTTPStatus, e.Code)
	}

	return fmt.Sprintf("telegram refused the message: HTTP %d, error_code %d: %s",
		e.HTTPStatus, e.Code, e.Description)
}

// Unwrap exposes the decode or contradiction cause to errors.Is.
//
// nil when Telegram gave a clean refusal, which is the common case: there is no
// underlying error there, only Telegram's answer.
func (e *APIError) Unwrap() error { return e.cause }

// decodeResponse turns one sendMessage reply into success (nil) or an
// *APIError (FR-035, FR-066).
//
// It fails closed at every step, and that is the requirement rather than a
// style. The failure this sink must never produce is the one Batch 6a hit three
// times in review: the post reported as delivered while nothing was stored. A
// reply that cannot be read does not say the message arrived, so it is a
// failure — not a shrug.
//
// The order of the checks is load bearing:
//
//   - A body that does not decode is a failure regardless of status, including
//     a 200. An empty body, an HTML error page, and a truncated JSON object all
//     land here.
//   - `ok == false` is a failure regardless of status, and its decoded fields
//     are carried through. Telegram answers a refusal with a 4xx, but the
//     status is not what the contract keys off; `ok` is.
//   - `ok == true` under a non-2xx status is a contradiction and fails, because
//     neither half can be trusted once they disagree.
//
// The partially populated payload from a decode failure is deliberately
// discarded. encoding/json fills the fields it managed before the error — a
// body with `"error_code": "400"` as a string leaves `description` set and
// `error_code` zero — and acting on half a decode would let Batch 9's rescue
// predicate fire on fields nobody successfully read. HTTPStatus survives
// because it comes from the response line, not the body.
//
// body is []byte rather than an io.Reader because the read is bounded by the
// caller (maxResponseBytes) and the read error has to be distinguishable from a
// decode error: a connection cut mid-body is a transport failure, not Telegram
// saying something malformed.
func decodeResponse(status int, body []byte) error {
	var payload apiResponse

	if err := json.Unmarshal(body, &payload); err != nil {
		return &APIError{HTTPStatus: status, cause: err}
	}

	if !payload.OK {
		return &APIError{
			HTTPStatus:  status,
			Code:        payload.ErrorCode,
			Description: payload.Description,
		}
	}

	if status < 200 || status > 299 {
		return &APIError{
			HTTPStatus:  status,
			Code:        payload.ErrorCode,
			Description: payload.Description,
			cause:       errContradictoryStatus,
		}
	}

	return nil
}
