package post

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrEmptyMessage reports a message that is empty or consists only of
// whitespace. Both front doors match on it with errors.Is to show the user a
// correction prompt and exit non-zero without contacting a sink (FR-010).
var ErrEmptyMessage = errors.New("message is empty or contains only whitespace")

// ErrInvalidUTF8 reports a message carrying bytes that are not valid UTF-8
// (decision DEC-D4, issue #104). It is matched by both front doors with
// errors.Is, exactly as ErrEmptyMessage is, and routes through the same FR-010
// treatment: a correction prompt, no destination contacted, a failure exit.
//
// It is a second sentinel rather than a second phrasing of the first because
// the two say different things to the user — one says "you typed nothing", the
// other "your terminal handed us bytes we cannot carry" — and only a distinct
// value lets a front door, or a test, tell them apart.
var ErrInvalidUTF8 = errors.New("message contains bytes that are not valid UTF-8")

// Message is the text the user submitted.
//
// It holds the original text and nothing else. There is deliberately no field
// for a trimmed, normalized, or otherwise cleaned form: FR-011 requires every
// sink to receive exactly what the user typed, and the surest way to guarantee
// that is for no other form to exist.
type Message struct {
	// Original is exactly what the user entered, untrimmed (FR-011, FR-012).
	Original string
}

// Validate reports whether the message may be posted (FR-009, FR-010).
//
// A message is valid when trimming leading and trailing Unicode whitespace
// leaves something behind. strings.TrimSpace tests with unicode.IsSpace, which
// covers every form the spec enumerates — ASCII space, tab, LF, CR, and the
// U+3000 ideographic space — so the matrix does not have to be restated here
// and cannot drift from it.
//
// Validate returns only an error, never a string. The trimmed value exists as
// an intermediate inside this function and nowhere else, which makes sending
// the trimmed form structurally impossible rather than merely discouraged.
// This is the pairing (FR-009 validates on the trimmed text, FR-011 delivers
// the untrimmed text) most easily broken by a well-meaning refactor.
//
// # The UTF-8 rule (decision DEC-D4, issue #104)
//
// A message carrying bytes that are not valid UTF-8 is refused. FR-009
// enumerates only the blank rule, so this is a decision and not a derivation,
// and the reason it was taken is that the alternative's central promise — "we
// store what you typed" — is not true of any destination this program has.
// Telegram's documented contract is UTF-8 only and tdlib's clean_input_string
// answers 400 "Strings must be encoded in UTF-8", whose text does not match the
// formatting-rescue predicate, so the chat sink fails closed. The diagnostic log
// is JSON Lines, and JSON is UTF-8 by definition, so slog's handler substitutes
// U+FFFD silently — which makes SC-008 and FR-068 unsatisfiable for exactly
// these messages. Obsidian reads and writes notes as UTF-8 only, so the note's
// copy becomes U+FFFD at the user's next save of that file. The real status quo
// was therefore obsidian succeeds, telegram fails, exit 1: a half-delivered post,
// which is the outcome the two-sink design exists to prevent. Both readings cost
// the user a retry; this one tells them while their text is still in front of
// them.
//
// The accepted cost is recorded rather than hidden: a terminal that reliably
// produces non-UTF-8 bytes — a legacy Shift_JIS or EUC-JP locale is the
// realistic case — cannot use the CLI front door until its locale is fixed. Not
// a regression in outcome, since that user gets a half-delivered post today, but
// a real change for them.
//
// # Why the order of the two checks does not decide anything
//
// The two rejections are mutually exclusive, and that is a property of
// TrimSpace rather than an accident worth relying on quietly. Trimming removes
// only runes for which unicode.IsSpace holds; a byte that is not valid UTF-8
// decodes to utf8.RuneError with a width of one and is not a space, so it
// survives trimming and the trimmed result is non-empty. A message therefore
// cannot be both blank and invalid, and neither sentinel can be reported for a
// message the other rule owns. The blank check is written first because FR-009
// is the rule the spec states; TestValidateReportsOneSentinelPerRejectionReason
// pins both directions so a later reordering cannot change which reason a user
// is shown without failing.
//
// utf8.ValidString rather than a range loop over the string: a range loop
// yields utf8.RuneError for both an invalid byte and a legitimately encoded
// U+FFFD, so the loop form would refuse a message a user is entitled to send.
func (m Message) Validate() error {
	if strings.TrimSpace(m.Original) == "" {
		return ErrEmptyMessage
	}

	if !utf8.ValidString(m.Original) {
		return ErrInvalidUTF8
	}

	return nil
}
