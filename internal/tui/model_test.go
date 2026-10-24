package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nirier/systemd-conf-tui/internal/service"
)

func press(m Model, key tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: key})
	return next.(Model)
}

func prepared() Model {
	m := New(false)
	m.fields[nameField].input.SetValue("demo")
	m.fields[execField].input.SetValue("/usr/bin/true")
	return m
}

func TestQuickFlowAndModeSwitch(t *testing.T) {
	m := New(false)
	m = press(m, tea.KeyEnter)
	if m.page != 0 || m.notice == "" {
		t.Fatal("empty form should not advance")
	}
	m = prepared()
	m = press(m, tea.KeyEnter)
	if m.page != 1 {
		t.Fatal("expected runtime page")
	}
	m = press(m, tea.KeyEnter)
	if !m.isPreview() || !strings.Contains(m.preview.View(), "default.target") {
		t.Fatal("expected user unit preview", m.preview.View())
	}
	m = press(m, tea.KeyF2)
	if !m.system || !strings.Contains(m.preview.View(), "multi-user.target") {
		t.Fatal("switch must regenerate preview", m.preview.View())
	}
	m = press(m, tea.KeySpace)
	if m.enable {
		t.Fatal("space must toggle enable")
	}
	m = press(m, tea.KeyTab)
	m = press(m, tea.KeySpace)
	if m.start {
		t.Fatal("space must toggle start")
	}
	if next := press(m, tea.KeyEsc); next.page != 1 {
		t.Fatal("esc should go back")
	}
}

func TestAdvancedFlowKeepsValues(t *testing.T) {
	m := press(prepared(), tea.KeyF3)
	if !m.advanced || m.page != 2 {
		t.Fatal("F3 opens advanced fields")
	}
	m.fields[memoryField].input.SetValue("512M")
	m = press(m, tea.KeyEnter)
	if !m.isCustom() {
		t.Fatal("expected custom directives")
	}
	m.extra.SetValue("[Service]\nNoNewPrivileges=yes")
	m = press(m, tea.KeyCtrlN)
	if !m.isPreview() || !strings.Contains(m.preview.View(), "MemoryMax=512M") {
		t.Fatal("advanced settings absent from preview", m.preview.View())
	}
	m = press(m, tea.KeyF3)
	if !m.isPreview() || m.config().Extra == "" || m.config().MemoryMax != "512M" {
		t.Fatal("hiding advanced fields must preserve settings")
	}
}

func TestSwitchWithSystemIdentityBlocksUserInstall(t *testing.T) {
	m := prepared()
	m.system = true
	m.fields[userField].input.SetValue("nobody")
	m.page = 2
	m.setPreview()
	m = press(m, tea.KeyF2)
	if m.system || m.isPreview() || !strings.Contains(m.notice, "User / Group") {
		t.Fatal("invalid preview must not remain installable", m)
	}
}

func TestFailureBackAndFreshModelSize(t *testing.T) {
	m := prepared()
	m.width, m.height = 120, 40
	m.result = &installedMsg{err: errors.New("sudo cancelled")}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(Model)
	if m.result != nil {
		t.Fatal("pre-install failure should allow editing")
	}
	m.result = &installedMsg{result: service.Result{Written: true}, err: errors.New("start failed")}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if next.(Model).result == nil {
		t.Fatal("installed unit must not be retried as a new install")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(Model)
	if m.width != 120 || m.height != 40 {
		t.Fatal("new form lost terminal dimensions")
	}
}

func TestViewsFitTerminal(t *testing.T) {
	for _, size := range [][2]int{{48, 20}, {80, 24}, {120, 40}} {
		m := prepared()
		m.advanced = true
		m.width, m.height = size[0], size[1]
		m.resize()
		for page := range m.pages() {
			m.page = page
			m.notice = "测试提示"
			if m.isPreview() {
				m.setPreview()
			}
			view := m.View()
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Errorf("size=%v page=%d view=%dx%d\n%s", size, page, lipgloss.Width(view), lipgloss.Height(view), view)
			}
		}
	}
}

func TestLongFailureCanScroll(t *testing.T) {
	m := prepared()
	m.width, m.height = 48, 20
	next, _ := m.Update(installedMsg{err: errors.New(strings.Repeat("需要排查的错误信息\n", 50))})
	m = next.(Model)
	if lipgloss.Height(m.View()) > m.height || lipgloss.Width(m.View()) > m.width {
		t.Fatal("error view exceeds terminal dimensions")
	}
	m = press(m, tea.KeyDown)
	if m.preview.YOffset == 0 {
		t.Fatal("error details must be scrollable")
	}
}
