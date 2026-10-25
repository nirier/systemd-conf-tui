package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
)

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s, err := Load()
	if err != nil || s.Language != i18n.Chinese {
		t.Fatalf("missing settings must default to Chinese: %+v, %v", s, err)
	}
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		if err := Save(Settings{Language: language}); err != nil {
			t.Fatal(err)
		}
		s, err = Load()
		if err != nil || s.Language != language {
			t.Fatalf("settings not persisted: %+v, %v", s, err)
		}
	}
	if err := Save(Settings{Language: "fr"}); err == nil {
		t.Fatal("unsupported language accepted")
	}
	s, err = Load()
	if err != nil || s.Language != i18n.Chinese {
		t.Fatal("invalid save changed previous settings")
	}
	path := filepath.Join(dir, "systemd-conf-tui", "config.json")
	for _, data := range []string{`{`, `{"language":"fr"}`, `null`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := Load()
		if err == nil || s.Language != i18n.Chinese {
			t.Fatalf("invalid settings must report error and fall back: %+v, %v", s, err)
		}
	}
}

func TestSettingsRejectRelativeConfigDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if _, err := Load(); err == nil {
		t.Fatal("relative config directory accepted")
	}
	if err := Save(Settings{Language: i18n.English}); err == nil {
		t.Fatal("saved settings outside an absolute config directory")
	}
}
