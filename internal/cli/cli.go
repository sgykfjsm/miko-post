package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Exit statuses (FR-059, FR-060).
//
// Named constants rather than bare 0 and 1 because they are the one value this
// front door returns and the only thing cmd/mp does with the result. They are
// exported so the process-level test asserts against the same constants the
// binary computes, instead of two literals that agree today.
const (
	ExitSuccess = 0
	ExitFailure = 1
)

// The message-argument separator (FR-004).
//
// A named constant so the requirement's "exactly one ASCII space" is visible at
// the join and cannot be widened to a Fields/Join round trip, which would also
// collapse the spacing inside a single quoted argument.
const argumentSeparator = " "

// ErrConfigWithoutMessage is FR-006's error: -c/--config supplied with no
// message to post.
//
// The text is contracts/cli-interface.md's required message, verbatim, and it is
// returned from Parse rather than checked later so that the window path is never
// reached: an Invocation with ModeWindow cannot come back from a command line
// that named a settings file. FR-006 says "MUST NOT open the window", and the
// strongest form of that is the dispatch never seeing a window request.
var ErrConfigWithoutMessage = errors.New("--config is only available when posting from CLI")

// ErrEmptyConfigPath is returned for -c or --config given an empty value.
//
// Without it `mp -c "" hello` would post with the default settings, because ""
// is also how Invocation spells "no override". A user who typed -c meant to
// point somewhere else, and an unset shell variable in `mp -c "$CONF" hello` is
// the realistic way to get here; posting on the default settings instead is
// the silent half-success FR-005 is written to prevent.
var ErrEmptyConfigPath = errors.New("-c/--config needs a path to a settings file")

// Mode is what one invocation asks the binary to do (FR-002, FR-003).
type Mode int

const (
	// ModePost posts the message given on the command line.
	ModePost Mode = iota

	// ModeWindow opens the window, which is what no message arguments means.
	ModeWindow

	// ModeHelp prints help and exits 0 (FR-007).
	ModeHelp
)

// String makes a failed comparison in a test readable, and a Mode printed
// anywhere else honest about which value it holds.
func (m Mode) String() string {
	switch m {
	case ModePost:
		return "post"
	case ModeWindow:
		return "window"
	case ModeHelp:
		return "help"
	default:
		return fmt.Sprintf("Mode(%d)", int(m))
	}
}

// Invocation is one parsed command line.
//
// It is a value with no behaviour on purpose: parsing decides what was asked
// for, and doing it is somebody else's job. That is what keeps the dispatch
// decision (FR-002, FR-003) testable without a filesystem, a network or a
// window.
type Invocation struct {
	// Mode is what to do.
	Mode Mode

	// Message is the joined message text, exactly as it will be posted
	// (FR-004, FR-011). Meaningful for ModePost only.
	//
	// It is not trimmed, not normalized, and not validated here: FR-011 requires
	// every destination to receive the original text, and post.Message.Validate
	// is the single place FR-009's rule lives.
	Message string

	// ConfigPath is the -c/--config value, or "" for the resolved default.
	//
	// Always "" for ModeWindow, and that is FR-005 rather than tidiness: the
	// settings-file override applies to command-line posting only and must not
	// change what the window reads. Dropping the value at the parse boundary
	// makes that structural — the window path is handed nothing to misuse —
	// instead of a rule each future caller has to remember.
	//
	// Supplying it without a message is FR-006's error rather than a window
	// request, so the field and ModeWindow never meet: see
	// ErrConfigWithoutMessage.
	ConfigPath string
}

// Parse turns an argument vector into an Invocation (FR-002 – FR-004, FR-008).
//
// argv is the arguments *after* the program name, as os.Args[1:] gives them.
//
// # What the flag package decides, and why it is left to
//
// Go's flag package stops parsing at the first argument that is not a flag, so
// `mp hello -x` posts "hello -x" rather than rejecting -x, and `mp -- -x` posts
// "-x". Both are the behaviour a message-taking command wants, and neither is
// obtained by looking at argv by hand. An unknown flag *before* the message —
// `mp -x hello` — is still an error, which is also right: it is a typo, and
// posting the typo would be worse than refusing it.
//
// The flag set writes its own errors to io.Discard and never exits. The default
// ExitOnError would call os.Exit from inside a library, which is precisely what
// T039 forbids — there is exactly one os.Exit in this program — and would also
// make every parse failure untestable. The error text is produced here instead,
// so the message a user sees is this package's and not the flag package's.
//
// Standard input is never consulted (FR-008). There is no code here that could:
// the message comes from argv and from nowhere else, which is a stronger
// statement than a check would be.
func Parse(argv []string) (Invocation, error) {
	var configPath string

	flags := flag.NewFlagSet("mp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	// One variable behind two names, because -c and --config are the same
	// option (contracts/cli-interface.md) and two variables would let them
	// disagree. Go's flag package treats a single and a double dash
	// identically, so registering "config" also accepts --config.
	const configUsage = "path to the configuration file, for CLI posting only"

	flags.StringVar(&configPath, "c", "", configUsage)
	flags.StringVar(&configPath, "config", "", configUsage)

	if err := flags.Parse(argv); err != nil {
		// FR-007: help wins over everything else on the command line, including
		// a -c without a message, because a user asking how to use the command
		// is better served by the answer than by an error about how they used it.
		if errors.Is(err, flag.ErrHelp) {
			return Invocation{Mode: ModeHelp}, nil
		}

		return Invocation{}, err
	}

	// Whether the flag was given at all, which the value cannot say: "" is both
	// "not given" and "given empty". Visit reports only flags that were set.
	configGiven := false

	flags.Visit(func(f *flag.Flag) {
		if f.Name == "c" || f.Name == "config" {
			configGiven = true
		}
	})

	arguments := flags.Args()

	if len(arguments) == 0 {
		// FR-006: a settings file named with nothing to post is an error, and
		// the window is not opened on some other settings instead.
		if configGiven {
			return Invocation{}, ErrConfigWithoutMessage
		}

		// FR-002: no message arguments opens the window, on the default
		// resolved settings path.
		return Invocation{Mode: ModeWindow}, nil
	}

	if configGiven && configPath == "" {
		return Invocation{}, ErrEmptyConfigPath
	}

	// FR-004: exactly one ASCII space between adjacent arguments, and nothing
	// else touched. An empty argument contributes no characters and still gets
	// its separators, so `mp a "" b` is "a  b" — two spaces, because there are
	// two adjacencies. That is what the requirement says, and it is the reading
	// that keeps the rule a property of adjacency rather than of content.
	return Invocation{
		Mode:       ModePost,
		Message:    strings.Join(arguments, argumentSeparator),
		ConfigPath: configPath,
	}, nil
}
