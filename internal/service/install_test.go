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

func config() unit.Config {
	c := unit.Default()
	c.Name, c.ExecStart = "test-app", "/usr/bin/true"
	return c
}

func TestUserInstall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var calls [][]string
	run := func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	r, err := Install(config(), Options{Enable: true, Start: true}, run)
	if err != nil || !r.Written {
		t.Fatalf("%+v %v", r, err)
	}
	data, err := os.ReadFile(r.Path)
	if err != nil || !strings.Contains(string(data), "ExecStart=/usr/bin/true") {
		t.Fatalf("%s %v", data, err)
	}
	if info, _ := os.Stat(r.Path); info.Mode().Perm() != 0644 {
		t.Fatal(info.Mode())
	}
	if len(calls) != 4 || calls[0][0] != "systemd-analyze" || calls[0][1] != "--user" {
		t.Fatal(calls)
	}
	want := [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "test-app.service"}, {"systemctl", "--user", "start", "test-app.service"}}
	if !reflect.DeepEqual(calls[1:], want) {
		t.Fatalf("got %v want %v", calls[1:], want)
	}
	_, err = Install(config(), Options{}, run)
	if err == nil || len(calls) != 4 {
		t.Fatal("existing file must stop all commands", err, calls)
	}
}

func TestInstallFailureState(t *testing.T) {
	for _, phase := range []string{"verify", "daemon-reload", "enable", "start"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var calls []string
			run := func(name string, args ...string) (string, error) {
				calls = append(calls, strings.Join(args, " "))
				for _, arg := range args {
					if arg == phase {
						return "", errors.New("fake failure")
					}
				}
				return "", nil
			}
			r, err := Install(config(), Options{Enable: true, Start: true}, run)
			if err == nil || r.Written != (phase != "verify") {
				t.Fatalf("%+v %v", r, err)
			}
			_, statErr := os.Stat(r.Path)
			if (statErr == nil) != r.Written {
				t.Fatalf("written flag must reflect disk: %v", statErr)
			}
			last := calls[len(calls)-1]
			if !strings.Contains(last, phase) {
				t.Fatal("continued after failure", calls)
			}
		})
	}
}

func TestWriteNewPreservesFilesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.service")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.service")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, link} {
		if err := WriteNew(path, []byte("replacement")); err == nil {
			t.Fatal("must not replace", path)
		}
	}
	got, _ := os.ReadFile(target)
	if string(got) != "original" {
		t.Fatal(string(got))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("staging files leaked", entries)
	}
}

func TestSystemInstallUsesSudoAndLiteralArgs(t *testing.T) {
	c := config()
	c.Name = "systemd-conf-tui-" + filepath.Base(t.TempDir())
	c.ExecStart = `/bin/sh -c 'echo $(touch /tmp/nope)'`
	var calls [][]string
	run := func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "sudo" && args[0] == "mktemp" {
			return "/etc/systemd/system/.systemd-conf-tui-test\n", nil
		}
		return "", nil
	}
	r, err := Install(c, Options{System: true, Enable: true, Start: true}, run)
	if err != nil || !r.Written {
		t.Fatalf("%+v %v", r, err)
	}
	for i, call := range calls {
		if i == 0 {
			if call[0] != "systemd-analyze" || call[1] != "verify" {
				t.Fatal(call)
			}
		} else if call[0] != "sudo" {
			t.Fatal("unprivileged system operation", call)
		}
		for _, arg := range call {
			if strings.Contains(arg, "touch") {
				t.Fatal("unit contents must never be shell arguments", call)
			}
		}
	}
	if calls[3][1] != "ln" || calls[3][2] != "-T" || calls[len(calls)-1][1] != "rm" {
		t.Fatal(calls)
	}
}

func TestSystemCollisionStopsAndCleansStage(t *testing.T) {
	c := config()
	c.Name = "systemd-conf-tui-" + filepath.Base(t.TempDir())
	var calls [][]string
	run := func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "sudo" {
			switch args[0] {
			case "mktemp":
				return "/etc/systemd/system/.systemd-conf-tui-test\n", nil
			case "ln":
				return "", os.ErrExist
			}
		}
		return "", nil
	}
	r, err := Install(c, Options{System: true}, run)
	if err == nil || r.Written || calls[len(calls)-1][1] != "rm" {
		t.Fatalf("%+v %v %v", r, err, calls)
	}
	for _, call := range calls {
		if len(call) > 1 && call[1] == "systemctl" {
			t.Fatal("systemctl must not run after collision", calls)
		}
	}
}
