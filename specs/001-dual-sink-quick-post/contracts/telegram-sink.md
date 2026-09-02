# Contract: Telegram sink

**Requirements**: FR-031 – FR-043. **Design**: `docs/design.md` §7. **Research**: R-008.

## Request

```text
POST {baseURL}/bot{token}/sendMessage
```

`baseURL` defaults to `https://api.telegram.org` and is overridable in tests only (R-008).

| Field | Source | Note |
|---|---|---|
| `chat_id` | `sink.telegram.chat_id` | FR-031 |
| `text` | the **original, untrimmed** message | FR-012, FR-033 |
| `parse_mode` | `MarkdownV2` on attempt 1; **omitted** on the rescue | FR-033, FR-035 |
| `message_thread_id` | `sink.telegram.thread_id` | Sent only when configured; omitted otherwise (FR-032) |

## Verbatim rule (FR-033)

The first attempt sends the message **verbatim**. The application MUST NOT escape, transform, or
normalize characters that MarkdownV2 reserves. Consequently everyday punctuation (`.`, `-`, `!`,
`(`) routinely causes the first attempt to be rejected and the rescue to succeed. **This is
expected steady-state behavior, not a defect**, and the resulting log pattern must not be treated
as an error condition.

## Rescue trigger (FR-035, FR-038) — normative predicate

Exactly one unformatted retry runs **if and only if**:

```text
response.ok == false
AND response.error_code == 400
AND response.description contains "can't parse entities"
```

Any other failure — transport error, timeout, 401, 403, 429, 5xx, or a 400 with a different
description — makes **no second attempt of any kind** (FR-038, FR-041). Fail closed.

## Outcomes

| Situation | Sink result | Exit impact |
|---|---|---|
| Attempt 1 succeeds | success | none |
| Attempt 1 rejected for formatting, rescue succeeds | **success** (FR-036) | **none** (FR-061) |
| Attempt 1 rejected for formatting, rescue fails | failure; **both** attempts retained in the log (FR-037) | `1` |
| Any non-formatting failure | failure, no retry (FR-038, FR-041) | `1` |

## Timeouts

| Bound | Default | Requirement |
|---|---|---|
| One HTTP request | 30 s | FR-040 |
| The whole sink invocation, covering both attempts | 60 s | FR-015 |

## Credential (FR-042, FR-043)

`MIKO_POST_TELEGRAM_BOT_TOKEN` takes precedence over `bot_token`. The token MUST NEVER appear in
logs, on-screen errors, CLI errors, or settings dumps — including inside a URL echoed in an error
message, which is the most likely accidental leak given the token sits in the request path.

## No transport retry (FR-041)

v0.1 performs no automatic retry for transport errors, timeouts, HTTP status codes, or Telegram
API errors. The formatting fallback is a **format** fallback, not a transport retry
(constitution, Additional Constraints).
