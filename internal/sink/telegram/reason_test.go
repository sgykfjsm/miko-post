package telegram_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

type refusalSink struct{ err error }

func (s refusalSink) Name() string                             { return "telegram" }
func (s refusalSink) Send(context.Context, post.Message) error { return s.err }

func TestAPIRefusalClassification(t *testing.T) {
	type refusalCase struct {
		name         string
		status       int
		body         string
		reason, kind string
	}
	cases := []refusalCase{
		{"chat", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, "chat not found", "chat_not_found"},
		{"auth", 401, `{"ok":false,"error_code":401,"description":"SENTINEL"}`, "authentication failed", "unauthorized"},
		{"permission", 403, `{"ok":false,"error_code":403,"description":"SENTINEL"}`, "permission denied", "permission_denied"},
		{"rate", 429, `{"ok":false,"error_code":429,"description":"SENTINEL"}`, "rate limit exceeded", "rate_limited"},
		{"unknown wording", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found SENTINEL"}`, "delivery failed", "failed"},
		{"contradiction", 400, `{"ok":true,"error_code":400,"description":"Bad Request: chat not found"}`, "delivery failed", "failed"},
		{"mismatch", 500, `{"ok":false,"error_code":401}`, "delivery failed", "failed"},
		{"malformed", 401, `not json`, "delivery failed", "failed"},
		{"missing ok with credential", 401, `{"error_code":401,"description":"SENTINEL"}`, "delivery failed", "failed"},
		{"null ok with credential", 401, `{"ok":null,"error_code":401,"description":"SENTINEL"}`, "delivery failed", "failed"},
		{"null envelope", 401, `null`, "delivery failed", "failed"},
		{"server", 500, `{"ok":false,"error_code":500}`, "delivery failed", "failed"},
		{"formatting remains batch9", 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`, "delivery failed", "failed"},
	}
	// Every recognized code requires an explicit boolean refusal. Description
	// is optional: null and omission both mean no diagnostic text, so neither
	// can establish the description-dependent chat-not-found identity.
	for _, code := range []int{400, 401, 403, 429} {
		for _, ok := range []string{"", `"ok":null,`, `"ok":"false",`, `"ok":true,`} {
			cases = append(cases, refusalCase{fmt.Sprintf("invalid ok %d %s", code, ok), code,
				fmt.Sprintf(`{%s"error_code":%d,"description":"Bad Request: chat not found"}`, ok, code),
				"delivery failed", "failed"})
		}
		for _, errorCode := range []string{"", `"error_code":null,`} {
			cases = append(cases, refusalCase{fmt.Sprintf("missing code %d %s", code, errorCode), code,
				fmt.Sprintf(`{"ok":false,%s"description":"Bad Request: chat not found"}`, errorCode),
				"delivery failed", "failed"})
		}
		for _, description := range []string{"", `,"description":null`} {
			reason, kind := "delivery failed", "failed"
			switch code {
			case 401:
				reason, kind = "authentication failed", "unauthorized"
			case 403:
				reason, kind = "permission denied", "permission_denied"
			case 429:
				reason, kind = "rate limit exceeded", "rate_limited"
			}
			cases = append(cases, refusalCase{fmt.Sprintf("optional description %d %s", code, description), code,
				fmt.Sprintf(`{"ok":false,"error_code":%d%s}`, code, description), reason, kind})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", telegram.DecodeResponse(tc.status, []byte(tc.body), "SENTINEL"))
			o := post.New([]post.Sink{refusalSink{err}}, time.Second, nil).Post(post.Message{Original: "test"})
			r := o.Results[0]
			var apiErr *telegram.APIError
			if !errors.As(r.Err, &apiErr) || apiErr.HTTPStatus != tc.status {
				t.Fatalf("lost API diagnostic or HTTP status: %v", r.Err)
			}
			for _, rendered := range []string{r.Reason, apiErr.Description, fmt.Sprintf("%v %+v %#v", r.Err, r.Err, r.Err)} {
				if strings.Contains(rendered, "SENTINEL") {
					t.Fatalf("credential leaked: %s", rendered)
				}
			}
			if r.Success || r.Reason != tc.reason || post.ErrorType(r) != tc.kind || r.Err != err {
				t.Fatalf("wrong result: %v", r)
			}
		})
	}
	e := &telegram.APIError{HTTPStatus: 403, Code: 403}
	if !errors.Is(e, fs.ErrPermission) || errors.Is(e, errors.New("other")) {
		t.Fatal("sentinel matching is incorrect")
	}
	var nilError *telegram.APIError
	if nilError.Is(post.ErrUnauthorized) {
		t.Fatal("nil matches a refusal")
	}
}
