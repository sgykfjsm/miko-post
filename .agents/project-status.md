# Project status — miko-post

Updated 2026-09-29.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
**Closing.** All 91 tasks are `[x]` and every batch is merged. `main` is at
`56e02128e705a731604f6e9306f74c5e4f902212` (PR #140, Batch 13). Feature 001 is closed pending the
closeout PR from `sgykfjsm/close-feature-001`, which carries the spec-reconciler pass. It changes
records and docs only, no `.go` file.

## Completed
Batches 1–13 are merged. Batch 13 (hardening) merged as `56e0212`; its head `fe506f8` shares tree
`945c514b`. #96, #102, #103, #115 (partial), #118, #136 and #137 were closed on 2026-09-29, each
with a comment naming the merge. The FR-043/SC-006 hand-off was commented on #105
(issuecomment-5885237791). Detail is in `.agents/state.yaml` under `completed` 13.

## In progress
The closeout pass (`feature_closeout` in `state.yaml`), uncommitted on `sgykfjsm/close-feature-001`:

- `data-model.md`: `SinkResult`'s five render guards, the named-field rule and the `encoding/xml`
  boundary; `AllSucceeded`'s empty-slice fail-closed rule; the five-method, pointer-held `Secret`
  and its shape accessors.
- `contracts/`: `thread_id` must be positive when set; the `"unknown"` sentinel for `source` and
  `message_id`; the FR-030 native check recorded as run.
- `spec.md`: notes under FR-043 and SC-006 (DEC-J9), FR-066 and A-008 (DEC-I1, DEC-J2), FR-030
  (no window without an absolute home) and A-007 (the module path).
- `docs/design.md` and `design.ja.md` §11.2, `docs/USER_GUIDE.md` and `docs/DEVELOPER_GUIDE.md`.
- Batch working files moved to `.agents/archive/batch-artifacts/`, with an index README.

## Blockers
None.

## Next best action
Merge the closeout PR, checking that `closingIssuesReferences` reads back as intended. Then close
#105 with a comment naming what was reconciled. #101 (deferred, DEC-J3) and #138 (stamped release
binaries, DEC-I1's relaxation, future) stay open. Nothing else remains for feature 001.

## Important decisions
DEC-I1 and DEC-I2 (Batch 12), and DEC-J1 to DEC-J9 (Batch 13), are in `.agents/state.yaml`.
DEC-J3 defers #101 beyond v0.1.

## Touched files
`git status` on the branch is the authoritative list. The outgoing copy of this page is archived
as `.agents/archive/project-status-before-closeout.md`.
