## Summary

Batch 8 (US3, T052–T058) adds fixed, safe failure reasons and matching log classifications while preserving original diagnostic errors and independent sink outcomes. Telegram classification requires a valid explicit `ok:false` refusal; missing/null or malformed fields remain generic.

## Changes

- Add core error identities and Telegram matching for timeout, permission, chat-not-found, authentication and rate-limit failures.
- Verify existing independent deadlines, both-failure logging, CLI/GUI partial outcomes and diagnostic log paths. Earlier batches supplied those behaviors; this batch completes their acceptance coverage.
- Fix COR-001/ADV-001 with regression tests for missing/null `ok`, invalid types, response consistency, diagnostic identity and credential redaction.
- Reconcile all seven task checkboxes and preserve the original review history.

Closes #53
Closes #54
Closes #55
Closes #56
Closes #57
Closes #58
Closes #59

## Validation

- `make check` passed (formatting, vet, full race suite); focused race tests and native darwin/arm64 production build passed.
- Eight regressions failed before the fix and passed afterward.
- Real local HTTP/filesystem tests verify both successes, either failure, both failures and HTTP timeout, including persisted notes, JSONL events and CLI output. Headless GUI tests cover outcome rendering.
- Fresh contract, correctness and adversarial reviews passed after one fix pass, with no remaining findings. Reviewers' HTTP reruns were sandbox-blocked; the fixer's full suite exercised the local HTTP matrix.

## Notes

The existing DEC-A1 250 ms timeout grace is unchanged. Live Telegram, native GUI interaction and Intel Mac execution were not exercised in this batch. Batches 9–12 remain separate.
