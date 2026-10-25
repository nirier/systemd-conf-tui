package service

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nirier/systemd-conf-tui/internal/unit"
)

func TestReadAndInstallDocumentSnapshot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "native.service")
	content := "# keep this\r\n[Service]\r\nType=oneshot\r\nExecStart=\r\nExecStart=/usr/bin/true\r\n"
	if err := os.WriteFile(source, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	document, err := ReadDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	result, err := InstallDocument(document, Options{}, func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "systemd-analyze" {
			data, err := os.ReadFile(args[len(args)-1])
			if err != nil || string(data) != content {
				t.Fatal("verification changed content", err)
			}
		}
		return "", nil
	})
	if err != nil || !result.Written {
		t.Fatal(result, err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil || string(data) != content {
		t.Fatal("installed content differs", err)
	}
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], []string{"systemctl", "--user", "daemon-reload"}) {
		t.Fatal("install-only must not enable or start", calls)
	}
	if data, _ := os.ReadFile(source); string(data) != "changed source" {
		t.Fatal("source was modified")
	}
}

func TestReadDocumentRejectsInvalidSources(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, content string }{
		{"bad.txt", "[Service]"}, {"empty.service", ""}, {"binary.service", "[Service]\n\x00"},
		{"large.service", "[Service]\n" + strings.Repeat("a", unit.MaxDocumentSize)},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadDocument(path); err == nil {
			t.Fatal("accepted", tc.name)
		}
	}
	for _, path := range []string{dir, filepath.Join(dir, "missing.service")} {
		if _, err := ReadDocument(path); err == nil {
			t.Fatal("accepted", path)
		}
	}
}

func TestImportedInstallGuardsAndPartialFailures(t *testing.T) {
	for _, phase := range []string{"verify", "daemon-reload", "enable", "start"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			document := unit.Document{Name: "native", Content: "[Service]\nExecStart=/usr/bin/true\n"}
			calls := 0
			result, err := InstallDocument(document, Options{Enable: true, Start: true}, func(name string, args ...string) (string, error) {
				calls++
				for _, arg := range args {
					if arg == phase {
						return "", errors.New("fake failure")
					}
				}
				return "", nil
			})
			if err == nil || result.Written != (phase != "verify") {
				t.Fatal(result, err)
			}
			if result.Written {
				before := calls
				if _, err := InstallDocument(document, Options{}, func(string, ...string) (string, error) { calls++; return "", nil }); err == nil || calls != before {
					t.Fatal("collision ran commands")
				}
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		dir, err := Directory(false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("missing", filepath.Join(dir, "native.service")); err != nil {
			t.Fatal(err)
		}
		if _, err := InstallDocument(unit.Document{Name: "native", Content: "[Service]"}, Options{}, func(string, ...string) (string, error) { t.Fatal("symlink must stop commands"); return "", nil }); err == nil {
			t.Fatal("accepted symlink")
		}
	})
	t.Run("sudo cancellation", func(t *testing.T) {
		calls := 0
		r, err := InstallDocument(unit.Document{Name: "systemd-conf-tui-" + filepath.Base(t.TempDir()), Content: "[Service]\nExecStart=/usr/bin/true"}, Options{System: true}, func(name string, args ...string) (string, error) {
			calls++
			if name == "sudo" {
				if !reflect.DeepEqual(args, []string{"-v"}) {
					t.Fatal("mutation after cancellation", args)
				}
				return "", errors.New("cancelled")
			}
			return "", nil
		})
		if err == nil || r.Written || calls != 2 {
			t.Fatal(r, err, calls)
		}
	})
}
