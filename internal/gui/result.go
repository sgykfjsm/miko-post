package gui

import (
	"errors"
	"strings"

	"github.com/sgykfjsm/miko-post/internal/post"
)

func correction(err error) string {
	switch {
	case errors.Is(err, post.ErrEmptyMessage):
		return "Type a message before sending."
	case errors.Is(err, post.ErrInvalidUTF8):
		return "The message contains invalid UTF-8. Replace it with valid text and try again."
	default:
		return "The message could not be submitted."
	}
}

// Only the core's display fields belong here; Err is exclusively diagnostic.
func resultText(outcome post.Outcome, logPath string) string {
	var lines []string
	for _, result := range outcome.Results {
		if result.Success {
			lines = append(lines, result.Name+": sent")
		} else {
			lines = append(lines, result.Name+": "+result.Reason)
		}
	}
	if !outcome.Succeeded() {
		lines = append(lines, "Details: "+logPath)
	}
	return strings.Join(lines, "\n")
}
