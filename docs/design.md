# miko-post MVP Design Document

- Status: Approved MVP specification
- Target version: v0.1
- Application: `miko-post`
- CLI command: `mp`
- Implementation: Go + Fyne
- Japanese translation: [`design.ja.md`](./design.ja.md)

This English document is normative. The Japanese document is a translation; if the two differ, this document takes precedence.

## 1. Purpose

`miko-post` is a small tool for recording and posting a short message with as little friction as possible. It sends one message to two independent destinations, called sinks: Telegram and an Obsidian Daily Note.

The minimal GUI and CLI share the same posting core. The MVP prioritizes fast input, local preservation, simple operation, and structured diagnostics suitable for later human or AI-assisted investigation.

## 2. Scope

### 2.1 Included in v0.1

- One binary provides both CLI and GUI entry points.
- Running `mp` without arguments opens the GUI.
- Running `mp` with one or more message arguments posts from the CLI.
- Telegram and Obsidian run concurrently as independent sinks.
- A failure in one sink never cancels or suppresses the other sink.
- Results from all enabled sinks are collected, displayed, and logged.
- Configuration and state paths follow XDG conventions.
- CLI posting may use an explicitly selected configuration file.
- Logs use structured JSON Lines (JSONL) with size- or age-based rotation.

### 2.2 Out of scope for v0.1

- Image posting
- Configuration GUI
- Retry queue or automatic resend of failed Telegram posts
- Posting from standard input (`stdin`)
- Automatic HTTP retries
- Per-invocation sink flags such as `--no-telegram` or `--no-obsidian`
- Automatic retention or deletion of rotated logs
- Additional sink types

## 3. Architecture

```text
CLI ----\
         >-- Post(message) --+--> sink.telegram
GUI ----/                    +--> sink.obsidian
             |                    (concurrent, independent)
             +--> JSONL logging
```

The CLI and GUI are thin entry points. They load and validate configuration, invoke the shared posting service, and render the aggregated result. The posting core, individual sinks, configuration, GUI, and logging should remain separate responsibilities. The exact package layout is an implementation detail.

## 4. CLI

### 4.1 Syntax

```text
mp [options] [message...]
```

```text
-c, --config PATH   Configuration file for CLI posting
-h, --help          Show help
```

### 4.2 Dispatch rules

| Invocation | Behavior |
|---|---|
| `mp` | Open the GUI using the default XDG configuration file |
| `mp "hello"` | Post `hello` from the CLI using the default configuration file |
| `mp hello world` | Join message arguments with one ASCII space and post `hello world` |
| `mp -c ./config.toml "hello"` | Post from the CLI using `./config.toml` |
| `mp -c ./config.toml` | Print an error, do not open the GUI, and exit `1` |
| `mp --help` | Show help containing the resolved default configuration path |

`--config` is available only for CLI posting. It must never change the configuration used by the GUI. Supplying it without a message is an error:

```text
--config is only available when posting from CLI
```

Help must show the actual default path resolved for the current environment, not only an unresolved `$XDG_CONFIG_HOME` expression.

```text
Usage:
  mp [options] [message...]

If no message is specified, the GUI is launched.

Options:
  -c, --config PATH
        Path to the configuration file for CLI posting.
        Default: /Users/shige/.config/miko-post/config.toml

  -h, --help
        Show this help.
```

v0.1 does not read message content from standard input.

### 4.3 Message validation

Before starting any sink, trim leading and trailing Unicode whitespace for validation. This includes ASCII spaces, full-width spaces, tabs, and line breaks. If the trimmed value is empty, treat the message as an input error, invoke no sinks, show a correction prompt, and exit `1` in CLI mode.

Trimming is for validation only. For a valid message, sinks receive the original input rather than the trimmed value.

## 5. GUI

The GUI is implemented with Fyne and contains only:

- A multiline message field
- A Send button
- A Cancel button
- A compact result/error display

The message field receives focus at launch. Cancel closes the window without posting.

During submission, Send is disabled to prevent duplicate posts. The GUI invokes the shared posting service, waits for every enabled sink to complete, and displays a compact result for each sink.

Default auto-close delays:

- All enabled sinks succeeded: 15 seconds
- One or more enabled sinks failed: 30 seconds

Both values are configurable. A failure display identifies every failed sink and gives a short human-readable reason. Detailed errors and stack traces belong in JSONL logs rather than the GUI.

Keyboard behavior on macOS:

- `Esc`: cancel and close without posting
- `Enter`: insert a line break
- `Cmd+Enter`: send
- `Cmd+Q`: quit the application

The buttons and keyboard shortcuts must share the same validation and submission paths.

## 6. Posting orchestration

Every enabled sink receives the same original message. Telegram and Obsidian start concurrently and run to completion independently.

The orchestrator waits for and aggregates every enabled sink result. It must not return after the first error. If both sinks fail, both errors must be displayed and logged.

The result model should distinguish a short display reason from the detailed diagnostic error. For example:

```go
type SinkResult struct {
    Name    string
    Success bool
    Reason  string // Short, safe, human-readable reason
    Err     error  // Detailed diagnostic error for logging
}
```

This is illustrative and not a required Go API. Disabled sinks are not invoked and do not count as failures.

v0.1 does not queue or automatically resend failed Telegram messages. The Obsidian entry and structured logs provide the record of the attempt.

Each sink invocation has an independently configurable overall timeout, defaulting to 60 seconds. The timeout covers the sink's entire operation. A timed-out sink returns a failure result, while other sinks continue independently.

At startup, configuration is invalid if every sink is disabled. Show an actionable message asking the user to enable at least one sink, do not attempt a post, and exit `1` in CLI mode.

## 7. `sink.telegram`

Telegram configuration belongs under `[sink.telegram]`.

- Send text through the Telegram Bot API.
- Use `chat_id` as the destination.
- When `thread_id` is present, pass it as Telegram's `message_thread_id`.
- When `thread_id` is absent, post normally to the chat.
- First attempt delivery using `MarkdownV2`.
- Only when Telegram rejects the message with a MarkdownV2 parse error, retry once as plain text with no parse mode.
- If the plain-text fallback succeeds, the Telegram sink succeeds overall.
- Do not use the plain-text fallback as a general retry for failures unrelated to Markdown parsing.
- If the fallback also fails, the Telegram sink fails and both attempts are retained in logs.
- Each Telegram HTTP request has an independently configurable timeout, defaulting to 30 seconds.
- v0.1 performs no automatic retry for transport errors, timeouts, HTTP status codes, or Telegram API errors.

The fallback path must be observable through distinct events such as `telegram_markdown_failed` followed by `telegram_plaintext_succeeded` or `telegram_plaintext_failed`.

The MarkdownV2-to-plain-text fallback is a format fallback, not an HTTP retry. It remains required in v0.1. Both HTTP attempts, when needed, are bounded by the 30-second request timeout and by the 60-second overall sink timeout.

### 7.1 Bot token precedence

1. `MIKO_POST_TELEGRAM_BOT_TOKEN`
2. `sink.telegram.bot_token` in TOML

Only the bot token supports an environment-variable override in v0.1. The token must never appear in logs, UI errors, CLI errors, or configuration dumps.

## 8. `sink.obsidian`

Obsidian configuration belongs under `[sink.obsidian]`.

The sink appends one physical UTF-8 line to the end of the current Daily Note. It does not search for, create, or insert into a named section.

```text
<daily_note_dir>/<current date formatted with filename_format>
```

Example using the agreed configuration:

```text
/Users/shige/Dropbox/workspace/memo/shigeyukis-valut/journal/notes/daily/2026-08-27.md
```

`filename_format` uses Go `time.Format` syntax and includes the `.md` extension. If the file does not exist and `create_if_missing` is true, create it.

### 8.1 Append transformation

Use local time and the configured Go time format. Transform the message in this order:

1. Normalize `\r\n` and bare `\r` to `\n`.
2. Replace every `\n` inside the message with the literal string `<br>`.
3. Prefix the result with `- <formatted-time> `.
4. Append the entry followed by one `\n` to the Daily Note.

Single-line example:

```markdown
- 11:42 今日も美琴が可愛い♡
```

A multiline message is stored as one physical line:

```markdown
- 11:42 今日も美琴が可愛い♡<br>美琴愛してるよ💋<br>黒子も操祈も佐天さんもラブラブチュッチュッ😘
```

Open the file in append mode so existing content is never rewritten. Use UTF-8 encoding and LF (`\n`) for the appended line ending.

## 9. Configuration

Configuration uses TOML.

### 9.1 Default path

```text
$XDG_CONFIG_HOME/miko-post/config.toml
```

When `XDG_CONFIG_HOME` is unset or empty:

```text
~/.config/miko-post/config.toml
```

The GUI always uses this default path. CLI posting uses another file only when `-c` or `--config` is present.

### 9.2 v0.1 schema

```toml
# ~/.config/miko-post/config.toml

[sink.telegram]
enabled = true

# MIKO_POST_TELEGRAM_BOT_TOKEN takes precedence when set.
bot_token = "123456789:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
chat_id = "-1001234567890"

# Optional. Omit when posting directly to the chat.
thread_id = 12345

parse_mode = "MarkdownV2"
fallback_to_plain_text = true

# Timeout for one Telegram HTTP request.
http_timeout_seconds = 30


[sink.obsidian]
enabled = true
daily_note_dir = "/Users/shige/Dropbox/workspace/memo/shigeyukis-valut/journal/notes/daily"

# Go time.Format layouts.
filename_format = "2006-01-02.md"
time_format = "15:04"
create_if_missing = true


[posting]
# Overall timeout applied independently to each sink invocation.
sink_timeout_seconds = 60


[gui]
success_close_seconds = 15
error_close_seconds = 30


[logging]
format = "jsonl"

# Empty means the default XDG state path.
path = ""

# Rotate when either threshold is reached (OR).
rotate_size_mib = 10
rotate_after_days = 7

# Omit message bodies on success; include them for failed posts.
message_on_error_only = true

stack_trace = true
include_version = true
include_git_commit = true
```

The `sink.telegram` and `sink.obsidian` section names are normative. Top-level `[telegram]` and `[obsidian]` sections are not part of the v0.1 schema.

The command name, exit codes, UTF-8/LF encoding, sink concurrency, Obsidian `<br>` conversion, and Telegram fallback algorithm are application behavior rather than configuration options.

## 10. Exit status and user-facing results

```text
0 = all enabled sinks succeeded
1 = input, configuration, startup, or one-or-more-sink failure
```

If MarkdownV2 delivery fails but the plain-text fallback succeeds, Telegram counts as successful and does not cause exit `1`.

Both CLI and GUI identify every failed sink, provide a short reason for each failure, and make partial success visible.

```text
Obsidian: success
Telegram: failed — request timed out
See log for details: /Users/shige/.local/state/miko-post/app.jsonl
```

When both fail:

```text
Obsidian: failed — permission denied
Telegram: failed — request timed out
See log for details: /Users/shige/.local/state/miko-post/app.jsonl
```

Exact wording and stdout/stderr placement are implementation details. Completeness, brevity, and inclusion of the detailed log path are required.

## 11. JSONL logging

### 11.1 Default log path

```text
$XDG_STATE_HOME/miko-post/app.jsonl
```

When `XDG_STATE_HOME` is unset or empty:

```text
~/.local/state/miko-post/app.jsonl
```

A non-empty `logging.path` overrides the XDG path.

### 11.2 Recorded data

Every line is an independently valid JSON object. Depending on the event, records include:

- Timestamp with timezone
- Level and stable event name
- Source (`cli` or `gui`)
- Per-post `message_id` for correlating concurrent events
- Sink name and outcome
- Duration in milliseconds
- Error type, detailed error message, and HTTP status when available
- Obsidian target path when relevant
- Application version and git commit when enabled

When `message_on_error_only = true`, successful events omit message content; message length may still be recorded. If any sink fails, record the original message body needed to reconstruct that post. Never record secrets such as the Telegram bot token.

Expected event equivalents include:

```text
message_received
obsidian_append_started
obsidian_append_succeeded | obsidian_append_failed
telegram_send_started
telegram_send_succeeded | telegram_send_failed
telegram_markdown_failed
telegram_plaintext_succeeded | telegram_plaintext_failed
request_completed | request_completed_with_error
```

If both sinks fail, emit both detailed failures. Logging must not stop after the first error.

Record a stack trace for panics, unexpected errors, and failures where a trace is available and useful. Expected operational errors do not need an artificial trace. Collection follows the `stack_trace` setting.

Illustrative failure event:

```json
{"ts":"2026-08-27T11:42:03+09:00","level":"error","event":"telegram_send_failed","source":"cli","message_id":"01K...","sink":"telegram","message":"今日も美琴が可愛い♡","error_type":"timeout","error":"request timed out","duration_ms":10012,"app_version":"0.1.0","git_commit":"abc1234"}
```

### 11.3 Rotation

Rotate the current log when either condition is met:

```text
current size >= rotate_size_mib (default 10 MiB)
OR
current log age >= rotate_after_days (default 7 days)
```

v0.1 performs no automatic retention or deletion. Rotated logs remain until manually removed.

Append the rotation timestamp to the active filename using `%Y%m%d%H%M%S` semantics (Go layout `20060102150405`) in local time. For example:

```text
app.jsonl -> app.jsonl.20260827114203
```

## 12. Error handling

- Never let one sink failure cancel the other sink.
- Preserve, display, and log every sink failure from the same post.
- Convert internal errors into short, safe reasons for CLI and GUI display.
- Keep detailed diagnostic context in logs.
- Never expose the Telegram bot token in errors, logs, or configuration dumps.
- If configuration loading or validation fails, do not start sinks with partially valid configuration.
- Treat an empty message after Unicode-whitespace trimming as an input error and invoke no sinks.
- Treat an all-sinks-disabled configuration as a startup error and tell the user to enable at least one sink.
- Treat successful plain-text fallback as successful delivery while retaining the original MarkdownV2 failure for diagnostics.

## 13. Acceptance criteria

1. `mp` opens a Fyne GUI using the resolved default XDG configuration.
2. `mp hello world` joins arguments with spaces and posts to every enabled sink.
3. `mp -c PATH hello` uses `PATH`; `mp -c PATH` does not open the GUI and exits `1`.
4. `mp --help` displays the resolved default configuration path.
5. Telegram receives `chat_id` and the optional `message_thread_id` correctly.
6. Only a MarkdownV2 parse error triggers one plain-text fallback; fallback success is overall success.
7. Multiline input becomes one physical Obsidian line with `<br>` separators and a formatted local-time prefix.
8. A missing Daily Note is created when configured, while existing content is preserved during append.
9. Telegram and Obsidian run independently; failure of either never suppresses the other.
10. Partial and total failures show every failed sink and a short cause in both interfaces.
11. CLI exits `0` only when all enabled sinks succeed; otherwise it exits `1`.
12. GUI Send is disabled during posting and closes after 15 seconds on success or 30 seconds on failure by default.
13. JSONL logs correlate events for one post, omit successful message bodies, include the body on failure, and contain no secrets.
14. Logs rotate at 10 MiB or seven days, whichever occurs first, with no automatic deletion.
15. ASCII-space-only, full-width-space-only, tab-only, and line-break-only messages are rejected before any sink runs.
16. `Esc`, `Enter`, `Cmd+Enter`, and `Cmd+Q` perform cancel, newline, send, and quit respectively.
17. Telegram HTTP requests default to a 30-second timeout; each sink invocation defaults to a separate 60-second overall timeout.
18. No general HTTP retry occurs in v0.1; MarkdownV2 plain-text fallback continues to work as specified.
19. Rotated logs use a local timestamp suffix such as `app.jsonl.20260827114203`.
20. An all-sinks-disabled configuration fails at startup with an actionable correction message.

## 14. Installation

Distribution assumes the Go toolchain. Install with `go install`:

```bash
go install <module-path>/cmd/mp@latest
```

Replace `<module-path>` with the repository's final Go module path. The installed executable name is `mp`. Platform-specific application bundles, code signing, notarization, and separate installers are outside the v0.1 distribution requirement.
