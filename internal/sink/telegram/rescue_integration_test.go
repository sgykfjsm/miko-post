package telegram_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// Keep this in telegram_test to use its test-only HTTP origin seam while
// exercising the production service and app JSONL adapter together.
func TestRescueHTTPServiceJSONLOutcome(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprint(succeeds), func(t *testing.T) {
			count := 0
			server, rec := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
				count++
				if count == 1 {
					reply(400, `{"ok":false,"error_code":400,"description":"can't parse entities `+sentinelToken+`"}`)(w, r)
				} else if succeeds {
					reply(200, okBody)(w, r)
				} else {
					reply(403, `{"ok":false,"error_code":403,"description":"Forbidden `+sentinelToken+`"}`)(w, r)
				}
			})
			var output bytes.Buffer
			logger := logging.Open(logging.Options{Writer: &output, Source: logging.SourceCLI})
			outcome := post.New([]post.Sink{telegram.NewWithBaseURL(baseSettings(), server.URL)}, 5*time.Second, app.NewRecording(logger)).Post(mustMessage(t, "日本語 *message"))
			results := outcome.Results
			if outcome.Succeeded() != succeeds {
				t.Fatalf("aggregate success=%v", outcome.Succeeded())
			}
			if err := logger.Close(); err != nil {
				t.Fatal(err)
			}
			if len(rec.requests()) != 2 || len(results) != 1 {
				t.Fatalf("requests=%d results=%d", len(rec.requests()), len(results))
			}
			if post.AllSucceeded(results) != succeeds || results[0].Success != succeeds {
				t.Fatalf("outcome=%v", results)
			}
			if succeeds {
				if results[0].Err != nil || results[0].Reason != "" || post.ErrorType(results[0]) != "" {
					t.Fatalf("success=%v", results[0])
				}
			} else {
				var both *telegram.RescueError
				if !errors.As(results[0].Err, &both) || both.Markdown == nil || both.Plaintext == nil {
					t.Fatalf("missing diagnostics: %v", results[0].Err)
				}
				var final *telegram.APIError
				if !errors.As(results[0].Err, &final) || final.HTTPStatus != 403 || results[0].Reason != "permission denied" || post.ErrorType(results[0]) != "permission_denied" {
					t.Fatalf("final classification=%v error=%v", results[0], results[0].Err)
				}
				for _, text := range []string{"can't parse entities", "Forbidden"} {
					if !strings.Contains(results[0].Err.Error(), text) {
						t.Fatalf("lost %q: %v", text, results[0].Err)
					}
				}
			}
			raw := output.String()
			if strings.Contains(raw, sentinelToken) {
				t.Fatal("credential leaked")
			}
			if !strings.HasSuffix(raw, "\n") {
				t.Fatal("unterminated JSONL")
			}
			lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
			plain, final, terminal := "telegram_plaintext_succeeded", "telegram_send_succeeded", "request_completed"
			if !succeeds {
				plain, final, terminal = "telegram_plaintext_failed", "telegram_send_failed", "request_completed_with_error"
			}
			want := []string{"message_received", "telegram_send_started", "telegram_markdown_failed", plain, final, terminal}
			if len(lines) != len(want) {
				t.Fatalf("JSONL=%s", raw)
			}
			records := make([]map[string]any, len(lines))
			for i, line := range lines {
				if err := json.Unmarshal([]byte(line), &records[i]); err != nil {
					t.Fatal(err)
				}
				r := records[i]
				if r["event"] != want[i] || r["source"] != "cli" || r["message_id"] == "" || r["message_id"] == nil || r["message_id"] != records[0]["message_id"] {
					t.Fatalf("record %d=%v", i, r)
				}
				if i >= 1 && i <= 4 && r["sink"] != "telegram" {
					t.Fatalf("sink=%v", r)
				}
				if i >= 2 && i <= 4 {
					if duration, ok := r["duration_ms"].(float64); !ok || duration < 0 {
						t.Fatalf("duration=%v", r)
					}
				}
			}
			if records[2]["http_status"] != float64(400) || records[2]["level"] != "error" || !strings.Contains(fmt.Sprint(records[2]["error"]), "can't parse entities") {
				t.Fatalf("markdown=%v", records[2])
			}
			for _, i := range []int{3, 4} {
				r := records[i]
				if succeeds {
					if r["level"] != "info" || r["error"] != nil || r["error_type"] != nil || r["http_status"] != nil {
						t.Fatalf("success fields=%v", r)
					}
				} else if r["level"] != "error" || r["http_status"] != float64(403) || r["error_type"] != "permission_denied" || !strings.Contains(fmt.Sprint(r["error"]), "Forbidden") {
					t.Fatalf("failure=%v", r)
				}
			}
			if !succeeds && !strings.Contains(fmt.Sprint(records[4]["error"]), "can't parse entities") {
				t.Fatalf("overall diagnostic=%v", records[4])
			}
		})
	}
}
