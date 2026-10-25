package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
	"github.com/nirier/systemd-conf-tui/internal/service"
)

// All TUI tests use isolated preferences, including on hosts set to English.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "systemd-conf-tui-tests-*")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestLanguageSettingsSaveCancelAndRetainForm(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := prepared()
	m.fields[descriptionField].input.SetValue("中文服务描述")
	m.extra.SetValue("[Service]\nEnvironment=GREETING=你好")
	m.goPage(2)
	before, err := m.config().Render(m.system)
	if err != nil {
		t.Fatal(err)
	}
	m = press(m, tea.KeyF5)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEsc)
	if m.settingsOpen || m.language != i18n.Chinese {
		t.Fatal("cancel changed the language")
	}
	m = press(m, tea.KeyF5)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	if m.settingsOpen || m.language != i18n.English || !m.isPreview() {
		t.Fatal("save did not apply language in place")
	}
	after, err := m.config().Render(m.system)
	if err != nil || before != after {
		t.Fatal("language switch modified unit content")
	}
	if m.fields[nameField].input.Placeholder != "e.g. my-app; the .service suffix is optional" {
		t.Fatal("field placeholders did not switch")
	}
	if next := New(false); next.language != i18n.English {
		t.Fatal("language not restored on next launch")
	}
	m.result = &installedMsg{result: service.Result{Path: "/example/demo.service", Written: true}}
	m.resize()
	m = typeKeys(m, "n")
	if m.language != i18n.English || m.result != nil {
		t.Fatal("new service reset language")
	}
	m = press(m, tea.KeyF5)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	if m.language != i18n.Chinese || !strings.Contains(m.View(), "基本信息") {
		t.Fatal("switching back to Chinese failed")
	}
}

func TestSettingsSaveFailureKeepsCurrentLanguage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "systemd-conf-tui"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	m := press(prepared(), tea.KeyF5)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	if !m.settingsOpen || m.language != i18n.Chinese || m.settingsNotice == "" {
		t.Fatal("failed save must report an error and preserve active settings")
	}
}

func TestEnglishViewsAndSettingsFit(t *testing.T) {
	for _, size := range [][2]int{{48, 20}, {80, 24}, {96, 20}, {120, 40}} {
		m := prepared()
		m.width, m.height = size[0], size[1]
		m.setLanguage(i18n.English)
		m.advanced = true
		for page := range m.pages() {
			m.goPage(page)
			assertFits(t, m)
			assertEnglish(t, m.View())
		}
		m.help = true
		assertFits(t, m)
		assertEnglish(t, m.View())
		m.help = false
		m = press(m, tea.KeyF5)
		assertFits(t, m)
		m = deliver(m, tea.WindowSizeMsg{Width: 48, Height: 20})
		assertFits(t, m)
		m = press(m, tea.KeyEsc)
		m.result = &installedMsg{result: service.Result{Path: "/example/demo.service", Written: true}, err: i18n.Errorf("配置已删除，但 daemon-reload 失败，请手动重试：%w", os.ErrPermission)}
		m.resize()
		assertFits(t, m)
		assertEnglish(t, m.View())
		assertEnglish(t, m.Summary())
	}
	blank := New(false)
	blank.setLanguage(i18n.English)
	assertEnglish(t, blank.View())
	if !strings.Contains(blank.preview.View(), "Service names may contain") {
		t.Fatal("validation errors did not switch languages")
	}
}

func assertEnglish(t *testing.T, text string) {
	t.Helper()
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		t.Fatalf("untranslated application text: %s", text)
	}
}

func TestManagementLanguageAndSettingsGuards(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := managementModel()
	m.manager.units[0].Description = "Example app"
	m = press(m, tea.KeyF5)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	if m.manager.language != i18n.English {
		t.Fatal("management language not updated")
	}
	assertEnglish(t, m.View())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	m = deliver(m, cmd()) // Fake read-only queries; never execute mutations.
	if m.manager.mode != "confirm" || !strings.Contains(m.manager.content, "Action: disable") {
		t.Fatal("management preview not translated", m.manager.content)
	}
	m = press(m, tea.KeyF5)
	if m.settingsOpen {
		t.Fatal("settings must not interrupt a confirmation")
	}
	m = press(m, tea.KeyEsc)
	m = typeKeys(m, "/")
	m = press(m, tea.KeyF5)
	if m.settingsOpen || !m.manager.filtering {
		t.Fatal("settings interrupted filtering")
	}
}
