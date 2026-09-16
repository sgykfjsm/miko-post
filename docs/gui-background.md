# Issue 122 — subtle GUI background

Requested separately from Batch 9 on 2026-09-11. The original issue deferred this
from Batch 7; this request authorizes implementation now.

The optional `[gui].background_image_dir` is an absolute directory path. Empty
means no image. Each GUI launch randomizes the top-level regular `.png`, `.jpg`,
and `.jpeg` candidates (case insensitive), then uses the first decodable image.
Subdirectories and symbolic-link entries are skipped. Unsupported, corrupt,
unreadable, oversized, or missing images fall back to another candidate, or black
when none is usable. Limits: 8 MiB encoded and 16 million pixels.

Images preserve their aspect ratio and fit inside the window, with black bars if
needed. Fixed 12% opacity over black keeps text readable. The editor background is
transparent; widgets use dark-theme foregrounds. The selected image stays fixed
through editing and result display. No opacity setting or image posting is added.

Configuration example:

```toml
[gui]
background_image_dir = "/Users/you/Pictures/miko-backgrounds"
```

Verification covers selection, both formats, fallback, limits, no recursion,
layering, editor interaction, full tests, build, and native GUI readability.
