# Project status — miko-post

_Last updated: 2026-09-11_

Deliver v0.1 CLI and GUI over one shared independent dual-sink posting core.
Batches 1–7 are merged. Batch 7's squash `6f40dada0f92df828251dd4ee746b34772f593f0`
is the base of `sgykfjsm/batch-8-cycle`.

Batch 8 (US3, T052–T058, #53–#59) now has **review verdict: passed** after one
explicitly authorized correction pass. Fresh contract, correctness and adversarial
reviews accepted the full 13-file diff. COR-001/ADV-001 is resolved; no review blocker
remains. Publication is authorized; T052–T058 tracking is reconciled. No issue has been closed.

The correction changes only Telegram response.go and reason_test.go beyond the prior
Batch 8 implementation. Missing/null required `ok` now produces an untrusted-envelope
error and generic failure classification. Explicit false refusals retain specific
categories; optional null description equals omission, and chat-not-found still
requires exact text. HTTP status, original diagnostic identity and credential secrecy
are tested. Independent sink timeouts and DEC-A1's 250 ms grace remain unchanged.

Validation: eight before-fix regression failures reproduced; the corrected matrix,
focused race tests, make check (format/vet/full race suite), and native arm64 build
passed. Independent reviewers reran core/app/CLI/headless-GUI and non-network Telegram
checks. Their fresh HTTP reruns were sandbox-blocked; the fixer's passing full-suite
run exercised the real local HTTP/filesystem matrix. No live Telegram, native GUI
interaction during Batch 8, or Intel Mac run is claimed.

T056 is checked complete after the passed correction review; the original cycle 0
request-changes record is retained as history. Remote issues remain open until merge.
The pre-existing Claude integration manifest edit is preserved outside this change.

Fresh review execution used isolated Codex sessions after the collaboration thread limit.
Rejected/inconclusive setup attempts are retained; the final accepted reports verify the
canonical target, contract and stage fingerprints. See `batch-8-review.md` and the
[structured report](/Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T055009Z-f2faa64e/integrated.yaml).

Next: publish Batch 8 as a draft PR. Batches 9–12 and unrelated follow-ups remain separate.
