# Contract: Settings schema (TOML)

**Requirements**: FR-042, FR-052 – FR-058, FR-065. **Design**: `docs/design.md` §9.

## Path resolution (FR-053)

```text
$XDG_CONFIG_HOME/miko-post/config.toml
```
When `XDG_CONFIG_HOME` is unset **or empty**, fall back to:
```text
~/.config/miko-post/config.toml
```

The window always uses this default path. CLI posting uses a different file only with
`-c`/`--config` (FR-005).

## Section names (FR-054)

`[sink.telegram]` and `[sink.obsidian]` are normative. Top-level `[telegram]` / `[obsidian]`
sections are **not** part of the v0.1 schema and, under strict decoding (R-004), are a load error
rather than a silent no-op.

## Keys

### `[sink.telegram]`

| Key | Type | Default | Validation |
|---|---|---|---|
| `enabled` | bool | `false` | — |
| `bot_token` | string | — | Required when enabled and `MIKO_POST_TELEGRAM_BOT_TOKEN` is unset. At least 16 characters whenever present, enabled or not. Never logged or printed (FR-043). See **Credential length** below. |
| `chat_id` | string | — | Required when enabled; non-empty |
| `thread_id` | int | *absent* | Optional. **Absence**, not a sentinel, means "post to the chat directly" (FR-032, A-006). |
| `parse_mode` | string | `"MarkdownV2"` | Accepted and validated, **inert in v0.1** (FR-034) |
| `fallback_to_plain_text` | bool | `true` | Accepted and validated, **inert in v0.1** (FR-034) |
| `http_timeout_seconds` | int | `30` | > 0 and <= `9223372036` (FR-040). See **Timeout bounds** below. |

**Credential length**: `bot_token` must be at least `16` characters whenever it is present, and
that rule applies **even when the chat destination is disabled**. The token is handed to the
diagnostic logger as a redaction pattern, and the scrub is an unanchored substring replacement, so a
short value does not redact the credential — it rewrites every field it appears inside. Measured with
a one-character token, a single record came back with `event`, `sink` and `message_id` all corrupted,
and `message_id` is the identifier every record is correlated by. The bound is a collision threshold
rather than a strength requirement: sixteen consecutive bytes of a credential do not occur inside
ordinary field names, and a shorter pattern must. It is deliberately not Telegram's real
`<digits>:<35 chars>` shape, which would encode a third party's credential format as a validation
rule. `internal/logging` skips any pattern shorter than the same bound, as a second guard for callers
that construct settings without going through `Load` (issue #117).

**Credential precedence** (FR-042): `MIKO_POST_TELEGRAM_BOT_TOKEN` wins over `bot_token`. It is
the only setting with an environment override in v0.1.

`parse_mode` and `fallback_to_plain_text` are the only keys that are validated but deliberately do
not change behavior. This is a forward-compatibility affordance recorded in the spec's
Clarifications, not a defect: v0.1 delivery is fixed at MarkdownV2-first with exactly one
unformatted rescue.

### `[sink.obsidian]`

| Key | Type | Default | Validation |
|---|---|---|---|
| `enabled` | bool | `false` | — |
| `daily_note_dir` | string | — | Required when enabled; absolute path |
| `filename_format` | string | `"2006-01-02.md"` | Go `time.Format` layout, includes `.md`. Must not contain the `MST` zone-name element (see below). Must **render** a plain filename: no `/` or `\` (both, on every platform), no `..` element, no control character — it is joined onto `daily_note_dir`. |
| `time_format` | string | `"15:04"` | Go `time.Format` layout. Must not contain the `MST` zone-name element (see below). Must **render** a single line: no CR or LF (FR-048). |
| `create_if_missing` | bool | `true` | Governs FR-046 |

Both format keys reject Go's `MST` zone-name element. It is the only reference element whose output
is neither the document's own text nor a digit: it copies the zone abbreviation through verbatim, and
that string comes from the environment — `$TZ` may name an arbitrary TZif file and nothing constrains
the abbreviation inside it, so `"2006-01-02MST.md"` can render a path traversal and `"15:04 MST"` can
render a line break.

Rejecting the element is the rule, rather than inspecting what it renders, because a fixed `Location`
does not imply a fixed zone: one `Location` selects among arbitrarily many zones by instant
(`America/New_York` renders `EST` in January and `EDT` in July), so no fixed number of sampled
instants is sound — the transition schedule is an input, not a constant. With the element refused,
every remaining element renders from digits, Go's English month and day names, and `` +-,.: ``, so the
rendered value is instant-independent and one rendering decides the rules above.

The numeric offsets — `Z0700`, `Z07:00`, `Z07`, `Z070000`, `Z07:00:00`, `-0700`, `-07:00`, `-07`,
`-070000`, `-07:00:00` — remain accepted and are the supported way to put the zone in a name. Note
that Go's layout grammar has no literal `MST`: any `MST` its scanner reaches at a chunk boundary is
the element, so this restriction removes nothing a user could otherwise have expressed.

Problem messages quote the layout and its rendering elided to a fixed rune budget, so that a large
document cannot inflate the FR-058 message or the FR-064 log line.

### `[posting]`

| Key | Type | Default | Validation |
|---|---|---|---|
| `sink_timeout_seconds` | int | `60` | > 0 and <= `9223372036`. Applied **independently** to each sink (FR-015). See **Timeout bounds** below. |

#### Timeout bounds

Both timeout keys are bounded **above** as well as below, at `9223372036` seconds (about 292
years) — the largest whole second that survives conversion to the internal duration type.

The upper bound is not a policy about how long a timeout may be. It is arithmetic. A consumer has
to convert the configured seconds into a duration, and a value beyond the bound wraps:

| Configured | Converted | Bounded from below alone? |
|---|---|---|
| `9223372037` | `-2562047h47m…` | caught — negative |
| `4611686018427387904` | `0s` | caught — zero |
| **`18446744074`** | **`290.448384ms`** | **not caught — positive and small** |
| **`18446744075`** | **`1.290448384s`** | **not caught** |

The last two rows are why the bound exists: they are accepted by any "> 0" rule, and a user who
asked for ~584 years would silently get a deadline tighter than the default they were raising.
Recorded from issues #109 (`posting`) and #114 (`sink.telegram`); both keys are checked by one
shared rule in `internal/config/validate.go` so a third timeout key cannot acquire only half of it.

### `[gui]`

| Key | Type | Default | Validation |
|---|---|---|---|
| `background_image_dir` | string | `""` | Empty or absolute directory path; see [GUI background](../../../docs/gui-background.md) (#122) |
| `success_close_seconds` | int | `15` | >= 0 (FR-026) |
| `error_close_seconds` | int | `30` | >= 0 (FR-026) |

### `[logging]`

| Key | Type | Default | Validation |
|---|---|---|---|
| `format` | string | `"jsonl"` | `"jsonl"` only in v0.1 |
| `path` | string | `""` | **Empty means the default state path** (FR-056) |
| `rotate_size_mib` | int | `10` | > 0 (FR-072) |
| `rotate_after_days` | int | `7` | > 0 (FR-072) |
| `message_on_error_only` | bool | `true` | Governs FR-068 |
| `stack_trace` | bool | `true` | Governs FR-071 |
| `include_version` | bool | `true` | Governs FR-066 |
| `include_git_commit` | bool | `true` | Governs FR-066 |

**Default log path** (FR-065): `$XDG_STATE_HOME/miko-post/app.jsonl`, falling back to
`~/.local/state/miko-post/app.jsonl` when the variable is unset or empty. A non-empty
`logging.path` overrides it.

## Not configurable (FR-057, constitution principle V)

Exit codes, UTF-8/LF encoding, sink concurrency, the note `<br>` transformation, and the
formatting-fallback algorithm are application behavior and MUST NOT become settings.

## Failure semantics (FR-018, FR-058)

- Unreadable, unparseable, or invalid settings: no sink is started with partially valid settings;
  the failure surfaces through the front door that was used.
- Every sink disabled: a **startup error** with an actionable message asking the user to enable at
  least one sink, no post attempt, exit `1`.
