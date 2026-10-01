//go:build darwin && !ci

package gui

import "testing"

// Placing the candidate window and showing the composition need a real display and GLFW view, so a headless
// run proves only that a missing window is refused. Where the window opens is
// checked by hand on a Mac with the Japanese input method.
func TestInputMethodRefusesAMissingWindow(t *testing.T) {
	if _, err := installInputMethod(nil, nil); err == nil {
		t.Fatal("a missing window was accepted")
	}
}
