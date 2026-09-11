# Developer guide

## Formatting rescue

`internal/sink/telegram` owns the formatting-only predicate and two-attempt delivery.
Both attempts use the original context and shared request builder. `RescueError`
retains both sanitized errors and unwraps the final one for classification/status.

Per-call formatting reports travel through context to an optional
`post.FormattingRecorder`, then the app adapter maps them to stable JSONL events.
The per-sink record serializes attempt and terminal events and suppresses late reports.
This preserves the posting core's independence from config, logging and sink packages.

Run `make check` and `make build`. Rescue tests include a deterministic original-deadline
assertion, real HTTP request-timeout and wire checks, and production Service/JSONL
integration. Keep the negative no-retry cases when changing formatting behavior.

## GUI background

Selection and bounded decoding live in `internal/gui/background.go`; one decoded image
belongs to one window. The content stack uses black, an aspect-preserving image, and
foreground widgets under a dark theme with a transparent input background.

Limit tests use otherwise-decodable images at and above both size boundaries. Mutations
removing either limit or changing its boundary must fail those tests. Native readability
checks should use temporary XDG config/state and a temporary vault, with Telegram disabled.

See [background contract](gui-background.md) and the [configuration schema](../specs/001-dual-sink-quick-post/contracts/config-schema.md).
