package unit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
)

const MaxDocumentSize = 1024 * 1024

// Document retains native systemd syntax without adding generated defaults.
type Document struct {
	Name    string
	Content string
}

func (d Document) Filename() string { return Config{Name: d.Name}.Filename() }

func (d Document) Validate() error {
	if err := validateServiceName(d.Name); err != nil {
		return err
	}
	if len(d.Content) > MaxDocumentSize {
		return i18n.Errorf("服务内容不能超过 1 MiB")
	}
	if strings.TrimSpace(d.Content) == "" {
		return i18n.Errorf("请输入完整服务内容")
	}
	if err := ValidateDocumentText(d.Content); err != nil {
		return err
	}
	for line := range strings.SplitSeq(d.Content, "\n") {
		if strings.TrimSpace(line) == "[Service]" {
			return nil
		}
	}
	return i18n.Errorf("服务内容缺少 [Service] 段")
}

func validateServiceName(name string) error {
	if !namePattern.MatchString(strings.TrimSuffix(name, ".service")) || len((Config{Name: name}).Filename()) > 255 {
		return i18n.Errorf("服务名只能包含英文字母、数字、_、-、.，且不能以 . 或 - 开头")
	}
	return nil
}

func (c Config) Document(system bool) (Document, error) {
	content, err := c.Render(system)
	return Document{Name: c.Name, Content: content}, err
}

// ValidateDocumentText also checks pasted text before an editor can sanitize it.
func ValidateDocumentText(content string) error {
	if !utf8.ValidString(content) || strings.ContainsFunc(strings.ReplaceAll(content, "\r\n", "\n"), func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}) {
		return i18n.Errorf("服务内容必须是有效 UTF-8，且不能包含终端控制字符")
	}
	return nil
}
