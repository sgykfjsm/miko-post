# Developer guide

## Formatting rescue

`internal/sink/telegram` owns the formatting-only predicate and two-attempt delivery.
Both attempts use the original context and shared request builder. `RescueError`
retains both sanitized errors and unwraps the final one for classification/status.

Per-call formatting reports travel through context to an optional
`post.FormattingRecorder`, then the app adapter maps them to stable JSONL events.
The per-sink record serializes attempt and terminal events and suppresses late reports.
This preserves the posting core's independence from config, logging and sink packages.

Run `make check` and `make build`. Rescue tests include a deterministic original-deadline
assertion, real HTTP request-timeout and wire checks, and production Service/JSONL
integration. Keep the negative no-retry cases when changing formatting behavior.

## GUI background

Selection and bounded decoding live in `internal/gui/background.go`; one decoded image
belongs to one window. The content stack uses black, an aspect-preserving image, and
foreground widgets under a dark theme with a transparent input background.

Limit tests use otherwise-decodable images at and above both size boundaries. Mutations
removing either limit or changing its boundary must fail those tests. Native readability
checks should use temporary XDG config/state and a temporary vault, with Telegram disabled.

See [background contract](gui-background.md) and the [configuration schema](../specs/001-dual-sink-quick-post/contracts/config-schema.md).

## Secret-leak gate

`TestSecretLeakGate` in `internal/post/secret_leak_test.go` (T084; FR-043, FR-069, SC-006) posts
through the whole of what `mp` runs — `cli.Run`, `app.LoadSettings`, the real logger, both real
sinks and the orchestrator — with a sentinel token, then asserts the sentinel is on neither user
stream, in no file the run wrote, and in none of the window's result strings. It asserts the
outcome, not the mechanism; each layer has its own unit tests. The Telegram side is answered
in-process by `fakeTelegram`, which `installFakeTelegram` swaps in as `http.DefaultTransport`; it
never dials. That holds only while the sink uses the default transport, pinned by
`TestNewLeavesTheClientOnTheDefaultTransport`.

**Hard rule: never bypass a network seam in a mutant or a test.** Removing the fake transport, or
any other seam that stands in for the network, runs the real sink against `api.telegram.org`.

## Mutation testing

Review in this repository treats a test as evidence only when a mutant shows it can fail. Coverage
is not evidence: fully covered lines have carried assertions that could not fail. For each guard,
apply the smallest mutant that should break it, confirm `go vet ./...` still passes (so the failure
is the test's, not the compiler's), run the targeted test, then restore the file and check its
`sha256` against the original before moving on. After any fix, re-mutate the neighbouring lines: a
change can make an adjacent assertion unfailable.

## Credential shape

`config.Secret` has five render guards and a pointer-held value; see
[data-model.md](../specs/001-dual-sink-quick-post/data-model.md#settings). Do not add a `Reveal()`
call site to ask a question about the token's shape. Use `Len`, `IsEmpty`, `Trimmed` and
`IsPrintableASCII`; `Reveal()` is reserved for the places that must hand the value on (the request
URL, the sink's credential net, the logger's redaction pattern).

## Window start-up seams

`gui.Run` delegates to `runWith(errOut, homeDir, newApp)`. `homeDir` stands in for
`os.UserHomeDir` and `newApp` for the Fyne constructor, so tests can drive the no-home and
non-absolute-home refusals (DEC-J1, DEC-J7) and both settings outcomes without a display, using a
constructor that stops the test (`TestNoHomeDirectoryOpensNoWindow`). The line in `Run` that
supplies the real values is covered by a native probe, not a test.

## Build stamp

`make build` and `make install` stamp `VERSION` and `COMMIT` into `internal/version` through linker
flags; unless `COMMIT` is supplied, the commit is `git rev-parse --short=12`. `make stamp` prints the values that would be used.
`version.resolve` normalizes any lowercase-hex commit longer than 12 to its 12-character prefix
(`normalizeCommit`, DEC-J2), so every path that identifies a commit records one shape; a tagged
`go install` records `unknown` (DEC-I1).
