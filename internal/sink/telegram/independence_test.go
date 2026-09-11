package telegram_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/cli"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// Real HTTP and filesystem sinks, shared core and disk logger, through the CLI
// renderer. No live Telegram or user vault is contacted.
func TestRealSinksRemainIndependent(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		noteFails, chatFails, hang bool
	}{
		{"both succeed", false, false, false},
		{"chat fails", false, true, false},
		{"note fails", true, false, false},
		{"both fail", true, true, false},
		{"chat timeout", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := config.Defaults()
			settings.Sink.Obsidian.DailyNoteDir = t.TempDir()
			settings.Sink.Obsidian.FilenameFormat = "note.md"
			settings.Sink.Obsidian.CreateIfMissing = !tc.noteFails
			settings.Sink.Telegram.BotToken = config.NewSecret("SENTINEL-TOKEN")
			settings.Sink.Telegram.ChatID = "test-chat"
			settings.Sink.Telegram.Enabled = true
			settings.Logging.Path = filepath.Join(t.TempDir(), "diagnostic.jsonl")
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := r.ParseForm(); err != nil || r.Form.Get("text") != "preserve this thought" {
					t.Error("message did not arrive intact")
				}
				if tc.hang {
					<-r.Context().Done()
					return
				}
				if tc.chatFails {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
					return
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer server.Close()
			logger := app.OpenLogger(settings, logging.SourceCLI)
			defer logger.Close()
			limit := 2 * time.Second
			if tc.hang {
				limit = 150 * time.Millisecond
			}
			start := time.Now()
			outcome := post.New([]post.Sink{obsidian.New(settings.Sink.Obsidian), telegram.NewWithBaseURL(settings.Sink.Telegram, server.URL)}, limit, app.NewRecording(logger)).Post(post.Message{Original: "preserve this thought"})
			elapsed := time.Since(start)
			if err := logger.Close(); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 || len(outcome.Results) != 2 {
				t.Fatal("a sink was skipped or retried")
			}
			if outcome.Results[0].Success == tc.noteFails || outcome.Results[1].Success == (tc.chatFails || tc.hang) {
				t.Fatalf("lost real outcomes: %v", outcome.Results)
			}
			if outcome.Succeeded() != (!tc.noteFails && !tc.chatFails && !tc.hang) {
				t.Fatal("wrong aggregate status")
			}
			if tc.hang {
				if outcome.Results[1].Reason != "request timed out" || elapsed < limit || elapsed > limit+750*time.Millisecond {
					t.Fatalf("timeout result/bound: %v after %v", outcome.Results, elapsed)
				}
				t.Logf("real HTTP timeout: limit=%v elapsed=%v; note delivered", limit, elapsed)
			}
			if !tc.noteFails {
				data, err := os.ReadFile(filepath.Join(settings.Sink.Obsidian.DailyNoteDir, "note.md"))
				if err != nil || !bytes.Contains(data, []byte("preserve this thought")) {
					t.Fatalf("healthy note lost: %s %v", data, err)
				}
			}
			var out, errOut bytes.Buffer
			cli.Render(&out, &errOut, cli.Report{Results: outcome.Results, LogPath: logger.Path()})
			for i, name := range []string{"Obsidian", "Telegram"} {
				word := "success"
				if !outcome.Results[i].Success {
					word = "failed — " + outcome.Results[i].Reason
				}
				if !strings.Contains(out.String(), name+": "+word) {
					t.Fatalf("missing result: %s", out.String())
				}
			}
			if strings.Contains(out.String(), settings.Logging.Path) == outcome.Succeeded() {
				t.Fatal("failure log path visibility wrong")
			}
			data, err := os.ReadFile(settings.Logging.Path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("SENTINEL-TOKEN")) || strings.Contains(out.String(), "SENTINEL-TOKEN") {
				t.Fatal("credential leaked")
			}
			counts := map[string]int{}
			for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
				var record map[string]any
				if err := json.Unmarshal(line, &record); err != nil {
					t.Fatal(err)
				}
				counts[record["event"].(string)]++
				if record["message_id"] != outcome.ID.String() {
					t.Fatal("failure lost correlation")
				}
			}
			for i, prefix := range []string{"obsidian_append_", "telegram_send_"} {
				suffix := "succeeded"
				if !outcome.Results[i].Success {
					suffix = "failed"
				}
				if counts[prefix+suffix] != 1 {
					t.Fatalf("terminal event absent/duplicated: %v", counts)
				}
			}
		})
	}
}
