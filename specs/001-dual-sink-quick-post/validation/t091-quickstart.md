# T091 quickstart run

Run on macOS arm64 (Darwin 27.0.0, go1.27.1) on 2026-09-25, against the Batch 12 tree based on
`e0ef849024a97db44584ae808c0fecc89c36c71a`. The Batch 12 changes touch no production code, so
the binary behaves like `e0ef849`.

**Status: partial.** Everything that can be observed without the live Bot API and without a person
at the native window was run and passed. The live command-line checks were run on 2026-09-28 and
passed (see *Live run, 2026-09-28*). The rest is listed under *Owed to the maintainer* and keeps
T091 / #92 open.

## Method

The production `cmd/mp` was built unchanged from this tree (`go build ./cmd/mp`). Every
2026-09-25 run used the following:

- a temporary `XDG_CONFIG_HOME`, `XDG_STATE_HOME` and note directory;
- `MIKO_POST_TELEGRAM_BOT_TOKEN` unset;
- **`[sink.telegram] enabled = false` in every settings file.**

Telegram is disabled for a structural reason: the production binary has no setting that points
the Bot API elsewhere. With the chat destination enabled, any run would reach
`api.telegram.org`, even with a fake token. Local runs therefore exercise the Obsidian destination
and the shared front door only. Bare `mp`, which opens the window, was never run.

## Results

| Scenario | Checked here | Result |
|---|---|---|
| 1 — both succeed from the CLI | Obsidian half: exit `0`; note line `- HH:MM hello from quickstart`; four records sharing one `message_id`; no record carries `message` | pass (partial); the Telegram half passed live on 2026-09-28 |
| 2 — joining and whitespace | `mp hello world` → note line `hello world`, one space. ASCII spaces, U+3000 and tabs only: each printed the correction prompt, exited `1`, and created no log at all, so no `*_started` event. Line breaks only, re-run 2026-09-28 (the first run's `"$(printf '\n\n')"` was an empty argument, because command substitution strips trailing newlines): `$'\n\n'` (2 bytes, `0a 0a`) and `$'\r\n'` (2 bytes, `0d 0a`) each printed `mp: nothing to post: the message is empty or contains only whitespace. Type a message and try again.` on stderr, exited `1`, and created no log, no state directory and no note. `"  padded  "` kept both spaces. | pass |
| 3 — one sink broken, one healthy | not run: needs the chat destination live next to a broken note sink | owed; passed live on 2026-09-28 |
| 4 — formatting rescue | not run: needs the live Bot API to reject MarkdownV2 | owed; passed live on 2026-09-28 |
| 5 — multi-line into one note line | `one\ntwo\r\nthree\rfour` → one physical line `one<br>two<br>three<br>four`; `\r\n` gave one `<br>`; no CR left in the file | pass |
| 6 — append preserves content | a note without a trailing newline stayed byte-identical as a prefix and gained exactly two lines. With `create_if_missing = false` and the note deleted: `Obsidian: failed — delivery failed`, log path printed, exit `1`, note not recreated | pass (partial): "Telegram is unaffected" passed live on 2026-09-28 |
| 7 — the window | not run | owed |
| 8 — override and resolution | `--help` printed the resolved default path, exit `0`. `-c other.toml hi` wrote to the other vault only, exit `0`. `-c other.toml` with no message: `mp: --config is only available when posting from CLI`, exit `1`, no window. All destinations disabled: the actionable FR-018 message, exit `1`. | pass (partial): the window halves are owed |
| 9 — diagnostics | `rotate_size_mib = 1`, four 400 KB posts → `app.jsonl` plus `app.jsonl.20260925113402`; nothing deleted; 16 lines across both files, all parse. A failed post: one `message_id`, and the body is on `obsidian_append_failed`, the record reporting the failure (DEC-G1). Unwritable log directory: the post succeeded, exactly one `warning: diagnostics could not be written to <path>: <reason>`, exit `0` | pass |

Scenario 1's "no token anywhere" and Scenario 9's "no token in any line" were not observable here,
because no token was loaded. They are covered end to end by `TestSecretLeakGate` (T084), which drives
the same wiring with a sentinel token.

## Live run, 2026-09-28

Run by the coordinator against the maintainer's test bot and chat. The token and chat id were read
from a private file outside the repository and were never printed. The binary was built from this
tree; no production code differs from `e0ef849`. Settings used temporary directories and a
temporary vault. Everything passed.

| Check | Result |
|---|---|
| Scenario 1 as written (`hello from quickstart`, both destinations) | exit `0`; `Obsidian: success` / `Telegram: success`; events `message_received, telegram_send_started, obsidian_append_started, obsidian_append_succeeded, telegram_send_succeeded, request_completed`; one `message_id`; no `message` field; note line `- 16:53 hello from quickstart`. The first MarkdownV2 attempt succeeded, with no rescue. |
| Scenario 1, a message with `(` and `)` | exit `0`. The rescue ran (`telegram_markdown_failed` then `telegram_plaintext_succeeded`), which is expected, because the characters are reserved and sent verbatim (FR-033). |
| Scenario 3, one broken, and Scenario 6's last clause (`create_if_missing = false`, no note) | exit `1`. `Obsidian: failed — delivery failed`, `Telegram: success`, `See log for details: <path>`. Telegram delivered the complete message. The note was not created. |
| Scenario 3, both broken, and Scenario 4's negative case (a well-formed but invalid token) | exit `1`. `Obsidian: failed — delivery failed`, `Telegram: failed — authentication failed`. Logged error: `telegram refused the message: HTTP 401, error_code 401: Unauthorized: invalid token specified`, with `http_status` 401. Exactly one `telegram_send_failed` and no markdown or plaintext events, so no second attempt (FR-038). |
| Scenario 4, `release 1.0 (finally!) - shipped.` | exit `0`. `telegram_markdown_failed` came before `telegram_plaintext_succeeded`. Output shows `Telegram: success`, and nothing mentions a rescue. |
| #92 rider (DEC-D4): a raw `sendMessage` with `text=a%FFb` | HTTP 400, `{"ok": false, "error_code": 400, "description": "Bad Request: strings must be encoded in UTF-8"}`. **DEC-D4's premise holds; #104 stays closed.** |
| Token scan over stdout, stderr, every log, the notes, and the raw rider reply | 0 occurrences |

The token scan also discharges Scenario 1's "no token anywhere" and Scenario 9's "no token in any
line" against a real token.

## Owed to the maintainer

These are the manual justifications in [acceptance-matrix.md](../acceptance-matrix.md). Each
needs a real bot token and chat, a person at the window, or both. Items 1–4 and 7 were done on
2026-09-28 (see *Live run, 2026-09-28*), apart from item 1a, Scenario 1's `thread_id` case. Items
1a, 5 and 6 remain. Together they are the complete remainder of quickstart.md Scenarios 1–9 and
of the M1–M4 procedures.

1. **Scenario 1 in full**: both destinations enabled, and the message arrives in the chat.
   Done 2026-09-28.
   - **1a, still owed**: Scenario 1 with `thread_id` set, if the chat is a forum with topics: the
     message lands in the topic (criterion 5, M1).
2. **Scenario 3**: a broken note directory with a live chat destination, then both broken.
   Done 2026-09-28.
3. **Scenario 4**: `mp "release 1.0 (finally!) - shipped."` arrives as plain text. The run exits
   `0`, and the log shows `telegram_markdown_failed` then `telegram_plaintext_succeeded`. Then use a
   bad token: one attempt only. Done 2026-09-28.
4. **Scenario 6's last clause**: with `create_if_missing = false` and no note, Telegram still delivers.
   Done 2026-09-28.
5. **Still owed: Scenario 7 in full, on the native window** (M2, M3):
   - focus without a click;
   - `Enter` inserts a line break;
   - `Cmd+Enter` sends, and Send is disabled while sending;
   - the result panel names both outcomes (FR-025);
   - success auto-closes after 15 s with exit `0`, and failure after 30 s with exit `1`, each run
     idle and with a keypress during the countdown (the keypress cancels);
   - `Esc` before sending, with focus in the field and then on a button, closes without posting;
   - `Cmd+Q` quits.
6. **Still owed: Scenario 8's window halves** (M2, M4): bare `mp` after a `-c` post uses the default
   path. With every destination disabled, bare `mp` shows the startup-error window with the
   message and the path and no field. Each of the four dismissal routes (Quit, the close box,
   `Esc`, `Cmd+Q`) exits `1`, recorded per route. The native `Esc` has been owed since T082:
   Batch 11 found it dead there while a headless test passed.
7. **#92's rider (2026-09-10), tied to DEC-D4**: one live `sendMessage` against a throwaway bot
   with `text=a%FFb`, a percent-encoded invalid UTF-8 byte. If Telegram answers
   `400 Strings must be encoded in UTF-8`, DEC-D4's premise holds and nothing changes. If it
   accepts the byte, DEC-D4's premise fails and #104 should be reopened (Option C). Record the
   result here and on #92. Done 2026-09-28 and recorded here: the premise holds, and #104 stays
   closed.

Record each result in this file under a dated heading, with the date, commit and outcome.
