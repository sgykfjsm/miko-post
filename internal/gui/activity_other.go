//go:build !darwin || ci

package gui

import (
	"fmt"
	"fyne.io/fyne/v2"
)

func observeActivity(fyne.Window) (func() uint64, func(), error) {
	return nil, nil, fmt.Errorf("the windowed interface requires macOS with a native desktop driver")
}
