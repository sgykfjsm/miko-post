//go:build !darwin || ci

package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
)

func installInputMethod(fyne.Window, func(string, int)) (func(fyne.Position, fyne.Size), error) {
	return nil, fmt.Errorf("input-method handling requires macOS with a native desktop driver")
}
