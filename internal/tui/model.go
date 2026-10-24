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
	lastManaged      *managedMsg
	manager          *managerModel
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
	editing, help    bool
	previewFocused   bool
	editOriginal     string
	selection        int
}

type installedMsg struct {
	result service.Result
	err    error
}

func New(system bool) Model {
	definitions := [][3]string{
		{"服务名称", "例如 my-app；可省略 .service 后缀", ""},
		{"服务描述（可选）", "简短说明这个服务的用途", ""},
		{"启动命令 · ExecStart", "例如 /usr/bin/python3 /home/me/app.py；管道等语法需显式 /bin/sh -c '…'", ""},
		{"工作目录（可选）", "例如 /home/me/project；请使用绝对路径，不会展开 ~", ""},
		{"服务类型 · Type", "simple / exec / oneshot / forking / notify / notify-reload / dbus / idle", "simple"},
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
		input.Prompt = "› "
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

func (m Model) Init() tea.Cmd { return nil }

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
	left, right, height := m.dimensions()
	for i := range m.fields {
		m.fields[i].input.Width = max(1, left-8)
	}
	m.extra.SetWidth(max(1, left-4))
	m.extra.SetHeight(max(1, height-len(m.pages())-7))
	m.preview.Width = max(1, right-4)
	m.preview.Height = max(1, height-5)
	if m.width < 96 && m.isPreview() {
		m.preview.Height = max(1, height-7)
	}
	if m.result != nil {
		m.preview.Width = max(1, m.width-6)
		m.preview.Height = max(1, m.height-7)
		m.preview.SetContent(ansi.Hardwrap(m.resultContent(), m.preview.Width, true))
	} else {
		m.refreshPreview()
	}
}

func (m *Model) focusCurrent() {
	for i := range m.fields {
		m.fields[i].input.Blur()
	}
	m.extra.Blur()
	if !m.editing {
		return
	}
	if m.isCustom() {
		m.extra.Focus()
	} else if fields := m.visibleFields(); len(fields) > 0 {
		m.focus = min(m.focus, len(fields)-1)
		if len(fieldChoices(fields[m.focus])) == 0 {
			m.fields[fields[m.focus]].input.Focus()
		}
	}
}

func (m *Model) refreshPreview() {
	content, err := m.config().Render(m.system)
	if err != nil {
		content = "配置尚未完成\n\n" + err.Error() + "\n\n填写服务名称和启动命令后，这里会显示完整 unit。"
	}
	m.preview.SetContent(ansi.Hardwrap(content, m.preview.Width, true))
}

func (m *Model) setPreview() bool {
	m.refreshPreview()
	if err := m.config().Validate(m.system); err != nil {
		m.notice = err.Error()
		return false
	}
	m.preview.GotoTop()
	return true
}

func (m *Model) goPage(page int) {
	page = max(0, min(page, len(m.pages())-1))
	if page == len(m.pages())-1 && !m.setPreview() {
		return
	}
	m.page, m.focus, m.choice = page, 0, 0
	m.previewFocused, m.notice = false, ""
	m.focusCurrent()
	m.resize()
}

func (m *Model) next() { m.goPage(m.page + 1) }

func fieldChoices(index int) []string {
	switch index {
	case typeField:
		return []string{"simple", "exec", "oneshot", "forking", "notify", "notify-reload", "dbus", "idle"}
	case restartField:
		return []string{"on-failure", "always", "no", "on-success", "on-abnormal", "on-watchdog", "on-abort"}
	}
	return nil
}

func (m *Model) beginEdit() tea.Cmd {
	m.editing, m.notice = true, ""
	if m.isCustom() {
		m.editOriginal = m.extra.Value()
	} else {
		index := m.visibleFields()[m.focus]
		m.editOriginal = m.fields[index].input.Value()
		m.selection = 0
		for i, value := range fieldChoices(index) {
			if value == m.editOriginal {
				m.selection = i
			}
		}
	}
	m.focusCurrent()
	return textinput.Blink
}

func (m *Model) finishEdit(cancel bool) {
	if m.isCustom() {
		if cancel {
			m.extra.SetValue(m.editOriginal)
		}
	} else {
		index := m.visibleFields()[m.focus]
		if cancel {
			m.fields[index].input.SetValue(m.editOriginal)
		} else if choices := fieldChoices(index); len(choices) > 0 {
			m.fields[index].input.SetValue(choices[m.selection])
		}
	}
	m.editing = false
	m.focusCurrent()
	m.refreshPreview()
}

func (m Model) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.finishEdit(true)
			return m, nil
		case "ctrl+s":
			m.finishEdit(false)
			return m, nil
		case "enter":
			if !m.isCustom() {
				m.finishEdit(false)
				return m, nil
			}
		case "tab", "shift+tab":
			if !m.isCustom() {
				m.finishEdit(false)
				delta := 1
				if key.String() == "shift+tab" {
					delta = -1
				}
				m.focus = (m.focus + delta + len(m.visibleFields())) % len(m.visibleFields())
				return m, m.beginEdit()
			}
		}
	}
	var cmd tea.Cmd
	if m.isCustom() {
		m.extra, cmd = m.extra.Update(msg)
	} else {
		index := m.visibleFields()[m.focus]
		if choices := fieldChoices(index); len(choices) > 0 {
			if key, ok := msg.(tea.KeyMsg); ok {
				switch key.String() {
				case "j", "down":
					m.selection = (m.selection + 1) % len(choices)
				case "k", "up":
					m.selection = (m.selection + len(choices) - 1) % len(choices)
				case "g", "home":
					m.selection = 0
				case "G", "end":
					m.selection = len(choices) - 1
				}
			}
			return m, nil
		}
		m.fields[index].input, cmd = m.fields[index].input.Update(msg)
	}
	m.refreshPreview()
	return m, cmd
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.manager != nil {
		if result, ok := msg.(managedMsg); ok {
			m.lastManaged = &result
		}
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = size.Width, size.Height
			m.manager.width, m.manager.height = size.Width, size.Height
			m.manager.resize()
			return m, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok && !m.manager.busy && !m.manager.filtering && (key.String() == "f4" || (key.String() == "esc" && m.manager.mode == "list")) {
			m.system = m.manager.system
			m.manager = nil
			if m.isPreview() && !m.setPreview() {
				m.page, m.focus, m.previewFocused = 0, 0, false
			}
			m.resize()
			return m, nil
		}
		cmd := m.manager.update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case installedMsg:
		m.lastManaged = nil
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
			case "f4":
				m.manager = newManager(m.system, m.width, m.height)
				return m, m.manager.load()
			case "q", "esc", "enter":
				return m, tea.Quit
			case "n":
				fresh := New(m.system)
				fresh.width, fresh.height = m.width, m.height
				fresh.resize()
				return fresh, fresh.Init()
			case "b":
				if !m.result.result.Written {
					m.result = nil
					m.resize()
					m.setPreview()
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
		if m.help {
			if key == "?" || key == "esc" || key == "q" || key == "enter" {
				m.help = false
			}
			return m, nil
		}
		if m.editing {
			return m.updateEditor(msg)
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "?":
			m.help = true
			return m, nil
		case "f2":
			m.system = !m.system
			m.notice = "已切换模式；仅安装系统服务时请求 sudo。"
			m.refreshPreview()
			if m.isPreview() && !m.setPreview() {
				m.page, m.focus, m.previewFocused = 0, 0, false
			}
			m.resize()
			return m, nil
		case "f4":
			m.manager = newManager(m.system, m.width, m.height)
			return m, m.manager.load()
		case "f3":
			wasPreview := m.isPreview()
			m.advanced = !m.advanced
			m.page = 1
			if wasPreview {
				m.page = len(m.pages()) - 1
			} else if m.advanced {
				m.page = 2
			}
			m.focus, m.choice, m.previewFocused = 0, 0, false
			m.resize()
			return m, nil
		case "ctrl+n", "l", "right":
			m.next()
			return m, nil
		case "ctrl+p", "h", "left", "esc":
			if key == "esc" && m.previewFocused {
				m.previewFocused = false
			} else {
				m.goPage(m.page - 1)
			}
			return m, nil
		case "1", "2", "3", "4", "5":
			if page := int(key[0] - '1'); page < len(m.pages()) {
				m.goPage(page)
			}
			return m, nil
		case "tab", "shift+tab":
			m.previewFocused = !m.previewFocused
			return m, nil
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
			case " ", "enter":
				if !m.previewFocused {
					if m.choice == 0 {
						m.enable = !m.enable
					} else {
						m.start = !m.start
					}
				}
				return m, nil
			}
		}
		if m.previewFocused || key == "pgup" || key == "pgdown" {
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
		switch key {
		case "enter", " ":
			if !m.isPreview() {
				return m, m.beginEdit()
			}
		case "j", "down", "k", "up", "g", "home", "G", "end":
			count := len(m.visibleFields())
			if m.isPreview() {
				count = 2
			}
			if count > 0 {
				position := m.focus
				if m.isPreview() {
					position = m.choice
				}
				switch key {
				case "j", "down":
					position = (position + 1) % count
				case "k", "up":
					position = (position + count - 1) % count
				case "g", "home":
					position = 0
				case "G", "end":
					position = count - 1
				}
				if m.isPreview() {
					m.choice = position
				} else {
					m.focus = position
				}
			}
		}
		return m, nil
	}
	if m.editing {
		return m.updateEditor(msg)
	}
	return m, nil
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

func terminalRunner(stdin io.Reader, stdout, stderr io.Writer) service.Run {
	return func(name string, args ...string) (string, error) {
		fmt.Fprintf(stdout, "→ %s %s\n", name, strings.Join(args, " "))
		cmd := exec.Command(name, args...)
		var out, errOut bytes.Buffer
		cmd.Stdin = stdin
		cmd.Stdout = io.MultiWriter(stdout, &out)
		cmd.Stderr = io.MultiWriter(stderr, &errOut)
		if err := cmd.Run(); err != nil {
			return out.String(), fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(errOut.String()))
		}
		return out.String(), nil
	}
}

func (p *installProcess) Run() error {
	var err error
	p.result, err = service.Install(p.config, p.options, terminalRunner(p.stdin, p.stdout, p.stderr))
	if err != nil {
		fmt.Fprintln(p.stderr, err)
	}
	return err
}

// Summary is printed after leaving the alternate screen.
func (m Model) Summary() string {
	if m.lastManaged != nil {
		r := m.lastManaged
		if r.err != nil {
			return "操作未全部完成：" + r.err.Error() + "\n" + r.result.String()
		}
		return r.result.String()
	}

	if m.result == nil {
		return ""
	}
	if m.result.err != nil {
		return "操作未全部完成：" + m.result.err.Error()
	}
	return "已创建服务：" + m.result.result.Path
}

func (m Model) Failed() bool {
	if m.lastManaged != nil {
		return m.lastManaged.err != nil
	}
	return m.result != nil && m.result.err != nil
}

// Ensure terminal execution stays wired to Bubble Tea's lifecycle.
var _ tea.ExecCommand = (*installProcess)(nil)
