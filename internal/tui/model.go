package tui

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nirier/systemd-conf-tui/internal/service"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

const (
	nameField = iota
	descriptionField
	execField
	workingField
	typeField
	restartField
	restartSecField
	environmentField
	envFileField
	userField
	groupField
	afterField
	wantsField
	requiresField
	stopField
	reloadField
	timeoutField
	memoryField
	cpuField
	wantedField
)

type field struct {
	label, hint string
	input       textinput.Model
}

type Model struct {
	fields           []field
	extra            textarea.Model
	preview          viewport.Model
	page, focus      int
	width, height    int
	system, advanced bool
	enable, start    bool
	choice           int
	notice           string
	result           *installedMsg
}

type installedMsg struct {
	result service.Result
	err    error
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func New(system bool) Model {
	definitions := [][3]string{
		{"服务名称", "例如 my-app；可省略 .service 后缀", ""},
		{"服务描述（可选）", "简短说明这个服务的用途", ""},
		{"启动命令 · ExecStart", "例如 /usr/bin/python3 /home/me/app.py；管道等语法需显式 /bin/sh -c '…'", ""},
		{"工作目录（可选）", "例如 /home/me/project；请使用绝对路径，不会展开 ~", ""},
		{"服务类型 · Type", "simple / exec / oneshot / forking / notify / dbus / idle", "simple"},
		{"重启策略 · Restart", "on-failure / always / no / on-success / on-abnormal / on-watchdog / on-abort", "on-failure"},
		{"重启间隔 · RestartSec", "例如 3s / 1min", "3s"},
		{"环境变量（可选）", "systemd 语法，例如 \"PORT=8080\" \"APP_ENV=production\"；不要在 unit 中存放密钥", ""},
		{"环境文件 · EnvironmentFile", "例如 /home/me/.config/app.env；前缀 - 表示文件可不存在", ""},
		{"运行用户 · User", "仅系统模式有效；留空时系统服务以 root 运行", ""},
		{"运行用户组 · Group", "仅系统模式有效；留空使用默认组", ""},
		{"启动顺序 · After", "例如 network-online.target；用户模式依赖用户 manager 中的 unit", ""},
		{"弱依赖 · Wants", "例如 network-online.target；多个 unit 用空格分隔", ""},
		{"强依赖 · Requires", "多个 unit 用空格分隔", ""},
		{"停止命令 · ExecStop", "可选，使用 systemd 命令行语法", ""},
		{"重载命令 · ExecReload", "例如 /bin/kill -HUP $MAINPID", ""},
		{"启动超时 · TimeoutStartSec", "例如 90s / infinity", ""},
		{"内存上限 · MemoryMax", "例如 512M / 2G", ""},
		{"CPU 配额 · CPUQuota", "例如 50% / 200%", ""},
		{"安装目标 · WantedBy", "留空自动使用用户 default.target / 系统 multi-user.target", ""},
	}
	m := Model{system: system, enable: true, start: true, width: 88, height: 28}
	for _, d := range definitions {
		input := textinput.New()
		input.CharLimit = 4096
		input.Prompt = "  "
		input.Placeholder = d[1]
		input.SetValue(d[2])
		m.fields = append(m.fields, field{label: d[0], hint: d[1], input: input})
	}
	m.extra = textarea.New()
	m.extra.Placeholder = "[Service]\nNoNewPrivileges=yes\nPrivateTmp=yes\n\n[Unit]\nStartLimitBurst=5"
	m.extra.CharLimit = 64 * 1024
	m.extra.ShowLineNumbers = true
	m.preview = viewport.New(80, 12)
	m.resize()
	m.focusCurrent()
	return m
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

func (m Model) pages() []string {
	if m.advanced {
		return []string{"基本信息", "运行设置", "高级设置", "自定义指令", "预览安装"}
	}
	return []string{"基本信息", "运行设置", "预览安装"}
}

func (m Model) isPreview() bool { return m.page == len(m.pages())-1 }
func (m Model) isCustom() bool  { return m.advanced && m.page == 3 }

func (m Model) visibleFields() []int {
	switch m.page {
	case 0:
		return []int{nameField, descriptionField, execField}
	case 1:
		return []int{workingField, typeField, restartField, restartSecField, environmentField}
	case 2:
		if m.advanced {
			return []int{envFileField, userField, groupField, afterField, wantsField, requiresField, stopField, reloadField, timeoutField, memoryField, cpuField, wantedField}
		}
	}
	return nil
}

func (m Model) config() unit.Config {
	v := func(i int) string { return m.fields[i].input.Value() }
	return unit.Config{
		Name: v(nameField), Description: v(descriptionField), ExecStart: v(execField),
		WorkingDirectory: v(workingField), Type: v(typeField), Restart: v(restartField),
		RestartSec: v(restartSecField), Environment: v(environmentField),
		EnvironmentFile: v(envFileField), User: v(userField), Group: v(groupField),
		After: v(afterField), Wants: v(wantsField), Requires: v(requiresField),
		ExecStop: v(stopField), ExecReload: v(reloadField), TimeoutStartSec: v(timeoutField),
		MemoryMax: v(memoryField), CPUQuota: v(cpuField), WantedBy: v(wantedField), Extra: m.extra.Value(),
	}
}

func (m *Model) resize() {
	w := max(12, m.width-8)
	for i := range m.fields {
		m.fields[i].input.Width = w - 4
	}
	m.extra.SetWidth(w)
	m.extra.SetHeight(max(3, m.height-12))
	m.preview.Width = w
	m.preview.Height = max(3, m.height-17)
	if m.result != nil {
		m.preview.Height = max(3, m.height-6)
		m.preview.SetContent(lipgloss.NewStyle().Width(w).Render(m.resultContent()))
	}
}

func (m *Model) focusCurrent() {
	for i := range m.fields {
		m.fields[i].input.Blur()
	}
	m.extra.Blur()
	if m.isCustom() {
		m.extra.Focus()
	} else if fields := m.visibleFields(); len(fields) > 0 {
		m.focus = min(m.focus, len(fields)-1)
		m.fields[fields[m.focus]].input.Focus()
	}
}

func (m *Model) setPreview() bool {
	content, err := m.config().Render(m.system)
	if err != nil {
		m.notice = err.Error()
		return false
	}
	m.preview.SetContent(content)
	m.preview.GotoTop()
	return true
}

func (m *Model) next() {
	if err := m.config().Validate(m.system); err != nil {
		m.notice = err.Error()
		return
	}
	if m.page < len(m.pages())-1 {
		m.page++
		m.focus = 0
		m.notice = ""
		if m.isPreview() {
			m.setPreview()
		}
		m.focusCurrent()
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case installedMsg:
		m.result = &msg
		m.resize()
		m.preview.GotoTop()
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.result != nil {
			switch key {
			case "q", "esc", "enter":
				return m, tea.Quit
			case "n":
				fresh := New(m.system)
				fresh.width, fresh.height = m.width, m.height
				fresh.resize()
				return fresh, textinput.Blink
			case "b":
				if !m.result.result.Written {
					m.result = nil
					m.resize()
					m.setPreview()
				}
			}
			if m.result != nil {
				var cmd tea.Cmd
				m.preview, cmd = m.preview.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		switch key {
		case "f2":
			m.system = !m.system
			m.notice = "已切换模式；安装系统服务时才会请求 sudo 授权。"
			if m.isPreview() && !m.setPreview() {
				m.page, m.focus = 0, 0
				m.focusCurrent()
			}
			return m, nil
		case "f3":
			wasPreview := m.isPreview()
			m.advanced = !m.advanced
			m.focus, m.page = 0, 1
			if wasPreview {
				m.page = len(m.pages()) - 1
				m.setPreview()
			} else if m.advanced {
				m.page = 2
			}
			m.focusCurrent()
			return m, nil
		case "ctrl+n":
			m.next()
			return m, textinput.Blink
		case "ctrl+p", "esc":
			m.page = max(0, m.page-1)
			m.focus, m.notice = 0, ""
			m.focusCurrent()
			return m, textinput.Blink
		}
		if m.isPreview() {
			switch key {
			case "i":
				if !m.setPreview() {
					return m, nil
				}
				op := &installProcess{config: m.config(), options: service.Options{System: m.system, Enable: m.enable, Start: m.start}}
				return m, tea.Exec(op, func(err error) tea.Msg { return installedMsg{result: op.result, err: err} })
			case "e":
				content, err := m.config().Render(m.system)
				if err == nil {
					var path string
					path, err = filepath.Abs(m.config().Filename())
					if err == nil {
						err = service.WriteNew(path, []byte(content))
					}
					if err == nil {
						m.notice = "已导出 " + path
					}
				}
				if err != nil {
					m.notice = err.Error()
				}
				return m, nil
			case "tab", "shift+tab":
				m.choice = 1 - m.choice
				return m, nil
			case " ":
				if m.choice == 0 {
					m.enable = !m.enable
				} else {
					m.start = !m.start
				}
				return m, nil
			}
		} else if !m.isCustom() {
			switch key {
			case "enter":
				m.next()
				return m, textinput.Blink
			case "tab", "down", "shift+tab", "up":
				fields := m.visibleFields()
				if len(fields) == 0 {
					return m, nil
				}
				delta := 1
				if key == "shift+tab" || key == "up" {
					delta = -1
				}
				m.focus = (m.focus + delta + len(fields)) % len(fields)
				m.focusCurrent()
				return m, textinput.Blink
			}
		}
	}
	var cmd tea.Cmd
	if m.isPreview() {
		m.preview, cmd = m.preview.Update(msg)
	} else if m.isCustom() {
		m.extra, cmd = m.extra.Update(msg)
	} else if fields := m.visibleFields(); len(fields) > 0 {
		i := fields[m.focus]
		m.fields[i].input, cmd = m.fields[i].input.Update(msg)
	}
	return m, cmd
}

func checkbox(on bool) string {
	if on {
		return "[✓]"
	}
	return "[ ]"
}

func (m Model) View() string {
	if m.width < 48 || m.height < 20 {
		return "请将终端调整为至少 48 列 × 20 行。\nCtrl+C 退出\n"
	}
	if m.result != nil {
		return m.resultView()
	}
	var b strings.Builder
	mode := "USER · 当前用户"
	if m.system {
		mode = "SYSTEM · sudo 系统服务"
	}
	b.WriteString(titleStyle.Render("SYSTEMD CONF") + "   " + activeStyle.Render(mode) + "\n")
	pages := m.pages()
	for i, page := range pages {
		label := fmt.Sprintf("%d %s", i+1, page)
		if i == m.page {
			b.WriteString(activeStyle.Render(label))
		} else {
			b.WriteString(mutedStyle.Render(label))
		}
		b.WriteString("  ")
	}
	b.WriteString("\n\n")
	if m.isPreview() {
		dir, err := service.Directory(m.system)
		if err != nil {
			dir = err.Error()
		}
		b.WriteString("安装位置：" + filepath.Join(dir, m.config().Filename()) + "\n")
		if m.system {
			b.WriteString(mutedStyle.Render("系统服务默认以 root 运行；可在高级设置填写 User。") + "\n")
		} else {
			b.WriteString(mutedStyle.Render("自启作用于用户会话；退出登录后持续运行需另行启用 linger。") + "\n")
		}
		b.WriteString(m.preview.View() + "\n")
		for i, item := range []string{checkbox(m.enable) + " 启用自启", checkbox(m.start) + " 立即启动"} {
			if i == m.choice {
				item = activeStyle.Render("› " + item)
			} else {
				item = "  " + item
			}
			b.WriteString(item + "   ")
		}
		b.WriteString("\n" + mutedStyle.Render("安装前使用 systemd-analyze 校验；已有服务不会覆盖。") + "\n")
	} else if m.isCustom() {
		b.WriteString("追加原生 systemd 指令，先写段名，再逐行填写 Key=Value。\n")
		b.WriteString(mutedStyle.Render("支持 [Unit] / [Service] / [Install]，可使用重置指令；请在预览中核对。") + "\n")
		b.WriteString(m.extra.View() + "\n")
	} else {
		fields := m.visibleFields()
		count := min(len(fields), max(1, (m.height-11)/4))
		first := max(0, m.focus-count+1)
		last := min(len(fields), first+count)
		for pos := first; pos < last; pos++ {
			f := m.fields[fields[pos]]
			label := f.label
			if pos == m.focus {
				label = activeStyle.Render("› " + label)
			} else {
				label = "  " + label
			}
			b.WriteString(label + "\n" + f.input.View() + "\n" + mutedStyle.Render(f.hint) + "\n\n")
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf("字段 %d/%d · Tab / ↑↓ 切换字段，Enter 下一步", m.focus+1, len(fields))) + "\n")
	}
	if m.notice != "" {
		b.WriteString(errorStyle.Render(m.notice) + "\n")
	}
	keys := "Enter 继续 · Esc 返回 · ^C 退出\nTab/↑↓ 字段 · F2 模式 · F3 高级"
	if m.isCustom() {
		keys = "Ctrl+N 继续 · Esc 返回 · ^C 退出\nF2 模式 · F3 高级 · Enter 换行"
	} else if m.isPreview() {
		keys = "i 安装 · e 导出 · Esc 返回 · ^C 退出\nF2 模式 · Tab/空格 选项 · ↑↓ 滚动"
	}
	b.WriteString("\n" + mutedStyle.Render(keys))
	return m.frame(b.String())
}

func (m Model) resultView() string {
	return m.frame(m.preview.View() + "\n\n↑↓ 滚动 · n 新建 · q / Enter 退出\nb 返回修改（仅在尚未安装时）")
}

func (m Model) resultContent() string {
	r := m.result
	var b strings.Builder
	if r.err != nil {
		b.WriteString(errorStyle.Render("操作未全部完成") + "\n\n" + r.err.Error() + "\n\n")
		if r.result.Written {
			b.WriteString("服务文件已保留：" + r.result.Path + "\n请修复错误后使用下列命令检查或继续操作。\n")
		} else {
			b.WriteString("尚未安装服务。按 b 返回修改。\n")
		}
	} else {
		b.WriteString(activeStyle.Render("服务已创建") + "\n\n" + r.result.Path + "\n")
		b.WriteString(fmt.Sprintf("自启：%t    已请求启动：%t\n", m.enable, m.start))
	}
	ctl, logs := "systemctl --user", "journalctl --user"
	if m.system {
		ctl, logs = "sudo systemctl", "sudo journalctl"
	}
	name := m.config().Filename()
	b.WriteString("\n" + ctl + " status " + name + "\n" + logs + " -u " + name + " -f\n")
	if !m.system {
		b.WriteString("\n用户服务默认随用户会话运行。需要注销后运行时，可自行执行：\n  loginctl enable-linger \"$USER\"\n")
	}
	return b.String()
}

func (m Model) frame(content string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width-4, "…")
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// Bubble Tea releases the terminal while this command runs, so sudo can use
// the real terminal without reading passwords into the application.
type installProcess struct {
	config  unit.Config
	options service.Options
	result  service.Result
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

func (p *installProcess) SetStdin(r io.Reader)  { p.stdin = r }
func (p *installProcess) SetStdout(w io.Writer) { p.stdout = w }
func (p *installProcess) SetStderr(w io.Writer) { p.stderr = w }

func (p *installProcess) Run() error {
	run := func(name string, args ...string) (string, error) {
		fmt.Fprintf(p.stdout, "→ %s %s\n", name, strings.Join(args, " "))
		cmd := exec.Command(name, args...)
		var out, errOut bytes.Buffer
		cmd.Stdin = p.stdin
		cmd.Stdout = io.MultiWriter(p.stdout, &out)
		cmd.Stderr = io.MultiWriter(p.stderr, &errOut)
		if err := cmd.Run(); err != nil {
			return out.String(), fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(errOut.String()))
		}
		return out.String(), nil
	}
	var err error
	p.result, err = service.Install(p.config, p.options, run)
	if err != nil {
		fmt.Fprintln(p.stderr, err)
	}
	return err
}

// Summary is printed after leaving the alternate screen.
func (m Model) Summary() string {
	if m.result == nil {
		return ""
	}
	if m.result.err != nil {
		return "操作未全部完成：" + m.result.err.Error()
	}
	return "已创建服务：" + m.result.result.Path
}

func (m Model) Failed() bool { return m.result != nil && m.result.err != nil }

// Ensure terminal execution stays wired to Bubble Tea's lifecycle.
var _ tea.ExecCommand = (*installProcess)(nil)
