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

- Open with `O_APPEND|O_WRONLY`, plus `O_CREATE` **only** when `create_if_missing` is true.
- Never `O_TRUNC`; never read-modify-write. Existing content is never rewritten, truncated, or
  reordered — including when the existing file does not end in a line break (SC-009).
- UTF-8 encoding, LF line endings (FR-050).

## Missing note (FR-046)

| `create_if_missing` | Note absent | Result |
|---|---|---|
| `true` | created | success |
| `false` | not created | **sink fails**, other sinks unaffected |
