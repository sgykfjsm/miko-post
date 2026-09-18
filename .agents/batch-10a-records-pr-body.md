## Summary
Reconciles `.agents/` to the merged reality of Batch 10a, and closes the four issues that merge left
open. **No code changes** — this touches only `.agents/`.

Split out of the Batch 10b PR so that a record decision is reviewed on its own merits rather than
riding along with a code change. That split is itself a review finding (CON-002 on 10b).

## Why this needs a decision, not just a merge
**There are two independent reconciliations of the same event, and merging either one buries the
other.** This is one of them.

| | this PR (`e0a0c39`) | the other (`d057f45`, branch `sgykfjsm/batch-10b-diagnostics`, pushed, unmerged) |
|---|---|---|
| `review_verdict` | `not-final-at-merge` | `passed-with-notes` |
| `review_cycles` | 3 | 2 |
| adversarial stage | never completed | `findings-none-blocking` |
| files touched | `state.yaml`, `project-status.md`, `work-log.md`, `archive/` | `state.yaml` only |
| written | 2026-09-18, two days after the merge | 2026-09-16, two minutes after it |

### Why this PR records `not-final-at-merge`
The run manifest at `~/.agents/review-runs/sgykfjsm__miko-post/20260916T030000Z-3be7358f` is the
evidence, and it does not support `passed-with-notes`:

- It records **three** cycles (0, 1, 2), not two.
- Cycle 2 — the cycle that would have pronounced on the third fix pass — has contract `valid` and
  correctness `pass`, but its **adversarial stage sits at `status: running`**, and no `cycle-02`
  report directory was ever written.
- The third fix pass then applied six findings, three of them adversarial (ADV-007–ADV-009), so that
  stage *did* deliver findings. Nothing then reviewed the tree those fixes produced — and that is
  the tree that merged.
- Three fix passes is the loop's maximum, so it ran out of passes rather than reaching a verdict.

Every finding was addressed and the batch is merged and validated. But *"the review pronounced it
sound"* and *"the review ran out of road with every finding fixed"* are different claims, and only
the second is supported. That distinction is the whole reason this is `not-final-at-merge`.

### What the other record does better, and which is adopted here
`d057f45`'s `merge_verification` is stronger evidence than this session originally had: it verifies
**tree equality** — `0e3df5e` and the reviewed tip `73d9c57` share tree
`a26e21c56c2caa1c2100ab61e55341ed7cc654e1` — rather than merely matching head SHAs. That is carried
into this record, with attribution.

**If you prefer the other record, close this PR** and merge `d057f45` instead; Batch 10b will need
rebasing either way. What must not happen is both being merged independently, or one being merged
without the conflict being noticed — which is what nearly happened.

## What else is in here
- **Batch 10a is moved from `in_progress` to `completed`** with the merge facts: PR #126
  squash-merged 2026-09-16T14:32:40Z as `0e3df5e` from head `73d9c57`, all five branch commits
  listed and annotated.
- **`next_best_action` rewritten** to a single item, and `remaining_batches` / `next_batch_preparation`
  brought in line.
- **Issues #66, #69, #70 and #71 closed** as completed, each with a comment naming PR #126 and
  `0e3df5e`, the delivering code and the validation. They did not auto-close: the PR body
  deliberately said the issues stay open until review and merge, so it carried no closing keyword,
  and `closingIssuesReferences` was verified empty *before* the merge and recorded in
  `publication_note`. The check was done, the prediction was right, and no one owned the step it
  implied — so they sat open for two days.
- **`project-status.md` overwritten**, outgoing copy archived as
  `archive/project-status-before-batch-10b.md`, following the existing `before-batch-7/8/9` practice.

## Why the records drifted, recorded so the next reader meets it
Batch 10a's own last two commits (`9ba5b5d`, `73d9c57`) updated **only** `state.yaml`. So `state.yaml`
already knew the correction passes were pushed and the PR re-described, while `project-status.md` and
`work-log.md` were frozen at the third correction pass and still described #126 as open at `f3ab572`
with uncommitted work. Nothing recorded the merge at all, because it happened outside a session
running the state-keeper skill. A closing note in `project-status.md` says this, so the next reader
meets it rather than rediscovering it.

## Also still open, not addressed here
A closure sweep is overdue beyond 10a: **#60–#65** (T059–T064, delivered in Batch 9 / PR #125) are
merged but still open, as are review follow-ups **#115**, **#118** and **#119** — same cause, no
closing keyword and no owner for the post-merge step. Only the four issues 10a itself left open were
closed here, which is what was authorised.

## Validation
`make check` (gofmt, vet, full `-race` suite) green on `0e3df5e` across all ten packages before these
records were written; this commit changes no code, so nothing to re-run. PR #126 read back as
`state: closed, merged: true`; all four issues read back `CLOSED / COMPLETED`; `state.yaml` re-parsed
after editing.

## Relationship to Batch 10b
Batch 10b (`sgykfjsm/batch-10b-post-diagnostics`) currently carries this commit as its base. Once
this merges, 10b must be rebased onto the new `main` before its own PR is opened, so that it carries
only its own commits.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
