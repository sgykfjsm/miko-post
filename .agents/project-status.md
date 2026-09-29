# Project status — miko-post

Updated 2026-09-28.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
In execution, on the last batch. `main` is at `e0ef849024a97db44584ae808c0fecc89c36c71a`
(PR #135, Batch 11). On the Batch 12 branch, **90 of 91 tasks are `[x]`**: the 84 from before plus
T084–T087, T089 and T090. **T091 stays open**: its native-window scenarios and the `thread_id`
case need the maintainer.

## Completed
Batches 1–11, the GUI background (#122, via PR #125) and the records PR #134 are merged.

**Batch 11 record, written late.** PR #135 was squash-merged on 2026-09-24 as `e0ef849`. Head
`5005ea8` and the squash share tree `776bbbb5`. #76–#84, #119 and #130 were closed between
06:47:36 and 06:47:57 UTC, about a minute after the merge, each with a comment. Both follow-ups were published: #136 (Fyne writes into the cwd
when HOME is unset but XDG is set) and the #126 correction comment. None of that reached the
records until Batch 12, and this page still said "not pushed, no PR". Detail is in
`.agents/state.yaml` under `completed` 11.

## In progress
**Batch 12 — polish and gates.** It covers T084–T087 and T089–T091 (#85–#88, #90–#92), plus #95,
and #94 closes with it under DEC-I1. It is implemented on `sgykfjsm/batch-12-cycle` and uncommitted.
Review cycles 0–2 and fix passes 1–3 are done; review cycle 3, a full re-review, is next. Cycle 2
found the code converged and asked only for records and docs, which fix pass 3 (the last the loop
allows) applied. The history is in `.agents/state.yaml` under the Batch 12 entry.

- **T084**: `TestSecretLeakGate`. It drives the real wiring with a sentinel token, behind an
  in-process transport that never dials. Every redaction layer was mutated with its result
  predicted first, and the overlapping sink-side layers are documented as masking one another.
  A token with trailing whitespace (#137) is the case where only `withoutRequestURL` holds.
- **T085/T086**: `specs/001-dual-sink-quick-post/acceptance-matrix.md`.
- **T087**: `README.md`, plus `testdata/config/example.toml`, which two tests pin to the real
  defaults and the full schema.
- **T089**: gofmt, vet, build and `-race` are clean. The latest run was on the fix-pass-2 bytes
  (`e95d86d2…`); the coordinator re-runs `make check` on the final bytes at commit.
- **T090**: `go install …@latest` records the pseudo-version and a recovered commit. `make install`
  stamps both, and records `v0.1.0` under a local-only tag. #94 closes under DEC-I1: the
  documented stamped build (`make install`) records a real commit, verified by T090, and
  `go install` users of a tag get `unknown` by accepted decision.
- **T091, partial**: `validation/t091-quickstart.md`. The local part passed on 2026-09-25, and
  the live command-line part on 2026-09-28 against the maintainer's test bot. #92's rider got
  HTTP 400 for `text=a%FFb`, so DEC-D4's premise holds and #104 stays closed.

## Blockers
None for the PR. **T091 / #92 stays open for the maintainer.** The live command-line checks were
run on 2026-09-28 with the test bot the maintainer provided, and passed. Still owed: Scenario 1
with `thread_id` set, if the chat is a forum; Scenario 7 in full on the native window; and
Scenario 8's window halves, with each of the four startup-error-window dismissal routes recorded,
including the native `Esc` owed since T082.

## Next best action
The review converged: cycle 3 ended `passed-with-notes`, and four post-review wording and record
edits followed (see `state.yaml` `post_review_edits`). Re-run `make check` on the final bytes,
commit, and with the maintainer's go-ahead push and open the PR. The PR has no closing keywords, so
`closingIssuesReferences` must read back empty. After merge, close #85–#88, #90, #91 and #95
explicitly, naming the merge commit; #92 stays open. Close #94 the same way. GitHub #94 records
only the 2026-09-24 option (b), so its comment must also say four things: DEC-I1 (2026-09-28)
narrowed option (b); `make install`, the documented stamped build, records a real commit, as
verified by T090 (`v0.1.0` / `e0ef849` under a local-only tag); a tagged `go install` records
`git_commit = unknown`, which is accepted and documented in design §14 and README; and FR-066 and
A-008 are not yet annotated, which is handed to spec-reconciler (#105). That last precondition is
already met: the item was posted on #105 on 2026-09-28. Also comment on #92 with the 2026-09-28 live
results, the DEC-D4 outcome (#104 stays closed), and what is still owed. Then implement DEC-I2 (#137) and decide
the open non-task issues before `close-feature`: #96, #101, #102, #103, #105 (spec-reconciler),
#115, #118, #136, #137, #138. #138 is future work, to be considered later.

## Pending maintainer go-aheads
#122 was closed on 2026-09-28 with a comment naming PR #125, and #137 (whitespace around
`bot_token`) was filed. Both were done on the maintainer's go-ahead. Also on 2026-09-28: the
FR-066/A-008 item was posted on #105, the ADV-007 evidence and the DEC-I2 decision were posted on
#137, and #138 (a release workflow publishing stamped macOS binaries) was filed.

- **#96**: pick one `git_commit` shape. T090 observed three: 7 characters from `make`,
  12 from a pseudo-version, and 40 from a local build.

## Important decisions
Batch 11's DEC-H1 and DEC-H2, and Batch 10b's DEC-G series, are in `.agents/state.yaml`. Batch 12
coined DEC-I1 (2026-09-28): `go install …@latest` stays the primary install path, and a tagged
build's `git_commit = unknown` is an accepted, documented v0.1 limitation. It is recorded under
the Batch 12 entry's `review_cycle_0.decisions`, with DEC-I2 (2026-09-28, #137): `bot_token` and
`MIKO_POST_TELEGRAM_BOT_TOKEN` are trimmed at load, and a value with whitespace left inside is
rejected by `config.Validate`. DEC-I2 is scheduled after Batch 12 merges, not implemented in it.
DEC-I1's relaxation, published stamped binaries, is tracked as #138.

## Touched files
`git diff --stat e0ef849` on the branch is the authoritative list. The outgoing copy of this page
is archived as `.agents/archive/project-status-before-batch-12.md`.
