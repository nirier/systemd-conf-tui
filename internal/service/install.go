package service

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

// Run executes a program with literal arguments and returns its stdout.
type Run func(name string, args ...string) (string, error)

type Options struct {
	System bool
	Enable bool
	Start  bool
}

type Result struct {
	Path    string
	Written bool
}

func Directory(system bool) (string, error) {
	if system {
		return "/etc/systemd/system", nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", i18n.Errorf("XDG_CONFIG_HOME 必须是绝对路径")
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

// WriteNew publishes a complete file atomically and never replaces an existing
// file or symlink. The staging file lives on the destination filesystem.
func WriteNew(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".systemd-conf-tui-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), path); err != nil {
		return i18n.Errorf("无法创建 %s（已有文件不会覆盖）: %w", path, err)
	}
	return nil
}

func Install(c unit.Config, opts Options, run Run) (result Result, err error) {
	document, err := c.Document(opts.System)
	if err != nil {
		return result, err
	}
	return InstallDocument(document, opts, run)
}

func InstallDocument(document unit.Document, opts Options, run Run) (result Result, err error) {
	if err = document.Validate(); err != nil {
		return result, err
	}
	content := document.Content
	dir, err := Directory(opts.System)
	if err != nil {
		return result, err
	}
	result.Path = filepath.Join(dir, document.Filename())
	if _, statErr := os.Lstat(result.Path); statErr == nil {
		return result, i18n.Errorf("%s 已存在；请更换服务名，已有服务不会覆盖", result.Path)
	} else if !os.IsNotExist(statErr) {
		return result, statErr
	}
	tmp, err := os.MkdirTemp("", "systemd-conf-tui-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmp)
	source := filepath.Join(tmp, document.Filename())
	if err = os.WriteFile(source, []byte(content), 0600); err != nil {
		return result, err
	}
	verify := []string{"verify", source}
	if !opts.System {
		verify = append([]string{"--user"}, verify...)
	}
	if _, err = run("systemd-analyze", verify...); err != nil {
		return result, i18n.Errorf("unit 校验失败，尚未安装: %w", err)
	}
	if opts.System {
		if _, err = run("sudo", "-v"); err != nil {
			return result, err
		}
		// Stage as root on the target filesystem, then link without replacement.
		var staged string
		staged, err = run("sudo", "-n", "mktemp", filepath.Join(dir, ".systemd-conf-tui-XXXXXXXX"))
		if err != nil {
			return result, err
		}
		staged = strings.TrimSpace(staged)
		if filepath.Dir(staged) != dir || !strings.HasPrefix(filepath.Base(staged), ".systemd-conf-tui-") {
			return result, i18n.Errorf("sudo mktemp 返回了非预期路径")
		}
		defer func() {
			if _, cleanupErr := run("sudo", "-n", "rm", "-f", "--", staged); cleanupErr != nil && err == nil {
				err = i18n.Errorf("服务操作完成，但临时文件 %s 清理失败: %w", staged, cleanupErr)
			}
		}()
		if _, err = run("sudo", "-n", "install", "-m", "0644", "--", source, staged); err != nil {
			return result, err
		}
		if _, err = run("sudo", "-n", "ln", "-T", "--", staged, result.Path); err != nil {
			return result, i18n.Errorf("安装失败（已有文件不会覆盖）: %w", err)
		}
	} else {
		if err = os.MkdirAll(dir, 0755); err != nil {
			return result, err
		}
		if err = WriteNew(result.Path, []byte(content)); err != nil {
			return result, err
		}
	}
	result.Written = true
	actions := [][]string{{"daemon-reload"}}
	if opts.Enable {
		actions = append(actions, []string{"enable", document.Filename()})
	}
	if opts.Start {
		actions = append(actions, []string{"start", document.Filename()})
	}
	for _, args := range actions {
		name := "systemctl"
		if opts.System {
			name, args = "sudo", append([]string{"-n", "systemctl"}, args...)
		} else {
			args = append([]string{"--user"}, args...)
		}
		if _, err = run(name, args...); err != nil {
			return result, i18n.Errorf("文件已写入 %s，但 %s 失败；请根据错误修复后手动重试: %w", result.Path, strings.Join(args, " "), err)
		}
	}
	return result, nil
}

// ReadDocument bounds reads and opens nonblocking so a FIFO cannot stall the UI.
func ReadDocument(path string) (unit.Document, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return unit.Document{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return unit.Document{}, err
	}
	if !info.Mode().IsRegular() {
		return unit.Document{}, i18n.Errorf("只能读取普通 .service 文件")
	}
	if filepath.Ext(path) != ".service" {
		return unit.Document{}, i18n.Errorf("只能读取普通 .service 文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, unit.MaxDocumentSize+1))
	if err != nil {
		return unit.Document{}, err
	}
	document := unit.Document{Name: filepath.Base(path), Content: string(data)}
	// The destination name is editable, so validate content with a temporary name.
	check := document
	check.Name = "imported"
	if err := check.Validate(); err != nil {
		return unit.Document{}, err
	}
	return document, nil
}
