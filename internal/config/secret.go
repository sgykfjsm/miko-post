package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// redactedMarker stands in for a credential in every render of a Secret.
//
// One constant rather than a literal per method, so the guards below cannot
// drift apart and a test can assert against the same value the code emits.
const redactedMarker = "[redacted]"

// Secret holds the Telegram bot token.
//
// FR-043 says the credential must never appear in logs, on-screen errors,
// command-line errors, or settings dumps, and FR-069 says secrets must never be
// recorded. Neither is a rule a call site can be trusted to remember: the token
// is reachable from Settings, and Settings is exactly the kind of value someone
// prints while debugging. So the guarantee is built into the type. The value is
// unexported and every route out of it except Reveal returns the marker.
//
// The four-method redaction pattern (String, GoString, MarshalJSON, LogValue)
// is what data-model.md prescribes, and it is the same pattern SinkResult uses.
// Format is the fifth, and it is not decorative — see its comment. The comment
// on post.SinkResult claims the credential type does not need Format "because
// it has no exported fields to dump"; that reasoning is wrong, and this type
// carries the correction rather than inheriting the gap.
//
// There is deliberately no MarshalText and no MarshalTOML. v0.1 never writes a
// settings file back out, and the absence means a future round-trip has to add
// the method — and decide what it should emit — instead of silently inheriting
// one that works.
type Secret struct {
	// value is a pointer, and that is load bearing rather than idiomatic
	// preference.
	//
	// Unexported is not the same as unprintable: fmt reaches unexported fields
	// through reflection when it dumps a struct, and it cannot call a method
	// on one, so no guard on this type can intercept that dump. Two verbs
	// reach it — %p on a non-pointer and %w outside fmt.Errorf — because fmt
	// answers both before it consults Formatter. Held as a string, they print
	// the credential; held as a pointer, they print an address, because fmt
	// renders a nested pointer as its address rather than following it. That
	// is the same mechanism post.SinkResult relies on to keep its Err out of
	// the equivalent dump, and it is what makes the redaction total instead of
	// total-except-two-verbs. The test sweeps the whole alphabet in both
	// cases, so a change back to a plain string fails visibly.
	//
	// A nil pointer is the empty credential, which is what the zero Secret and
	// an unconfigured settings file both produce. Every method below handles
	// it, so a zero Secret is usable rather than a panic waiting to happen.
	//
	// Copying a Secret copies the pointer, so two copies alias one string.
	// That is safe because a Secret is immutable after construction: nothing
	// writes through the pointer, and UnmarshalText installs a new one rather
	// than assigning through the old.
	value *string
}

// NewSecret wraps a credential. The plain string should not outlive the call.
func NewSecret(value string) Secret {
	return Secret{value: &value}
}

// Reveal returns the real credential.
//
// This is the single deliberate way out of the type, and it is meant to be
// called at exactly one place: building the Telegram request URL (T033). It is
// named Reveal rather than Value or String so that a call to it reads as a
// decision at the call site and stands out in review.
func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}

	return *s.value
}

// Len reports the length of the secret in bytes without exposing it.
//
// It exists so a validation rule about the credential's *shape* does not have to
// become another Reveal() call site. The count of routes to the real value is a
// review obligation in this project (see data-model.md), and "is this token long
// enough to be safe as a redaction pattern?" is a question that can be answered
// without answering "what is the token?".
func (s Secret) Len() int {
	if s.value == nil {
		return 0
	}

	return len(*s.value)
}

// IsEmpty reports whether any credential is held.
//
// Validation needs to know whether the token is present without looking at it,
// so this exists rather than having a caller compare Reveal() to "" — which
// would put the plain value in a caller's expression for no reason.
func (s Secret) IsEmpty() bool {
	return s.value == nil || *s.value == ""
}

// Trimmed returns the credential without leading or trailing whitespace
// (DEC-I2, #137).
//
// It is a method rather than strings.TrimSpace(s.Reveal()) at the call site for
// the same reason as Len: trimming is a question about the credential's shape,
// and answering it must not add a route to the real value. The receiver is left
// untouched and a new Secret is returned, because a Secret is immutable after
// construction and copies alias one string (see the value field). The empty
// credential trims to itself.
func (s Secret) Trimmed() Secret {
	if s.value == nil {
		return s
	}

	trimmed := strings.TrimSpace(*s.value)
	if trimmed == *s.value {
		return s
	}

	return NewSecret(trimmed)
}

// IsPrintableASCII reports whether every byte of the credential is printable
// ASCII other than space, 0x21 to 0x7E (DEC-J9, #137), without exposing it.
//
// It answers a question about the credential's shape, like Len and Trimmed, so
// validation can ask it without adding a route to the real value.
//
// Every genuine Bot API token is printable ASCII, so the rule refuses nothing
// legitimate, and "printable ASCII" is a generic constraint on a credential
// rather than a copy of a third party's token format, which DEC-J6 declined to
// encode. What it refuses is every corruption that survives Trimmed: interior
// whitespace, controls, format characters such as a zero-width space (U+200B)
// or a byte-order mark (U+FEFF), graphic runes that render as nothing (U+FE0F,
// U+3164, U+2800), and visible non-ASCII lookalikes such as smart quotes or an
// IME's fullwidth colon. A token carrying any of these arms a redaction pattern
// that the bare token in a message body does not contain, and the review
// reproduced that leak through each class.
//
// It checks bytes rather than runes, so a byte that is not valid UTF-8 is
// refused as itself rather than depending on how a decoder would replace it.
// The two readings refuse the same tokens today: every byte from 0x80 up
// belongs to a rune above U+007F or decodes to U+FFFD, and both are out of
// range. So a mutant that ranges over runes survives, and no test can pin the
// choice; bytes are used because they state the rule without that argument.
//
// The empty credential reports true: it contains no byte outside the range, and
// whether absence is allowed is the Enabled question validation asks
// separately.
func (s Secret) IsPrintableASCII() bool {
	if s.value == nil {
		return true
	}

	for i := 0; i < len(*s.value); i++ {
		if b := (*s.value)[i]; b < 0x21 || b > 0x7e {
			return false
		}
	}

	return true
}

// UnmarshalText lets go-toml decode a bare string key into the type.
//
// go-toml/v2 honours encoding.TextUnmarshaler, so `bot_token = "..."` lands
// here rather than needing an intermediate string field on the settings struct.
// That matters: an intermediate field would be an exported plain string on
// Settings for the lifetime of the load, which is exactly the exposure this
// type exists to remove.
//
// The pointer receiver is required for the decoder to find the method; the
// decoder is handed an addressable struct field, so this is satisfied.
func (s *Secret) UnmarshalText(text []byte) error {
	// A new string and a new pointer, never a write through the existing one:
	// see the aliasing note on the field.
	value := string(text)
	s.value = &value

	return nil
}

// String backs %v, %s and %q through Format, and every implicit
// stringification (print, log, string concatenation through fmt) directly.
//
// The receiver is a value so both Secret and *Secret are covered.
func (s Secret) String() string {
	return redactedMarker
}

// GoString backs %#v, which ignores String and would otherwise print the
// struct's fields — including the unexported one. Verified: without this
// method, %#v on a Secret renders config.Secret{value:"<the token>"}.
//
// The output is deliberately not compilable Go, as %#v normally promises. The
// point is that the credential is not reproduced, so no faithful rendering
// exists.
func (s Secret) GoString() string {
	return "config.Secret{" + redactedMarker + "}"
}

// Format is what actually closes the fmt surface, and on this type it is load
// bearing rather than defensive.
//
// fmt consults Stringer for only v, s, q, x and X, and GoStringer for %#v.
// Every other verb falls through to fmt's bad-verb path, which suppresses
// method dispatch and dumps the struct's fields through reflection. Reflection
// reads unexported fields, so with String alone `%d` on a Secret renders
// {%!d(string=<the token>)} — and the same holds for a Secret nested inside
// Settings, because fmt applies the same dispatch to each field it walks. That
// makes a single mistyped verb anywhere in the program a credential disclosure.
// fmt.Formatter outranks both Stringer and GoStringer for every verb, so
// implementing it once covers all of them.
//
// Two verbs stay out of reach even so: %p on a non-pointer operand, and %w
// outside fmt.Errorf. fmt rejects both before consulting Formatter and then
// dumps the fields with dispatch suppressed. Neither can be intercepted by any
// method, which is why the field is a pointer — the dump then yields an address
// rather than the credential. Both are covered by the leak sweep in this
// package's test. %T is answered before Formatter too, but it never reads the
// value.
//
// Width and precision are ignored. They are padding for a display string, and a
// credential is not one.
func (s Secret) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('#') {
			fmt.Fprint(f, s.GoString())

			return
		}

		fmt.Fprint(f, s.String())
	case 's':
		fmt.Fprint(f, s.String())
	case 'q':
		fmt.Fprintf(f, "%q", s.String())
	default:
		// fmt's own wrong-verb shape, so the output still reads as the mistake
		// it is, with the marker standing in for the dump fmt would produce.
		fmt.Fprintf(f, "%%!%c(config.Secret=%s)", verb, s.String())
	}
}

// MarshalJSON covers encoding/json, which consults none of the fmt interfaces.
// A value receiver means a Settings containing a Secret is covered as well as a
// bare Secret.
//
// There is deliberately no UnmarshalJSON: the marker cannot round-trip back
// into a credential, and offering the method would imply it can.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(redactedMarker)
}

// LogValue covers log/slog, which consults neither String, GoString nor
// MarshalJSON for a value implementing slog.LogValuer — LogValuer outranks all
// three. This matters most of anywhere: the destination is the rotating on-disk
// log (T023), so an unguarded render would be written down and kept.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(redactedMarker)
}
