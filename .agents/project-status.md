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

## In progress
Nothing for feature 001.

## Blockers
None.

## Next best action
None for feature 001. Open work outside it: #101 (deferred, DEC-J3) and #138 (stamped release
binaries, DEC-I1's relaxation). Two stale code comments wait for the next code change: `Reveal`'s
"exactly one place" and `result.go`'s "four-method". Tagging `v0.1.0` is the maintainer's call.
Known limitation, unfiled: the IME candidate window opens at the top-left of the window instead of
under the cursor. GLFW's `firstRectForCharacterRange` returns the view origin. Upstream is open
(fyne-io/fyne#618, glfw/glfw#2130). The fix is a patched GLFW through a `replace` directive,
wired to `Entry`; the maintainer chose to live with it for now.

## Important decisions
DEC-I1 and DEC-I2 (Batch 12), and DEC-J1 to DEC-J9 (Batch 13), are in `.agents/state.yaml`.
DEC-J3 defers #101 beyond v0.1.

## Touched files
`git status` on the branch is the authoritative list. The outgoing copy of this page is archived
as `.agents/archive/project-status-before-closeout.md`.
