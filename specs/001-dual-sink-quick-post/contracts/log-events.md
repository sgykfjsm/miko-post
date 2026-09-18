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

## What is emitted as of T071-T074, and what is still owed

T040 emits `message_received`, the six sink lifecycle events, and the two terminal events. One
entry in the table above is not yet at its final state, and it is recorded here so that a missing
field reads as a scheduled gap rather than as a defect. The other two — `message` and `stack` —
were delivered by T071 and T073 and are described in their own sections below.

| Field | State after T040 | Owner of the rest |
|---|---|---|
| `error_type` | `timeout`, `permission_denied`, `chat_not_found`, `unauthorized`, `rate_limited`, or generic `failed`, selected with the display reason by one classifier | **T056 / Batch 8** adds specific categories; `timeout` and the generic fallback retain their meaning |
| `message` | ~~Not emitted at all.~~ **Delivered by T071**; see the message-capture rule below | — |
| `stack` | ~~Not emitted.~~ **Delivered by T073**; see the traces section below | — |

`message_len` and `message_bytes` **are** emitted, on `message_received`, as R-010 defines them.

T063 emits the three formatting-fallback events through a per-call context reporter and
an optional `post.FormattingRecorder` extension. They share the post ID and sink name,
carry each attempt's duration, and precede the overall Telegram outcome. Failures carry
`error_type`, `error`, and `http_status` when available. A failed rescue retains both
attempt errors, with classification and HTTP status following the final attempt. Late
reports after the orchestrator's terminal sink record are suppressed. The adapter's
vocabulary test now requires all registered names to be producible.

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

**Which record carries it (T071).** The body goes on **every record that reports a failure** — each
`*_failed` sink event and each formatting-fallback failure — because SC-008 asks for a post to be
re-sendable without consulting any other source, and those are the records that already name the
destination, the error type and the detail. One record, everything needed.

The terminal `request_completed_with_error` record carries it **only when no failure record could**.
That happens for a sink whose `Name` panics: it resolves to a sentinel the event vocabulary does not
cover (issue #110), so no lifecycle record is emitted and the terminal record is the post's only
record. Without the fallback FR-068 would be unmet for exactly that post; with it applied
unconditionally, a two-sink post that lost both destinations would carry the user's private text
three times.

With `message_on_error_only = false` the body is recorded on `message_received` instead — once, on
the post's intake event — and on no other record. FR-068 constrains only the enabled case, so this
is a choice: putting it on every record would repeat the body once per destination for no
reconstruction benefit.

The captured body passes through the same `Options.Redact` scrub as every other field, so a message
that happens to contain the bot token is redacted rather than leaked (FR-069).

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

**How the prohibition is kept (T073).** The stack is captured by `debug.Stack()` inside the deferred
`recover` in `internal/post`, which is the last moment the frames still exist — `recover()` returns
the panic value and nothing else. The error then carries it, and `post.Traced` is the only way to
ask for it. An expected operational error does not implement `Traced`, so there is no code path that
could manufacture a plausible stack for one: the absence of a trace in the record is the absence of
a trace in the error.

`stack` appears on the sink's `*_failed` record for a panic in `Send`, and on the terminal record for
a panic in `Name`, which has no lifecycle record to ride on. Two panics in one post keep the first
trace: a second full goroutine dump would be kilobytes of near-identical frames, and the `error`
field already records that both happened. Capture is unconditional and cheap; `stack_trace` governs
whether the trace is *recorded*.

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
