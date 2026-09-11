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
}

func newMessageEntry(submit, cancel func()) *messageEntry {
	e := &messageEntry{submit: submit, cancel: cancel}
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
	e.Entry.TypedKey(k)
}

// A button can acquire keyboard focus after a click or Tab. Esc must still
// dismiss the window there rather than disappearing into Button.TypedKey.
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
