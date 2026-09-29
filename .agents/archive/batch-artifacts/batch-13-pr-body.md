## Summary

Batch 13 is **hardening**: the maintainer's 2026-09-29 decisions on the remaining non-task issues,
taken after Batch 12 and during its review. The user-visible behaviour changes are:

- The bot token is trimmed of surrounding whitespace at load, from the settings file and from
  `MIKO_POST_TELEGRAM_BOT_TOKEN` alike.
- After the trim, a token that is not entirely printable ASCII without spaces (`0x21`–`0x7E`) is
  refused at startup, even when the chat destination is disabled (DEC-J9). That covers interior
  whitespace, controls, invisible characters such as a zero-width space, and non-ASCII lookalikes
  such as smart quotes. The message does not quote the token.
- **Compatibility**: with the chat destination disabled, a placeholder `bot_token` such as
  `"TODO add the token later"` is now refused at load and blocks both front doors until it is removed or
  replaced. This is deliberate: the token arms the redaction pattern either way.
- Neither window opens unless the home directory resolves to an absolute path. `mp` prints the
  reason and exits 1 instead.
- `git_commit` is 12 characters on every build path that identifies a commit.

Other changes do not alter behaviour:

- Test: the enumeration test for `SinkResult` render methods fails, rather than logs, when it
  cannot evaluate a method.
- Comments and contracts: credential-free errors, the XML boundary, the layout alphabet and the
  `safe()` fast path.

This PR also carries the **Batch 12 merge record**, following the batch-N-record-in-batch-N+1
pattern.

## Batch

- Batch: `Batch 13 — hardening`
- Objective: close the credential and startup-safety gaps the Batch 11 and 12 reviews found, and
  settle the remaining v0.1 non-task issues.

## Related issues

There are no closing keywords, as in #133, #135 and #139. Each issue is closed explicitly after
merge, with a comment naming the merge commit.

- #137, DEC-I2, DEC-J6 and DEC-J9: delivered here
- #136, DEC-J1 and DEC-J7: delivered here
- #96, DEC-J2: delivered here
- #102: delivered here
- #103: delivered here
- #115, DEC-J4 and DEC-J8: partial. Acceptance item 1, the `post.Sink` contract, is delivered
  here; items 2 and 3 were declined by DEC-J4, to be revisited when a second credential-bearing
  sink appears; item 4, re-evaluating #103's XML path, is delivered here through #103. The issue
  is closed after merge per DEC-J8, with a comment saying so.
- #118, DEC-J5: delivered here
- #101, DEC-J3: deferred beyond v0.1. It stays open, and the decision is commented on the issue.

## Changes

- **DEC-I2 and DEC-J9 (#137)**:
  - `config.Secret` gains `Trimmed` and `IsPrintableASCII`, so the new rules add no `Reveal()`
    call site.
  - `ResolveCredential` trims the file token and the `MIKO_POST_TELEGRAM_BOT_TOKEN` value of
    Unicode whitespace. A blank environment value counts as unset.
  - `Validate` refuses a token with any byte outside `0x21`–`0x7E`, whether or not the sink is
    enabled. The check is on bytes, so invalid UTF-8 is refused too. The message does not quote
    the token. DEC-J9 replaces DEC-J6's category rule (whitespace, Cf, non-graphic), which let
    U+FE0F, U+3164, U+2800, smart quotes and a fullwidth colon load.
  - `TestSecretLeakGate`'s whitespace cases require the trimmed token on the wire, and never the
    `%20` form. A padded token, plus the bare token pasted into a message, must come out redacted.
    Its zero-width and smart-quote cases must be refused at load: exit 1, the refusal on stderr
    and from `LoadSettings`, no request, no file written.
  - This closes the leak Batch 12 found (ADV-007) for whitespace, invisible and non-ASCII
    corruptions of the token, not for every corruption. Mistakes inside printable ASCII still
    load: the token pasted with its straight quotes, a `bot` prefix, a `TOKEN=` prefix. While such
    a token is configured, a bare token in any recorded message body is not redacted: a failed
    post's captured body, and every intake record when `logging.message_on_error_only` is false.
    With the chat destination enabled the token fails at Telegram; with it disabled only the
    intake case applies.
- **DEC-J1 and DEC-J7 (#136)**: `gui.Run` delegates to `runWith(errOut, homeDir, newApp)`. With no
  home directory, or one that is not an absolute path, it prints the reason and exits 1 before
  loading settings or constructing the Fyne app. A relative value is not printed. This covers
  both the posting window and the startup-error window. The command line is unaffected.
- **DEC-J2 (#96)**: `version.resolve` cuts any lowercase-hex commit longer than 12 characters to
  its 12-character prefix, and `make` stamps `git rev-parse --short=12`.
  - The issue's preferred 40 characters cannot be produced on the `go install` path, because a
    pseudo-version carries only 12. So 12 is the longest shape every path can produce.
  - A shorter stamp, `unknown`, or a non-hex `COMMIT` passes through unchanged.
- **#102**: the layout-safety argument in `validate.go` and `config-schema.md` is stated as a
  property ("no path separator, no control character, no lone dot"). The current list gains
  `AM`/`PM`, `am`/`pm` and `Z`.
- **#103**:
  - `result.go` names the `encoding/xml` boundary.
  - The method-enumeration test now fails on any argument-taking method other than `Format`.
    `t.Logf` never reached `make check`'s output.
- **DEC-J4 (#115)**: `post.Sink.Send`'s contract says a returned error must not carry a credential.
- **DEC-J5 (#118)**: `safe()`'s `Error()` scan is documented as an intentionally unpinned fast path
  that `%#v` subsumes. The comment says that a clean error pays for both scans.
- **Other**:
  - The `internal/gui/entry.go` `commandButton` comment now matches the Batch 12 window checks.
  - Contracts updated: `config-schema.md`, `gui-interface.md` and `log-events.md`.
  - The `quickstart.md` stamp example, the `docs/design.md` and `docs/design.ja.md` log example,
    README and `example.toml` token wording are updated.
  - Records: Batch 12 moved to `completed` with its merge facts, Batch 13 is in progress, and
    DEC-J1 to DEC-J9 are recorded.

## Validation

- [x] `make check` (gofmt, `go vet ./...`, `go test -race ./...` across 10 packages) and
  `go build ./...` are clean on go1.27.1 darwin/arm64.
- [x] **Mutants.** Each result was predicted. Three groups: before the review, fix pass 1
  (DEC-J6's rune test, since replaced) and fix pass 2 (DEC-J9). Test names are current.

  | Mutant | Result |
  |---|---|
  | file-token trim removed | killed: the credential precedence test and both leak-gate whitespace cases |
  | environment trim removed | killed: the credential precedence test |
  | blank environment value counted as set | killed: the credential precedence test |
  | interior-whitespace check removed | killed: the interior-space case of `TestANonPrintableASCIICredentialIsRefused`, enabled and disabled |
  | home-directory check removed | killed: `TestNoHomeDirectoryOpensNoWindow`; its constructor stops the test, so no window opens |
  | `normalizeCommit` made the identity | killed: `TestCommitHasOneShapeOnEveryPath` |
  | commit length set to 7 | killed: `TestCommitHasOneShapeOnEveryPath` |
  | an argument-taking `AppendText` added to `SinkResult` | killed: the enumeration test |
  | pass 1: `unicode.IsSpace` removed from the rune test | killed: interior space, both sources, enabled and disabled |
  | pass 1: `!unicode.IsGraphic` removed | killed: interior U+0007 |
  | pass 1: only `unicode.IsSpace` kept | killed: the leak gate's zero-width case (a request was made; `LoadSettings` accepted) |
  | pass 1: the check made `strings.Contains(value, " ")` | killed: tab, newline, U+200B, U+FEFF, U+0007 |
  | pass 1: `strings.TrimSpace` made an ASCII-only `strings.Trim` | killed: `TestAnNBSPPaddedCredentialIsTrimmedAndValidates` |
  | pass 1: refusal removed | killed: the credential refusal test and the leak gate's zero-width case |
  | pass 1: refusal moved inside `if t.Enabled` | killed: every disabled subtest |
  | pass 1: `!filepath.IsAbs(home)` check removed | killed: both relative-home subtests; the constructor stops the test |
  | pass 1: trim removed (leak gate) | killed: both whitespace cases, refused at load |
  | pass 1: trim and refusal both removed (leak gate) | killed, including by the escaped-`%` branch |
  | pass 1: `unicode.Is(unicode.Cf, r)` removed | **survived, equivalent** (no Cf rune is graphic); the rune test no longer exists |
  | pass 1: the gate's escaped-`%` branch removed | **survived**: the load refusal pre-empts it; documented as a second net |
  | pass 1: the zero-width case's no-request check removed | **survived**: the stderr, exit and `LoadSettings` checks also fail such a run. It is the first to fire on every product mutant that reaches the wire |
  | pass 2: lower bound `b < 0x21` made `b < 0x20` (admits space) | killed: interior space, both sources, enabled and disabled |
  | pass 2: upper bound `b > 0x7e` made `b > 0x7f` (admits DEL) | killed: interior DEL, both sources, enabled and disabled |
  | pass 2: the byte loop made a range over runes | **survived, equivalent**: every byte from 0x80 belongs to a rune above U+007F or decodes to U+FFFD, both refused, so `"\xa0"` is still refused. `IsPrintableASCII`'s comment says so |
  | pass 2: refusal disabled (`if false && ...`) | killed: all 13 credential refusal cases in all four variants, and the leak gate's zero-width and smart-quote cases |
  | pass 2: refusal made Enabled-only | killed: every disabled subtest (the leak gate runs enabled, so it cannot see this one) |

  No mutant touched a network seam: the fake `http.DefaultTransport` and `cli.run`'s
  `newService` seam stayed in place, and no Transport mutant was run.
- [x] **Native probes**, run from an empty directory with `env -i` and `XDG_CONFIG_HOME` set:
  - Both windows exit 1 at once, and the directory is still empty afterwards.
  - A CLI post without `HOME` still succeeds and writes nothing to the working directory.
  - `make build` and an unstamped `go build` of the same tree both record
    `git_commit=aa202f68da60`.
  - Fix pass 1: with `HOME=tmp`, `mp` exits 1 at once with the not-absolute message and the
    directory is still empty (DEC-J7).
  - Fix pass 1: a CLI post whose settings file has a `bot_token` ending in U+200B, with Telegram
    disabled, is refused at load, quoting nothing; exit 1, nothing posted (DEC-J6; DEC-J9 keeps
    this refusal).
  - Fix pass 2, with no `HOME` and Telegram disabled: a `bot_token` wrapped in smart quotes, and
    the placeholder `"TODO add the token later"`, are each refused at load with the DEC-J9
    message, quoting nothing; exit 1; the note folder, the log directory and the working
    directory stay empty.

## Review

This batch went through a staged review (contract, correctness, adversarial). The maintainer
authorized fix-and-rereview and made the decisions below:

| Cycle | Verdict | Required findings | Decisions |
|---|---|---|---|
| 0 | needs-human-decision | 2 (closure wording) | DEC-J6, DEC-J7, DEC-J8 |
| 1, after fix pass 1 | needs-human-decision | 1 (the rule's claim) | DEC-J9 |
| 2, after fix pass 2 | **passed-with-notes** | 0 | — |

**Post-review edits.** These were applied after the final verdict. They change wording, records and
the last clause of the refusal message only, and they were not re-reviewed:

- The DEC-J9 residual now names every recorded body: a failed post's captured body, and every
  intake record when `logging.message_on_error_only` is false. It also covers the disabled-sink
  case. The wording is updated in `credential.go`, `config-schema.md`, the leak-gate doc,
  acceptance-matrix row 13, README and this body (COR-006, CON-007).
- The refusal message adds "or remove it if you do not use Telegram". The compatibility note
  covers a value left in `MIKO_POST_TELEGRAM_BOT_TOKEN` (ADV-007).
- The FR-043 and SC-006 annotation for the residual is handed off to #105 (CON-008).
- The `result.go` comment now says the enumeration test fails on any method *other than
  `Format`*.

## Out of scope

- #101, deferred by DEC-J3.
- #105 (spec-reconciler, including the FR-066/A-008 annotation), handled at `close-feature`.
- #138, the release workflow, which is future work.
- `.specify/integrations/claude.manifest.json` has an unrelated local timestamp change. It is not in
  this PR.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
