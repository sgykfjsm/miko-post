# Project status — miko-post

Updated 2026-10-01.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
**Closed (2026-09-29).** All 91 tasks are `[x]`, Batches 1–13 are merged, and the spec-reconciler
closeout merged as PR #141 (`3fb79dd687e1019d633a7bd6a43142ac66caa026`). #105, its checklist, is
closed with a comment naming each reconciled item.

## Completed
Batches 1–13 are merged. Batch 13 (hardening) merged as `56e0212`; its head `fe506f8` shares tree
`945c514b`. #96, #102, #103, #115 (partial), #118, #136 and #137 were closed on 2026-09-29, each
with a comment naming the merge. The FR-043/SC-006 hand-off was commented on #105
(issuecomment-5885237791). Detail is in `.agents/state.yaml` under `completed` 13.

## Post-closure change
PR #143 (`3c6e827`, 2026-10-01) fixed a defect found by running the GUI on a Mac with the Japanese
IME: the Return that confirms a conversion reached the entry as a plain key press and inserted a
line break before the committed text. A plain `Enter` is now ignored in the message entry and
`Shift+Enter` inserts the line break; FR-022, the design docs and the README follow. The cause is
upstream (GLFW's Cocoa `keyDown` forwards keys before `interpretKeyEvents`; Fyne cannot see the
composition state). Confirmed by hand: confirming with Return adds no line break. Not confirmed by
hand: `Shift+Enter` inserting a line break and `Cmd+Enter` submitting.

PR #145 (`a5976d3`, 2026-10-01) added `[gui].background_opacity` (0 to 1, default 0.12, the old fixed
value), because the background image was hard to see. Out-of-range values, `nan` and `inf` are
refused at load. Confirmed by hand on a Mac: raising it works and looks fine.

PR #147 (`f8f6f4b`, 2026-10-01) fixed Japanese input on macOS beyond the line break. GLFW reports the
view origin as the caret and keeps the text being composed to itself, so the candidate window
opened at the window's corner and the conversion was invisible until confirmed. The GUI now
replaces four `GLFWContentView` text-input methods at run time (`internal/gui/ime_darwin.m`): the
candidate window follows the entry's caret, the composing text is drawn as an underlined overlay
with its own caret (`internal/gui/preedit.go`) and never enters the message, and the entry ignores
Backspace, the arrows, Return and Esc while a conversion is open. Spec FR-022a records it. Confirmed
by hand on a Mac. Cause of one unexplained reappearance of the top-left candidate window: unknown;
it did not reproduce and the logged rectangle was valid, so another build of the binary is the
likely reason.

PR #148 (`cd99768`, 2026-10-01) stopped the window posting the same message repeatedly. After every
enabled destination succeeds, Send and Cancel give way to a single Quit button and `Cmd+Enter` is
refused; a failed post keeps Send for a retry (a partial failure too, and a retry then posts to
every enabled destination again). FR-020, FR-024, scenarios 5 and 6 and the docs follow. Confirmed
by hand on a Mac.

## In progress
Nothing for feature 001.

## Blockers
None.

## Next best action
None for feature 001. Open work outside it: #101 (deferred, DEC-J3) and #138 (stamped release
binaries, DEC-I1's relaxation). Two stale code comments wait for the next code change: `Reveal`'s
"exactly one place" and `result.go`'s "four-method". Tagging `v0.1.0` is the maintainer's call.
Known limitations of the input-method hook (FR-022a), unfiled: the composing text is drawn over any
text to the right of the caret; it depends on the class name `GLFWContentView` and fails safe
(stderr note, old behaviour) if that changes; and it can go once the driver supports it
(fyne-io/fyne#618, glfw/glfw#2130, both open). Since the window now knows when a conversion is open,
a plain `Enter` could insert a line break again outside one instead of being ignored; that would
change FR-022 and is the maintainer's call.

## Important decisions
DEC-I1 and DEC-I2 (Batch 12), and DEC-J1 to DEC-J9 (Batch 13), are in `.agents/state.yaml`.
DEC-J3 defers #101 beyond v0.1.

## Touched files
`git status` on the branch is the authoritative list. The outgoing copy of this page is archived
as `.agents/archive/project-status-before-closeout.md`.
