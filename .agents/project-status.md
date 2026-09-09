# Project status — miko-post

_Last updated: 2026-09-07_

## Objective

Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): a single Go binary `mp` that
posts one short message to Telegram and to today's Obsidian daily note concurrently and
independently, from either a CLI or a GUI front door, through one shared posting core.

## Status

**In execution.** Planning artifacts are complete and stable. Work proceeds one reviewable batch
per PR, driven by the `run-batch-cycle` skill.

- Phase 1 Setup — complete (Batch 1, `0a00212`, PR #93)
- Phase 2 Foundational — **complete** (posting core contracts, settings, diagnostics, orchestrator)
- Phases 3-9 (six user stories, polish) — not started; Batch 6 is the first end-to-end slice

## Completed

- **Batch 1** — Go module, package skeleton, `internal/version` with build-info fallback, Makefile.
- **Batch 2** — posting core contracts: `Message`, `SinkResult`, the `Sink` interface (T006-T010,
  issues #7-#11). PR #97, merged to `main` as `3214bbd` on 2026-09-03.
- **Batch 3** — settings: XDG paths, strict TOML decoding onto defaults, the redacting `Secret`,
  credential precedence, and accumulating validation (T011-T020, issues #12-#21). Pins
  go-toml/v2@v2.4.3. PR #100, merged to `main` as `6d84ae9` on 2026-09-07; review verdict
  passed-with-notes after three fix cycles.
- **Batch 4** — logging foundation: the twelve stable event names as typed `Event` constants with a
  test that scans the package's own source so an unregistered addition cannot pass silently, and the
  `slog` JSON-handler logger (T021–T024, issues #22–#25). No new dependency. PR #106, merged to
  `main` as `8b7bed6` on 2026-09-07; review verdict passed-with-notes after one fix cycle, 100%
  statement coverage. Also carried three `.agents` bookkeeping commits that had no PR of their own.
- **Batch 5** — orchestrator: `post.Service` and `post.Outcome`, with the per-sink timeout enforced
  by the orchestrator rather than trusted to each sink (T025–T026, issues #26–#27). Pins
  oklog/ulid/v2@v2.1.2. PR #108, merged to `main` as `35d24e2` on 2026-09-08; review verdict
  passed-with-notes after three fix cycles, 100% statement coverage. **Completes Phase 2.**

## In progress

**Batch 6c — the US1 front door and wiring.** Branch `sgykfjsm/batch-6c-us1-front-door`, cut from
merged `main` and carrying the Batch 6b merge record. T029, T036–T040 (issues #30, #37–#41).
Packages `internal/cli`, `cmd/mp`, `internal/post`.

**A decision has to be settled before implementation starts.** T036 places `build.go` in
`internal/post`, which cannot compile: the sink packages import `internal/post` to implement
`post.Sink`, so constructing them from there is an import cycle. It would also break the property
Batch 5 established and every review since has verified, that `internal/post` imports no other
internal package. The placement — `cmd/mp`, or a small wiring package — is `batch_6_split.6c_blocker`.

6c is the first batch to wire a front door, so five obligations earlier batches correctly declined
all land here: #107 (`logging.Open` ignores `ResolvePath`, so a default install writes no
diagnostics), #41 (T040's event emission, which is also what makes `Options.Redact` non-inert),
#98 boxes 1–3, #110, and #111. T040 must reach `*telegram.APIError` through `errors.As` for
`http_status`.

Batch 6 as recorded was fourteen tasks across four packages and two unrelated external surfaces,
so triage split it into 6a (merged as `69e17e3`), 6b (merged as `13705e0`) and 6c (this branch).
**Do not re-derive Batch 6 as one unit.**

## Review follow-ups

Filed 2026-09-07 from the Batch 2 and Batch 3 reviews, which had left twelve findings in local run
state only: #101 (`elide` bound gaps), #102 (safe-alphabet enumeration), #103 (`SinkResult` XML and
test-skip), #104 (decision: invalid UTF-8), #105 (`data-model.md` staleness, for spec-reconciler),
plus acceptance notes on #57 (T056) and a progress comment on #4 (T003).

Filed 2026-09-07 from the Batch 4 review: #107 (`logging.Open` does not apply `ResolvePath`, so a
default install silently writes no diagnostics), plus acceptance notes on #69 (T068 must repeat the
non-regular-file guard and `O_APPEND`, which the `Options.Writer` seam bypasses), #41 (T040 must
construct the logger with `Options.Redact`, and the `slog.Duration` nanosecond trap) and #105 (the
`"unknown"` sentinel is outside the domains `log-events.md` declares).

## Blockers

None blocking. Two items are time-sensitive rather than blocking:

- Issue #98's decision is honoured in T025 and T030; T040 still owes the `path` field on the obsidian events and chat events carrying none.
- Issue #94 (`git_commit` reads `unknown` on tagged installs) must be resolved before `v0.1.0` is
  tagged. Still an open maintainer decision.

## Next best action

Merge PR #112 (Batch 6a), then run `run-batch-cycle` for **Batch 6b — the Telegram sink** (T028,
T033–T035; issues #29, #34, #35, #36).

**Batch 6c** — the CLI and wiring (T029, T036–T040) — comes last and owns every obligation the
earlier batches correctly declined:

- **#107** — `logging.Open` does not apply `ResolvePath`, so a default install writes no
  diagnostics at all (T039).
- **#41** — T040 must pass `Options.Redact` or the bot-token scrub is inert.
- **#98** — the `path` field on the three obsidian events, and chat events carrying none (T040).
  Batch 6a discharged the `Sink`-stays-two-methods box and the midnight property at the sink.
- **#109** — an upper bound on `sink_timeout_seconds`, at whichever task converts seconds to a
  `time.Duration` (T036).
- **#110** — a `Name()` panic leaves no trace for FR-071, if that is to be recorded.
- **#111** — `Targeter` reports the last *started* post's path, not the calling post's, when two
  posts overlap (T040).

6c also carries a known blocker: T036 places `build.go` in `internal/post`, which cannot compile —
the sink packages import `internal/post` to implement `post.Sink`, so `internal/post` constructing
them is an import cycle. Reproduced during the 6a contract review. It has to live in `cmd/mp` or a
small wiring package.

## Important decisions

| # | Decision | Where recorded |
|---|---|---|
| 1 | `AllSucceeded` returns `false` for an empty result slice, not the vacuous `true`. The aggregate drives the exit status; exiting 0 for a post that reached no destination is what the status exists to prevent. FR-018 should make it unreachable. | `internal/post/result.go`, PR #97 |
| 2 | `SinkResult` carries five render guards (`String`, `GoString`, `Format`, `MarshalJSON`, `LogValue`) routing `Err` through one `errMarker`, so no default Go render can emit the bot token. Matches the four-method pattern `data-model.md` prescribes for `Secret`, plus `Formatter`. | `internal/post/result.go`, PR #97 |
| 3 | Note events keep orchestrator ownership; the obsidian sink exposes its resolved target via an optional `Targeter` interface. Rejected re-deriving the path in the orchestrator, which double-calls `time.Now()` and misreports across local midnight. | Issue #98 |
| 4 | T003 is re-scoped: dependencies are pinned by the batch that first imports them (go-toml → settings, ulid → orchestrator, fyne → GUI), because `go mod tidy` drops an unimported requirement. | Issue #4 comment, `tasks.md` T003 |
| 5 | `Message` carries no rune/byte accessors; `data-model.md` lists them but T006 scopes the type to the original text and T072 places the derivation in `internal/logging`. | PR #97 |
| 6 | `Secret` holds its value behind a `*string` and implements five render guards, not the four `data-model.md` prescribes. `fmt` reaches unexported fields by reflection (`%d` printed the token), and `%p`/`%w` bypass `Formatter` — the pointer closes those two. | `internal/config/secret.go`, PR #100 |
| 7 | `Load` never renders the settings document: go-toml's `DecodeError.String()` echoes context from the enclosing table header, reproducing `bot_token` for any defect in `[sink.telegram]`. Position and key path only. | `internal/config/load.go`, PR #100 |
| 8 | The two Obsidian format keys are validated by what they **render**, and Go's `MST` element is rejected so the rendering is zone-independent. Sampling instants cannot work: a `Location` is not a zone, and a hostile TZif makes the transition schedule the attacker's. | `internal/config/validate.go`, `contracts/config-schema.md`, PR #100 |
| 9 | FR-018's all-sinks-disabled check stays out of `config.Load` — a front-door rule (T081 CLI, T082 GUI). `data-model.md` and `plan.md` amended. | `data-model.md`, `plan.md`, PR #100 |
| 10 | The logging type with no logging methods: `Logger.Post(messageID)` returns the only type that can emit. `message_id` is contractually on every record, and an attribute callers are asked to remember is one they forget — requiring it to *construct* the emitter makes the omission inexpressible. The event name travels in slog's message slot for the same reason: slog always emits a message, so the field cannot go missing. | `internal/logging/logger.go`, PR #106 |
| 11 | `Open` returns no error and no `*Degradation`. FR-076 requires a diagnostics failure to change nothing about the post, and an error return invites the caller that treats it as fatal or holds a nil `*Logger`; a second `*Degradation` return invited emitting FR-076's single warning twice. A discarding logger is still a logger, and `Degraded()` is the one source of truth — it also covers a write that fails after a successful open. | `internal/logging/logger.go`, PR #106 |
| 12 | `Open` refuses a non-regular file at the log path before opening it. `os.OpenFile` on a FIFO blocks inside `open(2)` until a reader attaches, so a named pipe at the log path stopped the post dead — no degradation, no warning, nothing. `os.Stat` not `Lstat`, so a symlink to a regular file still works. Device nodes are allowed: `logging.path` has no disable toggle, so `/dev/null` is how a user opts out. | `internal/logging/logger.go`, PR #106 |
| 13 | The bot-token scrub is a chokepoint in `internal/logging`, not call-site discipline. The contract requires an `error` field on failures and its only natural source is `SinkResult.Err` — a `*url.Error` whose URL carries the token, which no value type can defend because an error has no `LogValue` and slog hands it to `json.Marshal`. `Options.Redact` takes `config.Secret`s and `ReplaceAttr` removes them at every depth. Exact-substring, so it cannot mangle legitimate text. **T040/T063 must pass the resolved token.** | `internal/logging/logger.go`, PR #106 |
| 14 | The emission path `recover()`s and latches a panic as a degradation. `Options.Writer` is where rotation's rename/reopen logic will live, and a panic there would unwind into the sink's goroutine — diagnostics changing the post's outcome, which is the one thing FR-076 forbids. | `internal/logging/logger.go`, PR #106 |
| 15 | The orchestrator enforces the per-sink timeout rather than trusting each sink to honour its context. `os.OpenFile`/`os.File.Write` take no context, so T031's obsidian sink on a synced or network mount would otherwise hold a post open indefinitely with its sibling's finished result unreachable. Delivery runs on a buffered channel and `run` stops waiting at timeout + a 250 ms grace, abandoning the goroutine. The context stays primary; the grace makes a cooperative sink's own error win deterministically. Accepted: an abandoned goroutine (~4.9 KiB, uncapped, reclaimed on unblock) and a possible phantom write. | `internal/post/service.go`, PR #108 |
| 16 | Both of a `Sink`'s methods are sink code and both run inside the bound. `Name()` was outside it twice — first outside the panic guard, then outside the timer — and each time reproduced the same class of failure the other method's guard existed to prevent. The name is published on a buffered channel so abandonment still attributes the result. | `internal/post/service.go`, PR #108 |

## Touched files

- `internal/config/` — `secret.go`, `paths.go`, `settings.go`, `load.go`, `validate.go`,
  `credential.go`, their tests, and `export_test.go`
- `testdata/config/` — five TOML fixtures
- `go.mod`, `go.sum` — go-toml/v2@v2.4.3 as a direct requirement
- `specs/001-dual-sink-quick-post/` — `tasks.md` (T011-T020 complete), and amendments to
  `contracts/config-schema.md`, `data-model.md`, `plan.md`
- `internal/logging/` — `events.go`, `logger.go`, their tests, `logger_unix_test.go` (the FIFO
  case, build-tagged `unix`), and `export_test.go`
- `internal/post/` — `service.go`, `service_test.go` (the orchestrator), and `result.go` with one
  comment corrected
- `go.mod`, `go.sum` — `oklog/ulid/v2@v2.1.2` as a direct requirement
- `.specify/integrations/claude.manifest.json` — spec-kit installer timestamp, unrelated to the feature
