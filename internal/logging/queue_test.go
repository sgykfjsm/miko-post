package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type queuedTestWriter struct {
	mu sync.Mutex
	bytes.Buffer
	entered, release chan struct{}
	calls, closes    int
}

func (w *queuedTestWriter) Write(p []byte) (int, error) {
	if w.entered != nil {
		w.mu.Lock()
		first := w.calls == 0
		w.calls++
		w.mu.Unlock()
		if first {
			close(w.entered)
			<-w.release
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(p)
}
func (w *queuedTestWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closes++
	return nil
}

func TestAsyncRecordsKeepAdmissionTimeAndOrder(t *testing.T) {
	w := &queuedTestWriter{entered: make(chan struct{}), release: make(chan struct{})}
	l := Open(Options{Writer: w, Source: SourceCLI})
	p := l.PostAsync("id")
	p.Info(EventMessageReceived, slog.Int("sequence", 0))
	<-w.entered
	for i := 1; i < 10; i++ {
		p.Info(EventTelegramSendStarted, slog.Int("sequence", i))
	}
	admittedBy := time.Now()
	close(w.release)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(w.String()), "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d records, want 10", len(lines))
	}
	for i, line := range lines {
		var r struct {
			TS       time.Time `json:"ts"`
			Sequence int       `json:"sequence"`
			ID       string    `json:"message_id"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.Sequence != i || r.ID != "id" || r.TS.After(admittedBy) {
			t.Errorf("record %d: %+v, admitted by %v", i, r, admittedBy)
		}
	}
}

func TestAsyncFailureDiscardsBacklogAndEventuallyClosesOwnedWriter(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "flush timeout"
		if full {
			name = "queue full"
		}
		t.Run(name, func(t *testing.T) {
			w := &queuedTestWriter{entered: make(chan struct{}), release: make(chan struct{})}
			l := Open(Options{Writer: w, Source: SourceCLI})
			// Simulate a destination opened by the logger, so cleanup after a
			// late write is observable without a stalled filesystem.
			l.writer.owned = w
			p := l.PostAsync("id")
			p.Info(EventMessageReceived)
			<-w.entered
			p.Info(EventTelegramSendStarted)
			wantErr := errRecordFlushTimeout
			if full {
				for i := 0; i < recordQueueCapacity; i++ {
					p.Info(EventTelegramSendStarted)
				}
				wantErr = errRecordQueueFull
			}
			if err := l.Flush(); !errors.Is(err, wantErr) {
				t.Fatalf("Flush=%v, want %v", err, wantErr)
			}
			if err := l.Close(); !errors.Is(err, wantErr) {
				t.Errorf("Close=%v", err)
			}
			if got := l.Degraded(); got == nil || !errors.Is(got.Err, wantErr) {
				t.Errorf("degradation=%+v", got)
			}
			p.Info(EventRequestCompleted)
			close(w.release)
			select {
			case <-l.queue.done:
			case <-time.After(time.Second):
				t.Fatal("worker did not clean up after late write")
			}
			if w.calls != 1 || w.closes != 1 {
				t.Errorf("writes=%d closes=%d, want one of each", w.calls, w.closes)
			}
			if got := strings.Count(w.String(), "\n"); got != 1 {
				t.Errorf("late write followed by %d records", got)
			}
			if err := l.Close(); !errors.Is(err, wantErr) {
				t.Errorf("second Close lost degradation: %v", err)
			}
			if w.closes != 1 {
				t.Error("owned writer closed twice")
			}
		})
	}
}

func TestAsyncCloseHasABoundEvenWhenTheHandleCloseStalls(t *testing.T) {
	release := make(chan struct{})
	q := newRecordQueue(func() error { <-release; return nil })
	if err := q.close(); !errors.Is(err, errRecordFlushTimeout) {
		t.Errorf("Close=%v", err)
	}
	close(release)
	select {
	case <-q.done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
}
