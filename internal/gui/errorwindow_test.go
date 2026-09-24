package gui

import (
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

// TestTheStartupErrorWindowSaysWhenThereIsNoPath: the path line is kept, and
// says it could not be resolved, rather than disappearing.
func TestTheStartupErrorWindowSaysWhenThereIsNoPath(t *testing.T) {
	_, w := newTestErrorWindow(t, "resolve home directory", "")

	found := false

	for _, o := range widgetsOf(w.native.Content()) {
		if l, ok := o.(*widget.Label); ok && l.Text == "Settings file: "+unresolvedPath {
			found = true
		}
	}

	if !found {
		t.Error("with no resolvable path the window dropped the path line instead of saying so")
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
				if b, ok := o.(*widget.Button); ok && b.Text == "Quit" {
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
		{name: "Esc", dismiss: func(w *errorWindow) {
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
