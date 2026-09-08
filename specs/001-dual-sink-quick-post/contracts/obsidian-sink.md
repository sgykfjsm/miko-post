# Contract: Obsidian daily-note sink

**Requirements**: FR-044 – FR-051. **Design**: `docs/design.md` §8.

## Target path (FR-044, FR-051)

```text
<daily_note_dir>/<time.Now().Format(filename_format)>
```

All date and time rendering uses **local time** (FR-051).

## Scope limit (FR-045)

The sink appends one physical line to the **end** of the note. It MUST NOT search for, create, or
insert into a named section.

## Transformation (FR-047) — order is normative

1. Normalize `\r\n` → `\n`, then bare `\r` → `\n`
2. Replace every remaining `\n` with the literal text `<br>`
3. Prefix with `- ` + `time.Now().Format(time_format)` + one space
4. Write the result followed by **exactly one** `\n` (FR-048)

Step 1 must precede step 2 — otherwise a `\r\n` pasted from another application produces
`<br><br>`. The same text therefore yields the same stored line regardless of where it was copied
from.

### Examples

Single line:
```markdown
- 11:42 今日も美琴が可愛い♡
```

Multi-line input becomes **one physical line** (SC-010):
```markdown
- 11:42 今日も美琴が可愛い♡<br>美琴愛してるよ💋<br>黒子も操祈も佐天さんもラブラブチュッチュッ😘
```

## Write rule (FR-049, FR-050, constitution principle VI)

- Open with `O_APPEND|O_RDWR|O_NOFOLLOW`, plus `O_CREATE` **only** when `create_if_missing` is
  true.
- Never `O_TRUNC`; never read-modify-write. Existing content is never rewritten, truncated, or
  reordered — including when the existing file does not end in a line break (SC-009).
- UTF-8 encoding, LF line endings (FR-050).

`O_RDWR` rather than `O_WRONLY` because the separator rule below needs the note's last byte. One
byte is read at an explicit offset; nothing is rewritten, and `O_APPEND` still forces every write
to the end. Amended in Batch 6a.

### Symlinked note path — refused (amended in Batch 6a)

A note path that is itself a symbolic link is **refused**, not followed. Following one let anything
with write access to the vault redirect the append outside it: into an existing file, or — with a
dangling link and `O_CREATE` — into a file this sink then created wherever the link pointed. A link
to `/dev/null` was worse than either, reporting the post as delivered while storing nothing.

The precondition is write access to the vault, which already permits deleting the notes outright,
so this is not a privilege boundary. It is a scope one: this sink's promise is that it appends to a
note *inside* the configured vault, and a symlink breaks that promise without any setting saying so.

`O_NOFOLLOW` makes the refusal atomic where the platform has it; an `Lstat` check supplies the
message and covers platforms that do not.

### Separator — an entry always starts its own physical line (amended in Batch 6a)

When the note already has content and its last byte is **not** `\n`, one `\n` is written before the
entry.

Without it, an entry appended to an unterminated note continues the user's last line —
`- 09:15 an earlier thought- 11:42 a new thought` — so their own sentence looks edited and the new
capture is not a bullet. Notes saved by editors that omit a trailing newline are ordinary, and
after a torn write (a killed process, `ENOSPC`, a revoked mount) the note is *guaranteed* to end
mid-entry, so every later post would chain onto that fragment for as long as the file stood.

A failure to determine the last byte adds no separator and does not fail the post: a possibly
redundant blank line is a worse outcome than an occasional glued one, and both are far better than
not appending at all.

## Durability

A successful append means the bytes reached the kernel, not the disk. There is no `fsync`, so a
power loss can lose the last entry. FR-049 requires that existing content survive, which it does;
it does not require each entry to be durable before the command exits.

## Missing note (FR-046)

| `create_if_missing` | Note absent | Result |
|---|---|---|
| `true` | created | success |
| `false` | not created | **sink fails**, other sinks unaffected |

A missing **vault directory** is a different failure and must be reported as itself, not as the
`create_if_missing` refusal (amended in Batch 6a). `os.OpenFile` returns the same `ENOENT` either
way, and the two want opposite answers: one is the configuration behaving as asked, the other is a
vault that is not there. Reporting the second as the first tells the user to flip a setting that
cannot help — this sink does not create directories. `config.Validate` requires `daily_note_dir` to
be absolute but cannot require it to exist, since an external drive or a synced folder may
legitimately be absent when settings load and present when a post is made.
