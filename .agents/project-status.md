# Project status — miko-post

Updated 2026-09-16. Batches 1–9 and explicitly requested GUI background #122 are
merged. PR #125 merged as `17073829398aa540b6a8ea56a0155b0e30880dfc`;
its tree matches reviewed `7bb43d1` exactly. Both scopes passed contract,
correctness and adversarial review after fixes, combined make check and native
arm64 build. GitHub reported no CI checks; no new test run was needed for the
identical merge tree. Prior native/live-service validation limits remain in the reports.

Batch 10a — lossless log rotation (T065, T068–T070 / #66, #69–#71) — is implemented on
`sgykfjsm/batch-10-diagnostics-2` from merged main and opened as PR #126. Its review
verdict is not final, and three review correction passes are applied on top of the opened
head (the third is the last the review loop permits). Both are uncommitted: #126 still shows f3ab572 only, and its description is stale.
See [batch-10-plan.md](batch-10-plan.md). 10b (T066–T067, T071–T074: message capture,
traces and verification of existing counts/degradation behavior) is not started and stays
out of #126. Rotation race and partial-failure handling was the identified design risk and
is what the review has been examining. Batches 11–12 remain later work.

An earlier version of this file said Batch 10 was prepared on `sgykfjsm/batch-10-diagnostics`
and that no next-batch code was implemented. Both were true when written; neither is true
of the current tree, and the branch of that name was never created.

Next: finish the staged review of PR #126 and merge it, then implement Batch 10b.

Batch 10a's rotation code and tests are the new changes on top of the merge and planning
records. The unrelated pre-existing Claude manifest edit remains excluded. Issues #60–#65
and #122 remain open pending separate issue cleanup; the PR merge did not auto-close them.
#66 and #69–#71 stay open until #126 merges, although their tasks are marked complete in
`tasks.md`. Feature closeout is premature.
