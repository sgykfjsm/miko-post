# Phase 0 Research: miko-post v0.1 — Dual-Sink Quick Post

**Date**: 2026-09-01 | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

Assumption **A-002** of the spec fixes the major technology choices (Go, Fyne, TOML, JSONL,
XDG, Telegram, Obsidian daily notes) as already-made product constraints. This document therefore
resolves only the decisions the specification explicitly deferred to planning (A-007 through
A-011) plus the library- and platform-level unknowns that follow from them.

---

## R-001: Go module path

**Resolves**: A-007 (spec leaves the module path as a planning task).

- **Decision**: `github.com/sgykfjsm/miko-post`, with the binary at `cmd/mp`, so the documented
  install line in `docs/design.md` §14 becomes
  `go install github.com/sgykfjsm/miko-post/cmd/mp@latest`.
- **Rationale**: It matches the repository's actual `origin` remote
  (`git@github.com:sgykfjsm/miko-post.git`), which is the only path that makes `go install` work
  without a vanity-import redirect or a `replace` directive.
- **Alternatives considered**: A vanity domain (`miko.post/mp`) — rejected: requires hosting a
  meta-tag redirect for no v0.1 benefit. A shorter `github.com/sgykfjsm/mp` — rejected: the
  application name in the design document is `miko-post`; only the *command* is `mp`.

## R-002: Go language version

- **Decision**: `go.mod` declares `go 1.24`; development and CI use the locally installed
  toolchain (`go1.27.0 darwin/arm64`).
- **Rationale**: Fyne v2.8 requires Go 1.23+. Declaring 1.24 rather than 1.27 keeps the module
  installable for contributors a release or two behind without giving up anything v0.1 uses.
- **Alternatives considered**: Pinning `go 1.27` — rejected: needlessly narrows `go install`
  compatibility for a tool with no 1.25/1.26/1.27-specific dependency.

## R-003: GUI toolkit version and the keyboard-shortcut routing model

**Constrains**: FR-020 – FR-030 (windowed interface), constitution principle II.

- **Decision**: Fyne **v2.8.1**. Implement the message field as an **extended
  `widget.Entry`** that overrides both `TypedShortcut` (for `Cmd+Enter`) and `TypedKey` (for
  `Esc`), registered with `ExtendBaseWidget`. Register `Cmd+Q` on the window canvas via
  `Canvas().AddShortcut` with a `desktop.CustomShortcut`.
- **Rationale**: This is the single highest-risk area of the plan and the routing rule is not
  obvious. Fyne delivers key and shortcut events to the **focused widget first**. Because
  FR-021 requires the multi-line entry to hold focus from the moment the window appears, a
  shortcut registered only on the canvas is consumed by the focused `Entry` and never fires. The
  Fyne documentation's own remedy is to extend the widget and implement `TypedShortcut`, calling
  through to `Entry.TypedShortcut` for anything that is not the custom shortcut so standard
  editing shortcuts (copy, paste, select-all) keep working. `Esc` needs the same treatment through
  `TypedKey`, again delegating unrecognized keys to `Entry.TypedKey` so `Enter` still inserts the
  line break FR-022 requires.
- **Consequence for tasks**: the extended entry is a distinct, separately testable unit and must
  be built before the window that hosts it. Fyne's `fyne.io/fyne/v2/test` package drives
  `TypedKey`/`TypedShortcut` headlessly, so FR-022 is automatable rather than manual — which
  matters because the constitution requires an automated test or a recorded manual justification
  per acceptance criterion.
- **Alternatives considered**: Canvas-only shortcuts — rejected: silently dead while the entry has
  focus. A non-focusable custom widget rebuilt from `canvas.Text` — rejected: reimplements text
  editing, selection, and IME (the tool must accept Japanese input) for no gain.

## R-004: Configuration decoding

**Constrains**: FR-052 – FR-058.

- **Decision**: `github.com/pelletier/go-toml/v2` **v2.4.3**, decoding with
  `Decoder.DisallowUnknownFields()`. Validate the decoded struct in one pass that collects **all**
  problems before returning, and return a single aggregated error.
- **Rationale**: FR-054 makes `[sink.telegram]` / `[sink.obsidian]` normative and excludes
  top-level `[telegram]` / `[obsidian]`. Strict decoding turns a stale top-level section into a
  loud startup error instead of a silently ignored one that leaves the user's real settings
  unapplied — exactly the "partially valid configuration" that FR-058 and constitution principle
  IV forbid. go-toml/v2 also reports row/column positions, which makes the actionable message
  FR-030 requires genuinely actionable. Collecting all problems at once avoids the fix-one-error-
  rerun loop that a low-friction capture tool cannot afford.
- **Alternatives considered**: `BurntSushi/toml` v1.6.0 — viable, and its `MetaData.Undecoded()`
  can approximate strict mode, but it is a post-hoc check rather than a decode-time failure and
  its error positions are coarser.

## R-005: Diagnostics library

**Constrains**: FR-064 – FR-076, constitution principle III.

- **Decision**: Standard library `log/slog` with `slog.NewJSONHandler`, writing to a custom
  rotating `io.Writer` (see R-006). No third-party logging dependency. Wrap slog in a thin
  internal logger that owns the stable event-name vocabulary of FR-067 as typed constants.
- **Rationale**: `slog`'s JSON handler emits exactly one self-contained JSON object per line,
  which is the whole of FR-064. Keeping the vocabulary in typed constants rather than inline
  string literals is what makes FR-067's "stable event names" enforceable — a test can assert the
  full set, and a typo cannot silently invent a new event name that breaks after-the-fact
  correlation. Secret redaction (FR-069, FR-043) is handled by never putting the token into a
  record in the first place: the token lives in a dedicated type whose `LogValue()` returns a
  redaction marker, so even an accidental `slog.Any("cfg", cfg)` cannot leak it.
- **Alternatives considered**: `zerolog` / `zap` — rejected: faster than a single-user
  interactive tool will ever need, and an added dependency for no behavioral gain.

## R-006: Log rotation

**Constrains**: FR-072 – FR-075; **resolves** A-009 and A-011.

- **Decision**: A hand-written rotating writer. It records the active file's **creation time**
  when it opens the file, tracks the size in memory, and evaluates **both** conditions before
  every write. Rotation renames the active file to `<name>.<YYYYMMDDhhmmss>` in local time
  (Go layout `20060102150405`), then opens a fresh active file. Nothing is ever deleted.
- **Creation time on macOS (A-011)**: read from `os.Stat` →
  `info.Sys().(*syscall.Stat_t).Birthtimespec`, which darwin/arm64 populates. Isolate this in a
  build-tagged file with a portable fallback to `ModTime()`, so the one platform-specific call
  is contained and the rest of the package stays testable everywhere.
- **Rotation-name collision (A-009)**: the spec assumes second-precision suffixes never collide in
  single-user operation but leaves a collision rule to planning. **Decision**: if the target name
  already exists, append `-1`, `-2`, … until a free name is found, and never overwrite a rotated
  file. This costs a few lines and makes the constitution's "rotated logs MUST be retained" true
  unconditionally rather than probabilistically.
- **Rationale for not using `lumberjack`**: its defaults *delete* rotated files (`MaxBackups`,
  `MaxAge`), it has no age-since-creation trigger, and its suffix format is not the one FR-073
  specifies. Constitution principle VI forbids automatic deletion of any log file, so adopting a
  library whose primary feature must be disabled — and whose accidental re-enablement destroys
  user data — is the wrong trade.
- **Alternatives considered**: `gopkg.in/natefinch/lumberjack.v2` — rejected as above.

## R-007: Post correlation identifier

**Constrains**: FR-066 (per-post correlation identifier), SC-007.

- **Decision**: ULID via `github.com/oklog/ulid/v2` **v2.1.2**.
- **Rationale**: The design document's illustrative log record uses `"message_id":"01K..."`, which
  is a ULID (Crockford base32, time-ordered, `01`-prefixed in this era) rather than a UUID. Using
  ULIDs keeps the emitted records matching the normative example, and lexical sortability means a
  plain `sort` over grepped log lines reconstructs a post's event order even if timestamps tie.
- **Alternatives considered**: `google/uuid` v4 — rejected: not time-ordered, and diverges from the
  design document's example. A counter — rejected: not unique across the concurrent processes that
  edge case "a second post starts while a first is still running" contemplates.

## R-008: Telegram client

**Constrains**: FR-031 – FR-043; constitution "sink implementations MUST be testable without
contacting live external services".

- **Decision**: No Telegram SDK. A small client over `net/http` posting to
  `POST {baseURL}/bot{token}/sendMessage`, where `baseURL` is an unexported struct field
  defaulting to `https://api.telegram.org` and overridable in tests. One `http.Client` whose
  `Timeout` is the configured request timeout (FR-040), with the sink-wide timeout (FR-015)
  applied as a separate `context.WithTimeout` covering both attempts.
- **Detecting a formatting-parse rejection (FR-035)**: Telegram answers a MarkdownV2 parse failure
  with HTTP 400 and a JSON body whose `description` begins `Bad Request: can't parse entities`.
  The trigger predicate is therefore `ok == false && error_code == 400 &&
  strings.Contains(description, "can't parse entities")`. Every other failure — transport error,
  timeout, 401/403, 429, 5xx, or a 400 with any other description — must **not** trigger the
  rescue (FR-038, FR-041).
- **Rationale**: The whole surface used is one endpoint with four form fields
  (`chat_id`, `text`, `parse_mode`, `message_thread_id`). An SDK adds a dependency, its own retry
  and rate-limit policies that FR-041 forbids, and an error-shape abstraction that would make the
  narrow FR-035 predicate harder to express, not easier. An injectable base URL means every path
  in FR-033 – FR-041 — including both fallback branches — is covered by `httptest` without a live
  token.
- **Risk recorded**: the `description` string is Telegram's wire text, not a documented stable
  contract. It is matched in exactly one predicate function with a table-driven test enumerating
  the real response shapes, so a future wording change is a one-line fix in one place. A
  non-formatting 400 must fail closed (no rescue) rather than open.
- **Alternatives considered**: `go-telegram-bot-api` — rejected as above.

## R-009: Version and commit stamping

**Resolves**: A-008; constrains FR-066.

- **Decision**: `-ldflags "-X main.version=... -X main.commit=..."` at build time, injected into an
  `internal/version` package by `cmd/mp`. When the flags are absent — which is the normal case for
  `go install`, the documented v0.1 distribution channel — fall back to
  `runtime/debug.ReadBuildInfo()`, reading the module version and the `vcs.revision` build setting.
- **Rationale**: `go install` cannot pass ldflags, so a stamp-only approach would leave the
  documented install path with empty version fields in every log record. `ReadBuildInfo` fills
  exactly that gap. Both sources are read once at startup and cached, and the fields are omitted
  from records when `include_version` / `include_git_commit` are false (FR-055).
- **Alternatives considered**: `go generate` writing a Go source file — rejected: puts build
  metadata under version control and it goes stale.

## R-010: Recorded message length unit

**Resolves**: A-010; constrains FR-068.

- **Decision**: Record `message_len` as a **rune count** (`utf8.RuneCountInString`), and record
  `message_bytes` alongside it as the UTF-8 byte length.
- **Rationale**: The spec leaves the unit open and attaches no user-visible requirement. Recording
  both costs one integer and removes the ambiguity permanently: the rune count is what a user
  comparing against a remembered message will recognize (the tool's messages are routinely
  Japanese and emoji, where the two numbers differ by 3–4x), while the byte count is what explains
  a Telegram length rejection. Neither is message content, so both remain safe to record on a
  successful post under FR-068.

## R-011: Testing approach

**Constrains**: the constitution's Development Workflow and Quality Gates.

- **Decision**: Standard library `testing` throughout. `net/http/httptest` for the Telegram sink,
  `t.TempDir()` for the note sink and the log writer, and `fyne.io/fyne/v2/test` for headless
  widget tests. Table-driven tests for the whitespace matrix (FR-009), the note transformation
  order (FR-047), and the formatting-rejection predicate (R-008). A fake sink that blocks on a
  channel drives the concurrency, timeout, and partial-failure assertions the constitution
  requires — asserting that both sinks *ran* and both results were *reported*, not merely that the
  happy path returns success.
- **Rationale**: No assertion library is needed for this size of project, and keeping the
  dependency set to four modules (fyne, go-toml, ulid, and their transitives) keeps `go install`
  fast and the supply-chain surface small.
- **Secret-leak gate**: a dedicated test posts through a fully wired stack with a sentinel token
  value and asserts the sentinel appears in **no** produced log line and **no** user-facing string
  — the constitution names this as a required gate, so it is a test rather than a review habit.

---

## Resolved deferrals summary

| Spec item | Question left to planning | Resolution |
|---|---|---|
| A-007 | Go module path | `github.com/sgykfjsm/miko-post` (R-001) |
| A-008 | How version/commit are stamped | ldflags with `ReadBuildInfo` fallback (R-009) |
| A-009 | Rotation timestamp collision rule | Numeric `-N` disambiguator; never overwrite (R-006) |
| A-010 | Message length unit | Rune count, plus byte count (R-010) |
| A-011 | File creation time availability | `Birthtimespec` on darwin, `ModTime` fallback (R-006) |

**No `NEEDS CLARIFICATION` items remain.**
