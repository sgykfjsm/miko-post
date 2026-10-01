package config_test

import (
	"github.com/sgykfjsm/miko-post/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBackgroundDirectoryConfiguration(t *testing.T) {
	unsetToken(t)
	for _, dir := range []string{"", t.TempDir(), "relative/backgrounds"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte("[gui]\nbackground_image_dir = "+strconv.Quote(dir)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		settings, err := config.Load(path)
		if dir == "relative/backgrounds" {
			if err == nil || !strings.Contains(err.Error(), "gui.background_image_dir") {
				t.Fatalf("relative path: %v", err)
			}
		} else if err != nil || settings.GUI.BackgroundImageDir != dir {
			t.Fatalf("directory decode: %+v %v", settings.GUI, err)
		}
	}
}

func TestBackgroundOpacityConfiguration(t *testing.T) {
	unsetToken(t)
	for _, tc := range []struct {
		value   string
		want    float64
		wantErr bool
	}{
		{value: "", want: 0.12},
		{value: "0", want: 0},
		{value: "0.5", want: 0.5},
		{value: "1", want: 1},
		{value: "-0.1", wantErr: true},
		{value: "1.01", wantErr: true},
		{value: "nan", wantErr: true},
		{value: "inf", wantErr: true},
	} {
		body := "[gui]\n"
		if tc.value != "" {
			body += "background_opacity = " + tc.value + "\n"
		}
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		settings, err := config.Load(path)
		if tc.wantErr {
			if err == nil || !strings.Contains(err.Error(), "gui.background_opacity") {
				t.Errorf("%q: want a background_opacity error, got %v", tc.value, err)
			}
			continue
		}
		if err != nil || settings.GUI.BackgroundOpacity != tc.want {
			t.Errorf("%q: got %v, %v; want %v", tc.value, settings.GUI.BackgroundOpacity, err, tc.want)
		}
	}
}
