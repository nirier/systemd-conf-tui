package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nirier/systemd-conf-tui/internal/config"
	"github.com/nirier/systemd-conf-tui/internal/i18n"
)

func (m Model) tr(message string) string { return m.language.Text(message) }

func (m *managerModel) tr(message string) string { return m.language.Text(message) }

func (m Model) newManager() *managerModel {
	manager := newManager(m.system, m.width, m.height)
	manager.language = m.language
	manager.filter.Placeholder = m.tr("按名称或描述筛选")
	return manager
}

func (m *Model) setLanguage(language i18n.Language) {
	m.language = language
	for i := range m.fields {
		m.fields[i].input.Placeholder = m.tr(m.fields[i].hint)
	}
	if m.manager != nil {
		m.manager.language = language
		m.manager.filter.Placeholder = m.tr("按名称或描述筛选")
		m.manager.selectionContent()
	}
	m.resize()
}

func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "f5", "q":
		m.settingsOpen = false
		m.settingsNotice = ""
	case "j", "k", "up", "down", "h", "l", "left", "right", "tab", "shift+tab", " ":
		if m.settingsLanguage == i18n.English {
			m.settingsLanguage = i18n.Chinese
		} else {
			m.settingsLanguage = i18n.English
		}
	case "enter":
		if err := config.Save(config.Settings{Language: m.settingsLanguage}); err != nil {
			m.settingsNotice = m.tr("保存设置失败：") + m.language.Error(err)
			return m, nil
		}
		m.setLanguage(m.settingsLanguage)
		m.settingsNotice = ""
		m.notice = ""
		m.settingsOpen = false
	}
	return m, nil
}

func (m Model) settingsView() string {
	options := []struct {
		language i18n.Language
		label    string
	}{{i18n.Chinese, "中文"}, {i18n.English, "English"}}
	content := m.tr("语言 / Language") + "\n\n"
	for _, option := range options {
		line := "  " + option.label
		if option.language == m.settingsLanguage {
			line = activeStyle.Render("› " + option.label)
		}
		content += line + "\n"
	}
	content += "\n" + m.tr("Enter 保存后立即生效，下次启动沿用。")
	if m.settingsNotice != "" {
		content += "\n\n" + errorStyle.Render(m.settingsNotice)
	}
	return m.frame(panel(m.tr("设置 / Settings"), content, m.width-2, m.height-5, true) + "\n" +
		mutedStyle.Render(m.tr("j/k 选择语言 · Enter 保存 · Esc 取消")))
}
