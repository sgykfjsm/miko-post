package obsidian_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
)

// Keep the real sink and its real target report; pause A at its first context
// check, after it has reported its note but before it opens that note.
type overlappingNoteSink struct {
	*obsidian.Sink
	aReported, bCompleted chan struct{}
}

func (s *overlappingNoteSink) Send(ctx context.Context, message post.Message) error {
	if message.Original == "post A" {
		first := true
		ctx = observingContext{Context: ctx, observe: func() {
			if first {
				first = false
				close(s.aReported)
				<-s.bCompleted
			}
		}}
	}
	return s.Sink.Send(ctx, message)
}

func TestOverlappingServicePostsLogTheirOwnRealNotes(t *testing.T) {
	for _, failB := range []bool{false, true} {
		name := "both notes written"
		if failB {
			name = "second note refused"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			before := time.Date(2026, 9, 10, 23, 59, 59, 0, time.Local)
			after := before.Add(2 * time.Second)
			notes := []string{notePath(dir, before), notePath(dir, after)}
			if notes[0] == notes[1] {
				t.Fatal("clock dates must resolve distinct notes")
			}
			settings := settingsFor(dir)
			if failB {
				settings.CreateIfMissing = false
				if err := os.WriteFile(notes[0], nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var reads atomic.Int32
			realSink := obsidian.NewWithClock(settings, func() time.Time {
				if reads.Add(1) == 1 {
					return before
				}
				return after
			})
			sink := &overlappingNoteSink{Sink: realSink, aReported: make(chan struct{}), bCompleted: make(chan struct{})}
			logPath := filepath.Join(t.TempDir(), "events.jsonl")
			logger := logging.Open(logging.Options{Source: logging.SourceCLI, Path: logPath})
			defer logger.Close()
			service := post.New([]post.Sink{sink}, 5*time.Second, app.NewRecording(logger))
			aDone := make(chan post.Outcome, 1)
			go func() { aDone <- service.Post(post.Message{Original: "post A"}) }()
			select {
			case <-sink.aReported:
			case <-time.After(time.Second):
				close(sink.bCompleted)
				t.Fatal("A never reached the overlap window")
			}
			b := service.Post(post.Message{Original: "post B"})
			// B's complete post, including its terminal event, precedes A's
			// append. A getter of shared target state would now return B's note.
			close(sink.bCompleted)
			a := <-aDone
			if !a.Succeeded() || b.Succeeded() == failB {
				t.Fatalf("unexpected delivery: A=%+v B=%+v", a.Results, b.Results)
			}
			if a.ID == b.ID {
				t.Fatal("distinct posts share an identifier")
			}
			if reads.Load() != 2 {
				t.Fatalf("clock read %d times", reads.Load())
			}
			if err := logger.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			byID := map[string][]map[string]any{}
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("invalid JSONL: %v", err)
				}
				id, _ := record["message_id"].(string)
				byID[id] = append(byID[id], record)
			}
			if len(byID) != 2 {
				t.Fatalf("records belong to %d identifiers", len(byID))
			}
			for i, out := range []post.Outcome{a, b} {
				records := byID[out.ID.String()]
				finish, terminal := "obsidian_append_succeeded", "request_completed"
				if !out.Succeeded() {
					finish, terminal = "obsidian_append_failed", "request_completed_with_error"
				}
				want := []string{"message_received", "obsidian_append_started", finish, terminal}
				if len(records) != len(want) {
					t.Fatalf("%s: got %d records, want %d", out.ID, len(records), len(want))
				}
				for j, record := range records {
					if record["event"] != want[j] {
						t.Errorf("%s record %d event=%v, want %s", out.ID, j, record["event"], want[j])
					}
					if j == 1 || j == 2 {
						if record["path"] != notes[i] {
							t.Errorf("%s %s path=%v, want %s", out.ID, want[j], record["path"], notes[i])
						}
					}
				}
				if out.Succeeded() {
					content, err := os.ReadFile(notes[i])
					if err != nil || !strings.Contains(string(content), out.Message.Original) {
						t.Fatalf("%s not in its logged note: %q, %v", out.ID, content, err)
					}
				} else if _, err := os.Stat(notes[i]); !os.IsNotExist(err) {
					t.Fatalf("refused note exists or unexpected stat error: %v", err)
				}
			}
		})
	}
}
