# Phase 1 Data Model: miko-post v0.1

**Date**: 2026-09-01 | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md)

Entities are derived from the spec's **Key Entities** section and its functional requirements.
Field types are Go types; validation rules cite the requirement that imposes them.

---

## Message

The user's text. Owned by `internal/post`.

| Field | Type | Notes |
|---|---|---|
| `Original` | `string` | Exactly what the user entered. This is what every sink receives (FR-011, FR-012). |

**Validation** (FR-009, FR-010): `strings.TrimSpace` uses `unicode.IsSpace`, which covers ASCII
space, tab, LF, CR, and U+3000 ideographic space — the full matrix the spec enumerates. If the
trimmed result is empty the message is invalid. **The trimmed value is never stored and never
sent**; it exists only as an intermediate in the validation predicate. This is the single most
easily broken rule in the feature (FR-011 vs. FR-009), so validation returns only a `bool`/`error`
and never a string, making it structurally impossible to send the trimmed form by accident.

**Validation, second rule** (FR-009a, decision DEC-D4, issue #104): a message carrying bytes that
are not a valid UTF-8 encoding is invalid. The reading of FR-050 this rests on, and the evidence
behind it, are recorded in `spec.md` beside FR-009a; the short form is that no destination keeps
those bytes — the chat service refuses them, the JSON Lines log substitutes U+FFFD, and the note is
re-serialised as UTF-8 at the user's next save — so accepting them produced a half-delivered post
rather than the verbatim delivery FR-012 promises.

Two sentinels, not one: `ErrEmptyMessage` and `ErrInvalidUTF8`, both matched by both front doors
with `errors.Is`. They are distinct values because the two reasons have different fixes and a shared
"invalid message" would tell a user with a mis-encoded terminal to try typing something.

The two rules cannot both fire, and that is a property of trimming rather than of their order:
`strings.TrimSpace` removes only runes for which `unicode.IsSpace` holds, and a byte that is not
valid UTF-8 decodes to `utf8.RuneError` with a width of one and is not a space — so it survives
trimming and the trimmed result is never empty. The blank check is written first because FR-009 is
the rule the spec states.

The check is `utf8.ValidString` and deliberately not a range loop over the string: a range loop
yields `utf8.RuneError` both for an invalid byte and for a correctly encoded U+FFFD, so the loop
form would refuse text the user is entitled to send.

Note the scope: this is a rule about what a **front door accepts**. `Sink.Send` takes a `Message`
and a `Message` is a struct literal any caller can build, so the sinks do not assume validation ran
— `contracts/telegram-sink.md` records that the byte-preserving form encoding is kept as defence in
depth for exactly that reason.

**Derived, for diagnostics only** (R-010, FR-068): rune count and byte length.

---

## Post

One user submission. Owned by `internal/post`, where it is the type `Outcome`.

Named `Outcome` in code because `post.Post` stutters and because the type records
what a submission produced rather than the submission itself — `Service.Post`
performs it. Amended in the orchestrator batch (T025).

| Field | Type | Notes |
|---|---|---|
| `ID` | `ulid.ULID` | Per-post correlation identifier (FR-066, SC-007). Generated once at submission. |
| `Message` | `Message` | |
| `Results` | `[]SinkResult` | One per **enabled** sink (FR-016), in the order the sinks were given. |

A `Source` field was specified here and is deliberately **not** implemented. The
front door that knows the source does not read it back off this type, and the
log record's `source` comes from `logging.Options`, so the field would be state
nothing reads. Amended in T025.

**Aggregate rule** (FR-059, FR-061): the post succeeds only when every element of `Results` has
`Success == true`. A chat delivery rescued by the unformatted retry sets `Success == true`, so it
does not affect the exit status.

**Lifecycle**: `Validate → StartAll (concurrent) → AwaitAll → Aggregate`. `AwaitAll` waits for
every started sink (FR-014); it never returns on first error and never cancels a sibling
(constitution principle I).

`Validate` is the **caller's** step, not the orchestrator's: a whitespace-only submission is not a
post that failed but one that must never start, and the user needs FR-011's message from the front
door rather than a report saying every sink failed. `Service.Post` therefore requires an
already-validated message. Amended in T025.

---

## Sink

A named delivery target. Interface owned by `internal/post`; implementations in
`internal/sink/telegram` and `internal/sink/obsidian`.

| Member | Type | Notes |
|---|---|---|
| `Name()` | `string` | `"telegram"` / `"obsidian"`. Appears in results and log records. |
| `Send(ctx, Message)` | `error` | Bounded by the caller's context (FR-015). |

The orchestrator — not the sink — applies the per-sink overall timeout, converts a returned error
into a `SinkResult`, and emits the start/success/failure log events. This keeps the timeout,
result-shaping, and event vocabulary in one place for both sinks, per constitution principle II,
and means a third sink cannot accidentally acquire a different timeout or failure semantics.

**Disabled sinks are never constructed**, so they cannot be invoked and cannot appear in `Results`
(FR-016).

---

## SinkResult

Per-sink outcome. Owned by `internal/post`.

| Field | Type | Notes |
|---|---|---|
| `Name` | `string` | |
| `Success` | `bool` | |
| `Reason` | `string` | Short, safe, human-readable; **for display** (FR-017, FR-029). |
| `Err` | `error` | Detailed diagnostic; **for the log only** (FR-017). |
| `Duration` | `time.Duration` | Recorded as `duration_ms` (FR-066). |

**Invariant** (FR-029, FR-043, constitution principles III and IV): `Reason` is a
short classified phrase — `"request timed out"`, `"permission denied"`, `"chat not found"` — never
`Err.Error()`, never a wrapped error chain, and never anything derived from the credential. `Err`
is never rendered to the user. Enforced by a test that asserts `Reason` for every error class is
drawn from a fixed set.

---

## Settings

Validated configuration. Owned by `internal/config`. Loaded from one TOML file (FR-052) plus the
environment-supplied credential (FR-042). Section names `[sink.telegram]` and `[sink.obsidian]`
are normative (FR-054). See [contracts/config-schema.md](./contracts/config-schema.md) for the
full key list, types, defaults, and per-key validation rules.

**Load sequence**: resolve path → read → strict-decode → apply defaults → resolve the credential
from the environment → validate. Any failure aborts before a sink is constructed (FR-058).
Validation accumulates every problem and reports them together.

FR-018's **all-sinks-disabled check** is deliberately *not* part of `config.Load`. A document with
every sink disabled is a valid document, so `Load` accepts it; the check is a startup rule each
front door applies before any post is attempted — T081 for the CLI, spanning
`internal/config/validate.go` and `internal/cli/cli.go`, and T082 for the GUI, whose startup-error
window is where it surfaces (FR-030). T083 covers only routing a load or validation failure to the
front door that was used, not applying this rule. Sink construction (T036) and main wiring (T039)
therefore cannot assume `Load` already enforced it.

**Credential** (FR-042, FR-043, FR-069): held in a dedicated `Secret` type whose `String()`,
`GoString()`, `MarshalJSON()`, and `slog.LogValue()` all return a redaction marker. The real value
is reachable only through an explicit `Reveal()` method, called at **two** places as of Batch 6b —
building the Telegram request URL, and the Telegram sink's credential net, which cannot scan an
error for a string it has not been given (DEC-C2). What the type guarantees is that every route to
the value is explicit and greppable; the *count* of those routes is a **review obligation, not a
type invariant**, and a third caller may one day be as honest as the second.
`internal/config/secret.go` still says "exactly one place" and is owed the same correction —
deliberately not made in Batch 6b, whose boundary claim rests on `internal/config` being
byte-unchanged.

---

## DiagnosticRecord

One log line. Owned by `internal/logging`. Emitted as a single JSON object per line (FR-064).
See [contracts/log-events.md](./contracts/log-events.md) for the event vocabulary and field
matrix.

**Constant across every record**: `ts` (with time zone), `level`, `event`, `source`, `message_id`.
**Never present in any record**: the bot token (FR-069).

---

## DailyNote

The append-only per-day file. Owned by `internal/sink/obsidian`.

**Path** (FR-044): `daily_note_dir` joined with `time.Now().Format(filename_format)` in local time
(FR-051).

**Entry transformation** (FR-047, FR-048) — the order is normative and each step is separately
testable:

1. `\r\n` → `\n`, then bare `\r` → `\n`
2. every remaining `\n` → the literal `<br>`
3. prefix `"- " + time.Now().Format(time_format) + " "`
4. append the result followed by exactly one `\n`

Step 1 must precede step 2, or a `\r\n` pasted from another application yields `<br><br>`.

**Write rule** (FR-049, FR-050, constitution principle VI): `os.OpenFile` with
`O_APPEND|O_RDWR|O_NOFOLLOW`, plus `O_CREATE` when `create_if_missing` is true and without it when
that is false, so a missing note fails rather than being created (FR-046). Never `O_TRUNC`, never
read-modify-write. `O_RDWR` because the separator rule reads the note's last byte;
`O_NOFOLLOW`, with an `Lstat`, refuses a symlinked note path, and an `fstat` on the descriptor
refuses one that is not a regular file (amended in Batch 6a, see
`contracts/obsidian-sink.md`). A file not ending in a newline keeps every byte it has: one `\n` is
written *before* the entry so the entry starts its own physical line, which adds content and
rewrites none, as FR-049 requires.

---

## Entity relationships

```text
Settings ──constructs──> [enabled Sinks]
                              │
Message ──validated──> Post ──┼──> Sink.Send (concurrent, independent)
                          │   │
                          │   └──> SinkResult ──aggregated──> exit status
                          │
                          └──ID──> DiagnosticRecord (many, one per event)
```
