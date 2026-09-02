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

**Derived, for diagnostics only** (R-010, FR-068): rune count and byte length.

---

## Post

One user submission. Owned by `internal/post`.

| Field | Type | Notes |
|---|---|---|
| `ID` | `ulid.ULID` | Per-post correlation identifier (FR-066, SC-007). Generated once at submission. |
| `Source` | `Source` | `SourceCLI` or `SourceGUI` (FR-066). |
| `Message` | `Message` | |
| `Results` | `[]SinkResult` | One per **enabled** sink (FR-016). |

**Aggregate rule** (FR-059, FR-061): the post succeeds only when every element of `Results` has
`Success == true`. A chat delivery rescued by the unformatted retry sets `Success == true`, so it
does not affect the exit status.

**Lifecycle**: `Validate → StartAll (concurrent) → AwaitAll → Aggregate`. `AwaitAll` waits for
every started sink (FR-014); it never returns on first error and never cancels a sibling
(constitution principle I).

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
from the environment → validate → **all-sinks-disabled check** (FR-018). Any failure aborts before
a sink is constructed (FR-058). Validation accumulates every problem and reports them together.

**Credential** (FR-042, FR-043, FR-069): held in a dedicated `Secret` type whose `String()`,
`GoString()`, `MarshalJSON()`, and `slog.LogValue()` all return a redaction marker. The real value
is reachable only through an explicit `Reveal()` method called at exactly one place — building the
Telegram request URL. This makes the "never appears anywhere" requirement a property of the type
rather than a rule every future call site must remember.

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
`O_APPEND|O_WRONLY|O_CREATE` when `create_if_missing` is true, and without `O_CREATE` when it is
false, so a missing note fails rather than being created (FR-046). Never `O_TRUNC`, never
read-modify-write. A file not ending in a newline is appended to as-is — the transformation adds
no leading newline, matching FR-049's prohibition on rewriting existing content.

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
