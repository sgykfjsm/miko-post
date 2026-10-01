# User guide

Install and configure `mp` as described in the [README](../README.md#install). This guide covers
behavior worth knowing and what to do when `mp` refuses to start.

## Telegram formatting

Messages are sent unchanged using MarkdownV2. If Telegram explicitly rejects the
formatting, miko-post makes one plain-text attempt with the same text and destination.
A successful rescue counts as success. Other errors do not trigger a retry; a failed
rescue reports the final failure and retains both attempts in diagnostic events.

## Optional GUI background

Add an absolute directory to the default XDG configuration:

```toml
[gui]
background_image_dir = "/Users/you/Pictures/miko-backgrounds"
background_opacity = 0.3
```

Each window selects one usable top-level PNG/JPEG image and displays it over
black at `background_opacity`: 0 hides it, 1 shows it unchanged, and the default 0.12 is faint.
If the image is hard to see, raise it; text becomes harder to read as it grows. An empty setting disables the image. Missing, unreadable, corrupt or oversized
images fall back to another candidate or black; they do not prevent posting.
See [background behavior and limits](gui-background.md) for details.

## Troubleshooting

**"no destination is enabled"**: every `[sink.*]` section has `enabled = false`. Set
`enabled = true` under `[sink.obsidian]` or `[sink.telegram]`.

**A `sink.telegram.bot_token` refusal**: the token must be at least 16 characters and, after
leading and trailing whitespace is removed, contain only printable ASCII (no interior spaces,
invisible characters, smart quotes or other non-ASCII characters). This applies wherever the token
comes from (`bot_token` or `MIKO_POST_TELEGRAM_BOT_TOKEN`) and **even when `[sink.telegram]` is
disabled**, so a placeholder such as `"TODO add the token later"` blocks startup. Copy the token
again from @BotFather, or remove the key if you do not use Telegram. A token pasted with its quotes
or a `bot` / `TOKEN=` prefix is not refused but fails at Telegram; fix it, because while it is
configured a bare token typed into a message is not redacted from the log.

**"cannot open the window ... home directory"**: the window needs `HOME` set to an absolute path,
or its framework would write files into the current directory. Set `HOME`, or post from the
command line, which does not need it.

**Where is the diagnostic log?** `$XDG_STATE_HOME/miko-post/app.jsonl`, or
`~/.local/state/miko-post/app.jsonl` when that variable is unset or empty; `logging.path` overrides it. See
the [README](../README.md#diagnostic-log).

**`git_commit` reads `unknown`**: expected for a `go install` of a tagged version (an accepted v0.1
limitation). `make install` from a checkout records the commit.

The [design document](design.md) describes the CLI, sink settings and exit behavior.
