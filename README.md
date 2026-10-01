# miko-post

`mp` posts a short message to a Telegram chat and to today's Obsidian daily note at the same
time, from the command line or from a small window. The two destinations are independent: a
failure at one never stops, delays or hides the other, and every result is reported.

v0.1 targets macOS.

## Install

You need Go 1.24 or later and a C toolchain for the window's cgo dependencies. On macOS that
means the Xcode command line tools (`xcode-select --install`).

```bash
go install github.com/sgykfjsm/miko-post/cmd/mp@latest
```

This installs `mp` into `$(go env GOPATH)/bin` (or `$GOBIN`).

Once a version is tagged (from `v0.1.0` on), a `go install` of it records the version but not
the commit, so the diagnostic log reports `git_commit` as `unknown`. That is an accepted
limitation for v0.1 (decision DEC-I1). The version still identifies the release, and the tag
names exactly one commit.

To build from a checkout instead, use `make install`. It stamps both the version and the commit
into the binary through linker flags, so the log records both. See
[docs/design.md §14](docs/design.md#14-installation).

## Configure

Settings are one TOML file. `mp --help` prints the path it reads:

```text
$XDG_CONFIG_HOME/miko-post/config.toml    (or ~/.config/miko-post/config.toml)
```

Start from the commented example, which lists every key. Every value not marked `REPLACE` is the
default, except the two `enabled = true` lines: a destination is disabled by default, and the
example enables both. The commands below fetch the example only when no `config.toml` exists
yet, so they never overwrite your settings:

```bash
dir="${XDG_CONFIG_HOME:-$HOME/.config}/miko-post"
mkdir -p "$dir"
[ -e "$dir/config.toml" ] || curl -fsSL -o "$dir/config.toml" \
  https://raw.githubusercontent.com/sgykfjsm/miko-post/main/testdata/config/example.toml
chmod 600 "$dir/config.toml"
```

That fetches the example from `main`; for a tagged install you can put the tag in place of
`main`. From a checkout, this does the same without the network, and also leaves an existing file
alone:

```bash
dir="${XDG_CONFIG_HOME:-$HOME/.config}/miko-post"
mkdir -p "$dir"
cp -n testdata/config/example.toml "$dir/config.toml"
chmod 600 "$dir/config.toml"
```

Keep the `chmod 600` line either way: the file will hold your token.

Then replace the lines marked `REPLACE`:

- `[sink.telegram]`: `bot_token` and `chat_id`. Instead of writing the token into the file, you
  can set `MIKO_POST_TELEGRAM_BOT_TOKEN`, which takes precedence. The token is never logged or
  printed. Spaces or a newline around the token are trimmed, from the file and from the
  environment variable alike. After that, the token must be printable ASCII with no spaces: one
  with whitespace inside it, an invisible character such as a zero-width space, or a non-ASCII
  character such as a smart quote is refused at startup, even with the chat destination
  disabled, so copy it again from @BotFather (or remove a placeholder, if you do not use Telegram).
  Pasted straight quotes or a `bot` prefix are not caught this way: Telegram refuses such a token
  when you post, and until you fix it, the token pasted into a message is not hidden in the
  diagnostic log.
- `[sink.obsidian]`: `daily_note_dir`, the absolute path of your daily-notes folder.

Set `enabled = false` on a destination you do not use. At least one must be enabled. Unknown keys
and invalid values are refused at startup with a message saying what to fix, and nothing is
posted. The full schema is in
[contracts/config-schema.md](specs/001-dual-sink-quick-post/contracts/config-schema.md).

## Use

```bash
mp hello world              # posts "hello world" to every enabled destination
mp -c ~/other.toml hello    # uses another settings file for this post
mp                          # opens the window
mp --help                   # shows usage and the resolved settings path
```

Arguments are joined with single spaces. A message that is empty or only whitespace is refused
before anything is sent. `-c` applies to command-line posts only; `mp -c PATH` with no message is
an error, not a window.

The exit status is `0` only when every enabled destination succeeded, and `1` otherwise. Each
destination's result is printed, with a short reason for any that failed.

### The window

| Key | Action |
|---|---|
| `Cmd+Enter` | send |
| `Shift+Enter` | new line (a plain `Enter` does nothing, so it cannot disturb IME conversion) |
| `Esc` | close without posting |
| `Cmd+Q` | quit |

Send is disabled while a post is in flight. After a post in which every destination succeeded,
Send and Cancel give way to a single Quit button and the message cannot be sent again; after a
failure Send stays so you can retry. The window closes on its own, after 15
seconds on success or 30 seconds on failure (`[gui]` settings). Interacting with it first cancels
the automatic close. An optional faint background image is described in
[docs/USER_GUIDE.md](docs/USER_GUIDE.md).

### What each destination receives

- **Telegram** gets the text unchanged, sent as MarkdownV2. If Telegram rejects the formatting,
  `mp` makes exactly one plain-text retry, and a successful retry counts as success. Nothing
  else is retried.
- **Obsidian** gets one line appended to today's note, prefixed with the local time. Line breaks
  in the message become `<br>`, so a multi-line message stays one line. Existing content is never
  rewritten. The note is created if it is missing, unless `create_if_missing = false`.

## Diagnostic log

Each post is recorded as JSON Lines, by default at `$XDG_STATE_HOME/miko-post/app.jsonl` (or
`~/.local/state/miko-post/app.jsonl`). All records for one post share a `message_id`. By default
the message text is recorded only when a destination failed, so a lost message can be recovered
from the log and re-sent. The file rotates at 10 MiB or 7 days, whichever comes first. Rotated
files keep a local timestamp suffix, such as `app.jsonl.20260827114203`, and are never deleted.

If the log cannot be written, the post still happens, and `mp` prints one warning saying so.

## Documentation

- [docs/design.md](docs/design.md): the design, including the acceptance criteria.
  A Japanese version is at [docs/design.ja.md](docs/design.ja.md).
- [specs/001-dual-sink-quick-post/](specs/001-dual-sink-quick-post/): the specification, the
  plan, the contracts, and the [acceptance matrix](specs/001-dual-sink-quick-post/acceptance-matrix.md).
- [docs/DEVELOPER_GUIDE.md](docs/DEVELOPER_GUIDE.md): notes for working on the code. Run
  `make check` (format, vet, race-enabled tests) before sending a change.
