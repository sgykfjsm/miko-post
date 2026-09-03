package post

import (
	"errors"
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
