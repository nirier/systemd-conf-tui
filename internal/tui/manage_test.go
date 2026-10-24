package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nirier/systemd-conf-tui/internal/service"
)

func managementModel() Model {
	m := prepared()
	m.manager = newManager(false, m.width, m.height)
	m.manager.update(unitsMsg{units: []service.Unit{{Name: "app.service", State: "disabled", Active: "inactive", Description: "示例应用"}, {Name: "wake.timer", State: "enabled", Active: "active"}}})
	m.manager.run = func(name string, args ...string) (string, error) {
		all := strings.Join(args, " ")
		switch {
		case strings.Contains(all, "list-unit-files"):
			return `[{"unit_file":"app.service","state":"disabled"},{"unit_file":"wake.timer","state":"enabled"}]`, nil
		case strings.Contains(all, "list-units"):
			return `[]`, nil
		case strings.Contains(all, "show"):
			return "Id=app.service\nNames=app.service\nLoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\nFragmentPath=/example/app.service\nNeedDaemonReload=no", nil
		case strings.Contains(all, "cat"):
			return "# /example/app.service\n[Service]\nExecStart=/usr/bin/true\n", nil
		}
		return "", fmt.Errorf("unexpected command %s %s", name, all)
	}
	return m
}

func deliver(m Model, msg tea.Msg) Model { next, _ := m.Update(msg); return next.(Model) }

func TestManagementEntryAndModeSwitch(t *testing.T) {
	m := prepared()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF4})
	m = next.(Model)
	if m.manager == nil || !m.manager.busy || cmd == nil || m.manager.system {
		t.Fatal("F4 must open user management")
	}
	// Never run a command against the real manager in tests.
	m = deliver(m, unitsMsg{units: []service.Unit{}})
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(Model)
	if !m.manager.system || !m.manager.busy || cmd == nil {
		t.Fatal("F2 must query system units")
	}
	m = deliver(m, unitsMsg{units: []service.Unit{}})
	m = press(m, tea.KeyF4)
	if m.manager != nil || !m.system || m.config().Name != "demo" {
		t.Fatal("returning must preserve form and selected scope")
	}
}

func TestManagementFilterAndNavigation(t *testing.T) {
	m := managementModel()
	m = typeKeys(m, "j")
	if m.manager.selected != 1 {
		t.Fatal("navigation failed")
	}
	m = typeKeys(m, "/")
	m = typeKeys(m, "app")
	if !m.manager.filtering || len(m.manager.visible()) != 1 || m.manager.selected != 0 {
		t.Fatal("filter failed")
	}
	// q and navigation keys are ordinary input while filtering.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = next.(Model)
	if m.manager.filter.Value() != "appq" {
		t.Fatal("filter keys escaped into navigation")
	}
	m = press(m, tea.KeyEsc)
	if m.manager == nil || m.manager.filtering {
		t.Fatal("escape should finish filtering")
	}
	m = typeKeys(m, "/")
	m.manager.filter.SetValue("")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "G")
	if m.manager.selected != 1 {
		t.Fatal("last unit not selected")
	}
}

func TestManagementPreviewConfirmCancelAndBusyGuard(t *testing.T) {
	m := managementModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	if cmd == nil || !m.manager.busy || m.manager.plan != nil {
		t.Fatal("disable must prepare before confirming")
	}
	m = typeKeys(m, "y")
	if !m.manager.busy {
		t.Fatal("busy state changed")
	}
	m = deliver(m, cmd()) // Fake read-only queries only.
	if m.manager.mode != "confirm" || m.manager.plan == nil || !strings.Contains(m.manager.content, "ExecStart=/usr/bin/true") {
		t.Fatal("missing full confirmation preview")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = next.(Model)
	if cmd != nil || m.manager.plan.Action != service.Disable {
		t.Fatal("confirming operation must capture action keys")
	}
	m = press(m, tea.KeyEsc)
	if m.manager.mode != "list" || m.manager.plan != nil {
		t.Fatal("cancel did not discard plan")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = next.(Model)
	m = deliver(m, cmd())
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if cmd == nil || !m.manager.busy {
		t.Fatal("explicit confirmation must use terminal execution")
	}
	// The returned tea.Exec command is deliberately not executed.
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(Model)
	if cmd != nil || m.manager.system {
		t.Fatal("mode switched while action in flight")
	}
	m = deliver(m, managedMsg{result: service.ManagementResult{Completed: []string{"mask 已完成：app.service"}}})
	if m.manager.busy || m.manager.mode != "result" || !strings.Contains(m.Summary(), "mask") || m.Failed() {
		t.Fatal("operation result not reported")
	}
}

func TestManagementErrorAndPartialResultScroll(t *testing.T) {
	m := managementModel()
	m.width, m.height = 48, 20
	m.manager.width, m.manager.height = 48, 20
	m.manager.resize()
	m = deliver(m, managedMsg{result: service.ManagementResult{Removed: []string{"/example/app.service"}}, err: errors.New(strings.Repeat("reload 失败，请检查\n", 40))})
	if !m.Failed() || !strings.Contains(m.Summary(), "已删除") || !strings.Contains(m.manager.content, "操作未全部完成") {
		t.Fatal("partial failure lost")
	}
	assertFits(t, m)
	m = typeKeys(m, "j")
	if m.manager.preview.YOffset == 0 {
		t.Fatal("long result not scrollable")
	}
}

func TestManagementViewsFitAndFullPreviewRemainsReachable(t *testing.T) {
	for _, size := range [][2]int{{48, 20}, {80, 24}, {95, 24}, {96, 20}, {120, 40}} {
		m := managementModel()
		m = deliver(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, mode := range []string{"list", "preview", "confirm", "result"} {
			m.manager.mode = mode
			m.manager.setContent(strings.Repeat("Environment="+strings.Repeat("长参数", 40)+"\n", 25) + "END_MARKER")
			assertFits(t, m)
			if mode == "list" {
				m = press(m, tea.KeyTab)
				assertFits(t, m)
			}
			var content strings.Builder
			for !m.manager.preview.AtBottom() {
				content.WriteString(m.manager.preview.View())
				m = typeKeys(m, "j")
			}
			content.WriteString(m.manager.preview.View())
			if !strings.Contains(content.String(), "END_MARKER") {
				t.Fatal("preview end is unreachable", size, mode)
			}
		}
	}
}

func TestManagementReadFailureDoesNotLeaveStalePlan(t *testing.T) {
	m := managementModel()
	m.manager.plan = &service.ManagementPlan{Name: "app.service", Action: service.Delete}
	m = deliver(m, managementPreviewMsg{err: errors.New("dependency check failed")})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if m.manager.mode == "confirm" || cmd != nil {
		t.Fatal("failed dependency check remained executable")
	}
}

func TestManagementSummarySurvivesReturningToCreation(t *testing.T) {
	m := managementModel()
	m = deliver(m, managedMsg{result: service.ManagementResult{Removed: []string{"/example/app.service"}}, err: errors.New("reload failed")})
	m = press(m, tea.KeyF4)
	if m.manager != nil || !m.Failed() || !strings.Contains(m.Summary(), "已删除") {
		t.Fatal("partial result lost on return")
	}
}

func TestReturningFromUserManagementRevalidatesSystemForm(t *testing.T) {
	m := prepared()
	m.system = true
	m.fields[userField].input.SetValue("nobody")
	m.goPage(2)
	m.manager = newManager(false, m.width, m.height)
	m = press(m, tea.KeyF4)
	if m.system || m.isPreview() || m.manager != nil {
		t.Fatal("invalid user form remained on install page")
	}
}
