package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// TestExampleSettingsAreTheDefaults pins testdata/config/example.toml (T087).
//
// The example tells its reader that every value they are not told to replace
// is the built-in default, so deleting a line changes nothing. That sentence
// is only true while the file and Defaults agree, and a default changed in
// code would otherwise leave the example quietly documenting the old one.
// Only the placeholders marked REPLACE, and the two enabled flags, differ.
func TestExampleSettingsAreTheDefaults(t *testing.T) {
	unsetToken(t)

	settings, err := config.Load(fixture(t, "example.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := settings.RequireDestination(); err != nil {
		t.Fatalf("the example enables no destination: %v", err)
	}

	want := config.Defaults()
	want.Sink.Telegram.Enabled = true
	want.Sink.Telegram.BotToken = config.NewSecret("replace-with-your-bot-token")
	want.Sink.Telegram.ChatID = "-1001234567890"
	want.Sink.Obsidian.Enabled = true
	want.Sink.Obsidian.DailyNoteDir = "/Users/you/Obsidian/Vault/Daily"

	if !reflect.DeepEqual(settings, want) {
		t.Errorf("example.toml differs from the defaults beyond its placeholders:\n got %#v\nwant %#v", settings, want)
	}
}

// TestExampleSettingsNameEveryKey checks the example shows the whole schema.
//
// Omitting a key would still load as its default, so the test above cannot
// see it. The reference is the key tables in contracts/config-schema.md, the
// normative list; a key commented out in the example, such as thread_id,
// still counts as shown.
func TestExampleSettingsNameEveryKey(t *testing.T) {
	example, err := os.ReadFile(fixture(t, "example.toml"))
	if err != nil {
		t.Fatal(err)
	}

	schema, err := os.ReadFile(filepath.Join("..", "..", "specs", "001-dual-sink-quick-post", "contracts", "config-schema.md"))
	if err != nil {
		t.Fatal(err)
	}

	shown := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^#? ?([a-z_]+) = `).FindAllStringSubmatch(string(example), -1) {
		shown[match[1]] = true
	}

	required := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| (?:string|int|bool) \\|").FindAllStringSubmatch(string(schema), -1)
	if len(required) < 20 {
		t.Fatalf("found only %d keys in config-schema.md; the pattern no longer reads its tables", len(required))
	}

	for _, match := range required {
		if !shown[match[1]] {
			t.Errorf("example.toml does not show %q", match[1])
		}
	}
}
