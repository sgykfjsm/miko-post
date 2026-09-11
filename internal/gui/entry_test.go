package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func TestCommandsWhileEntryHasFocus(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	sent, cancelled := 0, 0
	e := newMessageEntry(func() { sent++ }, func() { cancelled++ })
	w := a.NewWindow("entry")
	defer w.Close()
	w.SetContent(e)
	w.Canvas().Focus(e)
	focused := w.Canvas().Focused()
	if focused != e {
		t.Fatal("entry does not hold focus")
	}
	test.Type(focused, "日本語")
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	test.Type(focused, "second")
	if e.Text != "日本語\nsecond" || sent != 0 {
		t.Fatalf("Enter: %q, sent=%d", e.Text, sent)
	}
	shortcut := focused.(fyne.Shortcutable)
	shortcut.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierSuper})
	if sent != 1 {
		t.Fatal("Cmd+Enter did not submit")
	}
	// Standard editing must still be delegated to Entry.
	shortcut.TypedShortcut(&fyne.ShortcutSelectAll{})
	clipboard := test.NewClipboard()
	shortcut.TypedShortcut(&fyne.ShortcutCopy{Clipboard: clipboard})
	if clipboard.Content() != e.Text {
		t.Fatal("copy/select-all no longer work")
	}
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if cancelled != 1 || sent != 1 {
		t.Fatal("Esc did not cancel independently")
	}
	shortcut.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierSuper})
	if cancelled != 2 {
		t.Fatal("Cmd+Q was consumed by the entry")
	}
}
