# Project status — miko-post

Updated 2026-09-24.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
In execution. **75 of 91 tasks are `[x]`; 16 remain.** `main` is at
`dfdc69fb8c55065d6a2f470f801c0ddc1718ef13` (PR #134, records only, on top of Batch 10b's `d6fcb6b`) and `make check` (gofmt, vet, full `-race` suite) is
green there across all ten packages.

**US5 diagnostics is fully delivered.** Batches 1–9, the GUI background (#122), Batch 10a (lossless
rotation) and Batch 10b (post diagnostics) are all merged.

## Completed
**Batch 10b — US5 post diagnostics (T066, T067, T071–T074 / #67, #68, #72–#75)** — merged
2026-09-24 as `d6fcb6b` from PR #133, at reviewed head `a944dc9`. The squashed commit and the
reviewed head share tree `9c81f88bf10d83c39aed87cfe318a4356eb046bb`, so `main` carries byte-for-byte
what the review produced. All six issues were closed in the same session, each with a comment naming
the PR and the merge commit.

Delivered: FR-068's message capture (`message` on each record reporting a sink's own failure, with
the terminal record as a fallback only when no failure record could carry it), FR-071's traces
(`debug.Stack()` inside `internal/post`'s deferred `recover`, read back through the new
`post.Traced`), and FR-076's warning at the GUI front door, which `internal/gui` had never produced.
T072 needed no code change and was closed by mutation testing rather than by inspection.

Reviewed through all three stages with two fix passes. **37 mutants, 35 killed**; the two survivors
were a redundant guard that was deleted and one demonstrably equivalent mutant. `internal/app` at
100.0% of statements.

Two defects the review caught in the batch's own new code, both fixed before merge:

- The message body was written to the log on **every rescued — and therefore successful — post**,
  because FR-039's plaintext rescue emits a failure-shaped record inside a successful post.
- The GUI's per-post health probe called the **flushing** `Degraded()`, whose 250 ms timeout
  permanently fails the record queue: one slow write discarded every record of every later post in
  the session and froze the window. Measured as 1 surviving record against 12. Fixed with
  `logging.DegradedSoFar`, and the wiring is now pinned by an AST scan because swapping it back had
  survived the entire suite.

## In progress
Nothing. The worktree carries only the long-standing, deliberately excluded
`.specify/integrations/claude.manifest.json` edit.

## Blockers
None.

## Next best action
**Implement Batch 11 — US6 settings resolution: T075–T083, issues #76–#84**, on a fresh branch from
merged `main`, then the usual three-stage review. This is the CLI front door's remaining behaviour:
the `-c`/`--config` override reaching only the CLI posting path and never the window constructor
(FR-005, constitution principle V), `--config` without a message erroring without opening the window
(FR-006), help output carrying the **resolved** default settings path (FR-007), the
all-sinks-disabled startup error (FR-018) and the minimal startup-error window (FR-030).

Two things to settle early: **T081 is the requirement T039's own acceptance note records as not
implemented** — both destinations disabled currently reports after the fact and exits `1` rather
than refusing beforehand — and `contracts/cli-interface.md` is the contract T080 must match exactly.
T082 depends on the `internal/gui` package from US2, which exists.

Then Batch 12 (polish and gates, T084–T091).

## Important decisions
Batch 10b's decisions are recorded in full in `.agents/state.yaml` under `completed` 10b as
DEC-G1, DEC-G1a and DEC-G2 through DEC-G8. The load-bearing ones:

- **DEC-G1 / DEC-G1a** — the body goes on the records reporting a sink's own outcome, never on the
  formatting-fallback records, which are emitted before the post's outcome exists.
- **DEC-G3** — the stack is captured at the recovery point in `internal/post`, because `recover()`
  returns the value alone and the frames are gone by any later layer.
- **DEC-G4 / DEC-G8** — FR-071's ban on manufactured traces is discharged by construction, and
  "unexpected errors" yielding no `stack` is the requirement met rather than deferred.
- **DEC-G5 / DEC-G6** — the GUI warns once per session, and the warning replaces the log-path line
  rather than joining it.

## Decisions — cleared 2026-09-24
The standing decision backlog was resolved in one pass. **Four are decided, documented and closed**:

- **#132** — the body is captured **in full, with no bound**. SC-008's "re-sendable without
  consulting any other source" taken literally. The accepted consequence is documented in
  `contracts/config-schema.md`: `rotate_size_mib` bounds the file *between* records, not the size
  of a record, and FR-074 forbids reclaiming the space.
- **#127** — accept and document. Concurrent front doors scatter records across archives; a
  symlinked `logging.path` is unsupported under rotation.
- **#128** — accept the boundary. `rotate_after_days` is meaningful only on darwin with a recorded
  birth time.
- **#129** — accept and document. The repair newline can cross a rotation.

**Three are decided but stay open, because the decision creates scoped work:**

- **#94** — stamp version and commit through linker flags in the release path. Documented in
  `docs/design.md` §14 and its Japanese counterpart; `make install` already does it. Closes when
  **T090** asserts against the documented release path (Batch 12).
- **#119** — an internal seam on `cli.Run`, no user-visible setting. **Scoped into Batch 11.**
- **#130** — the load-time upper bound for the rotation keys. **Scoped into Batch 11**, which works
  on settings validation anyway.

## Housekeeping — swept 2026-09-24
**#60–#65 closed** (T059–T064, delivered in Batch 9 / PR #125). Verified before closing: each task
is `[x]`, #125 reads back as merged, and the implementation is present.

**#115, #118 and #119 deliberately kept open** — they are real unfinished work, not stale
bookkeeping, and each now carries a comment recording the check so the next sweep does not
bulk-close them. #115's credential-free-error invariant is still unenforced, #118's `Error()` scan
is still neither pinned nor documented as intentionally unpinned, and #119's
two-destinations-both-succeeding path is still unreachable from argv.

**#119 is worth deciding before Batch 11**, since US6 is the CLI front door work that will be in
that seam anyway.

40 issues open overall.

## Touched files
This reconciliation touches `.agents/state.yaml`, `.agents/project-status.md`,
`.agents/work-log.md` and `.agents/archive/project-status-before-batch-11.md`. Batch 10b's own files
are listed under `touched_files` in `state.yaml`.
