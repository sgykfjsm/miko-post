package gui

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

type resultSink struct {
	name string
	err  error
}

func (s resultSink) Name() string                             { return s.name }
func (s resultSink) Send(context.Context, post.Message) error { return s.err }

func TestWindowReportsBothCoreOutcomes(t *testing.T) {
	for _, noteFails := range []bool{false, true} {
		for _, chatFails := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "note ok", true: "note failed"}[noteFails], map[bool]string{false: "chat ok", true: "chat failed"}[chatFails]}, "/"), func(t *testing.T) {
				var noteErr, chatErr error
				if noteFails {
					noteErr = errors.Join(fs.ErrPermission, errors.New("SENTINEL-DIAGNOSTIC"))
				}
				if chatFails {
					chatErr = &telegram.APIError{HTTPStatus: 400, Code: 400, Description: "Bad Request: chat not found"}
				}
				service := post.New([]post.Sink{resultSink{"obsidian", noteErr}, resultSink{"telegram", chatErr}}, time.Second, nil)
				h := setup(t, service.Post)
				h.w.entry.SetText("retain me")
				h.w.submit()
				h.drain(t)
				note := "obsidian: sent"
				if noteFails {
					note = "obsidian: permission denied"
				}
				chat := "telegram: sent"
				if chatFails {
					chat = "telegram: chat not found"
				}
				expected := note + "\n" + chat
				status := 0
				if noteFails || chatFails {
					expected += "\nDetails: " + h.w.logPath
					status = 1
				}
				if h.w.result.Text != expected || h.w.status != status {
					t.Fatalf("result=%q status=%d; want %q status=%d", h.w.result.Text, h.w.status, expected, status)
				}
				h.w.close()
				if h.w.status != status {
					t.Fatal("close lost exit status")
				}
			})
		}
	}
}
