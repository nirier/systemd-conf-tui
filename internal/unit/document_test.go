package unit

import (
	"strings"
	"testing"
)

func TestDocumentPreservesNativeSyntax(t *testing.T) {
	content := "# comment\r\n[Service]\r\nType=oneshot\r\nExecStart=\r\nExecStart=/usr/bin/echo \\\r\n\t hello\r\nExecStart=/usr/bin/true\r\n"
	d := Document{Name: "native.service", Content: content}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.Content != content || d.Filename() != "native.service" {
		t.Fatal("document changed")
	}
	c := Default()
	c.Name, c.ExecStart = "generated", "/usr/bin/true"
	d, err := c.Document(false)
	rendered, _ := c.Render(false)
	if err != nil || d.Content != rendered {
		t.Fatal("generated document changed", err)
	}
}

func TestDocumentValidation(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"../bad", "[Service]"}, {"worker@.service", "[Service]"},
		{"ok", ""}, {"ok", "[Unit]\nDescription=missing service"}, {"ok", "# [Service]\nExecStart=/bin/true"},
		{"ok", "[Service]\n\x1b[2J"}, {"ok", "[Service]\nExecStart=/bin/true\revil"}, {"ok", "[Service]\n\x00"}, {"ok", "[Service]\n\xff"},
		{"ok", "[Service]\n" + strings.Repeat("a", MaxDocumentSize)},
	} {
		if err := (Document{Name: tc.name, Content: tc.content}).Validate(); err == nil {
			t.Fatalf("accepted %.50q %.50q", tc.name, tc.content)
		}
	}
	d := Document{Name: "ok", Content: "[Service]\n#" + strings.Repeat("a", MaxDocumentSize-len("[Service]\n#"))}
	if err := d.Validate(); err != nil {
		t.Fatal("size boundary", err)
	}
}
