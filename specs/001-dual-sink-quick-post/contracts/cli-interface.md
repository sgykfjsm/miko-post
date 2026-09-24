# Contract: Command-line interface

**Requirements**: FR-001 – FR-011, FR-059 – FR-063. **Design**: `docs/design.md` §4, §10.

## Synopsis

```text
mp [options] [message...]
```

| Option | Meaning |
|---|---|
| `-c`, `--config PATH` | Configuration file, **CLI posting only** (FR-005) |
| `-h`, `--help` | Show help (FR-007) |

## Dispatch table (normative)

| Invocation | Behavior | Exit |
|---|---|---|
| `mp` | Open the window using the **default resolved** config path (FR-002) | per post |
| `mp "hello"` | Post `hello` from the CLI (FR-003) | per post |
| `mp hello world` | Join args with exactly one ASCII space → `hello world` (FR-004) | per post |
| `mp -c ./c.toml "hello"` | Post using `./c.toml`, this post only (FR-005) | per post |
| `mp -c ./c.toml` | Error, **do not open the window** (FR-006) | `1` |
| `mp --help` | Help including the resolved default config path (FR-007) | `0` |

`--config` MUST NOT affect the configuration the window uses (FR-005, constitution principle V).
Standard input is never read for message content (FR-008).

## Required message text

```text
--config is only available when posting from CLI
```

## Message rejection (FR-009, FR-010)

A message is rejected before any destination is contacted, and there are **two** reasons. Each
produces its own correction prompt, its own sentinel in `internal/post`, and a failure exit; neither
reason is ever reported for the other.

| Reason | Sentinel | When |
|---|---|---|
| Empty or whitespace-only | `post.ErrEmptyMessage` | Trimming leading and trailing Unicode whitespace leaves nothing (FR-009) |
| Not valid UTF-8 | `post.ErrInvalidUTF8` | The joined message carries bytes that are not a valid UTF-8 encoding |

The second reason is a **decision**, not a derivation from FR-009 — issue #104, decision DEC-D4.
The spec is silent and its two relevant clauses (FR-011/FR-012 against FR-050 and constitution
principle VI) point in opposite directions. It was taken because the alternative's central promise
is false: no destination keeps the bytes. The chat service's documented contract is UTF-8 only and
it answers `400 Strings must be encoded in UTF-8`, whose text does not match the rescue predicate,
so that destination fails closed; the diagnostic log is JSON Lines and JSON is UTF-8 by definition,
so the handler substitutes U+FFFD and SC-008 and FR-068 become unsatisfiable for exactly these
messages; and Obsidian re-serialises a note as UTF-8 at the user's next save. The real behaviour
before this rule was *note succeeds, chat fails, exit 1* — a half-delivered post, which is the
outcome the two-destination design exists to prevent.

Accepted cost: a terminal that reliably produces non-UTF-8 bytes — a legacy Shift_JIS or EUC-JP
locale — cannot use the CLI front door until its locale is fixed. Not a regression in outcome, but
a real change for that user. The correction prompt names the locale for that reason.

Both prompts are user-facing text and both must be actionable: naming the rule is not enough, since
"invalid message" tells a user with a mis-encoded terminal to try typing something.

## Help output shape

Help MUST print the path resolved for the **current environment**, never a literal
`$XDG_CONFIG_HOME` expression (FR-007):

```text
Usage:
  mp [options] [message...]

If no message is specified, the GUI is launched.

Options:
  -c, --config PATH
        Path to the configuration file for CLI posting.
        Default: /Users/<user>/.config/miko-post/config.toml

  -h, --help
        Show this help.
```

## Result output

Every failed sink is named with a short reason, and partial success is visible (FR-062).
Failure output includes the diagnostic log path (FR-063).

```text
Obsidian: success
Telegram: failed — request timed out
See log for details: /Users/<user>/.local/state/miko-post/app.jsonl
```

Exact wording and the stdout/stderr split are implementation details; completeness, brevity, and
the log path are required.

## Exit status

| Code | Condition |
|---|---|
| `0` | Every enabled sink succeeded (FR-059). A post rescued by the unformatted retry counts as success (FR-061). |
| `1` | Input error, settings error, startup error, or any sink failure (FR-060). |

A diagnostics write failure never changes the exit status (FR-076), and adds **exactly one**
warning to the user-visible output naming the log path and the reason.

## Verification boundary

Everything above is implemented. One case is verified one layer below the process, and the
reason is recorded so the gap reads as a decision rather than an oversight (issue #119, option B):

**Two destinations both succeeding** is driven from a parsed command line through `cli.Run`'s
whole sequence — settings load, `app.Sinks`, the orchestrator, the note on disk, the report and
the exit status — with only the chat destination's network call substituted, in
`internal/cli/startup_test.go`. It is not reachable through a built binary, because the chat
destination's Bot API origin is unexported and no settings key may redirect it (FR-057,
constitution principle V). A regression dropping the second destination on the success path is
therefore caught in `internal/cli`, not only by `cmd/mp`'s partial-failure test.

**The startup-error window** (FR-030) is not exercised by a process test either: it waits to be
dismissed, so a test would hang, and on a developer's machine would open a real window. Its
content and every dismissal are tested headlessly in `internal/gui`, and the refusal that leads to
it is tested in `internal/app`.
