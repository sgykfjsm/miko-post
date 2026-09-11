# Project status — miko-post

_Last updated: 2026-09-11_

## Objective

Deliver the v0.1 CLI and GUI over one shared, independent dual-sink posting core.

## Current state

Batches 1–6c-2 are merged. GitHub confirms PR #121 merged at 2026-09-11T01:59:58Z;
this branch starts from its squash 928736699937396598343cf9d335c90a447d0296. The prior
snapshot is archived at `archive/project-status-before-batch-7.md`; decision history
and review records remain in state.yaml and work-log.md.

Batch 7 (US2, T041–T051, #42–#52) is implemented locally on
`sgykfjsm/batch-7-cycle` with **cycle 1 review passed**. Cycle 0 returned request-changes for missing
native focus-only evidence; that evidence is now supplied.
No commit, push, remote PR, or issue closure has occurred. A PR description is
prepared in the durable review directory under `cycle-01/input/pr-body.md`. T003 has all three dependency pins
but remains unchecked until go.sum is committed.

## Behavior and scope

The focused multiline Fyne window uses the shared core and source:gui recorder.
Send and Cmd+Enter share validation and duplicate prevention. Results show safe
per-sink reasons. Auto-close uses the configured delay and preserves exit status;
dismissing an in-flight post hides the window while the sinks and log finish.
A small macOS native counter observes input and key-window notifications for T049.
It does not consume input or expose typed content. Settings-error UI (T082), richer
failure reasons, and later diagnostics work remain deferred.

## Validation and gaps

`make check` and native arm64 build passed. Headless Fyne race tests cover editing,
submission, validation, timers, partial results and two-sink dismissal. Native checks
used only temporary notes with Telegram disabled: launch focus, newline, Cmd+Enter,
source:gui logs, success/failure auto-close with process status 0/1, background-click
and keypress retention, and subsequent successful dismissal were observed.
T049 native focus-only revalidation passed on the unchanged production binary:
standard About-panel key-window loss followed by AXRaise with the app active
produced actual loss/gain notifications with no additional capture-window input.
Success/failure results remained visible 25.647s/44.603s beyond the configured 45s
deadline, then exited 0/1 on manual dismissal. The same passive observer allowed an
idle control to exit automatically after 45.091s. The contaminated cross-app trial
was excluded. Real IME composition and Intel Mac execution remain unverified.

Raw traces, screenshots, exit receipts, verifier, diagnostic source and binary
provenance are retained in `cycle-01/input/native-validation/` of the review run.
`specs/001-dual-sink-quick-post/validation/t049-native-focus.md` records the method.
Production/test/dependency source hashes match cycle0. Only the verification note
and tasks.md evidence annotation changed for this recheck.

## Tracking and next action

No pre-implementation non-code issue was safely closable. All selected issues remain
open through review. Existing follow-ups and Batches 8–12 remain deferred. The
pre-existing Claude manifest timestamp was preserved; state bookkeeping is separate
from the implementation review target.

Cycle 0 inspected all 14 implementation files and found no confirmed code defect.
Both behavioral stages withheld approval for the native focus-only evidence gap;
CON-001 covered the T049 completion annotation. The user authorized revalidation
and rereview. Cycle 1 now includes the 15-file full diff and fresh raw evidence;
all three stages passed and CON-001 is resolved. No production source fix was needed.

Next: publish the reviewed Batch 7 change when authorized. No issues have been closed.

User-requested background-image enhancement is recorded as GitHub #122: choose one
random image from a configured directory at GUI launch and render it subtly under
the dark appearance. Implementation is deferred and excluded from Batch 7.

- Cycle summary: `batch-7-cycle.md`
- Review: `/Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T022010Z-8e1ba8b0/integrated.yaml`
- Prepared PR: the same review directory's `cycle-01/input/pr-body.md`
