package telegram_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

const formattingBody = `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: x"}`

func TestRescuePreservesTextAndBothOutcomes(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, mode := range []string{config.ParseModeHTML, config.ParseModeMarkdown, config.ParseModeMarkdownV2, ""} {
			for _, succeeds := range []bool{false, true} {
				settings := baseSettings()
				thread := int64(42)
				settings.ThreadID = &thread
				settings.ParseMode = mode
				settings.FallbackToPlainText = fallback
				count := 0
				server, rec := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
					count++
					if count == 1 {
						reply(400, formattingBody)(w, r)
						return
					}
					if succeeds {
						reply(200, okBody)(w, r)
					} else {
						reply(403, `{"ok":false,"error_code":403,"description":"Forbidden"}`)(w, r)
					}
				})
				sink := telegram.NewWithBaseURL(settings, server.URL)
				text := "  日本語 *x &+%\nsecond line  "
				var attempts []post.FormattingAttempt
				ctx := post.WithFormattingReporter(context.Background(), func(a post.FormattingAttempt) { attempts = append(attempts, a) })
				err := sink.Send(ctx, mustMessage(t, text))
				if (err == nil) != succeeds {
					t.Fatalf("success %v: %v", succeeds, err)
				}
				requests := rec.requests()
				if len(requests) != 2 {
					t.Fatalf("requests=%d", len(requests))
				}
				for i, r := range requests {
					if r.form.Get(telegram.FieldText) != text || r.form.Get(telegram.FieldChatID) != settings.ChatID || r.form.Get(telegram.FieldThreadID) != "42" {
						t.Fatalf("request %d changed text or destination", i)
					}
				}
				if requests[0].form.Get(telegram.FieldParse) != config.ParseModeMarkdownV2 {
					t.Fatal("first mode")
				}
				if _, present := requests[1].form[telegram.FieldParse]; present {
					t.Fatal("rescue parse_mode present")
				}
				if len(attempts) != 2 || attempts[0].Plain || attempts[0].Err == nil || !attempts[1].Plain || (attempts[1].Err == nil) != succeeds {
					t.Fatalf("attempts=%+v", attempts)
				}
				if !succeeds {
					var both *telegram.RescueError
					if !errors.As(err, &both) || both.Markdown == nil || both.Plaintext == nil || !strings.Contains(err.Error(), "can't parse entities") || !strings.Contains(err.Error(), "Forbidden") {
						t.Fatalf("lost attempt: %v", err)
					}
				}
				server.Close()
			}
		}
	}
}

func TestRescueRetainsPerRequestTimeout(t *testing.T) {
	count := 0
	server, rec := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			reply(400, formattingBody)(w, r)
			return
		}
		<-r.Context().Done()
	})
	settings := baseSettings()
	settings.HTTPTimeoutSeconds = 1
	sink := telegram.NewWithBaseURL(settings, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	err := sink.Send(ctx, mustMessage(t, "x"))
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("request bound did not expire independently: err=%v overall=%v", err, ctx.Err())
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 3*time.Second {
		t.Fatalf("request timeout elapsed=%v", elapsed)
	}
	if len(rec.requests()) != 2 {
		t.Fatal("rescue not reached")
	}
}
