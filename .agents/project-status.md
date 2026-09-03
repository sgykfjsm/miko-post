# Project status — miko-post

_Last updated: 2026-09-03_

## Objective

Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): a single Go binary `mp` that
posts one short message to Telegram and to today's Obsidian daily note concurrently and
independently, from either a CLI or a GUI front door, through one shared posting core.

## Status

**In execution.** Planning artifacts are complete and stable. Work proceeds one reviewable batch
per PR, driven by the `run-batch-cycle` skill.

- Phase 1 Setup — complete (Batch 1, `0a00212`, PR #93)
- Phase 2 Foundational — **1 of 4 concerns complete** (posting core contracts)
- Phases 3-9 (six user stories, polish) — not started

## Completed

- **Batch 1** — Go module, package skeleton, `internal/version` with build-info fallback, Makefile.
- **Batch 2** — posting core contracts: `Message`, `SinkResult`, the `Sink` interface (T006-T010,
  issues #7-#11). PR #97, merged to `main` as `3214bbd` on 2026-09-03.

## In progress

Nothing. PR #97 is merged; no work is mid-flight.

## Blockers

None blocking. Two items are time-sensitive rather than blocking:

- Issue #98's decision must be honored when T025/T030/T040 are written, not discovered afterwards.
- Issue #94 (`git_commit` reads `unknown` on tagged installs) must be resolved before `v0.1.0` is
  tagged. Still an open maintainer decision.

## Next best action

Run `run-batch-cycle` for **Batch 3 — Settings** (T011-T020, issues #12-#21), which pins
`github.com/pelletier/go-toml/v2` and unblocks US6.

## Important decisions

| # | Decision | Where recorded |
|---|---|---|
| 1 | `AllSucceeded` returns `false` for an empty result slice, not the vacuous `true`. The aggregate drives the exit status; exiting 0 for a post that reached no destination is what the status exists to prevent. FR-018 should make it unreachable. | `internal/post/result.go`, PR #97 |
| 2 | `SinkResult` carries five render guards (`String`, `GoString`, `Format`, `MarshalJSON`, `LogValue`) routing `Err` through one `errMarker`, so no default Go render can emit the bot token. Matches the four-method pattern `data-model.md` prescribes for `Secret`, plus `Formatter`. | `internal/post/result.go`, PR #97 |
| 3 | Note events keep orchestrator ownership; the obsidian sink exposes its resolved target via an optional `Targeter` interface. Rejected re-deriving the path in the orchestrator, which double-calls `time.Now()` and misreports across local midnight. | Issue #98 |
| 4 | T003 is re-scoped: dependencies are pinned by the batch that first imports them (go-toml → settings, ulid → orchestrator, fyne → GUI), because `go mod tidy` drops an unimported requirement. | Issue #4 comment, `tasks.md` T003 |
| 5 | `Message` carries no rune/byte accessors; `data-model.md` lists them but T006 scopes the type to the original text and T072 places the derivation in `internal/logging`. | PR #97 |

## Touched files

- `internal/post/` — `message.go`, `message_test.go`, `result.go`, `result_test.go`, `sink.go`
- `specs/001-dual-sink-quick-post/tasks.md` — T006-T010 marked complete
- `.specify/integrations/claude.manifest.json` — spec-kit installer timestamp, unrelated to the feature
