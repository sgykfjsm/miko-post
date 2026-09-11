package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
)

type rescueTransport func(*http.Request) (*http.Response, error)

func (f rescueTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRescueUsesRemainingOverallDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	sink := New(config.TelegramSettings{HTTPTimeoutSeconds: 30})
	calls := 0
	sink.client.Transport = rescueTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		// Compare the actual request deadline, independent of scheduler latency.
		// A fresh rescue budget or detached context must fail even if it times out.
		if got, ok := r.Context().Deadline(); !ok || !got.Equal(wantDeadline) {
			t.Errorf("attempt %d deadline = %v (%v), want original %v", calls, got, ok, wantDeadline)
		}
		if calls == 1 {
			return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":400,"description":"can't parse entities"}`))}, nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	err := sink.Send(ctx, post.Message{Original: "x"})
	if calls != 2 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
