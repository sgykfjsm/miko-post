package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// maxResponseBytes caps how much of a reply is read.
//
// A sendMessage reply is a few hundred bytes. What arrives instead, when
// something between here and Telegram goes wrong, is whatever a proxy or a
// captive portal felt like serving — an HTML error page of no particular size.
// The cap costs nothing on the real path and bounds the allocation one reply
// can cause. A 1 MiB sendMessage response is not a response we should be acting
// on, and readResponseBody is what makes that a refusal rather than a hope.
const maxResponseBytes = 1 << 20

// errResponseTooLarge reports a reply with more to give than the cap allows.
var errResponseTooLarge = errors.New("the reply exceeded the readable size limit")

// readResponseBody reads at most maxResponseBytes and refuses a reply that had
// more to send.
//
// The limit is read plus one byte, and that byte is the whole point. An earlier
// version read exactly the cap and this file claimed that "truncating a large
// body makes its JSON invalid, which fails closed by the same route as any
// other unparseable reply". Measured, that is false at the boundary: a body
// whose JSON document ends at byte maxResponseBytes exactly, followed by
// anything at all, truncates on a complete document, parses, and — for
// `{"ok":true,…}` under a 200 — reports the post delivered on a reply we did
// not finish reading. That is this package's one forbidden outcome arriving
// through the guard meant to prevent it.
//
// One extra byte separates "the reply was exactly this long" from "there was
// more", which truncation alone cannot distinguish. A reply of exactly the cap
// still succeeds; one byte past it fails, and fails as itself rather than as a
// decode error, because "the reply was too large to read" and "Telegram sent
// something malformed" are different things to put in front of a user.
func readResponseBody(body io.Reader) ([]byte, error) {
	read, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}

	if len(read) > maxResponseBytes {
		return nil, errResponseTooLarge
	}

	return read, nil
}

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
// They are necessary but not sufficient, and T061 has to be told so. `ok` is
// not one of them — it survives only as the absence of a cause — because two
// structurally different replies fill Code and Description from the same body:
// the genuine refusal (`ok: false`, no cause) and the contradiction (`ok: true`
// under a non-2xx, cause errContradictoryStatus; see decodeResponse). A
// predicate reading the three exported fields alone therefore fires on a reply
// that said `ok: true`, which R-008 requires to fail closed, and producing one
// takes nothing exotic — a proxy or captive portal answering 400 with
//
//	{"ok":true,"error_code":400,"description":"Bad Request: can't parse entities …"}
//
// is indistinguishable from a real formatting rejection through Code and
// Description alone. So T061 must additionally exclude
// errors.Is(err, errContradictoryStatus): unexported, but rescue.go will be in
// this package. That the two paths stay distinguishable is asserted by
// TestAContradictoryReplyIsNotAFormattingRejection rather than left to this
// comment.
//
// # FR-043 is a property of this struct
//
// Nothing on it carries the request URL: the URL is where the token lives, and
// Sink.Send strips it from every transport error before wrapping (see
// withoutRequestURL). Description is a different matter, and two earlier
// versions of this comment got it wrong in turn. The first said the fields come
// from a body "Telegram does not echo credentials into". They come from a body,
// and this same file spends two paragraphs on who else writes that body — a
// proxy rewriting a status, a captive portal answering for Telegram. Such a
// thing quoting the request it could not forward,
//
//	{"ok":true,"error_code":400,
//	 "description":"cannot proxy POST https://api.telegram.org/bot<token>/sendMessage"}
//
// puts the credential straight into Description.
//
// The second said that was fine because Sink.safe caught it at the boundary. It
// does not, and could not: safe rewrites what an error *prints*, and Description
// is exported. A caller holding the returned error is one errors.As from the
// struct itself, and the token was still in the field — measured, on both the
// ok:false and ok:true paths. That is not a hypothetical caller. T040 has to
// read HTTPStatus off this type to satisfy contracts/log-events.md, errors.As
// is the mechanism this comment recommends for it, and internal/post/result.go
// deliberately leaves SinkResult.Err unredacted for whatever assembles the log
// record. The supported path to the status was also the path to the credential.
//
// So the redaction happens here, at construction, before the string is ever
// stored: decodeResponse takes the token and scrubs it out of any body-derived
// text (see redactToken). Scrubbed, not dropped — Batch 9's T061 predicate
// reads Description for "can't parse entities", and a blanked field would
// answer the sweep while breaking the rescue. Sink.safe stays where it is as
// the net over renderings it cannot see inside; this is the one over the value.
// Asserted rather than assumed: the leak sweep in sink_test.go sends exactly
// that body on both the ok:true and ok:false paths, renders the result through
// %v, %+v and %#v, and then walks the chain with errors.As to check the fields
// no rendering of the outer error would reach.
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
// caller (readResponseBody) and the read error has to be distinguishable from a
// decode error: a connection cut mid-body is a transport failure, not Telegram
// saying something malformed.
//
// token is the credential to scrub out of the decoded description, and it is a
// parameter because this is a package function with no Sink to ask. Of the
// shapes available — take a scrubbing closure, take the token, or let Sink.Send
// clean the error afterwards — the token is chosen because it is required by
// the signature, so no future call site can construct an *APIError without
// deciding what to do about the credential, and because a nil closure would
// disarm the guard silently. The empty string is the unconfigured sink and is a
// no-op, matching Sink.safe. The remaining fields cannot carry a credential:
// two are ints, and the decode cause is an encoding/json error, whose messages
// quote at most one offending character of the body and never a run of it.
func decodeResponse(status int, body []byte, token string) error {
	var payload apiResponse

	if err := json.Unmarshal(body, &payload); err != nil {
		return &APIError{HTTPStatus: status, cause: err}
	}

	if !payload.OK {
		return &APIError{
			HTTPStatus:  status,
			Code:        payload.ErrorCode,
			Description: redactToken(payload.Description, token),
		}
	}

	if status < 200 || status > 299 {
		return &APIError{
			HTTPStatus:  status,
			Code:        payload.ErrorCode,
			Description: redactToken(payload.Description, token),
			cause:       errContradictoryStatus,
		}
	}

	return nil
}
