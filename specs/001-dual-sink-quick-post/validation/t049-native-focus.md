# T049 native focus-regain verification

Verified on macOS arm64, 2026-09-11, against the unchanged Batch 7 GUI implementation
based on `928736699937396598343cf9d335c90a447d0296`. This supplies the missing FR-028 /
SC-012 native evidence identified by Batch 7's first review (CON-001).

## Method

Build the production `cmd/mp` binary unchanged. Run it in a temporary app bundle
so desktop automation can identify it, with Telegram disabled and an isolated
Obsidian note directory. Configure both close delays to 45 seconds for the check.
The failure case uses a nonexistent note directory.

Load a **passive, test-only** diagnostic library through `DYLD_INSERT_LIBRARIES`.
It observes `NSWindowDidBecomeKeyNotification`, `NSWindowDidResignKeyNotification`,
`isKeyWindow`, visibility, and input-event counts for the window titled `miko-post`.
It returns every observed event unchanged, generates no events or notifications,
and never activates or changes a window. Its input mask is a superset of the
production mask, including key and mouse releases. A one-second snapshot records
continued visibility and key-window status. No message contents are captured.

First run an idle control with the same diagnostic library: after submission,
leave the window untouched and verify automatic termination at the configured delay.
This guards against the observation machinery itself preventing application exit.

For each focus case:

1. Submit a message and wait for its result and submission-key releases to settle.
2. Open macOS's standard About panel from the application menu. Observe the capture
   window's actual key-status loss while the application remains active.
3. Close the About panel, then perform the capture window's accessibility `Raise`
   action. Observe actual key-status gain. Merely ordering a background application's
   window forward was insufficient in the earlier attempt; this case establishes
   the native transition through notification and `isKeyWindow` evidence.
4. Verify that the capture window receives **no additional input events** throughout
   that interval or the following wait. All clicks operate the menu/About panel.
5. Wait beyond the original deadline and verify the result is still visible.
6. Dismiss with Esc and record the actual process exit code.

This exercises **window** focus regain via a second native window in the same
application. It does not claim a separately isolated cross-application-switch test.
A cross-application automation trial was discarded because the tool introduced
mouse input into the capture window when reactivating it.

## Results

| Case | Native loss → regain | Additional capture-window input | Result | Process exit |
| --- | --- | --- | --- | --- |
| Idle control | None before deadline | 0 after submission settled | Automatically exited after 45.091 s | 0 |
| Successful post | `key=false` → `key=true`; focus count 0 → 1 | 0; input count remained 112 | Visible 25.647 s beyond deadline, then manually dismissed | 0 |
| Failed post | `key=false` → `key=true`; focus count 0 → 1 | 0; input count remained 124 | Visible 44.603 s beyond deadline, then manually dismissed | 1 |

The success path emitted `request_completed`; the failure path emitted
`request_completed_with_error`. Both used the shared service with `source: gui`.
The trace verifier asserts real loss/gain ordering before the deadline, stable
input count through a visible/key-window snapshot at least five seconds beyond the
deadline, and the expected process exit. All three cases passed.

Raw JSONL traces, process receipts, screenshots, observer source, verifier, binary
SHA-256 and build/source provenance are retained in the Batch 7 review packet's
`native-validation/` directory. That evidence also preserves the discarded trial;
it is not counted as a passing focus-only test.

Fresh automated verification: `go test -race -count=1 ./internal/gui ./cmd/mp` passed.
The native build passed with the existing duplicate `-lobjc` linker warning.
The earlier full `make check` result remains applicable because production code is
unchanged. This verification resolves T049's native focus gap; it does not claim
Intel Mac execution, real Japanese IME composition, or live Telegram verification.
