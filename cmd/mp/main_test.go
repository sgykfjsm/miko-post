package main_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Why this file builds and runs the real binary.
//
// Every other test in this repository calls a function and inspects what it
// returned. That is the right shape for almost everything and it is structurally
// unable to check the one thing this package is for. cli.Run returns an int;
// main hands it to os.Exit. A test asserting the returned int passes unchanged
// against `os.Exit(1)` written above that call, against `os.Exit(0)`, and
// against an os.Exit that is never reached at all — the exit status is a
// property of the process, and only a process has one.
//
// Two more properties are only observable here. os.Exit runs no deferred
// function, so anything written through a writer that is flushed by a defer is
// lost at exit — and a test that injects its own unbuffered io.Writer cannot see
// that, because the buffering it is asking about is the one it replaced. And
// what actually reaches a terminal's stdout and stderr, separately, is what the
// user sees; in-process tests assert against two buffers that were never file
// descriptors.

// binary builds cmd/mp once and returns the path to it.
//
// Once rather than per test, because `go build` costs about a second and every
// case below needs the same binary. The output goes under the test binary's own
// temporary directory, which the toolchain removes.
var binary = sync.OnceValues(buildBinary)

func buildBinary() (string, error) {
	dir, err := os.MkdirTemp("", "mp-build")
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "mp")

	build := exec.Command("go", "build", "-o", path, ".")

	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, out)
	}

	return path, nil
}

// result is one run of the binary.
type result struct {
	status int
	stdout string
	stderr string
}

// run executes the binary with argv, in an environment whose home and XDG roots
// all point inside the test's temporary directory.
//
// The environment is replaced rather than extended, which matters more than it
// looks: the binary resolves a default settings path and a default log path from
// $XDG_CONFIG_HOME, $XDG_STATE_HOME and $HOME, so a test running under a real
// user's environment could read that user's settings file and append to their
// diagnostic log. MIKO_POST_TELEGRAM_BOT_TOKEN is likewise cleared, since a
// developer with one exported would otherwise change what these runs do.
//
// PATH is kept because the child is a Go binary that may need the dynamic loader
// on some platforms, and keeping it costs nothing.
func run(t *testing.T, home string, argv ...string) result {
	t.Helper()

	path, err := binary()
	if err != nil {
		t.Fatalf("build the binary: %v", err)
	}

	command := exec.Command(path, argv...)
	command.Env = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"PATH=" + os.Getenv("PATH"),
	}

	var stdout, stderr strings.Builder

	command.Stdout = &stdout
	command.Stderr = &stderr

	runErr := command.Run()

	got := result{stdout: stdout.String(), stderr: stderr.String()}

	switch {
	case runErr == nil:
		got.status = 0
	default:
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			t.Fatalf("run the binary: %v", runErr)
		}

		got.status = exit.ExitCode()

		// A signal, or an exit code the platform could not report, comes back
		// as -1. Reporting that as an ordinary failure status would let a
		// crashing binary pass a test expecting 1.
		if got.status < 0 {
			t.Fatalf("the process did not exit normally: %v\nstdout: %s\nstderr: %s",
				runErr, got.stdout, got.stderr)
		}
	}

	return got
}

// world is one run's filesystem: a home directory, a vault, and a settings file.
type world struct {
	home       string
	configPath string
	noteDir    string
	logPath    string
}

// newWorld writes a settings document with the obsidian destination enabled and
// the chat destination configured by telegram.
//
// A settings file rather than defaults, because the defaults disable both
// destinations and the point of these runs is to exercise the wired program.
func newWorld(t *testing.T, telegram string) world {
	t.Helper()

	home := t.TempDir()

	w := world{
		home:       home,
		configPath: filepath.Join(home, "config.toml"),
		noteDir:    filepath.Join(home, "notes"),
		logPath:    filepath.Join(home, "state", "miko-post", "app.jsonl"),
	}

	if err := os.MkdirAll(w.noteDir, 0o700); err != nil {
		t.Fatalf("create the vault: %v", err)
	}

	document := fmt.Sprintf(`
[sink.obsidian]
enabled = true
daily_note_dir = %q

%s

[logging]
path = %q
`, w.noteDir, telegram, w.logPath)

	if err := os.WriteFile(w.configPath, []byte(document), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}

	return w
}

// telegramDisabled is the settings fragment for a run with one destination.
const telegramDisabled = "[sink.telegram]\nenabled = false"

// telegramThatFailsBeforeTheNetwork is a chat destination that cannot reach a
// socket.
//
// "%zz" is not a valid percent escape, so url.JoinPath refuses it while the
// request URL is being assembled and the sink fails before any connection is
// attempted. That is what makes a real two-destination partial failure testable
// at all: the constitution keeps live external services out of the test suite,
// and there is no second local destination to fail instead.
//
// If this ever starts contacting the network, the run will slow down and the
// failure reason will change — both visible — rather than quietly becoming a
// live-service test.
//
// The token is long enough to clear config.MinBotTokenLength, which validation
// now enforces on every present token whether or not the sink is enabled (issue
// #117). It still carries the "%zz" that breaks url.JoinPath, so what this
// fixture tests is unchanged; a three-character token would now be refused at
// load time and the run would never reach a sink.
const telegramThatFailsBeforeTheNetwork = "[sink.telegram]\nenabled = true\n" +
	"bot_token = \"1234567890:AA-invalid-escape-%zz\"\nchat_id = \"-100123\"\n" +
	"http_timeout_seconds = 1"

// notes lists what the vault holds.
func (w world) notes(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(w.noteDir)
	if err != nil {
		t.Fatalf("read the vault: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestTheBinaryExitsZeroWhenEveryDestinationSucceeded is FR-059 as a process
// property.
//
// It is also the case that kills the mutant this whole file exists for:
// hard-coding the exit call to a failure status leaves every in-process test
// green and fails only here.
func TestTheBinaryExitsZeroWhenEveryDestinationSucceeded(t *testing.T) {
	w := newWorld(t, telegramDisabled)

	got := run(t, w.home, "-c", w.configPath, "hello", "world")

	if got.status != 0 {
		t.Fatalf("status = %d, want 0\nstdout: %s\nstderr: %s", got.status, got.stdout, got.stderr)
	}

	if got.stdout != "Obsidian: success\n" {
		t.Errorf("stdout = %q, want the one-line report", got.stdout)
	}

	if got.stderr != "" {
		t.Errorf("stderr = %q, want nothing", got.stderr)
	}

	// FR-004 through a real shell-shaped argv, and the proof that the report
	// above was about a post that happened.
	names := w.notes(t)
	if len(names) != 1 {
		t.Fatalf("the vault holds %v, want one note", names)
	}

	raw, err := os.ReadFile(filepath.Join(w.noteDir, names[0]))
	if err != nil {
		t.Fatalf("read the note: %v", err)
	}

	if !strings.HasSuffix(string(raw), "hello world\n") {
		t.Errorf("the note holds %q, want it to end with the joined message", raw)
	}
}

// TestTheBinaryReportsPartialFailure is FR-060 and FR-062: one destination
// succeeded, one did not, the exit status is 1, and both are named.
func TestTheBinaryReportsPartialFailure(t *testing.T) {
	w := newWorld(t, telegramThatFailsBeforeTheNetwork)

	got := run(t, w.home, "-c", w.configPath, "half a post")

	if got.status != 1 {
		t.Fatalf("status = %d, want 1\nstdout: %s\nstderr: %s", got.status, got.stdout, got.stderr)
	}

	if !strings.Contains(got.stdout, "Obsidian: success") {
		t.Errorf("the successful destination is not reported: %q", got.stdout)
	}

	if !strings.Contains(got.stdout, "Telegram: failed") {
		t.Errorf("the failed destination is not reported: %q", got.stdout)
	}

	// FR-063.
	if !strings.Contains(got.stdout, "See log for details: "+w.logPath) {
		t.Errorf("the log path is missing from failure output: %q", got.stdout)
	}

	// FR-013, FR-014: the destination that worked still worked.
	if names := w.notes(t); len(names) != 1 {
		t.Errorf("the vault holds %v, want the note the successful destination wrote", names)
	}

	// FR-043: the token this run configured is not in anything the user sees.
	if strings.Contains(got.stdout+got.stderr, "%zz") {
		t.Errorf("the credential reached the user-visible output:\n%s\n%s", got.stdout, got.stderr)
	}
}

// The two written correction prompts, matched by a phrase the prompt supplies
// and the error text does not.
//
// "whitespace" and "UTF-8" both appear in the sentinels themselves, so a run
// that reported the wrong reason — or no reason, falling through to the arm that
// repeats the error — would still match them. That is not hypothetical: a mutant
// collapsing the two cases survived a check written that way.
const (
	blankPromptMarker    = "Type a message and try again."
	encodingPromptMarker = "switch it to UTF-8 and try again."
)

// TestTheBinaryRejectsAMessageItMustNotSend is FR-010 as a process property,
// for both rejection reasons (decision DEC-D4).
//
// The success case above runs through the same fixture, so the "no destination
// contacted" assertion here is a claim about behaviour rather than about a vault
// that could never have held anything.
func TestTheBinaryRejectsAMessageItMustNotSend(t *testing.T) {
	tests := []struct {
		name string
		// argv is the message as a shell would hand it over.
		argv []string
		want string
	}{
		{name: "whitespace only", argv: []string{"   "}, want: blankPromptMarker},
		{name: "a tab and a full-width space", argv: []string{"\t　"}, want: blankPromptMarker},
		{name: "an empty argument", argv: []string{""}, want: blankPromptMarker},
		{name: "invalid UTF-8", argv: []string{"a\xffb"}, want: encodingPromptMarker},
		{name: "shift_jis bytes", argv: []string{"\x93\xfa\x96\x7b\x8c\xea"}, want: encodingPromptMarker},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, telegramThatFailsBeforeTheNetwork)

			argv := append([]string{"-c", w.configPath}, tt.argv...)

			got := run(t, w.home, argv...)

			if got.status != 1 {
				t.Fatalf("status = %d, want 1\nstdout: %s\nstderr: %s",
					got.status, got.stdout, got.stderr)
			}

			if names := w.notes(t); len(names) != 0 {
				t.Errorf("a rejected message reached the vault: %v", names)
			}

			if got.stdout != "" {
				t.Errorf("a rejected message produced a report: %q", got.stdout)
			}

			if !strings.Contains(got.stderr, tt.want) {
				t.Errorf("stderr = %q, want it to explain %q", got.stderr, tt.want)
			}

			other := blankPromptMarker
			if tt.want == blankPromptMarker {
				other = encodingPromptMarker
			}

			if strings.Contains(got.stderr, other) {
				t.Errorf("stderr = %q, which also reports the other rejection reason", got.stderr)
			}

			// No log either: validation runs before the logger is constructed,
			// so a rejected message leaves no trace on disk at all.
			if _, err := os.Stat(w.logPath); err == nil {
				t.Errorf("a rejected message opened the log at %s", w.logPath)
			}
		})
	}
}

// TestTheBinaryStillExitsZeroWithBrokenDiagnostics is FR-076 as a process
// property: the exit status reflects only the destination outcomes, and exactly
// one warning is added.
//
// Counting occurrences rather than checking presence, because printing the
// warning twice is the specific failure FR-076 forbids and a Contains check
// passes on it.
func TestTheBinaryStillExitsZeroWithBrokenDiagnostics(t *testing.T) {
	w := newWorld(t, telegramDisabled)

	if err := os.MkdirAll(w.logPath, 0o700); err != nil {
		t.Fatalf("put a directory at the log path: %v", err)
	}

	got := run(t, w.home, "-c", w.configPath, "diagnostics are broken")

	if got.status != 0 {
		t.Fatalf("status = %d, want 0 — FR-076 says diagnostics do not change the exit "+
			"status\nstdout: %s\nstderr: %s", got.status, got.stdout, got.stderr)
	}

	if got.stdout != "Obsidian: success\n" {
		t.Errorf("the reported outcome changed: %q", got.stdout)
	}

	if count := strings.Count(got.stderr, "warning:"); count != 1 {
		t.Errorf("got %d warnings, want exactly 1:\n%s", count, got.stderr)
	}

	if !strings.Contains(got.stderr, w.logPath) {
		t.Errorf("the warning does not name the log path: %q", got.stderr)
	}

	if names := w.notes(t); len(names) != 1 {
		t.Errorf("the post did not run: the vault holds %v", names)
	}
}

// TestTheBinaryOpensTheLogItPromisesToWrite pins the FR-063 path end to end.
//
// The file is empty in this batch and that is expected: the events themselves
// are T040, in the following pull request. What is being checked is that the
// path the failure output names is a file this run actually created — a report
// pointing at a file that does not exist is worse than no report.
func TestTheBinaryOpensTheLogItPromisesToWrite(t *testing.T) {
	w := newWorld(t, telegramDisabled)

	got := run(t, w.home, "-c", w.configPath, "hello")

	if got.status != 0 {
		t.Fatalf("status = %d, want 0\nstderr: %s", got.status, got.stderr)
	}

	if _, err := os.Stat(w.logPath); err != nil {
		t.Fatalf("the log the program resolved does not exist: %v", err)
	}
}

// TestTheBinaryResolvesTheDefaultSettingsAndLogPaths is issue #107 at the
// process level, and the only place the default resolution is exercised at all
// — every other run passes -c and an explicit logging.path.
//
// It is the wiring #107 describes: `logging.path` unset is the default, and
// before the fix a default install reported its outcomes normally and wrote no
// diagnostics anywhere.
func TestTheBinaryResolvesTheDefaultSettingsAndLogPaths(t *testing.T) {
	home := t.TempDir()
	noteDir := filepath.Join(home, "notes")

	if err := os.MkdirAll(noteDir, 0o700); err != nil {
		t.Fatalf("create the vault: %v", err)
	}

	configPath := filepath.Join(home, "config", "miko-post", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("create the config directory: %v", err)
	}

	document := fmt.Sprintf("[sink.obsidian]\nenabled = true\ndaily_note_dir = %q\n\n%s\n",
		noteDir, telegramDisabled)

	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}

	// No -c: the settings path is resolved from XDG_CONFIG_HOME.
	got := run(t, home, "a default install")

	if got.status != 0 {
		t.Fatalf("status = %d, want 0\nstdout: %s\nstderr: %s", got.status, got.stdout, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("a default install warned about something: %q", got.stderr)
	}

	defaultLog := filepath.Join(home, "state", "miko-post", "app.jsonl")
	if _, err := os.Stat(defaultLog); err != nil {
		t.Errorf("an unset logging.path wrote no diagnostics to %s: %v", defaultLog, err)
	}
}

// TestTheBinaryRefusesToPretendTheWindowExists pins the gap US2 fills.
//
// No message arguments means the window (FR-002), and the window is T041 – T051
// in batch 7. Until then the run must say so and exit 1: exiting 0 would report
// success for a capture that never happened, and that is the outcome class this
// whole program is shaped to avoid.
func TestTheBinaryRefusesToPretendTheWindowExists(t *testing.T) {
	w := newWorld(t, telegramDisabled)

	got := run(t, w.home)

	if got.status != 1 {
		t.Fatalf("status = %d, want 1\nstdout: %s\nstderr: %s", got.status, got.stdout, got.stderr)
	}

	if !strings.Contains(got.stderr, "not implemented") {
		t.Errorf("stderr = %q, want it to say the window is unimplemented", got.stderr)
	}

	if names := w.notes(t); len(names) != 0 {
		t.Errorf("the window path posted something: %v", names)
	}
}

// TestTheBinaryRejectsACommandLineItCannotParse covers the parse-failure exit
// path, which has its own os.Exit-adjacent branch in dispatch.
func TestTheBinaryRejectsACommandLineItCannotParse(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "an unknown flag", argv: []string{"-x", "hello"}, want: "not defined"},
		{name: "help, which is not implemented yet", argv: []string{"--help"}, want: "not implemented"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, telegramDisabled)

			got := run(t, w.home, tt.argv...)

			if got.status != 1 {
				t.Fatalf("status = %d, want 1\nstdout: %s\nstderr: %s",
					got.status, got.stdout, got.stderr)
			}

			if !strings.Contains(got.stderr, tt.want) {
				t.Errorf("stderr = %q, want it to mention %q", got.stderr, tt.want)
			}

			if got.stdout != "" {
				t.Errorf("a parse failure printed to stdout: %q", got.stdout)
			}
		})
	}
}

// TestTheProgramHasExactlyOneExitCallSite is T039's literal requirement.
//
// It is a source scan and it is worth having as one. Every behavioural test
// above observes the status of a process that exited; none of them can see a
// second os.Exit on a path they do not take, and the paths that would grow one
// are precisely the error branches — a settings failure, a parse failure — where
// an author reaches for an early exit and skips the cleanup the single call site
// exists to guarantee.
//
// It scans this whole command directory rather than main.go alone, since a
// second file here would be the natural place for the second call.
//
// The scan is over the syntax tree and not over the text. A grep for "os.Exit("
// counts the occurrences in this file's own explanatory comments — it did, on
// the first run — and a check that can be satisfied or broken by editing a
// comment is not a check on the program.
func TestTheProgramHasExactlyOneExitCallSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the command directory: %v", err)
	}

	fileSet := token.NewFileSet()
	found := 0
	scanned := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		scanned++

		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Exit" {
				return true
			}

			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "os" {
				return true
			}

			found++

			t.Logf("os.Exit call site at %s", fileSet.Position(call.Pos()))

			return true
		})
	}

	// A scan that read nothing would report zero call sites and pass a check
	// written as "not more than one".
	if scanned == 0 {
		t.Fatal("the scan read no source files")
	}

	if found != 1 {
		t.Errorf("found %d os.Exit call sites across %d files, want exactly 1", found, scanned)
	}
}
