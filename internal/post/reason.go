package post

import (
	"context"
	"errors"
	"io/fs"
)

// Sink-specific errors may match these identities through errors.Is. The core
// owns the vocabulary without importing a sink or parsing diagnostic prose.
// These are pointer-backed sentinels; classification never wraps or replaces Err.
var (
	ErrChatNotFound = errors.New("chat not found")
	ErrUnauthorized = errors.New("authentication failed")
	ErrRateLimited  = errors.New("rate limit exceeded")
)

const (
	reasonTimedOut   = "request timed out"
	reasonFailed     = "delivery failed"
	errorTypeTimeout = "timeout"
	errorTypeFailed  = "failed"
)

// classify selects constants only. Deadline wins when an error matches more
// than one category; unknown errors fail closed to an unspecific reason.
func classify(err error) (reason, kind string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return reasonTimedOut, errorTypeTimeout
	case errors.Is(err, fs.ErrPermission):
		return "permission denied", "permission_denied"
	case errors.Is(err, ErrChatNotFound):
		return "chat not found", "chat_not_found"
	case errors.Is(err, ErrUnauthorized):
		return "authentication failed", "unauthorized"
	case errors.Is(err, ErrRateLimited):
		return "rate limit exceeded", "rate_limited"
	default:
		return reasonFailed, errorTypeFailed
	}
}

func reasonFor(err error) string {
	reason, _ := classify(err)
	return reason
}

// ErrorType is the log classification of a failed result. Success is
// authoritative, including for caller-constructed results with a diagnostic.
func ErrorType(result SinkResult) string {
	if result.Success {
		return ""
	}
	_, kind := classify(result.Err)
	return kind
}
