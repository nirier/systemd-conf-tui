package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nirier/systemd-conf-tui/internal/service"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func (m Model) dimensions() (left, right, height int) {
	width := max(8, m.width-2)
	left, right = width, width
	if m.width >= 96 {
		left = width * 2 / 5
		right = width - left - 1
	}
	return left, right, max(6, m.height-6)
}

// Panel dimensions include borders and padding; clip by terminal cells, not bytes.
func panel(title, content string, width, height int, focused bool) string {
	color := lipgloss.Color("240")
	if focused {
		color = lipgloss.Color("81")
		title = titleStyle.Render(title)
	} else {
		title = mutedStyle.Render(title)
	}
	lines := strings.Split(title+"\n"+content, "\n")
	lines = lines[:min(len(lines), height-2)]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, width-4), "…")
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).
		Padding(0, 1).Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func (m Model) View() string {
	if m.width < 48 || m.height < 20 {
		return "请将终端调整为至少 48 列 × 20 行。\nCtrl+C 退出\n"
	}
	if m.result != nil {
		return m.resultView()
	}
	left, right, height := m.dimensions()
	mode := "USER · 当前用户"
	if m.system {
		mode = "SYSTEM · sudo 安装"
	}
	header := titleStyle.Render("SYSTEMD CONF") + "  " + activeStyle.Render(mode)
	var body string
	if m.help {
		body = panel("? 快捷键", m.helpContent(), m.width-2, height, true)
	} else {
		var steps []string
		for i, page := range m.pages() {
			label := fmt.Sprintf("  %d  %s", i+1, page)
			if m.page == i {
				label = activeStyle.Render(fmt.Sprintf("› %d  %s", i+1, page))
			}
			steps = append(steps, label)
		}
		navHeight := len(steps) + 3
		settings := panel("配置步骤 · h/l 切换", strings.Join(steps, "\n"), left, navHeight, !m.previewFocused)
		settings += "\n" + m.fieldsView(left, height-navHeight)
		preview := m.previewView(right, height)
		if m.width >= 96 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, settings, " ", preview)
		} else if m.previewFocused || m.isPreview() {
			body = preview
		} else {
			body = settings
		}
	}
	status := "浏览 · Enter 编辑 · l 下一步 · Tab 查看预览"
	if m.editing {
		status = "编辑 · Enter 保存 · Esc 取消 · Tab 保存并编辑下一项"
		if m.isCustom() {
			status = "编辑自定义指令 · Ctrl+S 保存 · Esc 取消"
		}
	} else if m.isPreview() {
		status = "安装前请核对完整预览 · 已有文件不会覆盖"
	}
	if m.notice != "" {
		status = errorStyle.Render(m.notice)
	} else {
		status = mutedStyle.Render(status)
	}
	keys := "j/k 移动  h/l 步骤  Enter 编辑  Tab 面板"
	if m.previewFocused {
		keys = "j/k 滚动  PgUp/PgDn 翻页  Tab 返回配置"
	} else if m.isPreview() {
		keys = "j/k 选项  Space 切换  i 安装  e 导出"
	}
	footer := fmt.Sprintf("1–%d 步骤  F2 模式  F3 高级  ? 帮助  q 退出", len(m.pages()))
	if m.editing {
		keys = "Enter 保存  Esc 取消  Tab 编辑下一项"
		footer = "编辑时按键作为文字输入 · Ctrl+C 退出"
		if m.isCustom() {
			keys = "Enter 换行  Ctrl+S 保存  Esc 取消"
		} else if len(fieldChoices(m.visibleFields()[m.focus])) > 0 {
			keys = "j/k 选择  Enter 确认  Esc 取消"
			footer = "确认后返回浏览 · Ctrl+C 退出"
		}
	}
	if m.help {
		keys = "Enter / Esc / ? 关闭帮助"
		footer = "Ctrl+C 退出程序"
	}
	return m.frame(header + "\n" + body + "\n" + status + "\n" +
		mutedStyle.Render(keys) + "\n" + mutedStyle.Render(footer))
}

func (m Model) fieldsView(width, height int) string {
	title := m.pages()[m.page]
	if m.editing {
		title += " · 编辑中"
	}
	available := height - 3
	var content string
	switch {
	case m.isPreview():
		items := []string{checkbox(m.enable) + " 启用自启", checkbox(m.start) + " 立即启动"}
		for i, item := range items {
			if i == m.choice && !m.previewFocused {
				items[i] = activeStyle.Render("› " + item)
			} else {
				items[i] = "  " + item
			}
		}
		content = strings.Join(items, "\n")
		if available > 3 {
			content += "\n\n" + titleStyle.Render("i 安装服务    e 仅导出")
		}
		if available > 6 {
			note := "自启随用户会话；注销后运行需启用 linger。"
			if m.system {
				note = "默认以 root 运行；高级设置可指定 User。"
			}
			content += "\n\n" + mutedStyle.Render(ansi.Wrap(note, width-4, ""))
		}
	case m.isCustom():
		if m.editing {
			content = m.extra.View()
		} else {
			content = mutedStyle.Render("Enter 编辑原生 systemd 指令") + "\n" + m.extra.Value()
			if m.extra.Value() == "" {
				content += mutedStyle.Render("[Service]\nNoNewPrivileges=yes\nPrivateTmp=yes")
			}
		}
	default:
		fields := m.visibleFields()
		index := fields[m.focus]
		choices := fieldChoices(index)
		if m.editing && len(choices) > 0 {
			title = m.fields[index].label + " · Enter 确认"
			count := max(1, available-1)
			first := max(0, m.selection-count+1)
			var lines []string
			for i := first; i < min(len(choices), first+count); i++ {
				line := "  " + choices[i]
				if i == m.selection {
					line = activeStyle.Render("› " + choices[i])
				}
				lines = append(lines, line)
			}
			content = strings.Join(lines, "\n")
		} else {
			count := max(1, (available-1)/2)
			first := max(0, m.focus-count+1)
			var lines []string
			for pos := first; pos < min(len(fields), first+count); pos++ {
				f := m.fields[fields[pos]]
				label, value := "  "+f.label, "  "+f.input.Value()
				if f.input.Value() == "" {
					value = mutedStyle.Render("  未设置")
				}
				if pos == m.focus && !m.previewFocused {
					label = activeStyle.Render("› " + f.label)
					if m.editing {
						value = f.input.View()
					}
				}
				lines = append(lines, label, value)
			}
			content = strings.Join(lines, "\n")
			title += fmt.Sprintf(" · %d/%d", m.focus+1, len(fields))
		}
		used := lipgloss.Height(content)
		if used < available {
			hint := "j/k 选择 · Enter 确认 · Esc 取消"
			if !m.editing || len(choices) == 0 {
				hint = m.fields[index].hint
			}
			hints := strings.Split(ansi.Wrap(hint, width-4, ""), "\n")
			hints = hints[:min(len(hints), available-used)]
			content += strings.Repeat("\n", available-used-len(hints)+1) + mutedStyle.Render(strings.Join(hints, "\n"))
		}
	}
	return panel(title, content, width, height, !m.previewFocused)
}

func (m Model) previewView(width, height int) string {
	dir, err := service.Directory(m.system)
	path := filepath.Join(dir, m.config().Filename())
	if err != nil {
		path = err.Error()
	}
	title := fmt.Sprintf("完整预览 · %d%%", int(m.preview.ScrollPercent()*100))
	content := mutedStyle.Render(ansi.Truncate(path, width-4, "…")) + "\n" + m.preview.View()
	if m.width < 96 && m.isPreview() {
		for i, item := range []string{checkbox(m.enable) + " 启用自启", checkbox(m.start) + " 立即启动"} {
			if i == m.choice && !m.previewFocused {
				item = activeStyle.Render("› " + item)
			} else {
				item = "  " + item
			}
			content += "\n" + item
		}
	}
	content += "\n" + mutedStyle.Render("长行已折行 · Tab 聚焦后 j/k 滚动")
	return panel(title, content, width, height, m.previewFocused || (m.width < 96 && m.isPreview()))
}

func checkbox(on bool) string {
	if on {
		return "[✓]"
	}
	return "[ ]"
}

func (m Model) helpContent() string {
	if m.width < 96 || m.height < 22 {
		return strings.Join([]string{
			"j/k 或 ↑↓ 移动 · h/l 或 ←→ 切换步骤",
			fmt.Sprintf("Enter 编辑 · 1–%d 跳转步骤 · Tab 切换面板", len(m.pages())),
			"g/G 首项 / 末项 · Ctrl+N/P 下一步 / 上一步",
			"编辑：Enter 保存 · Esc 取消 · Tab 下一项",
			"多行：Enter 换行 · Ctrl+S 保存 · Esc 取消",
			"编辑时 j/k/q 等均为文字；枚举项用 j/k 选择",
			"F2 用户 / 系统模式 · F3 展开 / 收起高级",
			"安装页：j/k 选项 · Space 切换 · i 安装",
			"e 导出 · Tab 聚焦预览后 j/k 或翻页键滚动",
			"? / Esc 关闭帮助 · q / Ctrl+C 退出",
		}, "\n")
	}
	return strings.Join([]string{
		"浏览配置                       编辑字段",
		"j/k 或 ↑↓  移动 / 滚动           Enter    保存单行 / 确认选择",
		"h/l 或 ←→  上一步 / 下一步       Esc      取消本次编辑",
		fmt.Sprintf("1–%d        跳转配置步骤          Tab      保存并编辑下一项", len(m.pages())),
		"Tab        配置 / 预览面板       Ctrl+S   保存（含多行）",
		"Enter      编辑当前字段          多行编辑时 Enter 换行",
		"g/G        首项 / 末项           编辑时可正常输入 j/k/q 等字符",
		"Ctrl+N/P   下一步 / 上一步",
		"F2 用户 / 系统模式  ·  F3 展开 / 收起高级配置",
		"预览安装：j/k 选项 · Space 切换 · i 安装 · e 导出",
		"完整预览：Tab 聚焦 · j/k 滚动 · PgUp/PgDn 翻页",
		"? / Esc 关闭帮助 · q / Ctrl+C 退出（编辑时 q 为文字）",
	}, "\n")
}

func (m Model) resultView() string {
	body := panel("操作结果", m.preview.View(), m.width-2, m.height-4, true)
	return m.frame(body + "\n" + mutedStyle.Render("j/k 滚动 · n 新建 · q / Enter 退出") + "\n" +
		mutedStyle.Render("b 返回修改（仅在尚未安装时）"))
}

func (m Model) frame(content string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width-2, "…")
	}
	return lipgloss.NewStyle().Padding(1, 1).Render(strings.Join(lines, "\n"))
}
