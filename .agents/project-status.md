# Project status — miko-post

Updated 2026-09-11. Batches 1–8 are merged; Batch 8 merged as `48fdd6a` (#124).
Batch 9 / T059–T064 / #60–#65 and the separately requested GUI feature #122 are
implemented, reviewed, committed and pushed on `sgykfjsm/batch-9-review-fixes`
in draft PR [#125](https://github.com/sgykfjsm/miko-post/pull/125).

Both scopes passed fresh contract, correctness and adversarial reviews after one
correction pass each. Batch 9 fixed two validation gaps (COR-001/COR-002): original
request-deadline equality, rejected reset-deadline mutation, second-request timeout
and real HTTP→Service→JSONL integration. Issue #122 fixed its COR-001 with decodable
boundary fixtures; four removed/tightened guard mutations fail. No required finding
or decision remains. Complete reports: `batch-9-review.md` and `issue-122-review.md`.

Batch 9 supplies one trusted formatting-only rescue, original text/destination,
omitted parse_mode, shared deadline and correlated fallback events. Failed rescue
retains both diagnostics and classifies by the final attempt. T059–T064 are checked.
Issue #122 adds optional `gui.background_image_dir`, top-level random PNG/JPEG,
12% opacity over black, aspect-preserving placement and graceful bounded fallback.

Validation: combined make check and native arm64 build passed; uncached core/app/
Telegram race tests passed. Native GUI checks verified multilingual editor content,
success/failure output and black fallback with temporary config/state/vault. No live
Telegram, real IME-composition or Intel Mac validation is claimed.

Durable spec/contracts, English/Japanese design notes and user/developer guides are
reconciled. Prior status is archived; tasks remain because Batches 10–12 are unfinished.
Review targets and publication patches are preserved under the external review runs.
See `change-groups.json` for the separate commit boundaries.

Feature commits: `1cac8b2` (Batch 9) and `5fd39bd` (#122). Documentation follows in
a separate commit. Publication combines both explicitly requested scopes on the existing
branch, following commit-and-pr branch policy. No history was rewritten or issue closed.
The pre-existing Claude manifest edit is preserved and excluded from every group.

Next: obtain acceptance and merge draft PR #125. Batch 10 follows acceptance/merge.
The feature is not ready for closeout.
