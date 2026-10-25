package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nirier/systemd-conf-tui/internal/i18n"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

func TestQuickPreviewAndValidationLocation(t *testing.T) {
	m := prepared()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if cmd != nil || !m.isPreview() {
		t.Fatal("first i must only open preview")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("second i should offer install")
	}
	m = New(false)
	m.fields[nameField].input.SetValue("demo")
	m = typeKeys(m, "i")
	if m.isPreview() || m.visibleFields()[m.focus] != execField || m.notice == "" {
		t.Fatal("missing command must be selected")
	}
	m = prepared()
	m.fields[environmentField].input.SetValue("bad\\")
	m = typeKeys(m, "i")
	if m.page != 1 || m.visibleFields()[m.focus] != environmentField {
		t.Fatal("invalid runtime field not selected")
	}
	m = press(prepared(), tea.KeyEnter)
	m = typeKeys(m, "i")
	if !m.editing || m.isPreview() {
		t.Fatal("i must be text while editing")
	}
	m = press(m, tea.KeyCtrlO)
	if m.importer != nil {
		t.Fatal("editing must isolate import shortcut")
	}
}

func TestFileImportAndDraftRestore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "native.service")
	content := "# keep\r\n[Service]\r\nExecStart=/usr/bin/echo \\\r\n\t hello\r\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	m := prepared()
	m.goPage(2)
	m.enable = false
	m.choice = 1
	m = press(m, tea.KeyCtrlO)
	if m.importer == nil || m.enable || m.start {
		t.Fatal("import defaults must be install only")
	}
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, path)
	m = press(m, tea.KeyEnter)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(Model)
	if cmd == nil || !m.importer.loading {
		t.Fatal("expected asynchronous read")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.importer.loading || m.importer.name.Value() != "native.service" {
		t.Fatal("file name not filled")
	}
	d, err := m.document()
	if err != nil || d.Content != content {
		t.Fatal("file bytes changed", err)
	}
	m = typeKeys(m, "i")
	if !m.isPreview() {
		t.Fatal("import preview missing")
	}
	m = press(m, tea.KeyF2)
	d, _ = m.document()
	if !m.system || d.Content != content {
		t.Fatal("mode switch rewrote native content")
	}
	m = press(m, tea.KeyEsc)
	m = typeKeys(m, "G")
	m = press(m, tea.KeyEnter)
	m = press(m, tea.KeyCtrlS)
	d, err = m.document()
	if !m.isPreview() || err != nil || d.Content != content {
		t.Fatal("unchanged editor must retain original bytes", err)
	}
	m = press(m, tea.KeyEsc)
	m = press(m, tea.KeyEsc)
	if m.importer != nil || !m.isPreview() || m.config().Name != "demo" || m.enable || !m.start || m.choice != 1 {
		t.Fatal("draft or options lost")
	}
}

func TestPasteImportSaveCancelAndExport(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	m := press(prepared(), tea.KeyCtrlO)
	m = press(m, tea.KeyEnter) // select paste
	m = typeKeys(m, "jj")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "pasted")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "j")
	m = press(m, tea.KeyEnter)
	content := "# native\n[Service]\nType=oneshot\nExecStart=/usr/bin/true\nExecStart=/usr/bin/true\n"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(content), Paste: true})
	m = next.(Model)
	if !m.editing || m.isPreview() {
		t.Fatal("paste triggered navigation")
	}
	m = press(m, tea.KeyCtrlS)
	d, err := m.document()
	if err != nil || !m.isPreview() || d.Content != content {
		t.Fatal("paste not preserved", err, d.Content)
	}
	m = typeKeys(m, "e")
	data, err := os.ReadFile("pasted.service")
	if err != nil || string(data) != content {
		t.Fatal("export mismatch", err)
	}
	m = press(m, tea.KeyEsc)
	m = typeKeys(m, "G")
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "changed")
	m = press(m, tea.KeyEsc)
	d, _ = m.document()
	if d.Content != content || m.editing {
		t.Fatal("cancel changed saved content")
	}
	m = press(m, tea.KeyEnter)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[2J"), Paste: true})
	m = next.(Model)
	if m.notice == "" || strings.Contains(m.importer.body.Value(), "[2J") {
		t.Fatal("unsafe paste was sanitized silently")
	}
}

func TestImportReadFailureKeepsContentAndName(t *testing.T) {
	m := press(prepared(), tea.KeyCtrlO)
	m.importer.name.SetValue("custom")
	m.importer.content = "[Service]\nExecStart=/usr/bin/true\n"
	m.importer.path.SetValue(filepath.Join(t.TempDir(), "missing.service"))
	cmd := m.readImport()
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.notice == "" || m.importer.name.Value() != "custom" || m.importer.content == "" {
		t.Fatal("read failure lost inputs")
	}
	// A completion from a previous import must not affect a new session.
	owner := m.importer
	m.cancelImport()
	m.openImport()
	next, _ = m.Update(importLoadedMsg{owner: owner, document: unit.Document{Name: "stale", Content: "[Service]"}})
	m = next.(Model)
	if m.importer.content != "" {
		t.Fatal("stale read changed new draft")
	}
}

func TestImportedViewsFitAndTranslate(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, size := range [][2]int{{48, 20}, {96, 20}, {120, 36}} {
			m := press(prepared(), tea.KeyCtrlO)
			m.width, m.height = size[0], size[1]
			m.setLanguage(language)
			check := func() {
				t.Helper()
				assertFits(t, m)
				if language == i18n.English {
					for _, r := range m.View() {
						if unicode.Is(unicode.Han, r) {
							t.Fatal("untranslated import UI", m.View())
						}
					}
				}
			}
			m.importer.name.SetValue("native")
			m.importer.content = "[Service]\n" + strings.Repeat("Environment=PORT=8080\n", 110)
			for _, focus := range []int{0, 1, 2, 3} {
				m.focus = focus
				check()
				next, _ := m.updateImport(tea.KeyMsg{Type: tea.KeyEnter})
				m = next.(Model)
				check()
				if m.editing {
					m = press(m, tea.KeyEsc)
				}
			}
			m = typeKeys(m, "i")
			check()
			m = press(m, tea.KeyTab)
			m = typeKeys(m, "G")
			check()
			if m.preview.ScrollPercent() != 1 {
				t.Fatal("long import preview cannot reach end")
			}
			m = press(m, tea.KeyEsc)
			m = press(m, tea.KeyEsc)
			m = typeKeys(m, "?")
			check()
		}
	}
}

func TestImportTabEditingAndContentLimit(t *testing.T) {
	m := press(prepared(), tea.KeyCtrlO)
	m.focus = 2
	m = press(m, tea.KeyEnter)
	m = typeKeys(m, "native")
	m = press(m, tea.KeyTab)
	if !m.editing || m.focus != 3 || m.importer.name.Value() != "native" {
		t.Fatal("Tab must save the name and open the content editor")
	}
	content := "[Service]\n" + strings.Repeat("# comment\n", 110)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(content), Paste: true})
	m = press(next.(Model), tea.KeyCtrlS)
	document, err := m.document()
	if err != nil || !m.isPreview() || document.Content != content {
		t.Fatal("multiline content was truncated", err)
	}
	m = press(m, tea.KeyEsc)
	m.focus = 3
	m = press(m, tea.KeyEnter)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("a", unit.MaxDocumentSize+1)), Paste: true})
	m = next.(Model)
	if m.notice == "" || m.importer.body.Value() != content {
		t.Fatal("oversized paste must be rejected without changing the editor")
	}
}

func TestImportTerminalPasteLineEndings(t *testing.T) {
	content := "[Service]\nType=oneshot\nExecStart=/usr/bin/true\n"
	for _, separator := range []string{"\r", "\r\n"} {
		m := press(prepared(), tea.KeyCtrlO)
		m.focus = 2
		m = press(m, tea.KeyEnter)
		m = typeKeys(m, "native")
		m = press(m, tea.KeyTab)
		pasted := strings.ReplaceAll(content, "\n", separator)
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
		m = press(next.(Model), tea.KeyCtrlS)
		document, err := m.document()
		if err != nil || !m.isPreview() || document.Content != content {
			t.Fatalf("paste with %q line endings failed: %v %q", separator, err, document.Content)
		}
	}
}

func TestLongInstallationPathIsFullyPreviewed(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("long-path-", 8))
	t.Setenv("XDG_CONFIG_HOME", root)
	m := press(prepared(), tea.KeyCtrlO)
	m.width, m.height = 48, 20
	m.importer.name.SetValue("native")
	m.importer.content = "[Service]\nExecStart=/usr/bin/true\n"
	m.resize()
	m = typeKeys(m, "i")
	view := strings.Join(strings.Fields(m.preview.View()), "")
	path := filepath.Join(root, "systemd", "user", "native.service")
	if !strings.Contains(view, path) {
		t.Fatal("target path is truncated", m.preview.View())
	}
}
