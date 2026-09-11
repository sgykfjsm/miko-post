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
| `level` | `"info"` \| `"error"` — lowercase; slog's own `INFO`/`ERROR` is renamed | always |
| `event` | stable name above | always |
| `source` | `"cli"` \| `"gui"` | always |
| `message_id` | ULID | always (per-post correlation, R-007) |
| `sink` | `"telegram"` \| `"obsidian"` | sink events |
| `duration_ms` | int | completion events |
| `error_type` | classified string | sink failure events |
| `error` | detailed message | sink failure events; terminal records only for sink-identity diagnostics (DEC-E3) |
| `http_status` | int | Telegram failures with a response |
| `path` | string | note events (FR-066) |
| `message` | string | see message-capture rule |
| `message_len` / `message_bytes` | int | rune count / byte count (R-010) |
| `app_version`, `git_commit` | string | when enabled (FR-066) |
| `stack` | string | when a trace is available and useful (FR-071) |

The terminal event reports the aggregate outcome and elapsed time. It does not repeat
sink errors or carry an aggregate `error_type`; consumers join the sink failure records
by `message_id` for those details. Under DEC-E3, either terminal event may carry `error`
when a sink's name panicked or has no registered event vocabulary. That diagnostic does
not change the terminal event's name or level, which still reflects delivery outcomes.

## What is emitted as of T040, and what is still owed

T040 emits `message_received`, the six sink lifecycle events, and the two terminal events. Three
entries in the table above are not yet at their final state, and each is recorded here so that a
missing field reads as a scheduled gap rather than as a defect.

| Field | State after T040 | Owner of the rest |
|---|---|---|
| `error_type` | `timeout`, `permission_denied`, `chat_not_found`, `unauthorized`, `rate_limited`, or generic `failed`, selected with the display reason by one classifier | **T056 / Batch 8** adds specific categories; `timeout` and the generic fallback retain their meaning |
| `message` | **Not emitted at all.** FR-068's capture rule needs `message_on_error_only` and the post's outcome, so no record carries the body yet — including a failure, where SC-008 wants it | **T071** |
| `stack` | Not emitted. A recovered panic from a sink's `Name` is reported as text on the terminal record (issue #110), which keeps the value rather than the trace | **T073** |

`message_len` and `message_bytes` **are** emitted, on `message_received`, as R-010 defines them.

The three formatting-fallback names are not produced by any code path yet (**T063**). That is
asserted rather than assumed: `internal/app`'s recorder test compares the set of names the adapter
can produce against `logging.AllEvents()` and lists exactly those three as deferred, so a name added
to the vocabulary and never mapped fails the build rather than becoming a query that silently
returns nothing.

`path` is on the three `obsidian_append_*` events and on no others. The sink reports the note it
resolved through `post.ReportTarget` from inside `Send`, before any I/O, and the orchestrator holds
that sink's start event until the report arrives — so all three records name the file the sink
actually opened, including a failed append (decision DEC-D3, issues #98 and #111).

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


## Diagnostic storage stalls (Batch 6c-2 review correction)

The production recorder admits records to one ordered worker per logger, with at most
256 pending records. Timestamps describe admission time. Delivery does not wait for
log storage. `Flush`, including the flush performed by `Degraded`, and `Close` each
wait at most 250 ms; a full queue or an expired wait permanently disables further
records for that logger and surfaces through the front door's single diagnostics warning.
Normal write errors retain the logger's existing recovery behavior.

Normal shutdown drains the queue before reporting results. On overload or a stall,
pending records are discarded: preserving delivery takes precedence over diagnostics
(FR-076). An in-flight write cannot be cancelled and may land late; no pending records
follow it after the queue is disabled. The one worker retains its handle until that
operation returns and cleanup completes. A permanently stalled operation therefore
retains one worker/handle per logger, not one per post. A process crash can lose queued
records. Reopening the logger starts a fresh queue; there is no automatic replay.
