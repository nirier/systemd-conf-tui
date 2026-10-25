package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nirier/systemd-conf-tui/internal/service"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

type importModel struct {
	name, path                               textinput.Model
	body                                     textarea.Model
	content                                  string
	paste, loading                           bool
	returnChoice, returnOffset               int
	returnNotice                             string
	returnPage, returnFocus                  int
	returnPreview, returnEnable, returnStart bool
}

type importLoadedMsg struct {
	owner    *importModel
	document unit.Document
	err      error
}

func (m *Model) openImport() {
	im := &importModel{name: textinput.New(), path: textinput.New(), body: textarea.New(),
		returnPage: m.page, returnFocus: m.focus, returnPreview: m.previewFocused,
		returnChoice: m.choice, returnOffset: m.preview.YOffset, returnNotice: m.notice,
		returnEnable: m.enable, returnStart: m.start}
	im.name.CharLimit, im.path.CharLimit = 4096, 4096
	im.body.CharLimit = 0
	im.body.MaxHeight = 0
	im.body.ShowLineNumbers = true
	im.body.Placeholder = "[Unit]\nDescription=My service\n\n[Service]\nExecStart=/usr/bin/true"
	m.importer = im
	m.page, m.focus, m.choice = 0, 0, 0
	m.enable, m.start, m.previewFocused = false, false, false
	m.notice = ""
	m.resize()
}

func (m *Model) cancelImport() {
	im := m.importer
	m.importer = nil
	m.page, m.focus, m.previewFocused = im.returnPage, im.returnFocus, im.returnPreview
	m.enable, m.start = im.returnEnable, im.returnStart
	m.editing, m.notice = false, im.returnNotice
	m.choice = im.returnChoice
	if m.isPreview() {
		m.setPreview()
	}
	m.resize()
	m.preview.SetYOffset(im.returnOffset)
}

func (im *importModel) resize(width, height int) {
	im.name.Width, im.path.Width = max(1, width-8), max(1, width-8)
	im.body.SetWidth(max(1, width-4))
	im.body.SetHeight(max(1, height-3))
}

func (m Model) updateImport(msg tea.Msg) (tea.Model, tea.Cmd) {
	im := m.importer
	if key, ok := msg.(tea.KeyMsg); ok {
		k := key.String()
		if m.editing && m.focus == 3 && key.Type == tea.KeyRunes {
			if key.Paste {
				// Terminals may encode pasted newlines as CR or CRLF.
				text := strings.ReplaceAll(string(key.Runes), "\r\n", "\n")
				key.Runes = []rune(strings.ReplaceAll(text, "\r", "\n"))
				msg = key
			}
			if len(string(key.Runes)) > unit.MaxDocumentSize {
				m.notice = m.tr("服务内容不能超过 1 MiB")
				return m, nil
			}
			if err := unit.ValidateDocumentText(string(key.Runes)); err != nil {
				m.notice = m.language.Error(err)
				return m, nil
			}
		}
		if !m.editing && m.previewFocused {
			switch k {
			case "tab", "shift+tab", "esc":
				m.previewFocused = false
				return m, nil
			case "q":
				return m, tea.Quit
			}
			cmd := m.scrollPreview(msg)
			return m, cmd
		}
		if m.editing {
			switch k {
			case "esc":
				switch m.focus {
				case 1:
					im.path.SetValue(m.editOriginal)
				case 2:
					im.name.SetValue(m.editOriginal)
				}
				m.editing = false
			case "ctrl+s", "enter", "tab", "shift+tab":
				if m.focus != 3 || k == "ctrl+s" {
					if m.focus == 3 && im.body.Value() != m.editOriginal {
						im.content = im.body.Value()
					}
					m.editing = false
					if m.focus == 3 {
						m.goPage(1)
					}
				}
			}
			if !m.editing {
				im.path.Blur()
				im.name.Blur()
				im.body.Blur()
				m.refreshPreview()
				if k == "tab" || k == "shift+tab" {
					delta := 1
					if k == "shift+tab" {
						delta = -1
					}
					m.focus = 1 + (m.focus-1+delta+3)%3
					return m.updateImport(tea.KeyMsg{Type: tea.KeyEnter})
				}
				return m, nil
			}
		} else {
			switch k {
			case "esc", "ctrl+o":
				m.cancelImport()
				return m, nil
			case "q":
				return m, tea.Quit
			case "j", "down":
				m.focus = (m.focus + 1) % 4
			case "k", "up":
				m.focus = (m.focus + 3) % 4
			case "g", "home":
				m.focus = 0
			case "G", "end":
				m.focus = 3
			case "tab", "shift+tab":
				m.previewFocused = !m.previewFocused
			case "r":
				if !im.paste {
					return m, m.readImport()
				}
			case "enter", " ":
				m.notice = ""
				switch m.focus {
				case 0:
					im.paste = !im.paste
				case 1:
					if im.paste {
						m.focus = 3
						im.body.SetValue(im.content)
						m.editOriginal = im.body.Value()
						im.body.Focus()
					} else {
						m.editOriginal = im.path.Value()
						im.path.Focus()
					}
					m.editing = true
				case 2:
					m.editOriginal = im.name.Value()
					im.name.Focus()
					m.editing = true
				case 3:
					im.body.SetValue(im.content)
					m.editOriginal = im.body.Value()
					im.body.Focus()
					m.editing = true
				}
				if m.editing {
					return m, textinput.Blink
				}
			}

			return m, nil
		}
	}
	if !m.editing {
		return m, nil
	}
	var cmd tea.Cmd
	switch m.focus {
	case 1:
		im.path, cmd = im.path.Update(msg)
	case 2:
		im.name, cmd = im.name.Update(msg)
	case 3:
		im.body, cmd = im.body.Update(msg)
	}
	return m, cmd
}

func (m *Model) readImport() tea.Cmd {
	im := m.importer
	path := im.path.Value()
	im.loading = true
	m.notice = m.tr("正在读取服务文件…")
	return func() tea.Msg {
		d, err := service.ReadDocument(path)
		return importLoadedMsg{owner: im, document: d, err: err}
	}
}

func (m Model) importFieldsView(width, height int) string {
	im := m.importer
	if m.editing {
		content := im.body.View()
		if m.focus == 1 {
			content = im.path.View()
		}
		if m.focus == 2 {
			content = im.name.View()
		}
		return panel(m.tr("导入服务")+m.tr(" · 编辑中"), content, width, height, true)
	}
	source := m.tr("文件路径（r 读取）")
	if im.paste {
		source = m.tr("粘贴原文")
	}
	path := im.path.Value()
	if im.paste {
		path = m.tr("Enter 打开多行编辑器")
	}
	items := []string{m.tr("导入方式") + ": " + source, m.tr("文件路径") + ": " + path,
		m.tr("服务名称") + ": " + im.name.Value(), fmt.Sprintf(m.tr("服务原文 · %d 字节"), len(im.content))}
	if im.paste {
		items[1] = m.tr("粘贴原文") + ": " + path
	}
	available := max(1, height-3)
	start := max(0, m.focus-available+1)
	for i := range items {
		if i == m.focus {
			items[i] = activeStyle.Render("› " + items[i])
		} else {
			items[i] = "  " + items[i]
		}
	}
	content := strings.Join(items[start:min(len(items), start+available)], "\n")
	return panel(m.tr("导入服务"), content, width, height, !m.previewFocused)
}

func (m *Model) locateInvalidField() {
	if m.importer != nil {
		m.page, m.focus, m.previewFocused = 0, 3, false
		if (unit.Document{Name: m.importer.name.Value(), Content: "[Service]"}).Validate() != nil {
			m.focus = 2
		}
		return
	}
	_, err := m.document()
	if err == nil {
		return
	}
	text := err.Error()
	index := nameField
	if strings.Contains(text, "启动命令") {
		index = execField
	} else if strings.Contains(text, "User / Group") {
		index = userField
	} else if strings.Contains(text, "自定义指令") {
		m.advanced, m.page, m.focus, m.previewFocused = true, 3, 0, false
		return
	} else if strings.HasPrefix(text, "oneshot") {
		index = typeField
	} else {
		directives := []string{"Name", "Description", "ExecStart", "WorkingDirectory", "Type", "Restart", "RestartSec", "Environment", "EnvironmentFile", "User", "Group", "After", "Wants", "Requires", "ExecStop", "ExecReload", "TimeoutStartSec", "MemoryMax", "CPUQuota", "WantedBy"}
		for i, directive := range directives {
			if strings.HasPrefix(text, directive+" ") {
				index = i
				break
			}
		}
	}

	if index >= envFileField {
		m.advanced = true
	}
	for page := range len(m.pages()) - 1 {
		m.page = page
		for focus, field := range m.visibleFields() {
			if field == index {
				m.focus, m.previewFocused = focus, false
				return
			}
		}
	}
}
