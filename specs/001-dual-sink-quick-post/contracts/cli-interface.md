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

A diagnostics write failure never changes the exit status (FR-076).
