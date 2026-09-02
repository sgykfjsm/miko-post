# Contract: Windowed interface

**Requirements**: FR-020 – FR-030, FR-027, FR-059. **Design**: `docs/design.md` §5.

## Composition (FR-020)

The window contains exactly: a multi-line message field, a Send control, a Cancel control, and a
compact result/error area. Nothing else.

## Focus (FR-021)

The message field holds keyboard focus when the window appears. No click is required before
typing.

## Keyboard contract (FR-022, macOS — A-003)

| Key | Behavior | Note |
|---|---|---|
| `Enter` | Insert a line break; send nothing | Default `Entry` behavior; must survive the extension |
| `Cmd+Enter` | Send | Delivered via the extended entry's `TypedShortcut` (R-003) |
| `Esc` | Cancel and close without contacting any sink | Delivered via the extended entry's `TypedKey` (R-003) |
| `Cmd+Q` | Quit the application | Canvas shortcut |

Because the message field holds focus, a shortcut registered **only** on the canvas never fires
for `Cmd+Enter` or `Esc`. See research R-003.

## Submission (FR-023, FR-024, FR-025)

- The Send control and `Cmd+Enter` share one validation and submission path (FR-023,
  constitution principle II).
- Send is disabled for the whole submission so a post cannot be submitted twice (FR-024).
- The window waits for **every** enabled sink to finish, then shows a compact per-sink result
  (FR-025).

## Result display (FR-029)

Every failed sink is named with a short human-readable reason. Detailed errors and stack traces
are never shown in the window — they belong in the log.

## Auto-close (FR-026, FR-027, FR-028)

| Outcome | Default delay | Setting |
|---|---|---|
| Every enabled sink succeeded | 15 s | `gui.success_close_seconds` |
| At least one sink failed | 30 s | `gui.error_close_seconds` |

- Auto-close **terminates the process**, and the exit status follows the same rule as the CLI
  (FR-027).
- **Any** user interaction while the delay is pending — keypress, click, or the window regaining
  focus — cancels the auto-close, leaving the result on screen until the user dismisses it
  (FR-028). The process may then stay alive indefinitely; the exit status when it finally closes
  still reflects the post's outcomes.

## Startup-failure window (FR-030)

When settings are invalid or every sink is disabled and the windowed path was launched, a minimal
error window shows the actionable message and the **resolved settings path**, offers no message
field, and exits `1` when dismissed.

## Durability (spec Edge Cases)

If the window is dismissed while a post is in flight, the post's outcome is still recorded in the
diagnostic log even though no one is left to read the on-screen result.
