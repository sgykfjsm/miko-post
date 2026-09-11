# Project status — miko-post

_Last updated: 2026-09-11_

## Objective

Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): a single Go binary `mp` that
posts one short message to Telegram and to today's Obsidian daily note concurrently and
independently, from either a CLI or a GUI front door, through one shared posting core.

## Status

**In execution.** Planning artifacts are complete and stable. Work proceeds one reviewable batch
per PR, driven by the `run-batch-cycle` skill.

- Phase 1 Setup — complete (Batch 1, `0a00212`, PR #93)
- Phase 2 Foundational — **complete** (posting core contracts, settings, diagnostics, orchestrator)
- Phase 3 US1 (the CLI slice) — scheduled implementation complete with Batch 6c-2; review requests changes
- Phases 4-9 (five user stories, polish) — not started; Batch 7 is US2, the Fyne GUI

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

- **Batch 6a** — Obsidian daily-note sink (T027, T030–T032). PR #112, merged as `69e17e3`.
- **Batch 6b** — Telegram chat sink (T028, T033–T035). PR #116, merged as `13705e0`.
- **Batch 6c-1** — the US1 front door and wiring: the `internal/app` composition root, CLI parsing
  and rendering, and `cmd/mp` with one `os.Exit` (T029, T036–T039; issues #30, #37–#40). Also
  discharged #104 (the invalid-UTF-8 decision), #107, #109 and #114. PR #120, merged as `f757707`.

## In progress

**Batch 6c-2 — event emission.** Branch `sgykfjsm/batch-6c2-event-emission-2`,
committed through **`1420095`**. The complete local batch passed correctness and
independent adversarial review in cycle 1; no code findings remain. Overall review
verdict remains **request-changes for publication metadata only (CON-101)**.
PR #121 still points to `124abea`; nothing was pushed or changed remotely.

The prior three findings are resolved: bounded asynchronous production logging,
real overlapping-post JSONL ID/path validation across midnight (including refusal),
and explicit terminal failure-field applicability. One ordered logger worker holds
at most 256 pending entries; flush/close wait at most 250 ms. Saturation or timeout
drops the backlog and warns; an in-flight write may land late and retain its worker
and handle until it returns. These costs are documented and tested.

Fresh validation at `1420095`: `make check`, an uncached full race suite, and diff
checks passed. Coverage is post/app 100.0%, logging 99.3%. Correctness inspected all
114 hunks across 26 files. A fresh read-only ephemeral Codex session performed the
independent adversarial stage after the built-in launcher reached its thread cap;
it verified file hashes and inspected tests, relying on the fresh executed evidence.
The historical 32-mutant campaign and live Telegram run were not rerun at this head.

**Publication requirements:** refresh the PR body for the new queue policy, current
validation, and T040 completion with #119 still open. A draft is stored beside the
cycle-1 report. Selected acceptance for #41/#98/#110/#111 is supported by this local
head, but no issues were closed. **CON-102 is non-blocking:** T053's annotation should
acknowledge the existing both-failure logging test without closing adjacent work.

Under **DEC-D2** the orchestrator emits through a domain-shaped `post.Recorder` declared in
`internal/post`, with the mapping onto `logging.Event` in `internal/app/recorder.go`, so
`internal/post` still imports no other internal package — `internal/app/layering_test.go` fails the
build if it acquires one. Under **DEC-D3** `post.ReportTarget` carries the resolved note per call on
the context, which closes #98 and #111 together; `post.Targeter` is deleted, and the obsidian sink
lost its target field and mutex with it.

**Three decisions this batch took, recorded as DEC-E1 to DEC-E3 in `state.yaml`.** `TargetReporting`
is a declaration interface with one empty method, because the orchestrator has to know *before*
`Send` whether to hold a sink's start event, and no method can return a per-post value.
`error_type` is emitted now with two values from the same predicate that picks the display reason,
so T056 widens the vocabulary rather than changing it. #110's recovered `Name` panic goes on the
post's terminal record, because a panicking `Name` resolves to a sentinel the sink-keyed event
vocabulary cannot cover — which also means an unmappable sink name would otherwise have produced no
records and said nothing.

**All four inherited review obligations are discharged**, including the load-bearing one:
`TestTheAdapterProducesEveryOrchestratorReachableEvent` compares the adapter's producible set
against `logging.AllEvents()` in both directions, with the three formatting-fallback names listed as
deferred to T063. Killed by a mutant that removes the telegram lifecycle from the adapter's table.

**Both open questions answered rather than defaulted.** `error_type`: emitted (DEC-E2). `message`:
not emitted anywhere, with FR-068 recorded as knowingly unmet until T071 in `tasks.md` and in a new
section of `contracts/log-events.md`.

**One fix reaches outside the batch.** `internal/logging` emitted `level` as `"INFO"`/`"ERROR"` for
every logger with `Options.Redact` armed — so every real run since 6c-1 — because the credential
scrub consumed the level attribute before the rename could see it. T040's records cannot satisfy
`contracts/log-events.md` without it. Three lines, plus the test that arms `Redact` and would have
caught it; the package's own lowercase test passed throughout because it configures no credential.

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

Batch 6c-2 has no remaining code findings. CON-101 requires accurate PR publication metadata. Other tracked items:

- Issue #94 (`git_commit` reads `unknown` on tagged installs) must be resolved before `v0.1.0` is
  tagged. Still an open maintainer decision.
- Issue #119 is open and untouched: no test drives two destinations both succeeding, and `cli.Run`
  still has no sink seam. Batch 6c-2 changed `NewService`'s signature in that same function without
  closing it — a seam is its own decision, and the issue's acceptance is a mutant rather than a test.
- The live check that would settle DEC-D4's one unverified premise — one `sendMessage` with
  `text=a%FFb` — now hangs on #92 (T091), which is the only task with a bot token in scope. Issue
  #104 is closed.

## Next best action

Publish reviewed commit `1420095` with the prepared PR #121 description when authorized,
then verify the remote head and description. The local code review is complete;
CON-101 remains open until publication metadata is updated.

After 6c-2, **Batch 7 (US2, the Fyne GUI, T041–T051)** is next. It needs the `fyne/v2` pin, which is
the last third of issue #4 and the first dependency added since batch 5.
