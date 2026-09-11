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
