package post

import (
	"errors"
	"strings"
)

// ErrEmptyMessage reports a message that is empty or consists only of
// whitespace. Both front doors match on it with errors.Is to show the user a
// correction prompt and exit non-zero without contacting a sink (FR-010).
var ErrEmptyMessage = errors.New("message is empty or contains only whitespace")

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

// Validate reports whether the message may be posted (FR-009).
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
func (m Message) Validate() error {
	if strings.TrimSpace(m.Original) == "" {
		return ErrEmptyMessage
	}

	return nil
}
