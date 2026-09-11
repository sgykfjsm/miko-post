# Batch 7 cycle — US2 capture window

## Triage

Selected **Batch 7**, T041–T051 (#42–#52), with T003/#4 dependency-pin support.
PR #121 is merged as 928736699937396598343cf9d335c90a447d0296, present at this
branch's base. The shared core, sinks, settings and logger therefore satisfy the
prerequisites. One GUI front door is the coherent change boundary; no second
batch was implemented.

| Issue | Task | Classification | Disposition |
| --- | --- | --- | --- |
| #4 | T003 dependency pins | foundation, partly complete before this run | Fyne pin now present; keep open until go.sum committed |
| #42 | T041 focused keyboard tests | verification | batch-now |
| #43 | T042 shared submission tests | verification | batch-now |
| #44 | T043 extended Entry | feature-slice | batch-now |
| #45 | T044 focused window | feature-slice | batch-now |
| #46 | T045 shared submit | feature-slice | batch-now |
| #47 | T046 disabled Send | feature-slice | batch-now |
| #48 | T047 safe results | feature-slice | batch-now |
| #49 | T048 timers | feature-slice | batch-now |
| #50 | T049 interaction cancellation | feature-slice | verified in cycle 1; keep open pending publication |
| #51 | T050 quit/cancel | feature-slice | batch-now |
| #52 | T051 process status | feature-slice | batch-now |

All selected GUI issues required new implementation before this run; none was
safely closable as an administrative issue. The existing order remains Batch 7,
then sink-independence work (8), formatting rescue (9), diagnostics (10), settings
resolution (11), and final polish/gates (12). Existing follow-ups remain deferred.

The native event observer is required supporting code for T049: observing the
Entry alone misses clicks on blank space and internal child widgets. It counts
native input and key-window notifications, does not consume events, and retains
no message text. Startup-error UI remains explicitly owned by T082. No spec/plan
reconciliation is claimed while review is unresolved.

## Implementation and validation

Implementation is in internal/gui, main dispatch and tests, go.mod/go.sum, and
selected task annotations. It preserves the shared posting service and recorder.
The result timer uses the latest submission's outcome; no-post cancel exits 1.
Busy dismissal hides the window while all sinks and terminal logging finish.

- `make check` passed (format, vet, complete race suite).
- Uncached `go test -race -count=1 ./internal/gui ./cmd/mp` passed in review.
- Native arm64 binary build passed; duplicate `-lobjc` linker warning is present.
- Headless coverage includes focused keyboard/editing, validation, duplicate
  prevention, partial output/secret exclusion, default/zero/large/stale timers,
  and two independent sinks completing after dismissal.
- Native isolated-vault checks covered initial focus, Enter, Cmd+Enter, append,
  source:gui diagnostics, success/failure auto-close with process exits 0/1,
  background-click and keypress retention, and subsequent successful dismissal.
- Native focus-only regain is unverified. Accessibility Raise did not establish
  a real key-window transition; Dock inspection timed out. Real IME composition,
  Intel Mac execution, live Telegram, and native both-success delivery were not run.

## PR and worktree

A local PR description is prepared. No commit, push, remote PR or issue closure
has occurred. The unrelated pre-existing Claude manifest timestamp is preserved.
Project-state documentation is separate from the 14-file implementation review.
T003 remains unchecked pending its explicit committed-go.sum condition.

Durable packet and reports:
`/Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T022010Z-8e1ba8b0/`.

## Initial review (cycle 0, historical)

Mode: review-only; cycle 0; no fix loop.
Contract: valid, with CON-001 requiring correction of T049's checked state or
completion of its missing native focus check.
Correctness: inconclusive solely for the native focus acceptance gap; all 14
changed files/hunks inspected, no confirmed behavioral defect.
Adversarial: inconclusive for the same required native focus-only acceptance gap.
Native resources, shutdown, malformed input, concurrency, cancellation, disclosure,
compatibility and durability lanes inspected; no concrete defect established.

**Terminal verdict: request-changes.** Required finding CON-001 concerns T049's
completion claim; both behavioral stages withhold approval until focus-only
acceptance is established. No correction loop or review fix occurred. The original
14-file target remains unchanged.

## Initial non-code disposition (historical)

No issue was closed before implementation. No post-review closure is justified
while required validation and review remain unresolved. In particular #50 retains
its native focus acceptance, and #4 retains the committed-go.sum condition.

Next action: establish actual native key-window loss/regain during a pending
result timer without input to the target window, verify retention beyond the
deadline and the eventual exit status, reconcile T049, then rerun staged review.
If a native integration harness or human macOS check is needed, keep #50 open
until that evidence exists. A timer-counter unit test or accessibility Raise
without a proven focus transition is not sufficient.


## Revalidation and final review (cycle 1)

The user requested T049 native revalidation and staged rereview. The unchanged
production binary passed idle-control and actual native focus-only success/failure
checks. The About panel causes key loss; AXRaise restores key status while the app
is active. Raw notifications and snapshots prove the transition, with zero added
capture-window input. Results remain visible 25.647s/44.603s beyond the 45s deadline,
then manual dismissal exits 0/1. Idle control auto-exits after 45.091s.
The contaminated cross-app trial is excluded. See the T049 validation note and
cycle-01/input/native-validation in the durable review run for all raw evidence.

Fresh independent stages: contract valid, correctness pass, adversarial pass.
All 15 files and 17 hunks were inspected; fresh race tests passed. CON-001 is
resolved, with no remaining required findings or material validation gaps.
**Final verdict: passed.** No production correction was necessary. Intel Mac,
real IME, live Telegram/native both-success and isolated cross-app activation
remain disclosed limits. Cycle 0 reports remain preserved.

The local work remains uncommitted, with no push, remote PR or issue closure.
T003 still requires committed go.sum. Next: publish the reviewed Batch 7 change
when authorized, using cycle-01/input/pr-body.md. Full-feature reconciliation waits
for the remaining implementation batches.

Separately, user-requested future background images are recorded in #122. No
implementation or Batch 7 scope expansion was made for that request.
