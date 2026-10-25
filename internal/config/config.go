package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
)

type Settings struct {
	Language i18n.Language `json:"language"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", i18n.Errorf("XDG_CONFIG_HOME 必须是绝对路径")
	}
	return filepath.Join(dir, "systemd-conf-tui", "config.json"), nil
}

func Load() (Settings, error) {
	s := Settings{Language: i18n.Chinese}
	path, err := settingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var saved Settings
	if err := json.Unmarshal(data, &saved); err != nil {
		return s, err
	}
	if !saved.Language.Valid() {
		return s, i18n.Errorf("不支持的语言：%s", saved.Language)
	}
	return saved, nil
}

func Save(s Settings) error {
	if !s.Language.Valid() {
		return i18n.Errorf("不支持的语言：%s", s.Language)
	}
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
