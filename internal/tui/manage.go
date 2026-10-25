package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
	"github.com/nirier/systemd-conf-tui/internal/service"
)

type managerModel struct {
	language      i18n.Language
	system        bool
	units         []service.Unit
	selected      int
	width, height int
	filter        textinput.Model
	filtering     bool
	focused       bool
	busy          bool
	mode          string
	preview       viewport.Model
	content       string
	plan          *service.ManagementPlan
	run           service.Run
}

type unitsMsg struct {
	units []service.Unit
	err   error
}
type managementPreviewMsg struct {
	content string
	plan    *service.ManagementPlan
	err     error
}
type managedMsg struct {
	result service.ManagementResult
	err    error
}

func newManager(system bool, width, height int) *managerModel {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "按名称或描述筛选"
	filter.CharLimit = 256
	m := &managerModel{system: system, width: width, height: height, filter: filter, mode: "list", preview: viewport.New(80, 12), run: queryCommand}
	m.resize()
	return m
}

// Queries never prompt for credentials. Mutations use tea.Exec below so sudo
// receives the real terminal, just like installation.
func queryCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

func (m *managerModel) load() tea.Cmd {
	m.busy = true
	m.mode = "list"
	m.plan = nil
	m.setContent(m.tr("正在读取 unit…"))
	system, run := m.system, m.run
	return func() tea.Msg { units, err := service.ListUnits(system, run); return unitsMsg{units, err} }
}

func (m *managerModel) visible() []service.Unit {
	var result []service.Unit
	term := strings.ToLower(m.filter.Value())
	for _, u := range m.units {
		if strings.Contains(strings.ToLower(u.Name+" "+u.Description), term) {
			result = append(result, u)
		}
	}
	return result
}

func (m *managerModel) selectionContent() {
	units := m.visible()
	m.selected = max(0, min(m.selected, len(units)-1))
	if len(units) == 0 {
		m.setContent(m.tr("没有匹配的 unit。\nr 刷新 · / 筛选 · F2 切换用户 / 系统模式"))
		return
	}
	u := units[m.selected]
	m.setContent(fmt.Sprintf(m.tr("%s\n自启：%s\n运行：%s\n%s\n\nEnter 查看完整配置\nd disable（不停止）\nm mask（不停止）\nx 删除本地配置（检查依赖）\n\n操作前会显示预览，按 y 才执行。"), u.Name, u.State, m.tr(u.Active), u.Description))
}

func (m *managerModel) resize() {
	width := m.width - 6
	if m.width >= 96 {
		width = (m.width-3)/2 - 4
	}
	m.preview.Width = max(1, width)
	m.preview.Height = max(1, m.height-10)
	m.filter.Width = max(1, m.width-8)
	m.preview.SetContent(ansi.Hardwrap(m.content, m.preview.Width, true))
}

func (m *managerModel) setContent(content string) {
	m.content = content
	m.resize()
	m.preview.GotoTop()
}

func (m *managerModel) prepare(action service.Action) tea.Cmd {
	units := m.visible()
	if len(units) == 0 {
		return nil
	}
	name, system, run, language := units[m.selected].Name, m.system, m.run, m.language
	m.busy = true
	m.plan = nil
	m.mode = "preview"
	m.setContent(m.tr("正在读取配置和检查依赖…"))
	return func() tea.Msg {
		if action == "" {
			content, err := service.PreviewUnitInLanguage(name, system, run, language)
			return managementPreviewMsg{content: content, err: err}
		}
		plan, err := service.PrepareManagementInLanguage(name, action, system, run, language)
		return managementPreviewMsg{content: plan.Preview, plan: &plan, err: err}
	}
}

func (m *managerModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case unitsMsg:
		m.busy = false
		if msg.err != nil {
			m.plan = nil
			m.mode = "preview"
			m.setContent(m.language.Error(msg.err))
			return nil
		}
		m.units = msg.units
		m.selected = 0
		m.selectionContent()
		return nil
	case managementPreviewMsg:
		m.busy = false
		if msg.err != nil {
			m.plan = nil
			m.mode = "preview"
			m.setContent(m.tr("无法执行操作：\n") + m.language.Error(msg.err))
			return nil
		}
		m.plan = msg.plan
		if m.plan != nil {
			m.mode = "confirm"
		}
		m.setContent(msg.content)
		return nil
	case managedMsg:
		m.busy = false
		m.mode = "result"
		m.plan = nil
		content := msg.result.LocalizedString(m.language)
		if msg.err != nil {
			content = m.tr("操作未全部完成：\n") + m.language.Error(msg.err) + "\n\n" + content
		}
		m.setContent(content + m.tr("\n\nr 刷新列表，检查最新状态。"))
		return nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return tea.Quit
		}
		if m.busy {
			return nil
		}
		if m.filtering {
			switch key {
			case "enter", "esc":
				m.filtering = false
				m.filter.Blur()
				m.selectionContent()
				return nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.selected = 0
			m.selectionContent()
			return cmd
		}
		if key == "q" {
			return tea.Quit
		}
		if m.mode == "confirm" {
			switch key {
			case "esc", "n":
				m.mode = "list"
				m.plan = nil
				m.selectionContent()
				return nil
			case "y":
				if m.plan == nil {
					return nil
				}
				op := &manageProcess{plan: *m.plan}
				m.busy = true
				m.setContent(m.tr("正在执行操作…"))
				return tea.Exec(op, func(err error) tea.Msg { return managedMsg{result: op.result, err: err} })
			}
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return cmd
		}
		if key == "f2" {
			m.system = !m.system
			m.units = nil
			m.selected = 0
			return m.load()
		}
		if key == "r" {
			return m.load()
		}
		if key == "esc" || key == "f4" {
			if m.mode == "result" {
				return m.load()
			}
			m.mode = "list"
			m.plan = nil
			m.focused = false
			m.selectionContent()
			return nil
		}
		if m.mode != "list" {
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return cmd
		}
		switch key {
		case "/":
			m.filtering = true
			m.filter.Focus()
			return textinput.Blink
		case "tab":
			m.focused = !m.focused
			return nil
		case "enter":
			return m.prepare("")
		case "d":
			return m.prepare(service.Disable)
		case "m":
			return m.prepare(service.Mask)
		case "x":
			return m.prepare(service.Delete)
		case "?":
			m.mode = "preview"
			m.setContent(m.tr("用户 / 系统 unit 管理\n\nj/k 选择 · / 筛选 · Enter 完整配置\nd disable · m mask · x 删除本地配置\nF2 用户 / 系统 · r 刷新\nTab 聚焦详情 · j/k 或 PgUp/PgDown 滚动\nEsc 返回 · F4 返回创建 · q 退出\n\n默认用户模式。系统修改操作才请求 sudo。\ndisable / mask 不会停止运行中的 unit。\n删除要求先停止并 disable，且没有其他 unit 引用。\n删除只移除本地 unit 和专属 drop-in，保留共享配置。\n所有修改均需预览后按 y 确认。"))
			return nil
		}
		if m.focused || key == "pgup" || key == "pgdown" {
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return cmd
		}
		units := m.visible()
		switch key {
		case "j", "down":
			m.selected = min(len(units)-1, m.selected+1)
		case "k", "up":
			m.selected = max(0, m.selected-1)
		case "g", "home":
			m.selected = 0
		case "G", "end":
			m.selected = max(0, len(units)-1)
		default:
			return nil
		}
		m.selectionContent()
	}
	return nil
}

func (m *managerModel) view() string {
	mode := m.tr("USER · 当前用户")
	if m.system {
		mode = m.tr("SYSTEM · 系统 unit")
	}
	header := titleStyle.Render(m.tr("SYSTEMD 管理")) + "  " + activeStyle.Render(mode)
	height := m.height - 6
	units := m.visible()
	count := max(1, height-4)
	first := max(0, m.selected-count+1)
	var lines []string
	for i := first; i < min(len(units), first+count); i++ {
		u := units[i]
		line := fmt.Sprintf("  %s  [%s / %s]", u.Name, u.State, m.tr(u.Active))
		if i == m.selected {
			line = activeStyle.Render("› " + strings.TrimSpace(line))
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, m.tr("没有匹配的 unit"))
	}
	title := fmt.Sprintf("unit · %d/%d", min(m.selected+1, len(units)), len(units))
	detail := panel(m.tr("完整配置 / 操作详情"), m.preview.View(), m.width-2, height, true)
	if m.width >= 96 {
		left := (m.width - 3) / 2
		right := m.width - 3 - left
		detail = lipgloss.JoinHorizontal(lipgloss.Top, panel(title, strings.Join(lines, "\n"), left, height, !m.focused && m.mode == "list"), " ", panel(m.tr("完整配置 / 操作详情"), m.preview.View(), right, height, m.focused || m.mode != "list"))
	} else if m.mode == "list" && !m.focused {
		detail = panel(title, strings.Join(lines, "\n"), m.width-2, height, true)
	}
	status := m.tr("Enter 配置 · d disable · m mask · x 删除")
	if m.mode == "confirm" {
		status = m.tr("核对完整预览：y 确认 · n / Esc 取消")
	}
	if m.mode == "preview" || m.mode == "result" {
		status = m.tr("j/k 滚动 · PgUp/PgDown 翻页 · Esc 返回")
	}
	if m.busy {
		status = m.tr("正在处理，请稍候…")
	}
	search := m.tr("筛选：") + m.filter.Value()
	if m.filtering {
		search = m.filter.View()
	}
	footer := m.tr("/ 筛选  r 刷新  F2 模式  F4 创建  F5 设置  ? 帮助  q 退出")
	if m.width < 96 {
		footer = m.tr("/ 筛选 r 刷新 F2 模式 F4 创建 F5 设置 ? q")
	}
	return header + "\n" + detail + "\n" + mutedStyle.Render(search) + "\n" + mutedStyle.Render(status) + "\n" + mutedStyle.Render(footer)
}

type manageProcess struct {
	plan           service.ManagementPlan
	result         service.ManagementResult
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (p *manageProcess) SetStdin(r io.Reader)  { p.stdin = r }
func (p *manageProcess) SetStdout(w io.Writer) { p.stdout = w }
func (p *manageProcess) SetStderr(w io.Writer) { p.stderr = w }
func (p *manageProcess) Run() error {
	var err error
	run := terminalRunner(p.stdin, p.stdout, p.stderr)
	if p.plan.System {
		// Authenticate before the final checks; subsequent mutations use sudo -n.
		if _, err = run("sudo", "-v"); err != nil {
			return err
		}
	}
	p.result, err = service.ExecuteManagement(p.plan, run)
	return err
}

var _ tea.ExecCommand = (*manageProcess)(nil)
