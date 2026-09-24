package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// widgetsOf flattens a canvas object tree.
func widgetsOf(o fyne.CanvasObject) []fyne.CanvasObject {
	all := []fyne.CanvasObject{o}

	if c, ok := o.(*fyne.Container); ok {
		for _, child := range c.Objects {
			all = append(all, widgetsOf(child)...)
		}
	}

	return all
}

// interceptRecorder keeps what a window registers for dismissal and counts
// its closes. Fyne's test window stores the close intercept, and its canvas
// the shortcuts, but neither offers a way to fire them — and its driver keeps
// windows from earlier tests, so counting open windows proves nothing.
type interceptRecorder struct {
	fyne.Window
	intercept func()
	closes    int
	canvas    *shortcutRecorder
}

func (r *interceptRecorder) SetCloseIntercept(f func()) {
	r.intercept = f
	r.Window.SetCloseIntercept(f)
}

func (r *interceptRecorder) Close() {
	r.closes++
	r.Window.Close()
}

func (r *interceptRecorder) Canvas() fyne.Canvas {
	if r.canvas == nil {
		r.canvas = &shortcutRecorder{Canvas: r.Window.Canvas(), handlers: map[string]func(fyne.Shortcut){}}
	}

	return r.canvas
}

type shortcutRecorder struct {
	fyne.Canvas
	handlers map[string]func(fyne.Shortcut)
}

func (c *shortcutRecorder) AddShortcut(s fyne.Shortcut, h func(fyne.Shortcut)) {
	c.handlers[s.ShortcutName()] = h
	c.Canvas.AddShortcut(s, h)
}

func newTestErrorWindow(t *testing.T, message, path string) (fyne.App, *errorWindow) {
	t.Helper()

	a := test.NewApp()
	t.Cleanup(a.Quit)

	w := newErrorWindow(&interceptRecorder{Window: a.NewWindow("test")}, message, path)
	w.native.Show()

	return a, w
}

// TestTheStartupErrorWindowShowsTheMessageAndPathAndNoField is FR-030 (T082):
// the actionable message and the resolved settings path are on screen, and
// nothing on screen accepts a message.
func TestTheStartupErrorWindowShowsTheMessageAndPathAndNoField(t *testing.T) {
	const (
		message = "/x/config.toml: no destination is enabled"
		path    = "/x/config.toml"
	)

	_, w := newTestErrorWindow(t, message, path)

	var labels []string

	for _, o := range widgetsOf(w.native.Content()) {
		switch o := o.(type) {
		case *widget.Entry, *messageEntry:
			t.Errorf("the startup-error window offers a message field: %T", o)
		case *widget.Label:
			labels = append(labels, o.Text)
		}
	}

	text := strings.Join(labels, "\n")

	if !strings.Contains(text, message) {
		t.Errorf("the window does not show the message; labels:\n%s", text)
	}

	if !strings.Contains(text, "Settings file: "+path) {
		t.Errorf("the window does not show the settings path on its own line; labels:\n%s", text)
	}
}

// TestEveryDismissalClosesTheStartupErrorWindow: Quit, the close box, Esc and
// Cmd+Q all close it, which ends the event loop so Run returns its failure
// status. A dismissal that did nothing would leave a window the user cannot
// get rid of except by force-quitting.
func TestEveryDismissalClosesTheStartupErrorWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dismiss func(*errorWindow)
	}{
		{name: "Quit", dismiss: func(w *errorWindow) {
			for _, o := range widgetsOf(w.native.Content()) {
				if b, ok := o.(*commandButton); ok && b.Text == "Quit" {
					test.Tap(b)

					return
				}
			}

			t.Fatal("no Quit button")
		}},
		{name: "the close box", dismiss: func(w *errorWindow) {
			intercept := w.native.(*interceptRecorder).intercept
			if intercept == nil {
				t.Fatal("no close intercept was registered, so the close box bypasses close()")
			}

			intercept()
		}},
		{name: "Esc, as the driver delivers it: to the focused widget", dismiss: func(w *errorWindow) {
			focused := w.native.Canvas().Focused()
			if focused == nil {
				t.Fatal("nothing has focus, so this case would not exercise the focused route")
			}

			focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
		}},
		{name: "Esc with nothing focused", dismiss: func(w *errorWindow) {
			w.native.Canvas().Unfocus()
			w.native.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
		}},
		{name: "Cmd+Q", dismiss: func(w *errorWindow) {
			cmdQ := &desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierSuper}

			handler := w.native.(*interceptRecorder).canvas.handlers[cmdQ.ShortcutName()]
			if handler == nil {
				t.Fatal("no Cmd+Q shortcut was registered")
			}

			handler(cmdQ)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := newTestErrorWindow(t, "invalid settings", "/x/config.toml")

			tc.dismiss(w)

			if closes := w.native.(*interceptRecorder).closes; closes != 1 {
				t.Errorf("%s closed the window %d times, want 1", tc.name, closes)
			}
		})
	}
}

// TestTheWindowReadsOnlyTheDefaultSettings is T076's third clause: a window
// launched after a CLI post with -c reads the default resolved settings, not
// the override. There is no parameter through which an override could arrive,
// so what is asserted is the one thing that could still go wrong — that the
// window resolves the default rather than some other path.
//
// Not parallel: it sets XDG_CONFIG_HOME.
func TestTheWindowReadsOnlyTheDefaultSettings(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("MIKO_POST_TELEGRAM_BOT_TOKEN", "")

	defaultPath := filepath.Join(configHome, "miko-post", "config.toml")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o700); err != nil {
		t.Fatal(err)
	}

	document := "[sink.obsidian]\nenabled = true\ndaily_note_dir = \"" + t.TempDir() + "\"\n"
	if err := os.WriteFile(defaultPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	path, settings, err := windowSettings()
	if err != nil {
		t.Fatalf("windowSettings() = %v", err)
	}

	if path != defaultPath {
		t.Errorf("the window read %q, want the default %q", path, defaultPath)
	}

	if !settings.Sink.Obsidian.Enabled {
		t.Error("the window's settings are not the ones in the default file")
	}
}

// recordingApp is Fyne's headless app with the two calls startupFailure makes
// observed: the windows it creates, and Run, during which the test dismisses
// the window as a user would.
type recordingApp struct {
	fyne.App
	t       *testing.T
	windows []*interceptRecorder
	ran     bool
}

func (a *recordingApp) NewWindow(title string) fyne.Window {
	w := &interceptRecorder{Window: a.App.NewWindow(title)}
	a.windows = append(a.windows, w)

	return w
}

func (a *recordingApp) Run() {
	a.ran = true

	if len(a.windows) != 1 {
		a.t.Fatalf("Run started with %d windows, want the error window alone", len(a.windows))
	}

	a.windows[0].intercept()
}

// TestAStartupFailureShowsTheErrorWindowAndExitsOne is Run's settings-failure
// branch (FR-030, FR-058, T082, T083): the error reaches errOut, the one window
// shown is the startup-error window with no message field and the path it was
// handed, the event loop runs until it is dismissed, and the status is 1.
//
// The error text deliberately does not contain the path, so the path line is
// asserted on its own rather than satisfied by the message.
func TestAStartupFailureShowsTheErrorWindowAndExitsOne(t *testing.T) {
	base := test.NewApp()
	t.Cleanup(base.Quit)

	a := &recordingApp{App: base, t: t}

	var errOut strings.Builder

	status := startupFailure(&errOut, func() fyne.App { return a }, errors.New("no destination is enabled"), "/x/config.toml")

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}

	if want := "mp: no destination is enabled\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}

	if !a.ran {
		t.Error("the event loop never ran, so the error window was never on screen")
	}

	if len(a.windows) != 1 {
		t.Fatalf("created %d windows, want 1", len(a.windows))
	}

	w := a.windows[0]

	if w.closes != 1 {
		t.Errorf("the window was closed %d times, want 1", w.closes)
	}

	var shown, located bool

	for _, o := range widgetsOf(w.Content()) {
		switch o := o.(type) {
		case *widget.Entry, *messageEntry:
			t.Errorf("a startup failure showed a window with a message field: %T", o)
		case *widget.Label:
			shown = shown || strings.Contains(o.Text, "no destination is enabled")
			located = located || o.Text == "Settings file: /x/config.toml"
		}
	}

	if !shown {
		t.Error("the window shown does not carry the error")
	}

	if !located {
		t.Error("the window shown does not name the settings path it was handed")
	}
}

// TestAStartupFailureWithNoPathOpensNoWindow: with no resolvable path there is
// nothing for the window to show that stderr does not, and constructing the
// application would write Fyne's storage relative to the working directory, so
// the application is never constructed.
func TestAStartupFailureWithNoPathOpensNoWindow(t *testing.T) {
	var errOut strings.Builder

	constructed := false

	status := startupFailure(&errOut, func() fyne.App {
		constructed = true

		return test.NewApp()
	}, errors.New("resolve home directory"), "")

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}

	if want := "mp: resolve home directory\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}

	if constructed {
		t.Error("the application was constructed with no settings path to show")
	}
}
