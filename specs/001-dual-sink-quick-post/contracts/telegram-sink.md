# Contract: Telegram sink

**Requirements**: FR-031 – FR-043. **Design**: `docs/design.md` §7. **Research**: R-008.

## Request

```text
POST {baseURL}/bot{token}/sendMessage
```

`baseURL` defaults to `https://api.telegram.org` and is overridable in tests only (R-008).

| Field | Source | Note |
|---|---|---|
| `chat_id` | `sink.telegram.chat_id` | FR-031 |
| `text` | the **original, untrimmed** message | FR-012, FR-033 |
| `parse_mode` | `MarkdownV2` on attempt 1; **omitted** on the rescue | FR-033, FR-035 |
| `message_thread_id` | `sink.telegram.thread_id` | Sent only when configured; omitted otherwise (FR-032) |

### Body encoding — form, not JSON (amended in Batch 6b, DEC-C1)

The body is `application/x-www-form-urlencoded`. Telegram accepts either encoding, so this is a
decision rather than a default: **JSON cannot carry the user's bytes unchanged, and this sink is not
allowed to change them** (FR-011, FR-012, FR-033).

`encoding/json` substitutes U+FFFD for any byte sequence that is not valid UTF-8 and returns a
**nil error** while doing it — `json.Marshal(map[string]string{"text": "a\xffb"})` yields
`{"text":"a\ufffdb"}` and no error at all. On Unix a command-line argument may carry such bytes,
and they really do reach a destination: the obsidian sink writes them verbatim. A JSON body would
therefore leave Telegram holding U+FFFD where the note holds the raw `0xFF` — the two sinks storing
different text for one post, which is issue #113's option 3 and was rejected in advance as exactly
the divergence constitution principle II exists to prevent. Arriving at a rejected outcome by
accident, through a library's silent substitution, is the failure this encoding rules out.

`url.Values.Encode` percent-encodes byte by byte instead, so `0xFF` travels as `%FF` and arrives as
`0xFF`. What Telegram's server then does with a non-UTF-8 sequence is **outside our control and is
not part of this claim**. The claim is narrower, and is the only one a sink can honour: we do not
rewrite the user's bytes on the way out.

#### Settled in Batch 6c-1: the form encoding is **kept**, as documented defence in depth

Issue #104 (of which #113 is the duplicate) has been answered, and it was answered the way this
section anticipated: `Message.Validate` now rejects invalid UTF-8 (decision DEC-D4), so no front
door delivers those bytes to this sink any more.

The encoding stays, deliberately rather than by omission, and the reason is not sentiment about the
work already done. `Send` takes a `post.Message`, and a `post.Message` is a struct literal that any
caller can build; nothing in the type system says a value reaching this sink has been through
`Validate`. A sink that assumed validation had run would be trusting a caller it cannot see, and the
cost of not trusting is zero — form encoding is not slower, not longer, and not harder to read than
a JSON body. Removing it would buy nothing and would put the U+FFFD substitution back one refactor
away, in a package whose worst outcome class is "reported delivered, arrived different".

It is also the guard that is already in place if DEC-D4 is ever reversed. #104 records exactly what
would reverse it — chiefly, confirmation that the Bot API accepts `%FF` rather than answering `400`,
which was never checked against the live service.

The byte-exactness assertions for invalid sequences are kept alongside it, in
`internal/sink/telegram/sink_test.go`, and construct their `Message` through a helper that asserts
`Validate` really would refuse the bytes — so a row that stopped being a bypass fails rather than
quietly becoming a duplicate of the ordinary case.

### Request timeout — the in-package clamp is **kept** (DEC-C3, issue #114)

`sink.telegram.http_timeout_seconds` is now bounded above as well as below by
`config.Validate` (see `contracts/config-schema.md`, **Timeout bounds**), so a settings document
carrying `18446744074` is refused at load time with a message the user can act on. That is #114's
real fix and it has landed.

`requestTimeout`'s clamp inside this package is **kept**, and #114's acceptance requires that choice
to be made deliberately. The reasoning is the same as for the form encoding above and rests on the
same fact: `New` is exported and does not require validated settings — this package's own tests
construct a `config.TelegramSettings` directly, and so may any future caller. The floor is the part
that would actually be missed: `http.Client` reads `Timeout: 0` as *unbounded*, so a zero-valued
settings struct without the floor produces a request with no limit at all rather than FR-040's 30
seconds, which is a worse failure than the one the ceiling prevents.

What changed is the clamp's status, and the comment on `maxRequestTimeoutSeconds` says so: it is no
longer an interim measure standing in for a validation fix that had not been written. It is the
second of two independent guards, and the validation layer is the one that talks to the user.
Neither guard's test may be deleted on the grounds that the other exists.

## Verbatim rule (FR-033)

The first attempt sends the message **verbatim**. The application MUST NOT escape, transform, or
normalize characters that MarkdownV2 reserves. Consequently everyday punctuation (`.`, `-`, `!`,
`(`) routinely causes the first attempt to be rejected and the rescue to succeed. **This is
expected steady-state behavior, not a defect**, and the resulting log pattern must not be treated
as an error condition.

## Rescue trigger (FR-035, FR-038) — normative predicate

Exactly one unformatted retry runs **if and only if**:

```text
response.ok == false
AND response.error_code == 400
AND response.description contains "can't parse entities"
```

Any other failure — transport error, timeout, 401, 403, 429, 5xx, or a 400 with a different
description — makes **no second attempt of any kind** (FR-038, FR-041). Fail closed.

## Outcomes

| Situation | Sink result | Exit impact |
|---|---|---|
| Attempt 1 succeeds | success | none |
| Attempt 1 rejected for formatting, rescue succeeds | **success** (FR-036) | **none** (FR-061) |
| Attempt 1 rejected for formatting, rescue fails | failure; **both** attempts retained in the log (FR-037) | `1` |
| Any non-formatting failure | failure, no retry (FR-038, FR-041) | `1` |

## Timeouts

| Bound | Default | Requirement |
|---|---|---|
| One HTTP request | 30 s | FR-040 |
| The whole sink invocation, covering both attempts | 60 s | FR-015 |

## Credential (FR-042, FR-043)

`MIKO_POST_TELEGRAM_BOT_TOKEN` takes precedence over `bot_token`. The token MUST NEVER appear in
logs, on-screen errors, CLI errors, or settings dumps — including inside a URL echoed in an error
message, which is the most likely accidental leak given the token sits in the request path.

### What the sink does about it (amended in Batch 6b, DEC-C2)

**No error leaving `Send` carries the bot token.** Every `*url.Error` layer is stripped from a
transport failure **before** `fmt.Errorf` wraps it, and that order is load-bearing: `fmt.Errorf`
bakes the wrapped error's rendering into its own message, so wrapping first would copy the URL into
a string no later unwrapping could reach.

The stripping is **structural, not textual**. `%#v` on a `*url.Error` renders its exported `URL`
field whatever `Error()` says, so rewriting the message would leave the credential one verb away.
Dropping the wrapper keeps the cause — which carries host and port but never the path — so
`errors.Is(err, context.DeadlineExceeded)` still answers and a timeout is still reported as one.

Behind that stands a **textual net**: an error whose rendering still contains the token is returned
with the token replaced by the marker `config.Secret` prints, cause intact. It exists because the
claim is "no error out of `Send` carries the credential", and that claim must not rest on this
package having enumerated every error shape `net/http` can produce.

That net reads **two** renderings, not one. `%v`, `%s`, `%q` and `%+v` all route through `Error()`;
`%#v` does not, and reflects the exported fields whatever `Error()` chose to say. Scanning the
reflected form as well as the message is what makes the guarantee cover the surfaces this document
states it over.

The request URL is also **not the only carrier**. `APIError.Description` is copied from the response
body, and **the body is not necessarily Telegram's** — the proxy and captive portal this contract
already accounts for can quote the request they could not forward, credential and all.

That carrier is closed at the **value**, not at the rendering, and the distinction is the whole
point. Redacting an error's message leaves the exported field intact one `errors.As` away, and
`errors.As` is precisely what this contract tells T040 to call to reach `HTTPStatus` — so the
supported route to the log field was also the route to the credential. `decodeResponse` therefore
removes the token from any body-derived string **before the `*APIError` is constructed**. It is
scrubbed, not blanked: Batch 9's T061 predicate reads `Description` for `can't parse entities`, so
the credential comes out of the description rather than the description out of the error. **No
`*APIError` leaving this package holds the token in any field.**

**The sink boundary is the only layer that exists today.** `Options.Redact` cannot help: nothing on
this path constructs a logger, and the error is a value handed upward rather than a line written
down — `internal/post/result.go` deliberately leaves `SinkResult.Err` unredacted for whatever
assembles the log record. Issues #103 and #115 cover the general case; #115 is specifically about
this invariant now being load-bearing and enforced nowhere else. A sentinel-token sweep asserts
absence for every failure class across `Error()`, `%v`, `%+v` and `%#v`, and then walks the chain
with `errors.As` for both typed carriers — `*url.Error` must be gone, `*APIError` must survive with
its `Description` and its own `%v` and `%#v` clean — so the guarantee is checked rather than
described.

## No transport retry (FR-041)

v0.1 performs no automatic retry for transport errors, timeouts, HTTP status codes, or Telegram
API errors. The formatting fallback is a **format** fallback, not a transport retry
(constitution, Additional Constraints).

### Redirects are not followed (amended in Batch 6b, DEC-C4)

The HTTP client sets an explicit redirect policy: a **3xx is returned as a response, never
followed**. Leaving Go's default in place is not neutral — it follows up to ten hops — and both
things that follow are disqualifying:

- A **302** is reissued as a GET with no body. The user's message is never sent, and the redirect
  target's reply is then decoded as Telegram's, so `Send` reports **success**. That is a post
  reported delivered with nothing delivered, and it needs no involvement from Telegram at all,
  only something positioned to answer for the origin.
- A **307** or **308** replays the method and the body. One `Send` becomes several wire requests,
  each carrying the user's message — a re-send FR-041 and FR-019 forbid and the sink never chose.
- Either way, `net/http` sets a `Referer` on the cross-origin hop. It strips userinfo but **keeps
  the path**, and the path is `/bot{token}/sendMessage`, so following a redirect hands the
  credential to a third-party host. That is FR-043 broken on the wire, where no error-value guard
  can reach it.

The 3xx is handed back through `http.ErrUseLastResponse` rather than a bespoke error, so it is
judged by the same decode path as every other status and fails closed there, carrying its status
for the `http_status` log field. Which failure arm it lands on depends on the redirect's body: a
bodiless or HTML 3xx fails as an unreadable reply, while a 3xx whose body parses as `{"ok":true,…}`
is a body claiming success under a non-success status and therefore carries `errContradictoryStatus`
— which T061 must account for, since it is specified to exclude that sentinel. Asserted by counting
requests at the redirect target: zero.
