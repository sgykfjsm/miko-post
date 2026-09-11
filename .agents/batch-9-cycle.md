# Batch 9 — formatting rescue

Selected T059–T064 / #60–#65, the next planned US4 slice. Batch 8 merged as
48fdd6a on 2026-09-11; that commit is this branch's base. Existing settings validation
already satisfies the reserved-key validation portion of T064; delivery tests prove
both keys remain inert. No pre-implementation non-code closure is justified.

Scope: a trusted HTTP 400 / ok:false / error_code:400 response containing the exact
substring `can't parse entities` triggers one plain-text attempt. It omits parse_mode,
keeps text/chat/thread unchanged, and shares the original context deadline. All other
failures fail closed. Failed rescue retains both errors; final-attempt classification
and HTTP status control the overall failure. Per-call diagnostic reports preserve
correlation, stage ordering, duration, failure details and credential redaction.

Required support: optional formatting recorder in post and its app adapter. No core
import of config/logging/sinks; no shared mutable state on the Telegram sink.

Validation: predicate matrix; real local HTTP wire/outcome/reserved-key/deadline tests;
real JSONL correlation/order/fields/redaction tests; existing sink-independence tests;
make check and native build. No live Telegram validation claimed.

Issue #122 is explicitly requested separately and is excluded from this review target.
Batches 10–12, log message capture and GUI changes are excluded.

No tasks are marked complete before review. No issues closed. PR content is prepared
locally; no commit, push or remote PR is part of the review/fix step.
