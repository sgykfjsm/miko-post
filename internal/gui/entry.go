package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// messageEntry preserves Entry's editing and IME support while handling the
// commands that the focused entry would otherwise consume (research R-003).
type messageEntry struct {
	widget.Entry
	submit, cancel func()
	shiftHeld      func() bool
}

func newMessageEntry(submit, cancel func()) *messageEntry {
	e := &messageEntry{submit: submit, cancel: cancel, shiftHeld: shiftHeld}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	return e
}

func (e *messageEntry) TypedShortcut(s fyne.Shortcut) {
	if command, ok := s.(*desktop.CustomShortcut); ok && command.Modifier == fyne.KeyModifierSuper {
		switch command.KeyName {
		case fyne.KeyReturn:
			e.submit()
			return
		case fyne.KeyQ:
			e.cancel()
			return
		}
	}
	e.Entry.TypedShortcut(s)
}

func (e *messageEntry) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyEscape {
		e.cancel()
		return
	}
	// Only Shift+Return inserts a line break. The macOS driver hands the Return
	// that confirms an IME conversion to the entry as an ordinary key press, so
	// a plain Return would add a stray line break before the committed text.
	// Fyne cannot say whether a composition is active, hence the blanket rule.
	if (k.Name == fyne.KeyReturn || k.Name == fyne.KeyEnter) && !e.shiftHeld() {
		return
	}
	e.Entry.TypedKey(k)
}

func shiftHeld() bool {
	d, ok := fyne.CurrentApp().Driver().(desktop.Driver)
	return ok && d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0
}

// commandButton makes Esc dismiss the window even when a button holds keyboard
// focus, rather than disappearing into Button.TypedKey.
//
// That state is reachable in the startup-error window, which focuses its Quit
// button itself. In the posting window it is not reachable through user input
// (Batch 12, T091 window checks): the multi-line entry consumes Tab, and
// Fyne's Button.Tapped calls Focus(nil), so a click leaves nothing focused.
// There it is a defensive path, kept so that a layout change which does let a
// button take focus cannot bring back the dead Esc Batch 11 found.
type commandButton struct {
	widget.Button
	cancel func()
}

func newCommandButton(label string, tapped, cancel func()) *commandButton {
	b := &commandButton{cancel: cancel}
	b.Text, b.OnTapped = label, tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *commandButton) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyEscape {
		b.cancel()
		return
	}
	b.Button.TypedKey(k)
}
