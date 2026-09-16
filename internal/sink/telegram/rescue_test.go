package telegram

import (
	"errors"
	"testing"
)

func TestFormattingPredicate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"format", 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: x"}`, true},
		{"other400", 400, `{"ok":false,"error_code":400,"description":"chat not found"}`, false},
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"can't parse entities"}`, false},
		{"forbidden", 403, `{"ok":false,"error_code":403,"description":"can't parse entities"}`, false},
		{"rate", 429, `{"ok":false,"error_code":400,"description":"can't parse entities"}`, false},
		{"server", 500, `{"ok":false,"error_code":400,"description":"can't parse entities"}`, false},
		{"contradiction", 400, `{"ok":true,"error_code":400,"description":"can't parse entities"}`, false},
		{"missing", 400, `{"error_code":400,"description":"can't parse entities"}`, false},
		{"null", 400, `{"ok":null,"error_code":400,"description":"can't parse entities"}`, false},
		{"broken", 400, `{"ok":false,"error_code":400,"description":"can't parse entities"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFormattingRejection(decodeResponse(tc.status, []byte(tc.body), "")); got != tc.want {
				t.Fatalf("predicate = %v, want %v", got, tc.want)
			}
		})
	}
	if isFormattingRejection(errors.New("can't parse entities")) || isFormattingRejection(nil) {
		t.Fatal("non-API error rescued")
	}
}
