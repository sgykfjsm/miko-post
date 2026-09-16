package telegram

import (
	"errors"
	"fmt"
	"strings"
)

// isFormattingRejection fails closed on untrusted envelopes and HTTP failures
// unrelated to formatting, even if their bodies claim error_code 400.
func isFormattingRejection(err error) bool {
	var refusal *APIError
	return errors.As(err, &refusal) && refusal != nil && refusal.cause == nil &&
		refusal.HTTPStatus == 400 && refusal.Code == 400 && strings.Contains(refusal.Description, "can't parse entities")
}

// RescueError retains both failures. Classification follows the final attempt.
type RescueError struct{ Markdown, Plaintext error }

func (e *RescueError) Error() string {
	return fmt.Sprintf("MarkdownV2 attempt failed: %v; plain-text rescue failed: %v", e.Markdown, e.Plaintext)
}
func (e *RescueError) Unwrap() error { return e.Plaintext }
