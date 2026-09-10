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

// ErrHelpNotAvailable is returned for -h and --help.
//
// Help output is T080's, in batch 11: contracts/cli-interface.md specifies the
// exact text, including the settings path resolved for the current environment,
// and half of that is worse than none — a user shown an incomplete help page
// has no way to tell it is incomplete. So the flag is recognised and refused
// with a message that says so, rather than being reported as an unknown flag,
// which would read as a typo on the user's part.
//
// flag.ErrHelp is wrapped rather than replaced so the cause survives errors.Is
// for anything that wants to distinguish "asked for help" from "got the command
// line wrong", and so T080's implementation has a single value to delete.
//
// Exit status: this returns through Parse's error, so the process exits 1 today
// where the contract requires 0. That is a known gap, recorded here rather than
// papered over with a partial help page.
var ErrHelpNotAvailable = fmt.Errorf(
	"%w: help output is not implemented yet; see specs/001-dual-sink-quick-post/contracts/cli-interface.md",
	flag.ErrHelp)

// Mode is what one invocation asks the binary to do (FR-002, FR-003).
type Mode int

const (
	// ModePost posts the message given on the command line.
	ModePost Mode = iota

	// ModeWindow opens the window, which is what no message arguments means.
	ModeWindow
)

// String makes a failed comparison in a test readable, and a Mode printed
// anywhere else honest about which value it holds.
func (m Mode) String() string {
	switch m {
	case ModePost:
		return "post"
	case ModeWindow:
		return "window"
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
	// FR-006 says supplying it without a message is an error that must not open
	// the window. That is T079, in batch 11. Until it lands, `mp -c x.toml`
	// opens the window on the default settings, which satisfies FR-005 but not
	// FR-006. T079 needs to know the flag was supplied, so it will have to add a
	// field here; it is deliberately not added in advance, because a field
	// nothing reads is a field nobody maintains.
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
		if errors.Is(err, flag.ErrHelp) {
			return Invocation{}, ErrHelpNotAvailable
		}

		return Invocation{}, err
	}

	arguments := flags.Args()

	if len(arguments) == 0 {
		// FR-002: no message arguments opens the window, on the default
		// resolved settings path. ConfigPath is deliberately dropped; see the
		// field comment.
		return Invocation{Mode: ModeWindow}, nil
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
