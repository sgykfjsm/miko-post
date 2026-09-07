# Tasks: miko-post v0.1 — Dual-Sink Quick Post

**Input**: Design documents from `/specs/001-dual-sink-quick-post/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: **Included and required.** The constitution's Development Workflow and Quality Gates
mandate that every acceptance criterion in the governing specification have a corresponding
automated test or an explicitly recorded manual justification, and that concurrency, timeout, and
partial-failure behavior be covered by tests asserting both sinks ran and both results were
reported. Test tasks are therefore not optional here.

**Organization**: Tasks are grouped by user story so each story can be implemented, tested, and
delivered independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1–US6)
- Exact file paths are included in every task

## Path Conventions

Single Go module rooted at the repository root, per plan.md **Structure Decision**: binary in
`cmd/mp/`, all other packages under `internal/`. Go convention places tests beside the code they
exercise (`foo_test.go` next to `foo.go`) rather than in a separate `tests/` tree.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and build scaffolding

- [x] T001 Initialize the Go module as `github.com/sgykfjsm/miko-post` with `go 1.24` in `go.mod` (per research R-001, R-002)
- [x] T002 [P] Create the package skeleton directories `cmd/mp/`, `internal/{cli,gui,post,config,logging,version}/`, `internal/sink/{telegram,obsidian}/`, and `testdata/` per the plan's Source Code layout
- [ ] T003 [P] Add and pin dependencies `fyne.io/fyne/v2@v2.8.1`, `github.com/pelletier/go-toml/v2@v2.4.3`, `github.com/oklog/ulid/v2@v2.1.2` in `go.mod` and commit `go.sum`
  - **Deferred in Batch 1, partially done in Batch 3.** Go records a dependency only when a package imports it: `go get` marks all three `// indirect` and `go mod tidy` removes them, leaving `go.sum` empty. Pin each one in the batch that first imports it — go-toml in the settings batch, ULID in the orchestrator batch, Fyne in the GUI batch. The intended versions are recorded in research.md (R-003, R-004, R-007).
  - `github.com/pelletier/go-toml/v2@v2.4.3` is pinned as a **direct** requirement as of the settings batch (T016), with `go.sum` populated. `github.com/oklog/ulid/v2@v2.1.2` is pinned as of the orchestrator batch (T025). `fyne.io/fyne/v2@v2.8.1` remains outstanding until the GUI batch.
- [x] T004 [P] Implement build-time version and commit variables with a `runtime/debug.ReadBuildInfo()` fallback in `internal/version/version.go` (research R-009, resolves A-008)
- [x] T005 [P] Add a `Makefile` at the repository root with `build` (including `-ldflags -X` version stamping), `test`, `race`, `vet`, and `install` targets

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared posting core, settings, and diagnostics that every user story builds on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete. This phase is where
constitution principles I (Sink Independence) and II (One Posting Core) are physically enforced;
neither can be retrofitted later without rewriting every story.

### Message and results

- [x] T006 [P] Implement the `Message` type holding only the original untrimmed text, plus `Validate()` using `strings.TrimSpace`, in `internal/post/message.go` (FR-009 – FR-011)
- [x] T007 [P] Write the table-driven whitespace-rejection test covering ASCII spaces, U+3000 full-width spaces, tabs, line breaks, and mixtures, and asserting a valid message is delivered untrimmed, in `internal/post/message_test.go` (FR-009 – FR-011, spec Edge Cases)
- [x] T008 [P] Implement `SinkResult` with separate display `Reason` and diagnostic `Err` fields, and the aggregate success rule, in `internal/post/result.go` (FR-017, FR-059, FR-061)
- [x] T009 [P] Write aggregation tests covering all-success, partial-failure, and all-failure result sets in `internal/post/result_test.go` (FR-059 – FR-062, SC-004)
- [x] T010 [P] Define the two-method `Sink` interface (`Name`, `Send`) in `internal/post/sink.go` (data-model.md)

### Settings

- [x] T011 [P] Implement the `Secret` type whose `String`, `GoString`, `MarshalJSON`, and `LogValue` all redact, with a single `Reveal()` accessor, in `internal/config/secret.go` (FR-043, FR-069)
- [x] T012 [P] Write tests asserting `Secret` redacts under `fmt`, `%v`, `%#v`, `encoding/json`, and `slog`, in `internal/config/secret_test.go` (FR-043, FR-069)
- [x] T013 [P] Implement XDG config- and state-path resolution treating unset **and empty** variables as absent, in `internal/config/paths.go` (FR-053, FR-065)
- [x] T014 [P] Write path-resolution tests for `XDG_CONFIG_HOME`/`XDG_STATE_HOME` set, unset, and set-but-empty, in `internal/config/paths_test.go` (FR-053, FR-065)
- [x] T015 Define the settings structs and all defaults from `contracts/config-schema.md` in `internal/config/settings.go` (FR-055, FR-056)
- [x] T016 Implement loading with `go-toml/v2` strict decoding (`DisallowUnknownFields`) in `internal/config/load.go` (FR-052, FR-054, research R-004)
- [x] T017 Implement validation that accumulates **every** problem before returning one aggregated error, in `internal/config/validate.go` (FR-055, FR-058)
- [x] T018 Implement credential resolution with `MIKO_POST_TELEGRAM_BOT_TOKEN` taking precedence over `sink.telegram.bot_token`, as the only environment-variable override in v0.1, in `internal/config/credential.go` (FR-042)
- [x] T019 [P] Write credential-precedence tests covering env-only, file-only, both-set (env wins), and neither-set-while-enabled (validation error), in `internal/config/credential_test.go` (FR-042, FR-055)
- [x] T020 Add TOML fixtures (valid, unknown key, non-namespaced `[telegram]`, missing required key, all-disabled) in `testdata/config/` and the loader tests in `internal/config/load_test.go` (FR-054, FR-058)

### Diagnostics foundation

- [x] T021 [P] Define every stable event name from `contracts/log-events.md` as typed constants in `internal/logging/events.go` (FR-067)
- [x] T022 [P] Write a test asserting the exact complete set of event-name constants so a rename or typo cannot pass silently, in `internal/logging/events_test.go` (FR-067)
- [x] T023 Implement the `slog` JSON-handler logger, creating the log directory when missing and failing soft (never panicking, never blocking a post) when it cannot, in `internal/logging/logger.go` (FR-064, FR-075)
- [x] T024 Write tests asserting every emitted line is an independently valid self-contained JSON object carrying `ts`, `level`, `event`, `source`, and `message_id`, in `internal/logging/logger_test.go` (FR-064, FR-066, SC-007)

### Posting core

- [x] T025 Implement `post.Service` — generate the ULID, start every enabled sink in its own goroutine with its **own** `context.WithTimeout` derived from `context.Background()` (never a shared cancellable parent), await all via `sync.WaitGroup`, and aggregate — in `internal/post/service.go` (FR-012 – FR-016, constitution principle I)
- [x] T026 Write orchestrator tests using fake sinks that assert **both** sinks ran and **both** results were reported when one fails, when both fail, and when one blocks; run them under `-race`, in `internal/post/service_test.go` (FR-013, FR-014, FR-070, constitution Quality Gates)

**Checkpoint**: The posting core, settings, and diagnostics exist and are tested. User story work can begin.

---

## Phase 3: User Story 1 - Capture a thought from the terminal (Priority: P1) 🎯 MVP

**Goal**: A user types `mp "message"` and it lands in both Telegram and today's daily note, with a one-glance report and a correct exit status.

**Independent Test**: Run the command with a message and both sinks enabled. Verify the message arrives in the chat, appears as a new line in today's note, that both outcomes are reported, and that the command exits `0`.

### Tests for User Story 1

- [ ] T027 [P] [US1] Write daily-note append tests using `t.TempDir()` covering a new note, an existing note, and an existing note not ending in a newline, in `internal/sink/obsidian/sink_test.go` (FR-044, FR-046, FR-049, SC-009)
- [ ] T028 [P] [US1] Write Telegram happy-path tests against an `httptest` server asserting `chat_id`, verbatim `text`, `parse_mode=MarkdownV2`, and `message_thread_id` present only when configured, in `internal/sink/telegram/sink_test.go` (FR-031 – FR-033, FR-040)
- [ ] T029 [P] [US1] Write a CLI end-to-end test covering single-quoted message, multiple bare words joined with one ASCII space, and whitespace-only rejection with no sink contacted, in `internal/cli/cli_test.go` (FR-003, FR-004, FR-010)

### Implementation for User Story 1

- [ ] T030 [P] [US1] Implement daily-note path resolution from `daily_note_dir` and `filename_format` in local time, in `internal/sink/obsidian/path.go` (FR-044, FR-051)
- [ ] T031 [P] [US1] Implement the four-step entry transformation in its normative order — CRLF and bare CR to LF, then LF to `<br>`, then the `- <time> ` prefix — in `internal/sink/obsidian/transform.go` (FR-047, SC-010)
- [ ] T032 [US1] Implement the append writer using `O_APPEND|O_WRONLY` plus `O_CREATE` only when `create_if_missing` is true, never `O_TRUNC`, writing UTF-8 with exactly one trailing LF, in `internal/sink/obsidian/sink.go`; do **not** search for, create, or insert into a named section (FR-045, FR-046, FR-048 – FR-050)
- [ ] T033 [P] [US1] Implement the Telegram request builder with an injectable base URL defaulting to `https://api.telegram.org`, in `internal/sink/telegram/request.go` (FR-031, FR-032, research R-008)
- [ ] T034 [P] [US1] Implement Telegram response decoding of `ok`, `error_code`, and `description`, in `internal/sink/telegram/response.go` (FR-035, FR-066)
- [ ] T035 [US1] Implement `Send` performing the single MarkdownV2 attempt bounded by the configured request timeout, in `internal/sink/telegram/sink.go`; do **not** add a queue or automatic re-send (FR-019, FR-033, FR-040, FR-041)
- [ ] T036 [US1] Implement sink construction from settings that builds **only** enabled sinks, in `internal/post/build.go` (FR-016)
- [ ] T037 [US1] Implement flag parsing, message-argument joining with exactly one ASCII space, and dispatch between posting and the window, in `internal/cli/cli.go` (FR-002 – FR-004, FR-008)
- [ ] T038 [US1] Implement per-sink result rendering matching `contracts/cli-interface.md`, in `internal/cli/render.go` (FR-062)
- [ ] T039 [US1] Implement `cmd/mp/main.go` wiring settings, logging, and the posting service, with exactly **one** `os.Exit` call site computing the status from the aggregate — this single binary providing both front doors is what satisfies FR-001 (FR-001, FR-059, FR-060)
- [ ] T040 [US1] Emit `message_received`, the four sink start/succeed/fail events, and `request_completed`/`request_completed_with_error` from the orchestrator, in `internal/post/service.go` (FR-067)

**Checkpoint**: US1 is fully functional and independently testable — the MVP slice.

---

## Phase 4: User Story 2 - Capture a thought without a terminal (Priority: P1)

**Goal**: `mp` with no arguments opens a focused window that sends with one keystroke, reports both outcomes, and closes itself.

**Independent Test**: Launch with no arguments, type a multi-line message, send with `Cmd+Enter`, and verify both destinations received it, that the result panel names both outcomes, and that the application exits on its own with a status matching the outcomes.

### Tests for User Story 2

- [ ] T041 [P] [US2] Write headless tests using `fyne.io/fyne/v2/test` asserting `Enter` inserts a line break, `Cmd+Enter` submits, and `Esc` cancels **while the entry holds focus**, in `internal/gui/entry_test.go` (FR-022, research R-003)
- [ ] T042 [P] [US2] Write a window test asserting the Send control and `Cmd+Enter` reach the same submission path and that Send is disabled for the duration of a submission, in `internal/gui/window_test.go` (FR-023, FR-024)

### Implementation for User Story 2

- [ ] T043 [US2] Implement the extended `widget.Entry` overriding `TypedShortcut` and `TypedKey`, delegating unhandled events to the embedded `Entry` so standard editing and IME input keep working, in `internal/gui/entry.go` (FR-021, FR-022, research R-003)
- [ ] T044 [US2] Implement the window containing exactly the message field, Send, Cancel, and a compact result area, with the field focused at launch, in `internal/gui/window.go` (FR-020, FR-021)
- [ ] T045 [US2] Implement the single `submit()` path shared by the Send control and `Cmd+Enter`, calling the same `post.Service` the CLI uses, in `internal/gui/window.go` (FR-023, constitution principle II)
- [ ] T046 [US2] Disable the Send control for the whole submission so a post cannot be submitted twice, in `internal/gui/window.go` (FR-024)
- [ ] T047 [US2] Implement the compact per-sink result panel showing short reasons only, never detailed errors or traces, in `internal/gui/result.go` (FR-025, FR-029)
- [ ] T048 [US2] Implement the auto-close timers using `gui.success_close_seconds` and `gui.error_close_seconds`, in `internal/gui/window.go` (FR-026)
- [ ] T049 [US2] Cancel the pending auto-close on **any** interaction — keypress, click, or the window regaining focus — leaving the result on screen until dismissed, in `internal/gui/window.go` (FR-028, SC-012)
- [ ] T050 [US2] Register the `Cmd+Q` canvas shortcut and implement Cancel/`Esc` closing without contacting any sink, in `internal/gui/window.go` (FR-022)
- [ ] T051 [US2] Propagate the post outcome to the process exit status when the window closes, whether by auto-close or by the user, in `internal/gui/window.go` and `cmd/mp/main.go` (FR-027)

**Checkpoint**: US1 and US2 both work independently through the same posting core.

---

## Phase 5: User Story 3 - Never lose a message to a single broken destination (Priority: P1)

**Goal**: One broken destination never suppresses the other, and the user is told plainly which half failed.

**Independent Test**: Make exactly one destination fail, send a message, and verify the healthy destination still received it, that the report names the failed destination with a short reason, and that the run exits `1`.

### Tests for User Story 3

- [ ] T052 [P] [US3] Write a timeout test asserting a hanging sink yields a timeout failure at its configured limit while the other sink completes and reports its **real** outcome, in `internal/post/service_test.go` (FR-015, SC-011)
- [ ] T053 [P] [US3] Write a both-sinks-fail test asserting both failures are reported and both are logged, neither hidden behind the other, in `internal/post/service_test.go` (FR-014, FR-070)
- [ ] T054 [P] [US3] Write a `-race` test asserting a slow-failing sink cannot cancel or alter a concurrently running sibling, in `internal/post/service_test.go` (constitution principle I)

### Implementation for User Story 3

- [ ] T055 [US3] Apply each sink's overall timeout as its own independent context, converting expiry into a failure result rather than an abort of the post, in `internal/post/service.go` (FR-015)
- [ ] T056 [US3] Implement error classification producing short, safe display reasons from a fixed set (`request timed out`, `permission denied`, `chat not found`, …) while retaining the detailed error for the log, in `internal/post/reason.go` (FR-017, FR-029)
- [ ] T057 [US3] Include the resolved diagnostic log path in user-facing failure output from **both** front doors, in `internal/cli/render.go` and `internal/gui/result.go` (FR-063, SC-003)
- [ ] T058 [US3] Ensure partial success is visible from both front doors — a succeeded sink is reported alongside a failed one — in `internal/cli/render.go` and `internal/gui/result.go` (FR-062, SC-002)

**Checkpoint**: Sink independence is demonstrated, not merely intended.

---

## Phase 6: User Story 4 - Deliver reliably despite message formatting (Priority: P2)

**Goal**: Ordinary punctuation, emoji, and Japanese text still reach the chat, via exactly one unformatted rescue, without the user knowing.

**Independent Test**: Send a message the chat service rejects for formatting, and verify it is nonetheless delivered as unformatted text, that the run is reported as an overall success, and that both the rejection and the rescue appear in the log.

### Tests for User Story 4

- [ ] T059 [P] [US4] Write a table-driven test of the rescue predicate covering a formatting 400, a **non**-formatting 400, 401, 403, 429, 5xx, and a transport error — asserting a rescue **only** for the first — in `internal/sink/telegram/rescue_test.go` (FR-035, FR-038, FR-041)
- [ ] T060 [P] [US4] Write tests asserting a successful rescue yields overall sink success and a failed rescue yields failure with both attempts retained, in `internal/sink/telegram/sink_test.go` (FR-036, FR-037, FR-061)

### Implementation for User Story 4

- [ ] T061 [US4] Implement the rescue predicate `ok == false && error_code == 400 && description contains "can't parse entities"` as a single isolated function, failing closed for every other failure, in `internal/sink/telegram/rescue.go` (FR-035, FR-038, research R-008)
- [ ] T062 [US4] Implement the single unformatted retry that re-sends the message verbatim with `parse_mode` **omitted**, executed at most once and still bounded by the sink's overall timeout, in `internal/sink/telegram/sink.go` (FR-033, FR-035, FR-040)
- [ ] T063 [US4] Emit `telegram_markdown_failed` followed by `telegram_plaintext_succeeded` or `telegram_plaintext_failed` as distinct stable events, in `internal/sink/telegram/sink.go` (FR-039, FR-067)
- [ ] T064 [US4] Accept and validate `parse_mode` and `fallback_to_plain_text` while asserting by test that neither alters v0.1 delivery behavior, in `internal/config/validate.go` and `internal/sink/telegram/sink_test.go` (FR-034, FR-057)

**Checkpoint**: The chat destination survives ordinary punctuation without escaping the user's text.

---

## Phase 7: User Story 5 - Reconstruct what happened after the fact (Priority: P2)

**Goal**: The diagnostic log is a durable, machine-readable, secret-free record sufficient to re-send a failed post by hand.

**Independent Test**: Perform one successful post and one failing post, then verify the log contains one valid record per line, that records from a post share a correlation identifier, that the successful post's records contain no message body, and that the failed post's records do.

### Tests for User Story 5

- [ ] T065 [P] [US5] Write rotation tests covering the size trigger, the age trigger, both conditions evaluated before each write, the `YYYYMMDDhhmmss` suffix, and that no rotated file is ever deleted, in `internal/logging/rotate_test.go` (FR-072 – FR-074)
- [ ] T066 [P] [US5] Write message-capture tests asserting a fully successful post records no body while a failed post records the original body, in `internal/logging/logger_test.go` (FR-068, SC-008)
- [ ] T067 [P] [US5] Write a degraded-logging test asserting that with an unwritable log path every sink still runs, real outcomes are reported, the exit status is unchanged, and **exactly one** warning is emitted, in `internal/post/service_test.go` (FR-076, SC-013)

### Implementation for User Story 5

- [ ] T068 [US5] Implement the rotating writer that captures the active file's creation time at open, tracks size, and evaluates both conditions before every write, renaming the active file with the local-time `YYYYMMDDhhmmss` suffix, in `internal/logging/rotate.go` (FR-072, FR-073, research R-006)
- [ ] T069 [US5] Implement creation-time lookup via `Birthtimespec` behind a build tag, with a portable `ModTime()` fallback, in `internal/logging/birthtime_darwin.go` and `internal/logging/birthtime_other.go` (FR-072, resolves A-011)
- [ ] T070 [US5] Implement the rotation-name collision rule appending `-1`, `-2`, … so a rotated file is never overwritten, in `internal/logging/rotate.go` (resolves A-009, constitution principle VI)
- [ ] T071 [US5] Implement the `message_on_error_only` capture rule — omit the body on full success, include it when any sink failed — in `internal/logging/logger.go` (FR-068)
- [ ] T072 [US5] Record `message_len` as a rune count and `message_bytes` as the UTF-8 byte length on successful posts, in `internal/logging/logger.go` (resolves A-010, FR-068)
- [ ] T073 [US5] Record stack traces for panics and unexpected errors subject to the `stack_trace` setting, and **never** manufacture a trace for an expected operational error, in `internal/logging/logger.go` (FR-071)
- [ ] T074 [US5] Surface a single warning naming the log path and reason through the front door in use when diagnostics cannot be written, without altering reported outcomes or the exit status, in `internal/cli/render.go` and `internal/gui/result.go` (FR-076, A-012)

**Checkpoint**: A failed post is fully reconstructible from the log alone.

---

## Phase 8: User Story 6 - Point the tool at the right configuration (Priority: P3)

**Goal**: Settings resolve from the conventional location by default, a CLI-only override exists, and every settings failure is actionable through the front door in use.

**Independent Test**: With no override, confirm both front doors resolve the same conventional path. Then run a CLI post with an explicit settings file and confirm only that post used it, while a subsequently launched window still used the default.

### Tests for User Story 6

- [ ] T075 [P] [US6] Write a help-output test asserting the **resolved** default path appears rather than an unexpanded `$XDG_CONFIG_HOME` expression, in `internal/cli/help_test.go` (FR-007)
- [ ] T076 [P] [US6] Write override tests asserting `-c` applies to that CLI post only, that `-c` without a message errors without opening the window, and that a subsequently launched window uses the default path, in `internal/cli/cli_test.go` (FR-005, FR-006)
- [ ] T077 [P] [US6] Write tests for the all-sinks-disabled startup error and for unreadable and invalid settings, asserting no sink is started with partially valid settings, in `internal/config/validate_test.go` (FR-018, FR-058)

### Implementation for User Story 6

- [ ] T078 [US6] Enforce that the `-c`/`--config` value reaches only the CLI posting path and is never passed to the window constructor, in `internal/cli/cli.go` and `cmd/mp/main.go` (FR-005, constitution principle V)
- [ ] T079 [US6] Implement the `--config` without a message error printing `--config is only available when posting from CLI`, not opening the window, and exiting `1`, in `internal/cli/cli.go` (FR-006)
- [ ] T080 [US6] Implement help output matching `contracts/cli-interface.md` including the resolved default settings path, in `internal/cli/help.go` (FR-007)
- [ ] T081 [US6] Implement the all-sinks-disabled startup error with an actionable message, no post attempt, and a failure exit, in `internal/config/validate.go` and `internal/cli/cli.go` (FR-018)
- [ ] T082 [US6] Implement the minimal startup-error window showing the actionable message and the resolved settings path, offering no message field, exiting `1` when dismissed, in `internal/gui/errorwindow.go` (FR-030) — depends on the `internal/gui` package from US2
- [ ] T083 [US6] Surface settings load and validation failures through the front door that was used, in `internal/cli/cli.go` and `internal/gui/errorwindow.go` (FR-058)

**Checkpoint**: All six user stories are independently functional.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Constitution gates, traceability, and distribution readiness

- [ ] T084 [P] Write the secret-leak gate test that posts through a fully wired stack with a sentinel token and asserts the sentinel appears in **no** log line and **no** user-facing string, in `internal/post/secret_leak_test.go` (FR-043, FR-069, SC-006, constitution Quality Gates)
- [ ] T085 [P] Write the traceability matrix mapping all 20 acceptance criteria from `docs/design.md` §13 to their covering tests in `specs/001-dual-sink-quick-post/acceptance-matrix.md` (SC-014)
- [ ] T086 [P] Record explicit justifications for any acceptance criterion verified manually rather than automatically (live Telegram delivery, interactive auto-close cancellation) in `specs/001-dual-sink-quick-post/acceptance-matrix.md` (constitution Quality Gates)
- [ ] T087 [P] Write `README.md` with install, configuration, and usage, and add a commented example settings file at `testdata/config/example.toml`
- [x] T088 [P] Replace the `<module-path>` placeholder in `docs/design.md` §14 with `github.com/sgykfjsm/miko-post` (research R-001, resolves A-007)
- [ ] T089 Verify `go vet ./...` and `go build ./...` are clean and that `go test -race ./...` passes
- [ ] T090 Verify `go install github.com/sgykfjsm/miko-post/cmd/mp@latest` produces a working `mp` whose log records carry a non-empty version, and a commit recovered from the pseudo-version when `@latest` resolves to an untagged commit. A **tagged** install records no commit and correctly reports `unknown` — assert that, do not treat it as a failure (research R-009)
- [ ] T091 Execute all nine scenarios in [quickstart.md](./quickstart.md) end to end and record the results

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Setup — **blocks every user story**
- **User Stories (Phases 3–8)**: All depend on Foundational
- **Polish (Phase 9)**: Depends on the user stories being complete

### User Story Dependencies

- **US1 (P1)**: Foundational only. The MVP.
- **US2 (P1)**: Foundational only. Uses the same `post.Service` as US1 but shares no code with `internal/cli`, so it is independently testable.
- **US3 (P1)**: Foundational only for its orchestrator work (T052–T056). T057–T058 touch the rendering added by US1 and US2, so schedule them after those stories if all three are in flight.
- **US4 (P2)**: Needs the Telegram sink from US1 (T033–T035).
- **US5 (P2)**: Needs the logging foundation from Phase 2 (T023) and the orchestrator from T025.
- **US6 (P3)**: Needs the settings foundation from Phase 2. T082 additionally needs the `internal/gui` package from US2 — the only genuine cross-story dependency in the plan.

### Within Each User Story

- Tests are written first and must fail before the implementation lands
- Types before services, services before front doors, core before integration

### Parallel Opportunities

- T002–T005 (Setup) run in parallel
- T006–T014 and T021–T022 (Foundational) run in parallel — different files, no shared state
- T027–T029, T041–T042, T052–T054, T059–T060, T065–T067, T075–T077 (each story's tests) run in parallel within their story
- T030/T031 and T033/T034 run in parallel — separate files within their sinks
- US4, US5, and US6 can proceed in parallel once their prerequisites are met
- T084–T088 (Polish) run in parallel

---

## Parallel Example: User Story 1

```bash
# Launch all US1 tests together:
Task: "Daily-note append tests in internal/sink/obsidian/sink_test.go"
Task: "Telegram happy-path httptest tests in internal/sink/telegram/sink_test.go"
Task: "CLI end-to-end tests in internal/cli/cli_test.go"

# Then launch the independent US1 leaf implementations together:
Task: "Daily-note path resolution in internal/sink/obsidian/path.go"
Task: "Entry transformation in internal/sink/obsidian/transform.go"
Task: "Telegram request builder in internal/sink/telegram/request.go"
Task: "Telegram response decoding in internal/sink/telegram/response.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational — **critical**, blocks everything
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: run quickstart Scenarios 1 and 2
5. At this point the product's core value exists: one command, two destinations

### Incremental Delivery

1. Setup + Foundational → the posting core exists and is race-tested
2. **US1** → the CLI works end to end (**MVP**)
3. **US2** → the second front door, over the same core
4. **US3** → independence and partial failure are demonstrated
5. **US4** → the chat destination stops failing on ordinary punctuation
6. **US5** → failed posts become reconstructible
7. **US6** → settings resolution, override, and actionable startup errors
8. Polish → constitution gates and distribution

### Scope Discipline (constitution, Additional Constraints)

The spec's **Out of Scope for This Version** list is binding and must not be implemented
opportunistically: no image posting, no settings window, no retry queue, no stdin input, no
automatic transport retries, no escaping of the user's message, no runtime formatting-mode
selection, no per-invocation sink flags, no automatic log retention or deletion, no additional
sink types, and no platform bundles or code signing.

---

## Notes

- `[P]` tasks touch different files and have no dependency on an incomplete task
- Every user story phase is a complete, independently testable increment
- Verify each test fails before implementing against it
- Commit after each task or logical group
- **Total: 91 tasks** — Setup 5, Foundational 21, US1 14, US2 11, US3 7, US4 6, US5 10, US6 9, Polish 8
