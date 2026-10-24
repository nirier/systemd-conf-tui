package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		return "", fmt.Errorf("XDG_CONFIG_HOME 必须是绝对路径")
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
		return fmt.Errorf("无法创建 %s（已有文件不会覆盖）: %w", path, err)
	}
	return nil
}

func Install(c unit.Config, opts Options, run Run) (result Result, err error) {
	content, err := c.Render(opts.System)
	if err != nil {
		return result, err
	}
	dir, err := Directory(opts.System)
	if err != nil {
		return result, err
	}
	result.Path = filepath.Join(dir, c.Filename())
	if _, statErr := os.Lstat(result.Path); statErr == nil {
		return result, fmt.Errorf("%s 已存在；请更换服务名，已有服务不会覆盖", result.Path)
	} else if !os.IsNotExist(statErr) {
		return result, statErr
	}
	tmp, err := os.MkdirTemp("", "systemd-conf-tui-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmp)
	source := filepath.Join(tmp, c.Filename())
	if err = os.WriteFile(source, []byte(content), 0600); err != nil {
		return result, err
	}
	verify := []string{"verify", source}
	if !opts.System {
		verify = append([]string{"--user"}, verify...)
	}
	if _, err = run("systemd-analyze", verify...); err != nil {
		return result, fmt.Errorf("unit 校验失败，尚未安装: %w", err)
	}
	if opts.System {
		// Stage as root on the target filesystem, then link without replacement.
		var staged string
		staged, err = run("sudo", "mktemp", filepath.Join(dir, ".systemd-conf-tui-XXXXXXXX"))
		if err != nil {
			return result, err
		}
		staged = strings.TrimSpace(staged)
		if filepath.Dir(staged) != dir || !strings.HasPrefix(filepath.Base(staged), ".systemd-conf-tui-") {
			return result, fmt.Errorf("sudo mktemp 返回了非预期路径")
		}
		defer func() {
			if _, cleanupErr := run("sudo", "rm", "-f", "--", staged); cleanupErr != nil && err == nil {
				err = fmt.Errorf("服务操作完成，但临时文件 %s 清理失败: %w", staged, cleanupErr)
			}
		}()
		if _, err = run("sudo", "install", "-m", "0644", "--", source, staged); err != nil {
			return result, err
		}
		if _, err = run("sudo", "ln", "-T", "--", staged, result.Path); err != nil {
			return result, fmt.Errorf("安装失败（已有文件不会覆盖）: %w", err)
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
		actions = append(actions, []string{"enable", c.Filename()})
	}
	if opts.Start {
		actions = append(actions, []string{"start", c.Filename()})
	}
	for _, args := range actions {
		name := "systemctl"
		if opts.System {
			name, args = "sudo", append([]string{"systemctl"}, args...)
		} else {
			args = append([]string{"--user"}, args...)
		}
		if _, err = run(name, args...); err != nil {
			return result, fmt.Errorf("文件已写入 %s，但 %s 失败；请根据错误修复后手动重试: %w", result.Path, strings.Join(args, " "), err)
		}
	}
	return result, nil
}
