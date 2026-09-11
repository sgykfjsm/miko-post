# Batch 8 — US3 sink independence

Current status: **passed after authorized cycle 1 rereview**; publication authorized.
T052–T058 tracking is reconciled. See `project-status.md` for publication status and
`batch-8-review.md` for the review snapshot.

## Original cycle 0 record

Selected explicitly by the user. Prerequisites are merged: posting core and recorder,
both real sinks, CLI, and GUI (PR #123, squash 6f40dada). No competing open PR exists.
Seven selected tasks/issues were analyzed; remaining execution stays in Batches 9–12.

| Issue/task | Role | Pre-implementation disposition | Evidence / scope |
|---|---|---|---|
| #53 / T052 | verification | keep-open-noncode pending verification | Existing blocked-sink test; real-sink timeout matrix added |
| #54 / T053 | verification | keep-open-noncode pending verification | Existing service both-fail and Batch 6c-2 disk-log test; classification assertions strengthened |
| #55 / T054 | verification | keep-open-noncode pending race run | Existing sibling-context and independent-deadline tests |
| #56 / T055 | admin/verification | keep-open-noncode pending verification | Per-sink independent contexts already implemented |
| #57 / T056 | feature slice | needs-code, batch-now | Fixed-vocabulary classification and Telegram identity mapping |
| #58 / T057 | verification | keep-open-noncode pending verification | Resolved log path already rendered by both interfaces |
| #59 / T058 | verification | keep-open-noncode pending verification | Partial outcomes already rendered; both directions tested |

Classification confidence is high. No human-check candidate or hidden prerequisite was
identified. No issues were closed before implementation. Earlier batches' implementation
is credited explicitly rather than copied or falsely attributed to this batch.

The review boundary includes the classifier, Telegram error matching, tests, task evidence
and the one log-contract classification row. There is no production CLI/GUI edit.
Unknown errors retain generic `delivery failed`; errors.Is selects constants and never
renders diagnostics. Telegram matching requires an error with no decode/contradiction
cause and matching HTTP/body status. Chat-not-found description matching is exact.

The classifier introduces no wrapper. Pointer-backed sentinels and wrapped diagnostics
are covered by the %p leak assertion. Service success has nil Err; a caller-created
Success:true result is still a success even when Err is present. Attempt-level diagnostics
remain the recorder's responsibility for future formatting rescue.

Validation: make check (format, vet, full race suite) and native arm64 build passed.
Real local HTTP/filesystem tests exercise both successes, both partial directions, both
failures and a hanging request. They assert actual note content, one HTTP attempt, safe CLI
output, log path, both JSONL terminal events and common correlation ID. Existing and new
headless GUI tests verify safe per-sink output and exit status on close. The disk logger
both-fail test checks each detailed error and both classifications.

DEC-A1's 250 ms timeout grace is accepted prior behavior. No policy change is made.
No live Telegram, native GUI interaction or Intel execution was performed in this batch.
Batches 9 (format rescue), 10 (diagnostics), 11 (settings) and 12 (polish/gates) remain
separate; follow-up issues including #119 and #122 are not claimed closed.

PR title: `feat: classify sink failures and verify independent outcomes`
PR body and raw validation: `/tmp/miko-post-batch8-evidence/`.
Review packet: `/tmp/miko-post-batch8-packet/`.
The staged review returned **request-changes**: contract valid; both behavioral
reviewers found the same should-fix defect (COR-001 / ADV-001). All changes remain
uncommitted; no remote mutation.
Pre-existing `.specify/integrations/claude.manifest.json` is excluded and preserved.

Post-review cleanup: #53/#54/#55/#56/#58/#59 are verified completion candidates;
#57 remains open and needs code. No issues were closed or newly filed. T056 is unchecked
with a review annotation; all other selected tasks retain evidence-backed checks.

No fix-and-rereview cycle ran: review-only mode, zero fixes, one completed review cycle.
Durable log-contract vocabulary and task evidence are updated; guides and incomplete
feature task artifacts are intentionally unchanged. Only the previous project-status
snapshot was archived; useful implementation/review history is retained.

Next: fix COR-001 (also ADV-001) and rerun the full three-stage Batch 8 review.

## Authorized correction and cycle 1 rereview

The user authorized COR-001/ADV-001 fixes and all-three-stage rereview. One correction
changed Telegram response.go and reason_test.go. Eight before-fix failures reproduced;
classification regressions, make check and arm64 build passed afterward.

Contract, correctness and adversarial stages all passed on the full 13-file diff;
no findings remain. All seven issue requirements are review-approved. No task checkbox
or remote issue was changed. No staging, commit, push or PR publication occurred.

Fresh reviewers used isolated CLI sessions after the agent-thread limit. Rejected
metadata/setup attempts and sandbox-limited HTTP reruns are documented in the report;
the actual real-HTTP matrix passed in the fixer's full-suite run.

Next: reconcile T056 and prepared PR tracking with the passed review before publication.
