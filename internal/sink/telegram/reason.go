package telegram

import (
	"io/fs"

	"github.com/sgykfjsm/miko-post/internal/post"
)

// Is maps a trusted refusal to the core's error identities without changing
// the detailed error. Unreadable and contradictory replies remain generic.
// Description matching is exact and only selects a constant; arbitrary remote
// text is never copied into a display reason. Unknown wording stays generic.
func (e *APIError) Is(target error) bool {
	if e == nil || e.cause != nil || e.HTTPStatus != e.Code {
		return false
	}
	switch target {
	case post.ErrChatNotFound:
		return e.Code == 400 && e.Description == "Bad Request: chat not found"
	case post.ErrUnauthorized:
		return e.Code == 401
	case fs.ErrPermission:
		return e.Code == 403
	case post.ErrRateLimited:
		return e.Code == 429
	default:
		return false
	}
}
