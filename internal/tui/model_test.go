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

func typeKeys(m Model, keys string) Model {
	for _, key := range keys {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = next.(Model)
	}
	return m
}

func prepared() Model {
	m := New(false)
	m.fields[nameField].input.SetValue("demo")
	m.fields[execField].input.SetValue("/usr/bin/true")
	return m
}

func TestQuickFlowAndModeSwitch(t *testing.T) {
	m := New(false)
	m = typeKeys(m, "3")
	if m.page != 0 || m.notice == "" {
		t.Fatal("empty form should not advance")
	}
	m = prepared()
	m = press(m, tea.KeyCtrlN)
	if m.page != 1 {
		t.Fatal("expected runtime page")
	}
	m = press(m, tea.KeyCtrlN)
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
	m = typeKeys(m, "j")
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
	m = press(m, tea.KeyCtrlN)
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
	for _, size := range [][2]int{{48, 20}, {80, 24}, {95, 24}, {96, 20}, {120, 40}} {
		m := prepared()
		m.advanced = true
		m.width, m.height = size[0], size[1]
		m.resize()
		for page := range m.pages() {
			m.goPage(page)
			m.notice = "测试提示"
			if m.isPreview() {
				m.setPreview()
			}
			view := m.View()
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Errorf("size=%v page=%d view=%dx%d\n%s", size, page, lipgloss.Width(view), lipgloss.Height(view), view)
			}
			if !m.isPreview() {
				m.beginEdit()
				assertFits(t, m)
				m.finishEdit(true)
			}
			m.previewFocused = true
			assertFits(t, m)
			m.help = true
			assertFits(t, m)
			if !strings.Contains(m.View(), "关闭帮助") {
				t.Error("help must remain readable at minimum size", size)
			}
			m.help = false
		}
	}
}

func assertFits(t *testing.T, m Model) {
	t.Helper()
	view := m.View()
	if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
		t.Fatalf("view exceeds %dx%d: %dx%d\n%s", m.width, m.height, lipgloss.Width(view), lipgloss.Height(view), view)
	}
}

func TestBrowseEditAndCancel(t *testing.T) {
	m := New(false)
	m = typeKeys(m, "jk")
	if m.focus != 0 || m.fields[nameField].input.Value() != "" {
		t.Fatal("navigation must not type into fields")
	}
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "jj q123?hl")
	if !m.editing || m.help || m.page != 0 || m.fields[nameField].input.Value() != "jj q123?hl" {
		t.Fatal("navigation keys must become text in the editor")
	}
	m = press(m, tea.KeyEsc)
	if m.editing || m.fields[nameField].input.Value() != "" {
		t.Fatal("escape must restore the original field value")
	}
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "demo")
	m = press(m, tea.KeyTab)
	m = typeKeys(m, "示例服务")
	m = press(m, tea.KeyTab)
	m = typeKeys(m, "/usr/bin/true")
	m = press(m, tea.KeyEnter)
	if m.editing || m.config().Name != "demo" || m.config().Description != "示例服务" {
		t.Fatal("tab should commit and edit the next field")
	}
	if !strings.Contains(m.preview.View(), "ExecStart=/usr/bin/true") {
		t.Fatal("valid edits must update the live preview", m.preview.View())
	}
	m = typeKeys(m, "ll")
	if !m.isPreview() {
		t.Fatal("h/l should navigate the short flow")
	}
}

func TestEnumAndMultilineEditors(t *testing.T) {
	m := typeKeys(prepared(), "2j")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "jjjjj")
	m = press(m, tea.KeyEnter)
	if m.config().Type != "notify-reload" || m.editing {
		t.Fatal("service types must be selectable, including notify-reload")
	}
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "g")
	m = press(m, tea.KeyEsc)
	if m.config().Type != "notify-reload" {
		t.Fatal("cancel must retain the original enum value")
	}
	m = press(m, tea.KeyF3)
	m = typeKeys(m, "4")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "[Service]")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "PrivateTmp=yes")
	m = press(m, tea.KeyCtrlS)
	if m.editing || m.config().Extra != "[Service]\nPrivateTmp=yes" {
		t.Fatal("multiline editor must preserve newlines and save with Ctrl+S", m.config().Extra)
	}
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "x")
	m = press(m, tea.KeyEsc)
	if m.config().Extra != "[Service]\nPrivateTmp=yes" {
		t.Fatal("cancel must restore all multiline content")
	}
}

func TestPreviewFocusHelpAndInstallGuard(t *testing.T) {
	m := prepared()
	m.extra.SetValue("[Service]\n" + strings.Repeat("Environment=PORT=8080\n", 40))
	m.resize()
	m = press(m, tea.KeyTab)
	m = typeKeys(m, "j")
	if !m.previewFocused || m.preview.YOffset == 0 || m.focus != 0 {
		t.Fatal("focused preview must scroll independently of selected field")
	}
	m = press(m, tea.KeyEsc)
	if m.previewFocused || m.page != 0 {
		t.Fatal("escape must return to configuration from the preview")
	}
	m = typeKeys(m, "?")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if !m.help || cmd != nil {
		t.Fatal("help must capture keys without installing")
	}
	m = press(m, tea.KeyEsc)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd != nil {
		t.Fatal("live preview must not allow installation outside the final step")
	}
	m = typeKeys(m, "3")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("valid final preview should offer installation")
	}
	// Inspect the command without running it: tests must never install services.
	m.fields[nameField].input.SetValue("")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd != nil {
		t.Fatal("invalid configuration must block installation even on the final page")
	}
}

func TestLongPreviewWrapsWithoutDroppingContent(t *testing.T) {
	m := prepared()
	m.fields[execField].input.SetValue("/usr/bin/echo " + strings.Repeat("长参数", 80) + " END_MARKER")
	m.width, m.height = 48, 20
	m.goPage(2)
	if !strings.Contains(m.View(), "完整预览") || !strings.Contains(m.View(), "启用自启") {
		t.Fatal("narrow installation page must show the preview and options")
	}
	m = press(m, tea.KeyTab)
	var rendered strings.Builder
	for !m.preview.AtBottom() {
		rendered.WriteString(m.preview.View())
		m = typeKeys(m, "j")
	}
	rendered.WriteString(m.preview.View())
	if !strings.Contains(rendered.String(), "END_MARKER") || !strings.Contains(rendered.String(), "WantedBy=default.target") {
		t.Fatal("long directives and the end of the unit must remain reachable")
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
