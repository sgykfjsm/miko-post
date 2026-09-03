package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Load reads, decodes, resolves the credential for, and validates the settings
// file at path.
//
// This is the whole load sequence data-model.md describes, minus the
// all-sinks-disabled check that FR-018 assigns to the front door (T081). The
// steps run in this order for a reason: the credential is resolved after
// decoding so validation sees one resolved value, and validation runs last so
// it can report problems in a document that has already been shown to be
// well-formed.
//
// Any failure returns the zero Settings, never a partially populated one. That
// is FR-058 — no destination started with partially valid settings — expressed
// as a return convention, so a caller that ignores the error still cannot
// construct a sink from half a document. The zero value has both sinks
// disabled, so the worst a careless caller achieves is posting nowhere.
//
// A missing file is an error rather than "use the defaults". The defaults have
// every sink disabled, so silently accepting a missing file would surface as
// FR-018's all-sinks-disabled message — which points the user at their sink
// configuration when the real problem is that the file they think they edited
// is not the file being read. Naming the path that could not be read is the
// more actionable failure, and FR-058 already calls unreadable settings a
// failure.
func Load(path string) (Settings, error) {
	data, err := readFile(path)
	if err != nil {
		return Settings{}, err
	}

	settings, err := decode(data, path)
	if err != nil {
		return Settings{}, err
	}

	ResolveCredential(&settings)

	if err := settings.Validate(); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}

	return settings, nil
}

// readFile reads the settings document, distinguishing "not there" from
// "there and unreadable".
//
// The two failures need different words because they need different fixes: a
// missing file means the path is wrong or the file was never created, while a
// permission or I/O failure means the file is right and something else is
// wrong. os.ReadFile's own message says "open <path>: no such file or
// directory", which reads as an internal failure rather than as an instruction.
//
// fs.ErrNotExist is wrapped rather than replaced, so a caller that wants to
// offer to create the file (not a v0.1 behaviour) can still match on it.
func readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}

	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no settings file at %s: %w", path, err)
	}

	return nil, fmt.Errorf("read settings file %s: %w", path, err)
}

// decode applies strict TOML decoding on top of the defaults (FR-052, FR-054).
//
// Decoding into Defaults() rather than a zero Settings is what makes an absent
// key keep its default while an explicit `false` still wins; see Defaults.
//
// DisallowUnknownFields is what gives FR-054 teeth. Without it a top-level
// [telegram] table — the shape a user is most likely to try, and the shape the
// contract explicitly excludes — would decode successfully into nothing at all,
// and the user would be left with a sink that is configured in the file and
// disabled in the program with no message anywhere explaining the gap. With it,
// the same document is a load error naming the table.
func decode(data []byte, path string) (Settings, error) {
	settings := Defaults()

	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, decodeError(path, err)
	}

	return settings, nil
}

// decodeError renders a go-toml failure as a message that names where the
// problem is without reproducing what is there.
//
// This function exists entirely because of FR-043. go-toml's own presentation
// of a decode failure is toml.DecodeError.String(), which is genuinely good
// output — it prints the offending line with a caret under the offending token
// and one line of surrounding context. It is also unusable here. The context it
// prints starts at the enclosing table header, so for any problem anywhere in
// [sink.telegram] the snippet reproduces the whole table up to that point,
// bot_token included. Verified against v2.4.3: an unknown key three lines below
// bot_token still echoes the token verbatim.
//
// So position and key path are taken from the error and the source text is
// never touched. That loses the caret and keeps the credential, which is the
// right trade when the alternative writes the token into a message that
// FR-058 requires be shown to the user and FR-064 logs.
//
// Unknown keys are reported all at once. go-toml collects every one of them in
// StrictMissingError.Errors, and reporting them together matches how validation
// behaves a step later — a user fixing a settings file should not have to
// rediscover that one round trip per typo is the deal.
func decodeError(path string, err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		found := make([]string, 0, len(strict.Errors))

		for i := range strict.Errors {
			// Indexed rather than ranged by value: DecodeError carries
			// internal state and is documented as a pointer type, and Position
			// and Key are pointer methods.
			problem := &strict.Errors[i]
			row, column := problem.Position()

			found = append(found, fmt.Sprintf("%s:%d:%d: %s: %s",
				path, row, column, strings.Join(problem.Key(), "."), messageOf(problem)))
		}

		if len(found) == 1 {
			return errors.New(found[0])
		}

		return fmt.Errorf("%s: %d unknown or misplaced keys:\n  - %s",
			path, len(found), strings.Join(found, "\n  - "))
	}

	// A syntax error or a type mismatch. Both carry a position; only a type
	// mismatch carries a key. Neither message quotes a value from the
	// document, which is what makes it safe to pass through — asserted by the
	// sentinel sweep in load_test.go rather than trusted.
	var decodeErr *toml.DecodeError
	if errors.As(err, &decodeErr) {
		row, column := decodeErr.Position()

		if key := strings.Join(decodeErr.Key(), "."); key != "" {
			return fmt.Errorf("%s:%d:%d: %s: %s", path, row, column, key, messageOf(decodeErr))
		}

		return fmt.Errorf("%s:%d:%d: %s", path, row, column, messageOf(decodeErr))
	}

	return fmt.Errorf("%s: %w", path, err)
}

// messageOf strips go-toml's package prefix so a settings error does not read
// as though the settings file were Go source.
func messageOf(err error) string {
	return strings.TrimPrefix(err.Error(), "toml: ")
}
