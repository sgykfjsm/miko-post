## Summary
The diagnostic log now rotates losslessly. The active file's creation time and size are
captured when it is opened, both of FR-072's conditions are evaluated before every write,
and rotation renames the active log with a local-time `YYYYMMDDhhmmss` suffix before
opening a fresh one. Nothing is ever deleted, expired, compressed or overwritten: a
colliding rotated name takes `-1`, `-2`, … and the name is claimed with `O_CREATE|O_EXCL`
so two racing rotations cannot be handed the same one.

## Batch
Batch 10a / US5: T065, T068–T070, issues #66, #69–#71. Based on merged Batch 9 (#125).
10b (T066–T067, T071–T074: message capture, traces, the degradation warning) stays out.

## Decisions
- **DEC-F1** — rotation is built into `logging.Open` rather than supplied through
  `Options.Writer`, which batch 4 had anticipated. The writer owns the file's whole
  lifecycle either way, but inside the package it reuses `openLogFile`, so the
  non-regular-file refusal, `O_APPEND`, FR-075's directory creation and FR-076's
  degradation reporting are discharged once for every handle — the first and every one
  after a rotation — instead of being re-implemented by each front door. `Options.Writer`
  keeps its meaning and now documents that a supplied writer is never rotated.
- **DEC-F2** — the rotated name is claimed by creating it `O_CREATE|O_EXCL`, not by
  asking whether it exists and then renaming onto it. Stat-then-rename is a
  check-then-act that silently overwrites a rotated log when it loses the race, which is
  what FR-074 and constitution principle VI forbid.
- **DEC-F3** — `rotate_size_mib` and `rotate_after_days` are clamped where they are
  converted, in `internal/logging`. `config.Validate` bounds both keys from below only, and
  an unclamped conversion wraps negative — which does not disable rotation, it rotates on
  every single write. This follows DEC-C3's precedent; the user-visible upper bound in
  `internal/config` remains issue #101's territory.

## Validation
`make check` (gofmt, vet, full `-race` suite) and a native darwin/arm64 build.
`internal/logging` is at 99.5% statement coverage against a 99.3% baseline, with the only
uncovered blocks pre-existing. 28 mutants were built across the new guards — both
thresholds and their off-by-ones, each disable-guard, `O_EXCL`, the collision numbering,
the suffix layout and its time zone, both clamps, the placeholder cleanup, the archived
handle's close, the birthtime lookup and both its fallbacks, and each of the four wiring
sites from settings to writer — and all 28 were killed. Three needed a new test first:
a descriptor-leak guard, an age-triggered rotation through `Open`, and the bounded
collision search.

Not claimed: Intel Mac, a non-darwin platform, and a real multi-day-old log (age is driven
by a clock seam in unit tests and by backdating the file end to end).

## Scope
No change to the event vocabulary, redaction, the degradation contract, settings schema,
sink behaviour or exit status. Issues stay open until this is reviewed and merged.
