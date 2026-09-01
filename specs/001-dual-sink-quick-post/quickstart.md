# Quickstart: Validating miko-post v0.1

**Plan**: [plan.md](./plan.md) | **Contracts**: [contracts/](./contracts/)

This is a **validation guide** — how to prove the feature works end to end. Implementation detail
belongs in `tasks.md`.

## Prerequisites

- Go 1.24+ (`go1.27.0 darwin/arm64` verified locally)
- A C toolchain for Fyne's cgo dependencies (Apple clang, present with Xcode command line tools)
- macOS (A-003 scopes the keyboard contract to macOS for v0.1)
- For live Telegram checks only: a bot token and a chat id. **Every automated test runs without
  them** (constitution: sinks must be testable without contacting live services).

## Build

```bash
# Preferred: assembles the linker flags with the correct symbol paths and
# marks the build dirty when the tree has uncommitted or untracked changes.
make build

# Unstamped; falls back to the build info the toolchain embeds.
go build -o ./bin/mp ./cmd/mp

# Explicit stamping (R-009). The variables live in internal/version, not in
# main, and the linker silently ignores an -X flag naming a symbol that does
# not exist — so a wrong path here produces an unstamped binary with no error.
go build -ldflags "\
  -X github.com/sgykfjsm/miko-post/internal/version.version=0.1.0 \
  -X github.com/sgykfjsm/miko-post/internal/version.commit=$(git rev-parse --short HEAD)" \
  -o ./bin/mp ./cmd/mp

# Confirm what a build would embed, without building:
make stamp
```

`VERSION` and `COMMIT` may be overridden, from the command line or the
environment. Both must match `[A-Za-z0-9._+/-]+`; anything else fails the build
rather than being rewritten into a different release label. Note that make reads
the environment, so an ambient `VERSION` in your shell affects a plain
`make build`.

## Automated validation

```bash
go test ./...
go test -race ./internal/post/...   # concurrency and partial-failure behavior
go vet ./...
```

The race detector is not optional on `internal/post`: constitution principle I is a concurrency
guarantee, and the sink-independence tests are the only place it is mechanically checked.

## Scenario 1 — Both sinks succeed from the CLI (US1)

```bash
export XDG_CONFIG_HOME=$(mktemp -d)
export XDG_STATE_HOME=$(mktemp -d)
mkdir -p "$XDG_CONFIG_HOME/miko-post" /tmp/mp-daily
# write a config with both sinks enabled, daily_note_dir=/tmp/mp-daily
./bin/mp "hello from quickstart"; echo "exit=$?"
```

**Expect**: exit `0`; both outcomes reported; one new line in `/tmp/mp-daily/<today>.md` shaped
`- HH:MM hello from quickstart`; records in `$XDG_STATE_HOME/miko-post/app.jsonl` sharing one
`message_id`, with **no** `message` field (the post succeeded) and **no** token anywhere.

## Scenario 2 — Argument joining and whitespace (US1)

```bash
./bin/mp hello world          # expect the sinks to receive exactly "hello world"
./bin/mp "   "; echo "exit=$?" # ASCII spaces
./bin/mp "　"; echo "exit=$?"  # U+3000 full-width space
printf -v tabs '\t\t'; ./bin/mp "$tabs"; echo "exit=$?"
```

**Expect**: the first posts `hello world` (one ASCII space, FR-004). The rest are rejected before
any sink is contacted, print a correction prompt, and exit `1` (FR-009, FR-010). Confirm the log
shows **no** `*_started` event for either sink.

Then verify FR-011 — leading/trailing whitespace on a *valid* message is delivered untrimmed:

```bash
./bin/mp "  padded  "   # the note line must retain both spaces
```

## Scenario 3 — One sink broken, one healthy (US3, SC-002)

Point `daily_note_dir` at an unwritable path, or the Telegram base URL at an unreachable host.

**Expect**: the healthy sink still receives the **complete** message; the report names the failed
sink with a short reason; the log path appears in the output (FR-063); exit `1` (FR-059).

Repeat with **both** sinks broken: both failures reported, neither hidden (FR-070).

## Scenario 4 — Formatting rescue (US4, SC-005)

```bash
./bin/mp "release 1.0 (finally!) - shipped."
```

Reserved MarkdownV2 characters are sent verbatim (FR-033), so Telegram rejects the first attempt.

**Expect**: the message arrives as plain text; the run reports overall **success** and exits `0`
(FR-036, FR-061); the log contains `telegram_markdown_failed` followed by
`telegram_plaintext_succeeded` (FR-039). The user is never told a rescue happened.

Verify the negative case too — a non-formatting failure (bad token → 401) produces **no** second
attempt (FR-038).

## Scenario 5 — Multi-line into one note line (US1, SC-010)

Send a message containing `\n`, `\r\n`, and a bare `\r`.

**Expect**: exactly **one** physical line in the note, `<br>` between segments, `- HH:MM ` prefix,
and `\r\n` producing a single `<br>` rather than two (FR-047 step order).

## Scenario 6 — Append preserves existing content (SC-009)

Pre-create today's note **without** a trailing newline, then post twice.

**Expect**: the original content is byte-identical; two new lines appended; nothing reordered or
truncated (FR-049). Then set `create_if_missing = false`, delete the note, and post: the note sink
fails while Telegram is unaffected (FR-046).

## Scenario 7 — The window (US2)

```bash
./bin/mp
```

**Expect**, in order: the field has focus with no click (FR-021); `Enter` inserts a line break
(FR-022); `Cmd+Enter` sends and Send is disabled during the post (FR-024); the result panel names
both outcomes (FR-025); on full success the app closes and terminates itself after 15 s with exit
`0`, on failure after 30 s with exit `1` (FR-026, FR-027). Press a key during the countdown — the
auto-close is cancelled and the result stays until dismissed (FR-028, SC-012). `Esc` closes
without posting; `Cmd+Q` quits.

## Scenario 8 — Configuration override and resolution (US6)

```bash
./bin/mp --help                       # shows the RESOLVED default path, not $XDG_CONFIG_HOME
./bin/mp -c ./other.toml "hi"         # uses ./other.toml for this post only
./bin/mp -c ./other.toml; echo "exit=$?"  # error, no window, exit 1
./bin/mp                              # window still uses the DEFAULT path
```

Then set every `enabled = false`: the CLI prints an actionable message and exits `1` (FR-018);
the windowed path shows a minimal error window with the message and the resolved settings path,
no message field, exiting `1` when dismissed (FR-030).

## Scenario 9 — Diagnostics (US5)

```bash
# Rotation by size (FR-072, FR-073)
# set rotate_size_mib low, post repeatedly, then:
ls "$XDG_STATE_HOME/miko-post/"     # expect app.jsonl plus app.jsonl.YYYYMMDDhhmmss
# every line parses independently (SC-007):
while read -r l; do echo "$l" | python3 -m json.tool >/dev/null || echo "BAD: $l"; done \
  < "$XDG_STATE_HOME/miko-post/app.jsonl"
```

**Expect**: no rotated file deleted (FR-074); one `message_id` gathers a whole post; a failed
post's records carry the original body (FR-068, SC-008); no token in any line (SC-006).

Then make the log path unwritable and post: every sink still runs, real outcomes are reported,
**exactly one** warning names the log path and reason, and the exit status reflects only the sink
outcomes (FR-076, SC-013).

## Acceptance gate (SC-014)

Before the feature is considered done, walk the 20 numbered criteria in `docs/design.md` §13 and
confirm each is demonstrated by an automated test or, where genuinely manual (the interactive
auto-close cancellation in Scenario 7, live Telegram delivery), by a recorded manual
justification — the constitution requires one or the other for every criterion.
