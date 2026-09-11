## Summary
Telegram formatting rejections now get one plain-text rescue, preserving the original
message and omitting parse_mode. Other failures still make one attempt. Both requests
share the sink's deadline, and failed rescue keeps both errors for diagnostics.

## Batch
Batch 9 / US4: T059–T064, issues #60–#65. Prerequisite Batch 8 is merged (#124).
Adds correlated fallback events through the existing post/app logging boundary.
The two reserved Telegram settings remain validated and cannot alter delivery.

## Validation
Predicate matrix, local HTTP request/outcome/deadline tests, JSONL event ordering and
credential redaction tests, full race suite and native build. Results are recorded in
the batch review report; no live Telegram test is claimed.

## Scope
This document describes the Batch 9 commit. The combined publication PR also includes
the explicitly requested #122 GUI feature and documentation in separate commits.
Diagnostics message capture, broader retry behavior and Batches 10–12 remain out of scope. Issues remain open until the
reviewed implementation is accepted and merged.
