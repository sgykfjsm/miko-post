## Summary

Batch 12 is **polish and gates**, the last task-backed batch of v0.1. It adds the constitution's
secret-leak gate and the SC-014 acceptance matrix with its manual justifications. It also adds a
README and a commented example settings file that tests keep honest. It verifies vet, build and
`-race`, and the real `go install …@latest` and `make install` paths. The locally observable part
of the quickstart and its live command-line part are recorded; the native-window checks remain.

**No production code changes.** The Go changes are two new test files, plus one new test and its
accessor in `internal/sink/telegram`'s test files.

This PR also carries the **Batch 11 merge record**, following the batch-N-record-in-batch-N+1
pattern. PR #135 merged as `e0ef849`, and its issues were closed on the spot, but no record of
either reached `main`.

## Batch

- Batch: `Batch 12 — polish and gates`
- Objective: the constitution's quality gates (secret leak, acceptance traceability, clean
  toolchain) and distribution readiness (README, example settings, install paths).

## Related issues

No closing keywords, as in #133 and #135. Each issue is closed explicitly after merge, with a
comment naming the merge commit.

- T084 → #85: delivered here
- T085 → #86: delivered here
- T086 → #87: delivered here
- T087 → #88: delivered here
- T089 → #90: delivered here
- T090 → #91: delivered here.
- #94: closes under DEC-I1. The documented stamped build (`make install`) records a real commit,
  verified by T090. `go install` users of a tag get `git_commit = unknown` by accepted decision.
  `spec.md` FR-066 and A-008 are not yet annotated for DEC-I1; that is handed to spec-reconciler
  on #105, where the item was posted on 2026-09-28.
- #95: delivered here
- T091 → #92: **partial; stays open.** See *Out of scope*.

## Changes

- **T084, `internal/post/secret_leak_test.go`**: `TestSecretLeakGate` posts through `cli.Run`,
  `app.LoadSettings`, the real logger and both real sinks, with a sentinel token.
  - `http.DefaultTransport` is replaced by an in-process `RoundTripper`. The Telegram client sets
    no `Transport`, so this is its transport, and nothing can be dialled as long as that holds.
    `telegram.TestNewLeavesTheClientOnTheDefaultTransport` pins it.
  - Eight cases: success, transport failure, a token with trailing whitespace whose transport
    fails (#137), timeout, an echoed credential, the formatting rescue, both destinations failing,
    and a message containing the credential.
  - Assertions: the sentinel appears on neither user stream and in no file written, through the
    CLI front door or the window's GUI-source logger. It also appears in none of the strings
    `gui.resultText` reads, and in no guarded rendering of the outcome.
  - Two witnesses guard against vacuity: the token must appear in a request on the wire, and each
    case's log must carry evidence specific to its path. The rescue case needs
    `telegram_plaintext_failed` and the retry's own error; the both-fail case needs
    `obsidian_append_failed`; the timeout case needs `"error_type":"timeout"`, since net/http's
    "Client.Timeout exceeded" wording is racy.
- **T085/T086, `specs/001-dual-sink-quick-post/acceptance-matrix.md`**: all 20 criteria of
  `docs/design.md` §13, mapped to 104 named tests. Every name was matched mechanically against
  `go test -list`. The file includes:
  - twelve recorded divergences, where the spec refines or narrows §13 (D1–D12);
  - four manual items with justifications: live Telegram delivery, native launch and auto-close,
    native key routing, and native dismissal of the startup-error window.
  No criterion lacks automated coverage.
- **T087**: `README.md` covers install, configuration, usage, window keys, what each destination
  receives, and the diagnostic log. Its configure step fetches the example without a checkout and
  never overwrites an existing `config.toml`. `testdata/config/example.toml` lists every key. Every
  value not marked `REPLACE` is the default, except the two `enabled = true` lines. Two new tests in
  `internal/config/example_test.go` pin it:
  - `TestExampleSettingsAreTheDefaults`: the written values must equal `config.Defaults()` beyond
    the `REPLACE` placeholders. This pins the literal defaults (10, 7, 30, 60, 15, 30), which the
    matrix found were not pinned anywhere.
  - `TestExampleSettingsNameEveryKey`: every one of the 24 keys in `contracts/config-schema.md`
    must be shown.
- **#95**: `docs/design.ja.md` §14 uses the real module path. `grep -rn module-path docs` is empty.
- **DEC-I1**: `docs/design.md` §14 "Releasing" and its `design.ja.md` counterpart now say
  `make install` is the stamped build, and that a tagged `go install` recording `unknown` is an
  accepted v0.1 limitation, relaxed by publishing stamped binaries (tracked as #138). README's
  Install section says the same. `spec.md` FR-066 and A-008 are not yet annotated for DEC-I1;
  that is handed to spec-reconciler on #105, where the item was posted on 2026-09-28.
- **Records**: Batch 11 moved to `completed`, with its merge verified (head `5005ea8` and the squash
  share tree `776bbbb5`). The closures and the published follow-ups (#136, the #126 comment) are
  recorded. `tasks.md` T084–T087, T089 and T090 are ticked with delivery notes, and T091 carries a
  partial note. `project-status.md` has been rewritten; its previous copy is archived.
  `.agents/state.yaml` records review cycles 0–3, fix passes 1–3, the post-review edits, DEC-I1,
  DEC-I2 and the live DEC-D4 check.

## Validation

- [x] `make check` (gofmt, `go vet ./...`, `go test -race ./...`, 10 packages) and `go build ./...`
  are clean on go1.27.1 darwin/arm64. On 2026-09-28 the cycle-2 correctness review re-ran both,
  exit 0, on the fix-pass-2 diff (sha256 `e95d86d2…`). The cycle-3 correctness review ran
  `make check`, `go test -race ./...` and `go build ./...` again on the reviewed bytes
  (sha256 `7668d973…`), all exit 0. `make check` was run once more on the committed tree. The only extra output is the macOS
  linker warning `ignoring duplicate libraries: '-lobjc'`.
- [x] **T084 mutants**, re-run by the cycle-2 correctness review on the fix-pass-2 bytes
  (sha256 `e95d86d2…`):

  | Mutant | Expected | Result on `e95d86d2` |
  |---|---|---|
  | the formatting rescue never runs (predicate forced false) | killed | killed |
  | both-fail case with the note succeeding | killed | killed |
  | `Redact` armed only for non-GUI sources (GUI-only removal) | killed | killed (window subtest) |
  | `Redact` removed for the CLI only | killed | killed |
  | logger `Redact` nil | killed | killed |
  | `withoutRequestURL` short-circuited (alone) | killed | killed, by the trailing-whitespace case only |
  | `safe` and `withoutRequestURL` both removed | killed | killed |
  | `safe` and the `decodeResponse` scrub both removed | killed | killed |
  | client with its own `Transport` | killed | killed (against the seam test only) |
  | `safe` alone removed | survives | survived |
  | the `decodeResponse` scrub alone removed | survives | survived |

  For an exact token the sink-side layers mask each other in the wired stack, and the test says
  so. Each is pinned by `internal/sink/telegram`'s own tests. The `Transport` mutant was run only
  against `telegram.TestNewLeavesTheClientOnTheDefaultTransport`, because it bypasses the fake
  transport. The vacuity-witness mutants (fixture using another token, recorder dropping the error
  text) were killed on 2026-09-25 and after fix pass 1, and their witness-disabled control
  survived on 2026-09-25; they were not part of the `e95d86d2` run. No mutant removed a network
  seam.
- [x] Example-file mutants: four, each killed on the `e95d86d2` bytes. The first run's three were a
  changed default, a deleted key, and a deleted `background_image_dir`.
- [x] **T090**: `go install github.com/sgykfjsm/miko-post/cmd/mp@latest`, run from outside the
  module with `GOWORK=off`, resolved to `v0.0.0-20260924064643-e0ef849024a9`. It used the default
  module cache, not a fresh `GOMODCACHE`, and printed
  `go: downloading github.com/sgykfjsm/miko-post v0.0.0-20260924064643-e0ef849024a9`: the module
  came fresh from the proxy, while its dependencies were already cached. The binary had no
  `vcs.*` settings. A local post recorded `app_version` as that pseudo-version and
  `git_commit = e0ef849024a9`. `make install` from a clean clone recorded `e0ef849` / `e0ef849`
  untagged, and `v0.1.0` / `e0ef849` under a `v0.1.0` tag created only in that clone. The tagged
  `go install` case, commit `unknown`, is pinned by
  `version.TestResolve/tagged module install yields unknown commit`. It is accepted for v0.1
  (DEC-I1).
- [x] **T091, local part**: Scenarios 2, 5 and 9 in full, and the CLI or Obsidian halves of 1, 6
  and 8. All pass (`validation/t091-quickstart.md`). Scenario 2's line-breaks-only check was
  re-run with a real `$'\n\n'` argument, because the first run's `"$(printf '\n\n')"` was empty.
- [x] **T091, live command-line part**, 2026-09-28, against the maintainer's test bot, with the
  token read from a private file and never printed: Scenario 1, Scenario 3 with one and with both
  destinations broken, Scenario 4 and its bad-token negative case (one attempt only), and
  Scenario 6's last clause. All pass. #92's rider, a raw `sendMessage` with `text=a%FFb`, got
  HTTP 400 `Bad Request: strings must be encoded in UTF-8`: DEC-D4's premise holds, and #104
  stays closed. A token scan over every output, log, note and the rider reply found 0
  occurrences.
- [ ] T091, window part and `thread_id`: not run (below).

Apart from the 2026-09-28 live run, every run in this batch kept Telegram disabled or used the
in-process transport.

## Out of scope

- **T091 / #92, the window remainder.** Owed to the maintainer: Scenario 1 with `thread_id` set,
  if the chat is a forum; Scenario 7 in full on the native window; and Scenario 8's window halves,
  with each of the four dismissal routes of the startup-error window recorded, including the
  native `Esc` owed since T082. The list is in `validation/t091-quickstart.md`. #92 stays open.
- **`spec.md` FR-066 and A-008**: not yet annotated for DEC-I1. That is handed to spec-reconciler
  on #105, where the item was posted on 2026-09-28.
- **#137 / DEC-I2**, a `bot_token` with surrounding whitespace. For the URL / error path, the gate
  pins the one layer that covers it today. The message-body path has no layer for a
  whitespace-padded token: a message containing the core credential reaches `app.jsonl`
  verbatim, because `Redact` searches for the padded token (ADV-007). The maintainer decided on
  2026-09-28 (DEC-I2, on #137) to trim the token at load and reject interior whitespace. It is
  scheduled after this batch merges, before close-feature, and is not implemented here.
- **#138**, a release workflow that publishes stamped macOS binaries, which is DEC-I1's
  relaxation. Future work, to be considered later.
- **#96**, a single `git_commit` shape. T090 observed three: 7 characters from `make`, 12 from a
  pseudo-version, and 40 from a local build. Choosing one is a decision.
- #101, #102, #103, #115, #118 and #136 are defects or test gaps, not polish. #105 is the
  spec-reconciler checklist. With #96, #137 and #138 above, these are the open non-task issues:
  #96, #101, #102, #103, #105 (spec-reconciler), #115, #118, #136, #137, #138.
- `.specify/integrations/claude.manifest.json` has an unrelated local timestamp change. It is not
  in this PR.

## Review

Staged review (contract, correctness, adversarial), with fix-and-rereview authorized by the
maintainer:

| Cycle | Verdict | Required findings |
|---|---|---|
| 0 | needs-human-decision (decided as DEC-I1) | 6 |
| 1, after fix pass 1 | request-changes | 2 |
| 2, after fix pass 2 | request-changes (code converged) | 5, records only |
| 3, after fix pass 3 | **passed-with-notes** | 0 |

**Post-review edits.** These were applied after the final verdict, outside the fix loop, which had used
all three passes. They are wording and records only, and they were not re-reviewed:

- `testdata/config/example.toml`: `filename_format` "must end in .md", which is what `validate.go` checks (COR-008).
- `tasks.md` T089: the 2026-09-25 run was on the *initial* tree (COR-009).
- `README.md`: the "never logged or printed" sentence names the #137 exception until DEC-I2 lands (ADV-013).
- `.agents/*`: the cycle-3 record, and a #92 progress comment added to the post-merge plan (CON-017).

## Notes for reviewers

- The leak gate is not parallel, because it swaps a process-global transport. Nothing else in
  `internal/post` touches `http.DefaultTransport`.
- In the acceptance matrix's Result log, M1 is done except `thread_id` topic routing, and M2–M4
  read "pending — T091" on purpose. None of the window checks is claimed.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
