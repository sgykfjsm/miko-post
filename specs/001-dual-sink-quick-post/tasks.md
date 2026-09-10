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

- [x] T027 [P] [US1] Write daily-note append tests using `t.TempDir()` covering a new note, an existing note, and an existing note not ending in a newline, in `internal/sink/obsidian/sink_test.go` (FR-044, FR-046, FR-049, SC-009)
- [x] T028 [P] [US1] Write Telegram happy-path tests against an `httptest` server asserting `chat_id`, verbatim `text`, `parse_mode=MarkdownV2`, and `message_thread_id` present only when configured, in `internal/sink/telegram/sink_test.go` (FR-031 – FR-033, FR-040)
  - The `message_thread_id` negative is asserted as an **exact key set** rather than a single lookup, because the lookup form passes when the key is misspelled, when the form was never populated, and when no request was sent at all. Wire spellings are pinned against literals in one place so no assertion can agree with a typo in the constant it reads.
- [x] T029 [P] [US1] Write a CLI end-to-end test covering single-quoted message, multiple bare words joined with one ASCII space, and whitespace-only rejection with no sink contacted, in `internal/cli/cli_test.go` (FR-003, FR-004, FR-010)
  - **A fourth case landed with it: the invalid-UTF-8 rejection** (FR-009a, decision DEC-D4, issue #104). Both rejection reasons assert that no destination was contacted, and the same table posts successfully through the same vault fixture — so the emptiness assertion is a claim about behaviour rather than about a recorder that could never have recorded anything.
  - **The prompts are matched by a phrase only the prompt supplies**, not by "whitespace" or "UTF-8". Both words appear in the sentinels themselves, so a selection that fell through to the arm repeating the error still matched them; a mutant collapsing the two cases survived a check written that way and was caught during this batch's mutation run.
  - **Exit status is not asserted here and cannot be.** `cli.Run` returns an `int`; a test reading it says nothing about `os.Exit` being reached with that value, and `os.Exit` skips deferred flushes so a buffered writer would lose the report invisibly. `cmd/mp/main_test.go` builds the real binary and runs it for both. Verified: with `main` mutated to `os.Exit(1)` unconditionally, and again with stdout wrapped in a defer-flushed `bufio.Writer`, the whole of `./internal/...` stays green and only the process tests fail.

### Implementation for User Story 1

- [x] T030 [P] [US1] Implement daily-note path resolution from `daily_note_dir` and `filename_format` in local time, in `internal/sink/obsidian/path.go` (FR-044, FR-051)
- [x] T031 [P] [US1] Implement the four-step entry transformation in its normative order — CRLF and bare CR to LF, then LF to `<br>`, then the `- <time> ` prefix — in `internal/sink/obsidian/transform.go` (FR-047, SC-010)
- [x] T032 [US1] Implement the append writer using `O_APPEND|O_RDWR|O_NOFOLLOW` plus `O_CREATE` only when `create_if_missing` is true, never `O_TRUNC`, writing UTF-8 with exactly one trailing LF, in `internal/sink/obsidian/sink.go`; refuse a note path that is a symbolic link or not a regular file, write one leading LF when the note's last byte is not one so the entry starts its own physical line, report a missing daily-note directory as `ErrVaultMissing` rather than the `create_if_missing` refusal, and do **not** search for, create, or insert into a named section (FR-045, FR-046, FR-048 – FR-050; contracts/obsidian-sink.md as amended in Batch 6a)
  - **`post.Targeter` landed with this task**, since T030 is where the path is resolved and this batch is the first to have a type that implements it (issue #98). The obsidian sink records what `Send` actually wrote and returns that, never re-resolving. Against #98's four acceptance boxes: box 4 (`Sink` stays two methods, `Targeter` separate and optional) is **discharged**; box 2's midnight property is discharged **at the sink** but its logged half needs T040; boxes 1 (`path` on the three obsidian events) and 3 (chat events carry none) are **both** T040, and box 3 additionally needs the telegram sink to exist before anything can assert it.
- [x] T033 [P] [US1] Implement the Telegram request builder with an injectable base URL defaulting to `https://api.telegram.org`, in `internal/sink/telegram/request.go` (FR-031, FR-032, research R-008)
  - **The body is `application/x-www-form-urlencoded`, not JSON, and that is a decision (issue #113, DEC-C1).** `json.Marshal` substitutes U+FFFD for bytes that are not valid UTF-8 and returns a nil error, so a JSON body would have made Telegram store different text from the obsidian note for one post — #113's option 3, rejected in advance. Form encoding is byte-preserving. What Telegram's server then does with a non-UTF-8 sequence is outside our control; the claim is only that we do not rewrite the user's bytes. Recorded in `contracts/telegram-sink.md` as amended in Batch 6b.
- [x] T034 [P] [US1] Implement Telegram response decoding of `ok`, `error_code`, and `description`, in `internal/sink/telegram/response.go` (FR-035, FR-066)
  - Decoding fails closed at every step, and the decoded fields are carried on an exported `*telegram.APIError` reachable through `errors.As`. That type is also how `http_status` gets out of a sink whose `Send` returns a bare error — the same shape gap `post.Targeter` closes for `path` — so T040 has a route to the field contracts/log-events.md requires.
  - **Those fields are necessary but not sufficient for T061's rescue predicate, and T061 must be written knowing it.** `APIError` carries `HTTPStatus`, `Code` and `Description` but **not `ok`**; `ok` survives only as the *absence of a cause*, because two structurally different replies fill `Code` and `Description` from the same body — the genuine refusal (`ok: false`, no cause) and the contradiction (`ok: true` under a non-2xx, cause `errContradictoryStatus`). A predicate reading the three exported fields alone therefore rescues a reply that said `ok: true`, which R-008 requires to fail closed, and a proxy or captive portal answering `400` with `{"ok":true,"error_code":400,"description":"Bad Request: can't parse entities ..."}` is all it takes to reach it. **T061 must additionally exclude `errors.Is(err, errContradictoryStatus)`** — unexported, but `rescue.go` will be in-package. Asserted now by `TestAContradictoryReplyIsNotAFormattingRejection`, so a later flattening of the two paths fails here rather than in Batch 9.
- [x] T035 [US1] Implement `Send` performing the single MarkdownV2 attempt bounded by the configured request timeout, in `internal/sink/telegram/sink.go`; do **not** add a queue or automatic re-send (FR-019, FR-033, FR-040, FR-041)
  - **No error `Send` returns carries the bot token (DEC-C2).** The credential is in the request path, so net/http's `*url.Error` renders it verbatim and it would land in `post.SinkResult.Err`, which `internal/post/result.go` deliberately leaves unredacted. Every `*url.Error` layer is stripped before wrapping, with a textual net behind it, and a sentinel-token sweep asserts absence across `Error()`, `%v`, `%+v` and `%#v` for every failure class. The sink boundary is the only layer that exists today; #103 and #115 cover the general case. Recorded in `contracts/telegram-sink.md` as amended in Batch 6b.
  - **`http_timeout_seconds` is clamped at the conversion point (issue #114, DEC-C3).** T035 is the first code in the repository to turn a settings seconds value into a `time.Duration`; `18446744074` passes validation and wraps to a positive ~290ms deadline, and a non-positive value floors to FR-040's 30s because `http.Client` reads `Timeout: 0` as unbounded. Handled inside this package to keep the batch's footprint to itself, which is what lets the batch claim `internal/config` is byte-unchanged. **#114 owns the validation-layer fix** — rejecting the value at load time, where the user can be told — and names T035 as the converter; #109 is its sibling, scoping `posting.sink_timeout_seconds` rather than this key. #114 records this clamp as interim, and its acceptance carries an obligation forward: when the load-time fix lands, the clamp must be **either kept as documented defence in depth or removed as redundant — deliberately, not by omission**.
  - The sink deliberately does **not** implement `post.Targeter`: chat events carry no `path` (issue #98, box 3). A test asserts the negative, since Go cannot express it as a compile-time assertion.
- [x] T036 [US1] Implement sink construction from settings that builds **only** enabled sinks, in `internal/app/app.go` (FR-016)
  - **File path corrected from `internal/post/build.go`, which cannot compile** (decision DEC-D1). Both sink packages import `internal/post` to implement `post.Sink`, so `internal/post` constructing them is an import cycle; it was reproduced independently during the Batch 6a contract review. `cmd/mp` was rejected because `internal/gui` cannot import package `main` and would need a second copy of the wiring, and `internal/sink` because it can host this and nothing else. `internal/app` is the composition root and also holds the logger construction and the seconds-to-`Duration` conversion. **Issue #37's body still names `internal/post/build.go` and needs amending.**
  - **`internal/app` imports no front door**, or the cycle returns from the other side. Asserted, not conventional: `internal/app/layering_test.go` walks the module's import graph from source and enforces both that rule and `internal/post`'s "imports no other internal package" (DEC-D2) — the two boundaries the compiler does not enforce on its own. plan.md's Source Code layout has been amended to list the package.
  - Asserted by `Name()` across all four enabled/disabled combinations including both-disabled, not by `len()`: a length check passes when the wrong sink was built, and the swap mutant proves it — building the telegram sink under the obsidian flag keeps every count identical.
- [x] T037 [US1] Implement flag parsing, message-argument joining with exactly one ASCII space, and dispatch between posting and the window, in `internal/cli/cli.go` (FR-002 – FR-004, FR-008)
  - Joining is driven from a real `[]string` argv through the real flag set in every test. A test of the joining function alone cannot see the four things that actually go wrong between argv and a message: the parser eating a bare word, `mp -- -x`, an argument that already contains a space being re-split, and an empty argument dropped instead of contributing its separators.
  - **`--config` is dropped at the parse boundary for the window path** (FR-005), so the window is handed nothing to misuse rather than being trusted to ignore it. FR-006's error is T079 and is **not** implemented; `mp -c x.toml` therefore opens the window path on the default settings today. Recorded in `contracts/cli-interface.md` under **Not yet implemented**.
  - `-h`/`--help` is recognised and refused with a message saying help is unimplemented, rather than reported as an unknown flag, which would read to a user as their own typo. T080 owns the real output; the contract's exit `0` is not met today.
  - The flag set uses `ContinueOnError` and writes to `io.Discard`. `ExitOnError` would call `os.Exit` from inside a library, which T039 forbids and which would make every parse failure untestable.
- [x] T038 [US1] Implement per-sink result rendering matching `contracts/cli-interface.md`, in `internal/cli/render.go` (FR-062)
  - Takes a `Report` of plain strings rather than a `*logging.Logger`, so every FR-062 case is assertable byte for byte without a logger existing, and so the renderer has no route to `SinkResult.Err` — the half that carries the bot token and that `internal/post` deliberately leaves unredacted.
  - Assertions are on the **whole** of stdout, not substrings: a `Contains` check passes on a report that also printed something it should not have, and completeness and brevity are what the contract requires of this output.
  - FR-076's warning is counted, not checked for presence. `strings.Contains(out, "diagnostics")` passes when it is printed twice, and printing it twice is the specific mistake the requirement forbids.
- [x] T039 [US1] Implement the wiring of settings, logging, and the posting service with exactly **one** `os.Exit` call site computing the status from the aggregate — this single binary providing both front doors is what satisfies FR-001 — in `cmd/mp/main.go` (dispatch and the exit), `internal/app/app.go` (construction) and `internal/cli/run.go` (the CLI run sequence) (FR-001, FR-059, FR-060)
  - **File path corrected.** The task recorded `cmd/mp/main.go` alone. Under DEC-D1 `cmd/mp` parses argv via `internal/cli`, dispatches, and exits; the construction is `internal/app`'s so the GUI can reuse it, and the run sequence is `internal/cli`'s so it is testable in-process with injected writers. `main`'s body is one statement.
  - **The single call site is asserted twice**: behaviourally, by process runs that observe a real exit status for success, partial failure, rejection and a parse error; and structurally, by a syntax-tree scan of the command directory. The scan is over the AST and not the text — a grep for `os.Exit(` counts this file's own explanatory comments, which it did on the first run.
  - **Nothing writes through a buffered writer.** `os.Exit` runs no deferred function, so a defer-flushed writer loses the report, and an in-process test with an injected `io.Writer` cannot see it because the buffering it is asking about is the one it replaced.
  - Partial failure is reachable at the process level without contacting a live service: the chat destination is configured with `bot_token = "%zz"`, which makes `url.JoinPath` fail while the request URL is assembled, so the sink fails before any socket is opened.
  - **FR-018's startup error is not implemented** (T081). Both destinations disabled reports `No destination is enabled, so nothing was posted.` after the fact and exits `1`, rather than refusing beforehand with an actionable message.
- [ ] T040 [US1] Emit `message_received`, the four sink start/succeed/fail events, and `request_completed`/`request_completed_with_error` from the orchestrator, in `internal/post/service.go` (FR-067)
  - **Batch 6c-2.** Between 6c-1 and 6c-2 the binary creates its log file and leaves it empty, while printing that path on failure. Deliberate and recorded: the path is real and the file exists, only the records are owed. Under DEC-D2 the emission goes through a domain-shaped `post.Recorder` declared in `internal/post`, with the mapping onto `logging.Event` in `internal/app`, so `internal/post` keeps importing no other internal package — `internal/logging` imports `internal/config`, so `post -> logging` would reintroduce `post -> config` transitively. `internal/app/layering_test.go` now fails if it does.

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
  - **Already delivered by the orchestrator batch (T026).** `TestABlockingSinkYieldsATimeoutAndDoesNotStallItsSibling` asserts exactly this, including that the blocked sink's `Err` wraps `context.DeadlineExceeded` and that the post does not spend two timeouts. US3 is a verification pass: confirm it still holds once real sinks exist, and add SC-011's end-to-end measurement if that needs a running binary.
- [ ] T053 [P] [US3] Write a both-sinks-fail test asserting both failures are reported and both are logged, neither hidden behind the other, in `internal/post/service_test.go` (FR-014, FR-070)
  - **Half delivered by the orchestrator batch (T026).** The `both fail` subtest of `TestBothSinksRunAndBothResultsAreReported` covers the *reported* half, and FR-014 is satisfied. The *logged* half — FR-070's "logging must not stop after the first error" — has no assertion anywhere yet and cannot until T040 emits events. That half is what US3 still owes.
- [ ] T054 [P] [US3] Write a `-race` test asserting a slow-failing sink cannot cancel or alter a concurrently running sibling, in `internal/post/service_test.go` (constitution principle I)
  - **Already delivered by the orchestrator batch (T026).** `TestOneSinkFailingDoesNotCancelItsSibling` has the sibling observe its own context after its peer has failed, and the suite runs under `-race` via `make check`. It is the only test that dies to a shared-cancellable-parent mutant, so it is the load-bearing one for principle I. US3 is a verification pass.

### Implementation for User Story 3

- [ ] T055 [US3] Apply each sink's overall timeout as its own independent context, converting expiry into a failure result rather than an abort of the post, in `internal/post/service.go` (FR-015)
  - **Already implemented by the orchestrator batch (T025).** Both halves had to land together: FR-015 states them in one sentence, and a Service that let an expiry abort the post would have breached FR-014 on the day it shipped rather than in this phase. `Service.deliver` derives each context from `context.Background()` and converts expiry into a failure result. US3 is a verification pass.
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
  - **Batch 6c-1 left this deliberately undone and shaped the parser around it.** `Parse` drops the `--config` value for the window path so FR-005 holds structurally; the consequence is that `mp -c x.toml` opens the window on the default settings instead of erroring. T079 needs to know the flag was *supplied*, which the current `Invocation` cannot say — add a field then rather than now. `TestParseDispatchesToTheWindowOnlyWithNoMessage` pins today's behaviour and must be updated, not deleted.
- [ ] T080 [US6] Implement help output matching `contracts/cli-interface.md` including the resolved default settings path, in `internal/cli/help.go` (FR-007)
  - **`cli.ErrHelpNotAvailable` exists to be deleted by this task.** `-h`/`--help` is recognised today and refused with "help output is not implemented yet", exiting `1` where the contract requires `0`. `TestHelpIsRecognisedRatherThanTreatedAsATypo` and the `--help` row of `TestTheBinaryRejectsACommandLineItCannotParse` both fail once help works, which is intended: they exist so this becomes a deliberate change rather than a discovery.
- [ ] T081 [US6] Implement the all-sinks-disabled startup error with an actionable message, no post attempt, and a failure exit, in `internal/config/validate.go` and `internal/cli/cli.go` (FR-018)
  - Today a document with both destinations disabled loads cleanly, posts nowhere, prints `No destination is enabled, so nothing was posted.` and exits `1` — factual, and not FR-018's requirement, which is a refusal *before* a post is attempted carrying a message that names the fix. `cli.Render`'s `noDestinationsLine` and the "no destination was enabled" row of `TestRenderNamesEveryDestination` are what change.
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
