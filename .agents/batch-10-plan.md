# Batch 10 preparation — 2026-09-16

## Summary
Analyzed T065–T074 / issues #66–#75 against current issues, tasks, spec, plan,
constitution and existing logging/recorder code. Batch 9 and #122 merged in PR #125
as 17073829398aa540b6a8ea56a0155b0e30880dfc; its tree equals reviewed 7bb43d1.
Two coherent review scopes remain: filesystem rotation and post-level diagnostics.
No implementation began. Start 10a; preserve current event/redaction/degradation contracts.

## Classification
| Task / issue | Purpose | Category | Code needed | Blocks | Disposition |
|---|---|---|---|---|---|
| T065 / #66 | Rotation tests | verification | tests | 10a acceptance | batch-now |
| T066 / #67 | Message capture tests | verification | tests | 10b acceptance | batch-later |
| T067 / #68 | Degraded logging | verification | audit existing tests, fill gaps | 10b acceptance | batch-later |
| T068 / #69 | Rotating writer | feature-slice | yes | 10a | batch-now |
| T069 / #70 | Creation time | foundation | yes | age rotation | batch-now |
| T070 / #71 | Collision preservation | feature-slice | yes | safe rotation | batch-now |
| T071 / #72 | Failure message capture | feature-slice | yes | reconstruction | batch-later |
| T072 / #73 | Rune and byte counts | verification | existing recorder; verify coverage | 10b acceptance | batch-later |
| T073 / #74 | Diagnostic traces | feature-slice | yes | 10b acceptance | batch-later |
| T074 / #75 | Single warning | verification | existing front doors; verify gaps | 10b acceptance | batch-later |

## Proposed batches and execution order
1. **10a — Lossless log rotation**: T065, T068–T070 / #66, #69–#71.
   One PR focused on filesystem preservation and logger integration. Prerequisites:
   merged logger foundation, settings and event wiring (available). Implement both
   size and creation-age checks before every write; local timestamp suffix and
   collision numbering; native creation time with portable fallback; never delete,
   compress or overwrite archives. Exit: deterministic boundary/collision tests,
   real-file integration, failure injection proving posting remains unaffected,
   race checks, native build and three-stage review. Examine concurrent logger
   instances and rename/reopen partial failures before selecting the algorithm.
2. **10b — Reconstruct failed posts**: T066–T067, T071–T074 / #67–#68, #72–#75.
   One PR focused on correlated message capture, useful traces and degradation.
   Follow 10a integration so tests cover the final writer. Audit pre-satisfied counts
   and warnings; only mark complete with evidence from both front doors. Exit:
   success omits body under error-only mode, any failure captures recoverable text
   subject to secret redaction, expected errors have no fabricated trace, settings
   are respected, and failures cannot change outcomes/exit status.
3. Batch 11 settings resolution, then Batch 12 polish and gates remain later work.

## Immediate next action
Run implement-next-batch (or run-batch-cycle) for **Batch 10a** on
`sgykfjsm/batch-10-diagnostics`, based on merged main 1707382. Keep 10b and
unrelated backlog fixes out of its PR.

## Non-PR closure candidates
T072 and T074 have substantial prior implementation, but require acceptance
verification before closure. T067 has prior CLI evidence; verify all required
outcomes and GUI behavior. No issue is closed by this preparation.
Merged Batch 9 issues #60–#65 and #122 remain open on GitHub; closure was not part
of this merge action. Other old open issues are outside this focused triage.

## Ambiguities and risks
Task filenames predate the app recorder boundary: post-level capture belongs at
that boundary, not necessarily in the generic logger. Preserve package separation.
Rotation must handle timestamp collisions without a check-then-overwrite race;
partial rename/open failures must preserve archives and degrade safely.
Before 10b implementation, define useful unexpected-error trace provenance and
confirm secret-redaction precedence when the original body itself contains a token.
The pre-existing Claude manifest edit is unrelated and must remain excluded.
