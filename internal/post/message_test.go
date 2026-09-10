package post

import (
	"errors"
	"strconv"
	"testing"
)

// The whitespace matrix the spec enumerates, exercised alone and in mixtures.
// U+3000 is the ideographic space; a Japanese IME produces it routinely, and it
// is the one form a naive " \t\n" check misses.
func TestMessageValidateRejectsWhitespaceOnly(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "single ASCII space", input: " "},
		{name: "several ASCII spaces", input: "   "},
		{name: "single full-width space", input: "　"},
		{name: "several full-width spaces", input: "　　　"},
		{name: "tabs", input: "\t\t"},
		{name: "line feeds", input: "\n\n"},
		{name: "carriage returns", input: "\r\r"},
		{name: "CRLF line breaks", input: "\r\n\r\n"},
		{name: "vertical tab and form feed", input: "\v\f"},
		{name: "ASCII and full-width spaces mixed", input: " 　 　"},
		{name: "every form mixed", input: " \t　\r\n\v\f "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Message{Original: tt.input}.Validate()
			if !errors.Is(err, ErrEmptyMessage) {
				t.Fatalf("Validate() = %v, want %v", err, ErrEmptyMessage)
			}
		})
	}
}

// Each case carries whitespace that a trimming implementation would strip. The
// test asserts both halves of the rule that is easiest to break: the message is
// accepted (FR-009) *and* Original still holds every byte the user typed
// (FR-011).
func TestMessageValidateAcceptsAndLeavesTextUntrimmed(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "no surrounding whitespace", input: "hello"},
		{name: "leading ASCII space", input: "  hello"},
		{name: "trailing ASCII space", input: "hello  "},
		{name: "surrounding ASCII space", input: "  hello  "},
		{name: "surrounding full-width space", input: "　hello　"},
		{name: "surrounding tabs and line breaks", input: "\t\nhello\n\t"},
		{name: "trailing newline only", input: "hello\n"},
		{name: "internal whitespace preserved", input: "  hello\n\tworld  "},
		{name: "japanese text", input: "　こんにちは　"},
		{name: "emoji", input: " \U0001F363 "},
		{name: "whitespace-surrounded punctuation", input: "  -  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := Message{Original: tt.input}

			if err := message.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}

			if message.Original != tt.input {
				t.Errorf("Original = %q, want %q — validation must not trim", message.Original, tt.input)
			}
		})
	}
}

// invalidUTF8 is the byte-sequence matrix Validate must refuse (decision
// DEC-D4, issue #104).
//
// Every case is a shape a real terminal can produce, not a random byte. The
// truncated multi-byte sequences are what a mis-decoded paste or a cut buffer
// yields; the lone continuation byte and 0xFF are what a Shift_JIS or EUC-JP
// locale delivers for ordinary Japanese text; the surrogate half is what a
// UTF-16 source produces when it is copied through as UTF-8.
var invalidUTF8 = []struct {
	name  string
	input string
}{
	{name: "a lone 0xFF", input: "a\xffb"},
	{name: "a bare continuation byte", input: "a\x80b"},
	{name: "a truncated two-byte sequence", input: "a\xc3"},
	{name: "a truncated three-byte sequence", input: "\xe3\x81"},
	{name: "a truncated four-byte sequence", input: "\xf0\x9f\x8d"},
	{name: "an encoded UTF-16 surrogate half", input: "\xed\xa0\x80"},
	{name: "an overlong encoding of the ASCII slash", input: "\xc0\xaf"},
	{name: "invalid bytes surrounded by valid text", input: "hello \xff\xfe world"},
	{name: "invalid bytes surrounded by whitespace", input: " \xff "},
	{name: "a Shift_JIS-encoded Japanese word", input: "\x93\xfa\x96\x7b\x8c\xea"},
}

// TestMessageValidateRejectsInvalidUTF8 is decision DEC-D4's guard.
//
// The assertion is on the sentinel and not merely on "an error", because the
// only way a front door can render the right correction prompt is by matching
// this exact value with errors.Is — a test satisfied by any non-nil error would
// pass a guard that returned ErrEmptyMessage for a mis-encoded message and told
// the user to type something.
func TestMessageValidateRejectsInvalidUTF8(t *testing.T) {
	for _, tt := range invalidUTF8 {
		t.Run(tt.name, func(t *testing.T) {
			err := Message{Original: tt.input}.Validate()

			if !errors.Is(err, ErrInvalidUTF8) {
				t.Fatalf("Validate() = %v, want %v", err, ErrInvalidUTF8)
			}
		})
	}
}

// TestValidateReportsOneSentinelPerRejectionReason is issue #104's ordering
// acceptance: each rejection reason produces its own sentinel, and neither
// reason is ever reported for the other.
//
// Both directions matter and for different failures. A blank message answering
// ErrInvalidUTF8 would tell a user who pressed enter on an empty prompt to
// change their locale. A mis-encoded message answering ErrEmptyMessage would
// tell a user staring at a screen full of text that they typed nothing. The
// rules are mutually exclusive — an invalid byte is not whitespace, so it
// survives TrimSpace and the trimmed result is never empty — and this pins that
// property rather than trusting the reading of it.
func TestValidateReportsOneSentinelPerRejectionReason(t *testing.T) {
	blank := []string{"", "   ", "\t\n", "　", " \t　\r\n\v\f "}

	for _, input := range blank {
		t.Run("blank/"+strconv.Quote(input), func(t *testing.T) {
			err := Message{Original: input}.Validate()

			if !errors.Is(err, ErrEmptyMessage) {
				t.Errorf("Validate() = %v, want %v", err, ErrEmptyMessage)
			}

			if errors.Is(err, ErrInvalidUTF8) {
				t.Errorf("a blank message was rejected as invalid UTF-8: %v", err)
			}
		})
	}

	for _, tt := range invalidUTF8 {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			err := Message{Original: tt.input}.Validate()

			if !errors.Is(err, ErrInvalidUTF8) {
				t.Errorf("Validate() = %v, want %v", err, ErrInvalidUTF8)
			}

			if errors.Is(err, ErrEmptyMessage) {
				t.Errorf("a mis-encoded message was rejected as blank: %v", err)
			}
		})
	}
}

// TestMessageValidateAcceptsValidUTF8ThatLooksSuspect is the other half of
// DEC-D4's guard, and the half that a careless implementation fails.
//
// A range loop over a string yields utf8.RuneError for an invalid byte *and*
// for a correctly encoded U+FFFD, so a guard written that way refuses text the
// user is entitled to send — including the replacement character itself, which
// is exactly what a user pastes when they are copying out of a document that
// was already mangled. The other cases are valid sequences that a hand-written
// validator tends to get wrong: the maximum four-byte form, a combining mark, an
// RTL override, and a NUL, which is a legal UTF-8 code point whatever else one
// might think of it.
func TestMessageValidateAcceptsValidUTF8ThatLooksSuspect(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "an encoded replacement character", input: "a�b"},
		{name: "only replacement characters", input: "��"},
		{name: "the maximum four-byte code point", input: "\U0010FFFF"},
		{name: "an emoji with a variation selector", input: "\U0001F363️"},
		{name: "a combining mark", input: "é"},
		{name: "a right-to-left override", input: "a‮b"},
		{name: "an embedded NUL", input: "a\x00b"},
		{name: "japanese text in UTF-8", input: "日本語"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := Message{Original: tt.input}

			if err := message.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}

			if message.Original != tt.input {
				t.Errorf("Original = %q, want %q", message.Original, tt.input)
			}
		})
	}
}

// TestTheTwoSentinelsAreDistinct guards the shape of the sentinels themselves.
//
// errors.Is is reflexive over a shared value, so declaring ErrInvalidUTF8 as
// ErrEmptyMessage — or wrapping either in the other — would make every
// errors.Is assertion above pass while the two reasons became one. That is the
// mutation the ordering test cannot see, because it only ever asks whether a
// match holds.
func TestTheTwoSentinelsAreDistinct(t *testing.T) {
	if errors.Is(ErrEmptyMessage, ErrInvalidUTF8) || errors.Is(ErrInvalidUTF8, ErrEmptyMessage) {
		t.Fatalf("the two rejection sentinels match each other: %v / %v",
			ErrEmptyMessage, ErrInvalidUTF8)
	}

	if ErrEmptyMessage.Error() == ErrInvalidUTF8.Error() {
		t.Errorf("the two sentinels render identically: %q", ErrEmptyMessage.Error())
	}
}
