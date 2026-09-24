# Project status — miko-post

Updated 2026-09-18.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
In execution. 75 of 91 tasks in `tasks.md` are `[x]`; 16 remain. Batches 1–9, the separately
requested GUI background (#122) and Batch 10a are merged to `main`. `main` is at
`0e3df5ec524df20e0d1d46b4572e11dbe83e810a` and `make check` (gofmt, vet, full `-race` suite) is
green there across all ten packages.

A closure sweep is overdue beyond Batch 10a: #60–#65 (T059–T064, delivered in Batch 9 / PR #125) are
merged but still open, as are review follow-ups #115, #118 and #119. Same cause as #66/#69–#71 — no
closing keywords and no owner for the post-merge step.

## Completed
Batch 10a — lossless log rotation (T065, T068–T070 / #66, #69–#71) — is **merged**. PR #126 was
squash-merged on 2026-09-16 as `0e3df5e`, from head `73d9c57`, which carries all three review
correction passes; the merged tree is the reviewed tree. Validation on it: `make check`, a native
darwin/arm64 build, `internal/logging` at 99.5% statement coverage against a 99.3% baseline, and 44
mutants built and killed (28 across the batch's own guards, 16 across the three correction passes).
`GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/logging/` is clean.

The merged tree is provably the reviewed tree: `0e3df5e` and `73d9c57` share tree
`a26e21c56c2caa1c2100ab61e55341ed7cc654e1`.

**No final review verdict was ever recorded**, and the run manifest at
`~/.agents/review-runs/sgykfjsm__miko-post/20260916T030000Z-3be7358f` is the evidence. Cycle 2 — the
cycle that would have pronounced on the third fix pass — has contract `valid` and correctness `pass`,
but its adversarial stage sits at `status: running` and no `cycle-02` report directory was ever
written. The third fix pass then applied six findings, three of them adversarial (ADV-007–ADV-009),
so that stage did deliver; nothing then reviewed the tree those fixes produced, which is the tree
that merged. Three fix passes is the loop's maximum, so it ran out of passes rather than reaching a
verdict. Recorded as `review_verdict: not-final-at-merge` and deliberately not upgraded to
`passed-with-notes`: every finding was addressed, but "the review pronounced it sound" and "the
review ran out of road with every finding fixed" are different claims and only the second is
supported.

A competing reconciliation exists. Commit `d057f45` on branch `sgykfjsm/batch-10b-diagnostics` —
pushed, never merged to `main`, checked out in the sibling worktree
`sgykfjsm-batch-10-diagnostics-2` — recorded this same event two minutes after the merge as
`passed-with-notes` over two cycles with `adversarial: findings-none-blocking`. The manifest does not
support that: three cycles ran, and cycle 2's adversarial stage never completed. Its tree-equality
verification is better than what this session had and is adopted above. **The two records must be
reconciled before either reaches `main`.**

Issues #66, #69, #70 and #71 were closed on 2026-09-18, each with a comment naming PR #126 and
`0e3df5e`. They did not auto-close: the PR body deliberately stated that the issues stay open until
review and merge, so it carried no closing keyword, and `closingIssuesReferences` was verified empty
before the merge. That prediction was correct and nothing acted on it for two days.

The four deliberately deferred items were filed as **#127** (two `mp` processes sharing one log
path), **#128** (`rotate_after_days` is inert where `creationTime` falls back to `ModTime`),
**#129** (`safeWriter`'s truncated-line repair re-entering the rotation predicate) and **#130** (no
issue owns a load-time upper bound for the two rotation keys). All four are lossless, so FR-074
holds, and all four await a product decision.

## In progress
**Batch 10b — US5 post diagnostics (T066, T067, T071–T074 / #67, #68, #72–#75)** — is implemented
on `sgykfjsm/batch-10b-post-diagnostics` and **not yet reviewed, committed or pushed**. The PR body
is prepared at `.agents/batch-10b-pr-body.md`.

Three fields `contracts/log-events.md` listed as owed are delivered: `message` under FR-068's
capture rule, `stack` under FR-071, and FR-076's single warning at the GUI front door, which
`internal/gui` never produced (issue #75). T072 needed no code change — `message_len` and
`message_bytes` already carried R-010's semantics, and that was verified with three mutants rather
than by reading the code.

Validation: `make check`, native darwin/arm64 build, `internal/app` at 100.0% (matching baseline),
`internal/gui` at 73.4% against a 74.2% baseline — the whole difference being one new statement
inside the untestable `Run`. 28 mutants built, 27 killed; the survivor proved a `keepTrace` guard
unnecessary and the guard was deleted rather than given a test.

Note the branch name: **not** `sgykfjsm/batch-10b-diagnostics`. That name was already taken by the
pushed branch carrying the competing 10a reconciliation (`d057f45`), which is left untouched — the
collision is what surfaced it.

The long-standing, deliberately excluded `.specify/integrations/claude.manifest.json` edit remains
uncommitted, as it has been through every batch.

## Blockers
None.

## Next best action
**Review Batch 10b**, then publish and merge it, then close #67, #68 and #72–#75 explicitly — the
PR body carries no closing keyword, which is precisely what left #66 and #69–#71 open for two days
after #126. Two things a reviewer should go at first: DEC-G1's `claimBody`, where the body lands on
every failure record but on the terminal record only as a fallback, and DEC-G3's stack capture in
`internal/post`, which is outside T073's stated file. Then Batch 11 (US6, T075–T083) and Batch 12
(polish and gates, T084–T091).

The superseded plan for this batch, kept for the record: implement **Batch 10b** — US5 post diagnostics: T066, T067, T071 (`message_on_error_only` capture),
T072 (`message_len` as a rune count, `message_bytes` as the UTF-8 length), T073 (stack traces
subject to the `stack_trace` setting) and T074 (a single FR-076 warning through the front door in
use) — on a fresh branch from merged `main`, using `.agents/batch-10-plan.md`. Issues #67, #68,
#72–#75. Two standing notes land inside this batch: **#110** (a panic from `Sink.Name` is discarded
before T073 could record it, so closing it needs a change in `nameOf` itself) and the observation
carried to **#75** that `internal/gui` never calls `Degraded()`, so FR-076 yields zero warnings from
the GUI today — T074's remaining work, not a 10a regression. Then Batch 11 (US6, T075–T083) and
Batch 12 (polish and gates, T084–T091).

## Important decisions
Batch 10a's four decisions are recorded in full in `.agents/state.yaml` under `completed` 10a:

- **DEC-F1** — rotation is built into `logging.Open`, not supplied through `Options.Writer`, so the
  non-regular-file refusal, `O_APPEND`, FR-075's directory creation and FR-076's degradation
  reporting are discharged once for every handle. This supersedes the premise of the Batch 4
  acceptance note on #69, whose three boxes are discharged by construction; `state.yaml`'s ADV-D-002
  entry is annotated as superseded in part.
- **DEC-F2** — the rotated name is claimed with `O_CREATE|O_EXCL`, never stat-then-rename, which
  would silently overwrite a rotated log when it loses the race.
- **DEC-F3** — both rotation keys are clamped at the conversion in `internal/logging`, and this is
  the only guard there is, not an interim one: no issue owned a load-time upper bound, which is now
  #130.
- **DEC-F4** — `open` dates a file of exactly zero bytes from the process clock, so a rotation always
  clears its own trigger. Authority A-011, extended rather than narrowed. Accepted consequence: a
  pre-existing, genuinely old, *empty* log no longer rotates on its first write, which loses nothing.
  This is the batch's only production behaviour change and it came from review, not implementation.
- **DEC-ADV-006** is an **open question**, not a decision: whether to make FR-072's age trigger
  meaningful where creation time falls back to `ModTime`, or to accept "darwin with a recorded birth
  time" as the support boundary. Two shipped source files name the id, so it is defined in
  `state.yaml` under `completed` 10a and filed as #128.

## Touched files
`.agents/state.yaml`, `.agents/project-status.md`, `.agents/work-log.md` and
`.agents/archive/project-status-before-batch-10b.md` in this reconciliation. Batch 10a's own files,
and every earlier batch's, are listed under `touched_files` in `state.yaml`.

## Record-keeping note
Until 2026-09-18 this file, `work-log.md` and `state.yaml`'s `next_best_action` all still said
#126 was open at `f3ab572` with two uncommitted correction passes and a stale description. Every
part of that was true when written and none of it was true after 2026-09-16: `state.yaml`'s
`in_progress` entry had been brought up to the published head in the batch's own last two commits,
but nothing recorded the merge itself, and the two prose files were never updated past the third
correction pass. 55 issues remain open, oldest #4.
