# Project status — miko-post

Updated 2026-09-24.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
In execution. **84 of 91 tasks are `[x]`; 7 remain (Batch 12, T084–T091).** `main` is at
`dfdc69fb8c55065d6a2f470f801c0ddc1718ef13` (PR #134, records only, on top of Batch 10b's `d6fcb6b`).
The T075–T083 ticks are on the Batch 11 branch, which is not merged.

## Completed
Batches 1–10b, the GUI background (#122) and the records PR #134 are merged. Batch 10b's detail is
in the archived status page and in `.agents/state.yaml` under `completed`.

## In progress
**Batch 11 — US6 settings resolution (T075–T083 / #76–#84, plus #119 and #130).** It is implemented
on `sgykfjsm/record-pr134-run-batch11`: `13379f8` is the PR #134 merge record, `25a4f1d` the batch,
and fix passes 1 and 2 are uncommitted. It has **not been pushed and has no PR.** It delivers:

- FR-006: `-c` without a message is refused with the contract's text, and no window opens.
- FR-007: help prints the resolved default path and exits 0.
- FR-018 and FR-058: settings with no destination, or invalid settings, are refused through the
  shared `app.LoadSettings` before any logger or sink exists.
- FR-030: a minimal startup-error window.
- #130: load-time upper bounds on the rotation keys.
- #119: a two-destinations-succeeding test inside `internal/cli`.

The three-stage review has run three times (cycles 0, 1 and 2), with two fix passes. Cycle 0 had
ten findings across the three stages: CON-001–004, COR-001–003 and ADV-001–003. The most serious
was that **Esc could not dismiss the startup-error window** in the real driver while its test
passed. After fix pass 2, all 37 distinct mutants are killed, and the cycle 2 re-review is
finishing.

## Blockers
None. Both maintainer calls are made (2026-09-24): **DEC-H1** accepts the `-c ""` refusal, and
**DEC-H2** means no startup-error window opens when no settings path can be resolved.

## Next best action
Finish Batch 11's review loop, commit, then push and open the PR with the maintainer's go-ahead.
After merge, in the same session, close #76–#84, #119 and #130. Then Batch 12 (polish and gates,
T084–T091).

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
- **#119** — an internal seam on `cli.Run`, no user-visible setting. **Delivered in Batch 11**
  (not yet merged).
- **#130** — the load-time upper bound for the rotation keys. **Delivered in Batch 11** (not yet
  merged).

## Housekeeping — swept 2026-09-24
**#60–#65 closed** (T059–T064, delivered in Batch 9 / PR #125). Verified before closing: each task
is `[x]`, #125 reads back as merged, and the implementation is present.

**#115, #118 and #119 deliberately kept open** — they are real unfinished work, not stale
bookkeeping, and each now carries a comment recording the check so the next sweep does not
bulk-close them. #115's credential-free-error invariant is still unenforced, #118's `Error()` scan
is still neither pinned nor documented as intentionally unpinned, and #119's
two-destinations-both-succeeding path is still unreachable from argv.

**#4 and #42–#52 closed** on 2026-09-24 (T003 and T041–T051, delivered by Batch 7 / PR #123,
`6f40dad`), each with a comment naming the PR and merge commit.

28 issues open overall.

## Touched files
Batch 11's files are commit `25a4f1d` plus the uncommitted fix passes. `git diff --stat dfdc69f`
on the branch is the authoritative list. The outgoing copy of this page is archived as
`.agents/archive/project-status-before-batch-11-review.md`.
