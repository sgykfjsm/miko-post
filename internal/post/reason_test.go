package post

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestClassificationPreservesDiagnostics(t *testing.T) {
	const secret = "SENTINEL-CREDENTIAL"
	cases := []struct {
		name         string
		err          error
		reason, kind string
	}{
		{"deadline", context.DeadlineExceeded, "request timed out", "timeout"},
		{"permission", &os.PathError{Op: "open", Path: secret, Err: fs.ErrPermission}, "permission denied", "permission_denied"},
		{"chat", ErrChatNotFound, "chat not found", "chat_not_found"},
		{"authentication", ErrUnauthorized, "authentication failed", "unauthorized"},
		{"rate", ErrRateLimited, "rate limit exceeded", "rate_limited"},
		{"unknown text", errors.New("permission denied chat not found " + secret), "delivery failed", "failed"},
		{"cancel", context.Canceled, "delivery failed", "failed"},
		{"deadline priority", errors.Join(fs.ErrPermission, context.DeadlineExceeded), "request timed out", "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := fmt.Errorf("%s: %w", secret, tc.err)
			outcome := New([]Sink{&fakeSink{name: "obsidian", send: func(context.Context, Message) error { return original }}}, generousTimeout, nil).Post(mustMessage(t, "test"))
			r := outcome.Results[0]
			if r.Success || r.Reason != tc.reason || ErrorType(r) != tc.kind || r.Err != original {
				t.Fatalf("incorrect classification or lost diagnostic: %v", r)
			}
			if !strings.Contains(r.Err.Error(), secret) {
				t.Fatal("diagnostic was discarded")
			}
			format := "%p"
			if strings.Contains(fmt.Sprintf(format, r), secret) {
				t.Fatal("pointer format exposed diagnostic")
			}
		})
	}
	if reasonFor(nil) != "delivery failed" || ErrorType(SinkResult{}) != "failed" {
		t.Fatal("nil failure was not generic")
	}
	if ErrorType(SinkResult{Success: true, Err: ErrUnauthorized}) != "" {
		t.Fatal("success incorrectly classified as failure")
	}
}
