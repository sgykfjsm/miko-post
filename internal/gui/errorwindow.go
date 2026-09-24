package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// errorWindow is FR-030's minimal startup-error window: the settings could not
// be used, so there is nothing to post with, and this is how the windowed
// front door says so (T082, T083, FR-058).
//
// It shows two things and offers one action. The actionable message, which is
// the settings error's own text — config.Load's and config.RequireDestination's
// errors are written to be read by the user, each naming the key or file at
// fault — and the resolved settings path on a line of its own, because the fix
// is almost always "open this file" and a path buried mid-sentence is hard to
// copy. The action is Quit.
//
// It has no message field, and that is the requirement rather than minimalism:
// a field would invite typing a post that could not be sent, which is the
// worst outcome available to a window whose whole purpose is quick capture.
//
// Every way of dismissing it — Quit, the close box, Esc, Cmd+Q — does the same
// thing, and the process exits 1 whichever is used, since the startup failed
// however the window was closed.
type errorWindow struct {
	native fyne.Window
}

// unresolvedPath stands in for the settings path when it could not be resolved
// at all, which happens only when $HOME is unset and XDG_CONFIG_HOME is too.
// FR-030 asks for the resolved path, and when there is none the honest display
// says so instead of leaving the line out and inviting the question.
const unresolvedPath = "could not be resolved"

func newErrorWindow(native fyne.Window, message, path string) *errorWindow {
	w := &errorWindow{native: native}

	if path == "" {
		path = unresolvedPath
	}

	text := widget.NewLabel(message)
	text.Wrapping = fyne.TextWrapWord

	location := widget.NewLabel("Settings file: " + path)
	location.Wrapping = fyne.TextWrapBreak

	quit := widget.NewButton("Quit", w.close)
	quit.Importance = widget.HighImportance

	native.SetContent(container.NewBorder(nil, container.NewHBox(quit), nil, nil,
		container.NewVBox(widget.NewLabel("miko-post cannot start"), text, location)))
	native.Resize(fyne.NewSize(440, 220))
	native.SetCloseIntercept(w.close)
	native.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierSuper},
		func(fyne.Shortcut) { w.close() })
	native.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			w.close()
		}
	})
	native.Canvas().Focus(quit)

	return w
}

// close dismisses the window. Closing the only window ends the Fyne event loop,
// so a.Run returns and Run reports the failure status.
func (w *errorWindow) close() { w.native.Close() }
