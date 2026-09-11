# User guide

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
```

Each window selects one usable top-level PNG/JPEG image and displays it faintly over
black. An empty setting disables the image. Missing, unreadable, corrupt or oversized
images fall back to another candidate or black; they do not prevent posting.
See [background behavior and limits](gui-background.md) for details.

The [design document](design.md) describes the CLI, sink settings and exit behavior.
