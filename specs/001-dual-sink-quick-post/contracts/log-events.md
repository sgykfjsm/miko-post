# Contract: Diagnostic log events (JSONL)

**Requirements**: FR-064 – FR-076. **Design**: `docs/design.md` §11.

Every line is one independently valid, self-contained JSON object (FR-064, SC-007).

## Stable event names (FR-067 — minimum set, normative)

```text
message_received
obsidian_append_started
obsidian_append_succeeded
obsidian_append_failed
telegram_send_started
telegram_send_succeeded
telegram_send_failed
telegram_markdown_failed
telegram_plaintext_succeeded
telegram_plaintext_failed
request_completed
request_completed_with_error
```

These are typed constants (R-005), and a test asserts the complete set so a rename or typo cannot
silently break after-the-fact correlation.

The formatting-fallback path is observable as a distinct sequence (FR-039):
`telegram_markdown_failed` → `telegram_plaintext_succeeded` | `telegram_plaintext_failed`.

## Fields (FR-066)

| Field | Type | Present |
|---|---|---|
| `ts` | RFC 3339 with time zone | always |
| `level` | `"info"` \| `"error"` | always |
| `event` | stable name above | always |
| `source` | `"cli"` \| `"gui"` | always |
| `message_id` | ULID | always (per-post correlation, R-007) |
| `sink` | `"telegram"` \| `"obsidian"` | sink events |
| `duration_ms` | int | completion events |
| `error_type` | classified string | failures |
| `error` | detailed message | failures |
| `http_status` | int | Telegram failures with a response |
| `path` | string | note events (FR-066) |
| `message` | string | see message-capture rule |
| `message_len` / `message_bytes` | int | rune count / byte count (R-010) |
| `app_version`, `git_commit` | string | when enabled (FR-066) |
| `stack` | string | when a trace is available and useful (FR-071) |

## Message-capture rule (FR-068)

With `message_on_error_only = true` (the default):

- Post fully succeeded → **no** `message` field in any of its records; `message_len` /
  `message_bytes` may still appear.
- **Any** enabled sink failed → the original message body is recorded, sufficient to reconstruct
  and re-send the post by hand (SC-008).

## Secrets (FR-069, FR-043)

The bot token MUST NEVER appear in any record. Enforced by the `Secret` type (data-model.md) and
by a dedicated sentinel-token test.

## Multiple failures (FR-070)

When several sinks fail in one post, **every** failure is logged. Logging never stops after the
first error.

## Traces (FR-071)

Recorded for panics, unexpected errors, and failures where a trace is available and useful,
subject to `stack_trace`. Expected operational errors — a timeout, a 401, a missing note with
`create_if_missing = false` — MUST NOT get an artificially manufactured trace.

## Rotation (FR-072 – FR-075)

Rotate when **either** condition holds, evaluated **before every write**:

```text
size >= rotate_size_mib            (default 10 MiB)
OR
now - <file creation time> >= rotate_after_days   (default 7 days)
```

Creation time is captured when the active file is opened (FR-072, R-006).

Rotation renames the active file by appending a local-time `YYYYMMDDhhmmss` suffix, then opens a
new active file (FR-073):

```text
app.jsonl -> app.jsonl.20260827114203
```

If that name already exists, append `-1`, `-2`, … rather than overwriting (R-006, A-009).

**Nothing is ever deleted, expired, or compressed** (FR-074, constitution principle VI).

## Unwritable diagnostics (FR-075, FR-076)

1. Create the log directory if missing.
2. If logging still cannot proceed: every enabled sink still runs and reports its **real**
   outcome, the user-visible output carries **exactly one** warning naming the log path and the
   reason, and the exit status reflects only the sink outcomes.

## Example record

```json
{"ts":"2026-08-27T11:42:03+09:00","level":"error","event":"telegram_send_failed","source":"cli","message_id":"01K...","sink":"telegram","message":"...","error_type":"timeout","error":"request timed out","duration_ms":10012,"app_version":"0.1.0","git_commit":"abc1234"}
```
