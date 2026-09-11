package app_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

type heldLogWriter struct {
	event            string
	entered, release chan struct{}
	once             sync.Once
}

func (w *heldLogWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), `"event":"`+w.event+`"`) {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return len(p), nil
}

type countedSink struct {
	name            string
	calls           atomic.Int32
	waitForDeadline bool
}

func (s *countedSink) Name() string { return s.name }
func (s *countedSink) Send(ctx context.Context, _ post.Message) error {
	s.calls.Add(1)
	if s.waitForDeadline {
		<-ctx.Done()
	}
	return ctx.Err()
}

func TestStalledDiagnosticsDoNotChangeDelivery(t *testing.T) {
	for _, event := range []string{"message_received", "telegram_send_started", "telegram_send_succeeded", "request_completed"} {
		t.Run(event, func(t *testing.T) {
			w := &heldLogWriter{event: event, entered: make(chan struct{}), release: make(chan struct{})}
			defer close(w.release)
			logger := logging.Open(logging.Options{Source: logging.SourceCLI, Path: "/diagnostics.jsonl", Writer: w})
			sinks := []*countedSink{{name: "obsidian"}, {name: "telegram"}}
			done := make(chan post.Outcome, 1)
			go func() {
				done <- post.New([]post.Sink{sinks[0], sinks[1]}, 50*time.Millisecond, app.NewRecording(logger)).Post(post.Message{Original: "healthy destinations"})
			}()
			select {
			case <-w.entered:
			case <-time.After(time.Second):
				t.Fatal("selected event never reached the real writer")
			}
			select {
			case result := <-done:
				if !result.Succeeded() || len(result.Results) != 2 {
					t.Fatalf("logging changed delivery: %+v", result.Results)
				}
			case <-time.After(time.Second):
				t.Fatal("Post waits for a diagnostic write")
			}
			for _, sink := range sinks {
				if sink.calls.Load() != 1 {
					t.Errorf("%s invoked %d times", sink.name, sink.calls.Load())
				}
			}
			closed := make(chan error, 1)
			go func() { closed <- logger.Close() }()
			select {
			case err := <-closed:
				if err == nil {
					t.Fatal("Close concealed the blocked diagnostic write")
				}
			case <-time.After(time.Second):
				t.Fatal("Close waits indefinitely for the writer")
			}
			degraded := logger.Degraded()
			if degraded == nil || !strings.Contains(degraded.Warning(), "/diagnostics.jsonl") {
				t.Fatalf("missing diagnostic degradation: %+v", degraded)
			}
		})
	}
}

func TestStalledDiagnosticsPreserveASinksOwnTimeout(t *testing.T) {
	w := &heldLogWriter{event: "telegram_send_started", entered: make(chan struct{}), release: make(chan struct{})}
	defer close(w.release)
	logger := logging.Open(logging.Options{Source: logging.SourceCLI, Writer: w})
	defer logger.Close()
	good := &countedSink{name: "obsidian"}
	slow := &countedSink{name: "telegram", waitForDeadline: true}
	done := make(chan post.Outcome, 1)
	go func() {
		done <- post.New([]post.Sink{good, slow}, 50*time.Millisecond, app.NewRecording(logger)).Post(post.Message{Original: "partial delivery"})
	}()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("start write was not blocked")
	}
	select {
	case out := <-done:
		if len(out.Results) != 2 || !out.Results[0].Success || out.Results[1].Success || post.ErrorType(out.Results[1]) != "timeout" {
			t.Fatalf("wrong outcomes: %+v", out.Results)
		}
		if good.calls.Load() != 1 || slow.calls.Load() != 1 {
			t.Fatal("both sinks must run")
		}
	case <-time.After(time.Second):
		t.Fatal("diagnostic mutex defeated the sink timeout")
	}
}
