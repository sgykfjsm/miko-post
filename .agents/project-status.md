# Project status — miko-post

Updated 2026-09-29.

## Objective
Deliver v0.1 of `miko-post` (feature `001-dual-sink-quick-post`): one Go binary that posts a short
message to Telegram and today's Obsidian daily note concurrently and independently, from a CLI and
a GUI front door, through one shared posting core.

## Status
In execution, on a hardening batch after the last task-backed one. All 91 tasks are `[x]`. `main`
is at `aa202f68da6049bd2a991e426f3510bf743ee1d9` (PR #139, Batch 12).

## Completed
Batches 1–12 are merged. Batch 12 (polish and gates) merged as `aa202f6`, and its head `14468d9`
shares tree `e216bdc2`. #85–#88, #90–#92, #94 and #95 were closed on 2026-09-29, each with a
comment naming the merge. #92's comment carries the live and window results. #94's carries
DEC-I1's four points. Detail is in `.agents/state.yaml` under `completed` 12.

## In progress
**Batch 13 — hardening**, selected by the maintainer on 2026-09-29. It is implemented on
`sgykfjsm/batch-13-hardening`, uncommitted. The staged review converged: cycle 2 ended
`passed-with-notes` after fix pass 1 and fix pass 2 (DEC-J9), and the post-review wording edits
are listed in the PR body:

- **#137 / DEC-I2, DEC-J6 and DEC-J9**: the bot token is trimmed at load, from both sources, and
  after that a token with any byte outside printable ASCII without space (`0x21`–`0x7E`) is
  refused, whether or not the sink is enabled. Whitespace, invisible and non-ASCII corruptions can
  no longer load. Mistakes inside printable ASCII (pasted straight quotes, a `bot` or `TOKEN=`
  prefix) still load; such a token fails at Telegram, and while it is configured a bare token in a
  failed post's captured body is not redacted. A placeholder token is now refused even with the
  sink disabled.
- **#136 / DEC-J1 and DEC-J7**: with no home directory, or one that is not an absolute path,
  neither window opens.
- **#96 / DEC-J2**: `git_commit` is 12 characters on every build path.
- **#102**: the layout argument is restated as a property.
- **#103**: the XML boundary is named, and the enumeration test fails instead of logging.
- **#115 / DEC-J4 and DEC-J8**: the credential-free-error rule is written into `post.Sink`, and
  item 4 (re-evaluating #103's XML path) is delivered through #103; #115 closes as partial, since
  items 2 and 3 were declined.
- **#118 / DEC-J5**: the Error() fast path is documented.
- The `entry.go` comment is fixed.

Every new guard was mutated with its result predicted. All were killed except the predicted
survivors recorded in `state.yaml`: in fix pass 1, the Cf test (since removed) and two leak-gate
checks that stricter checks pre-empt; in fix pass 2, the equivalent rune-over-byte loop
(`fix_pass_2.mutation`). `make check` is clean.

## Blockers
None.

## Next best action
With the maintainer's go-ahead, commit Batch 13, push, and open the PR. The PR carries no closing
keywords, so `closingIssuesReferences` must read back empty. After merge, close each issue
explicitly with a comment that names the merge commit and carries the decision text and its date:

- #136: DEC-J1 and DEC-J7 (2026-09-29).
- #96: DEC-J2 (2026-09-29).
- #137: DEC-I2 (2026-09-28) and DEC-J9 (2026-09-29), which superseded DEC-J6's category rule,
  naming the residual: mistakes inside printable ASCII still load, and while configured a bare
  token in any recorded message body stays unredacted (a failed post's captured body, and every
  intake record when `message_on_error_only` is false).
- #105 (stays open): with the maintainer's go-ahead, a comment adding the FR-043/SC-006
  annotation for DEC-J9's residual to the spec-reconciler checklist.
- #115: DEC-J4 and DEC-J8 (2026-09-29), worded as a partial close: acceptance item 1 delivered,
  item 4 (re-evaluating #103's XML path) delivered through #103, items 2 and 3 declined by DEC-J4,
  to be revisited when a second credential-bearing sink appears.
- #118: DEC-J5 (2026-09-29).
- #102 and #103: what changed.

Then run `close-feature`: #105 is spec-reconciler's checklist, and it includes the FR-066 and
A-008 annotation. #101 (deferred, DEC-J3) and #138 (future) stay open.

## Important decisions
DEC-I1 and DEC-I2 (Batch 12), and DEC-J1 to DEC-J9 (Batch 13, 2026-09-29), are in
`.agents/state.yaml`. DEC-J3 defers #101 beyond v0.1.

## Touched files
`git diff --stat aa202f6` on the branch is the authoritative list. The outgoing copy of this page
is archived as `.agents/archive/project-status-before-batch-13.md`.
