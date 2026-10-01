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
	// A plain Return, such as the one that confirms an IME conversion, adds nothing.
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	if e.Text != "日本語" || sent != 0 {
		t.Fatalf("plain Return: %q, sent=%d", e.Text, sent)
	}
	e.shiftHeld = func() bool { return true }
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	test.Type(focused, "second")
	if e.Text != "日本語\nsecond" || sent != 0 {
		t.Fatalf("Shift+Return: %q, sent=%d", e.Text, sent)
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

func TestCaretIsReportedBesideTheTypedText(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	e := newMessageEntry(func() {}, func() {})
	var pos fyne.Position
	var size fyne.Size
	reports := 0
	e.caretMoved = func(p fyne.Position, s fyne.Size) { pos, size, reports = p, s, reports+1 }
	w := a.NewWindow("caret")
	defer w.Close()
	w.SetContent(e)
	w.Resize(fyne.NewSize(440, 260))
	w.Canvas().Focus(e)

	test.Type(e, "日本語")
	first, firstReports := pos, reports
	if firstReports == 0 || size.Height <= 0 {
		t.Fatalf("no caret reported: reports=%d size=%v", firstReports, size)
	}
	if first.X <= 0 || first.Y < 0 || first.Y > e.Size().Height {
		t.Fatalf("caret %v is outside the entry %v", first, e.Size())
	}

	test.Type(e, "かな")
	if pos.X <= first.X || pos.Y != first.Y {
		t.Fatalf("typing did not move the caret right on the same line: %v then %v", first, pos)
	}

	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn}) // ignored, see TestCommandsWhileEntryHasFocus
	e.shiftHeld = func() bool { return true }
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if pos.Y <= first.Y || pos.X >= first.X {
		t.Fatalf("a new line did not move the caret down and back to the left: %v", pos)
	}
}

func TestKeysThatSteerAConversionAreIgnoredWhileComposing(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	cancelled := 0
	e := newMessageEntry(func() {}, func() { cancelled++ })
	w := a.NewWindow("composing")
	defer w.Close()
	w.SetContent(e)
	w.Canvas().Focus(e)
	test.Type(e, "日本語")
	row, column := e.CursorRow, e.CursorColumn

	e.composing.Store(true)
	for _, key := range []fyne.KeyName{fyne.KeyBackspace, fyne.KeyDelete, fyne.KeyLeft, fyne.KeyReturn, fyne.KeyEscape} {
		e.TypedKey(&fyne.KeyEvent{Name: key})
	}
	if e.Text != "日本語" || e.CursorRow != row || e.CursorColumn != column || cancelled != 0 {
		t.Fatalf("a key reached the entry mid-conversion: %q at %d,%d cancelled %d", e.Text, e.CursorRow, e.CursorColumn, cancelled)
	}

	e.composing.Store(false)
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	if e.Text != "日本" {
		t.Fatalf("Backspace stopped working after the conversion ended: %q", e.Text)
	}
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if cancelled != 1 {
		t.Fatal("Esc stopped cancelling after the conversion ended")
	}
}
