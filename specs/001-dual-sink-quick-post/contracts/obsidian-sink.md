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

- Open with `O_APPEND|O_RDWR`, plus `O_NOFOLLOW` **where the platform provides it**, plus
  `O_CREATE` **only** when `create_if_missing` is true. Where the platform does not, the `Lstat`
  check under "Symlinked note path" below is the whole of the refusal.
- Never `O_TRUNC`; never read-modify-write. Existing content is never rewritten, truncated, or
  reordered — including when the existing file does not end in a line break (SC-009).
- UTF-8 encoding, LF line endings (FR-050).

`O_RDWR` rather than `O_WRONLY` because the separator rule below needs the note's last byte. One
byte is read at an explicit offset; nothing is rewritten, and `O_APPEND` still forces every write
to the end. Amended in Batch 6a.

This costs one case that previously worked, and the cost is accepted. A note whose mode denies
reading — `0200`, or a restrictive ACL, as a user, a sync client or a backup tool may leave it —
opened under `O_WRONLY` and now fails with `permission denied`. The alternative, retrying with
`O_WRONLY` when the read is refused, would give this sink a second write path that cannot read its
own note: the separator rule would then hold on some notes and not others, with nothing in the
output to say which. This failure is loud, names the file, and one `chmod` clears it.

### Symlinked note path — refused (FR-044, amended in Batch 6a)

A note path that is itself a symbolic link is **refused**, not followed. Following one let anything
with write access to the vault redirect the append outside it: into an existing file, or — with a
dangling link and `O_CREATE` — into a file this sink then created wherever the link pointed. A link
to `/dev/null` was worse than either, reporting the post as delivered while storing nothing.

The precondition is write access to the vault, which already permits deleting the notes outright,
so this is not a privilege boundary. It is a scope one: this sink's promise is that it appends to a
note *inside* the configured vault, and a symlink breaks that promise without any setting saying so.

`O_NOFOLLOW` makes the refusal atomic where the platform has it; an `Lstat` check supplies the
message and covers platforms that do not.

### Non-regular note path — refused (FR-044, amended in Batch 6a)

A note path that is not a **regular file** — a named pipe, a socket, a device node — is refused
before anything is written, with its own error distinct from the symlink one.

A named pipe is the case that requires this, and it reaches the `/dev/null` harm above with no
symlink involved at all. Under `O_WRONLY` a FIFO at the note path blocked inside `open(2)` until a
reader attached, so the post failed loudly on the orchestrator's timeout — an accidental guard that
`O_RDWR` removed. `O_RDWR` returns immediately, because the process holds the read end itself: the
entry goes into the pipe buffer, `close` discards it, and the post is reported as delivered while
the user's text is gone.

The check is an `fstat` on the descriptor `os.OpenFile` returned, not a second look at the path. It
describes the object actually opened and so cannot be raced, whereas another path-based check would
open a second TOCTOU window beside the one `O_NOFOLLOW` exists to close.

A descriptor whose `fstat` **fails** is refused on the same branch, with the same error. The
question being asked is whether this is a regular file, and a descriptor that cannot answer it is
not one this sink will write into; a separate error for a case that needs `EBADF` or a failing
filesystem to reach would be a branch no test could honestly exercise.

That arm wraps the underlying errno alongside the sentinel (amended in Batch 6a). `ESTALE` from a
revoked network mount and `EIO` from failing media arrive on notes that are perfectly ordinary
regular files, so a refusal reporting only "the note path is not a regular file" would send the
user looking for a named pipe that is not there — the same class of misdiagnosis `ErrVaultMissing`
exists to prevent.

Deliberately stricter than `internal/logging`, which permits a device node at the log path: there
`logging.path` has no companion "disabled" setting, so `/dev/null` is how a user turns diagnostics
off. This sink is already gated by `obsidian.enabled` and has no "discard my notes" reading, so the
test is a plain `IsRegular`.

### What the refusals cover — and what they do not (Batch 6a, DEC-B2-RESIDUAL)

Both refusals are about the **leaf path**, and both are decided at the open: the note path itself
is neither a symbolic link nor a non-regular file. Three things that defeat the same "the append
lands in the note the user is looking at" promise are accepted and not checked:

- A **hardlink** at the note path. It redirects the append to content outside the vault exactly as
  a symlink does, and it is a regular file, so neither check can see it. Refusing it means testing
  `Nlink > 1`, which vault snapshot, backup and dedup tools trip legitimately, and it needs a
  second build-tag pair for the platforms that cannot report link counts.
- A vault reached **through a symlinked parent directory**. `O_NOFOLLOW` constrains the final
  component only, and a symlinked vault directory is an ordinary way to keep a vault on another
  volume.
- The note's inode **replaced or unlinked after the open** (recorded in Batch 6a). Every check
  above is spent by the time the descriptor exists; from then on the sink holds an inode and the
  path it came from belongs to whoever writes the directory next. An external writer that saves the
  note as `write tmp; rename(tmp, note)` — Obsidian's own save, Obsidian Sync, iCloud Drive,
  Dropbox, Syncthing, `git checkout`, most editors — or that simply unlinks it, in the window
  between the open returning and the write completing, leaves the entry in the orphaned inode.
  Measured: the append reports success and the note now at the path does not contain it.

So the promise a successful append makes is precise, and narrower than the first two bullets alone
suggest: the bytes reached **the inode that was opened**, not necessarily the file that is at the
note path when the result is reported. The third residual is the one that reaches
delivered-but-absent — the outcome class the refusals above exist to prevent — with no symlink,
named pipe or hardlink anywhere in it, and it arrives from ordinary software the user installed on
purpose rather than from an attacker. DEC-B1's separator rule widens its window slightly, by
putting an `fstat` and a one-byte `ReadAt` between the open and the write where `O_WRONLY` had
nothing.

Detection is **not** attempted and is a separate product decision: the obvious test — `Nlink == 0`
on the descriptor after the write — needs a second build-tag pair for platforms that cannot report
link counts, cannot tell a sync client from the user deleting their own note, and converts a
microsecond race into a new failure mode of its own.

The first two need write access to the vault, which already permits deleting the notes outright.
The contract promises what it keeps rather than the stronger claim it cannot enforce.

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

The last byte is read before the write and not re-read, so a concurrent appender can invalidate it.
The cost is a redundant blank line when the foreign append ends in `\n`, or — when it does not, on
a note whose last byte did — **one glued line**, the outcome this rule exists to prevent, reached
through the window the rule's own read opens. Measured; recorded rather than fixed, because the
alternative is locking a file in the user's vault. Two of this sink's own senders cannot produce it:
every entry it writes ends in `\n`, so a stale reading is only ever stale in the harmless
direction. It takes a foreign appender that leaves a line open.

## Durability

A successful append means the bytes reached the kernel, not the disk. There is no `fsync`, so a
power loss can lose the last entry. FR-049 requires that existing content survive, which it does;
it does not require each entry to be durable before the command exits.

Power loss is one of the two ways a reported append can be absent from the note the user opens
afterwards. The other is the inode replacement recorded under "What the refusals cover" above,
where the bytes are as durable as any other write and the file holding them is simply no longer the
note. Neither is detected, and the two sections state the same promise from different ends: the
bytes reached the kernel, for the inode that was opened.

## Missing note (FR-046)

FR-046's "does not exist" means **the path is free** — nothing is at it — not merely that it does
not resolve. A dangling symbolic link does not resolve and the path is still occupied, so it is a
refusal and not a missing note. Where creation and a refusal both apply, **the refusal wins and
nothing is created**.

| At the note path | `create_if_missing` | Result |
|---|---|---|
| nothing | `true` | created, then appended — success |
| nothing | `false` | not created — **sink fails** with `ErrNoteMissing`, other sinks unaffected |
| a regular file | either | appended — success |
| a symbolic link, live **or dangling** | either | **refused** with `ErrNoteIsSymlink`; never followed, never created |
| a non-regular object (named pipe, socket, device node) | either | **refused** with `ErrNoteNotRegular`; nothing is written |
| the **vault directory** is absent | either | **sink fails** with `ErrVaultMissing`, never `ErrNoteMissing` |

The dangling-link row is the one that would otherwise contradict the first: with
`create_if_missing: true` this sink does **not** create the file the link points at. The two
refusal rows are the sections above, restated here because a reader who comes for FR-046's answer
should not have to have read them.

A missing **vault directory** is a different failure and must be reported as itself, not as the
`create_if_missing` refusal (amended in Batch 6a). `os.OpenFile` returns the same `ENOENT` either
way, and the two want opposite answers: one is the configuration behaving as asked, the other is a
vault that is not there. Reporting the second as the first tells the user to flip a setting that
cannot help — this sink does not create directories. `config.Validate` requires `daily_note_dir` to
be absolute but cannot require it to exist, since an external drive or a synced folder may
legitimately be absent when settings load and present when a post is made.
