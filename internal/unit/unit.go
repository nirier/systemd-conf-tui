package unit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Config contains systemd directive values. Exec commands use systemd's syntax,
// so shell operators require an explicit shell in ExecStart.
type Config struct {
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	ExecStart        string `json:"exec_start"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Type             string `json:"type,omitempty"`
	Restart          string `json:"restart,omitempty"`
	RestartSec       string `json:"restart_sec,omitempty"`
	User             string `json:"user,omitempty"`
	Group            string `json:"group,omitempty"`
	Environment      string `json:"environment,omitempty"`
	EnvironmentFile  string `json:"environment_file,omitempty"`
	After            string `json:"after,omitempty"`
	Wants            string `json:"wants,omitempty"`
	Requires         string `json:"requires,omitempty"`
	ExecStop         string `json:"exec_stop,omitempty"`
	ExecReload       string `json:"exec_reload,omitempty"`
	TimeoutStartSec  string `json:"timeout_start_sec,omitempty"`
	MemoryMax        string `json:"memory_max,omitempty"`
	CPUQuota         string `json:"cpu_quota,omitempty"`
	WantedBy         string `json:"wanted_by,omitempty"`
	Extra            string `json:"extra,omitempty"`
}

func Default() Config {
	return Config{Type: "simple", Restart: "on-failure", RestartSec: "3s"}
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

var managedNamePattern = regexp.MustCompile(`^(?:[A-Za-z0-9_:.-]|\\x[0-9A-Fa-f]{2}|@)+\.(?:service|socket|target|device|mount|automount|swap|timer|path|slice|scope)$`)

// ValidateName accepts literal systemd unit names, never paths or patterns.
func ValidateName(name string) error {
	if len(name) > 255 || !managedNamePattern.MatchString(name) {
		return fmt.Errorf("无效的 unit 名称: %q", name)
	}
	return nil
}

func (c Config) Filename() string {
	return strings.TrimSuffix(c.Name, ".service") + ".service"
}

func (c Config) Validate(system bool) error {
	name := strings.TrimSuffix(c.Name, ".service")
	if !namePattern.MatchString(name) || len(c.Filename()) > 255 {
		return fmt.Errorf("服务名只能包含英文字母、数字、_、-、.，且不能以 . 或 - 开头")
	}
	if strings.TrimSpace(c.ExecStart) == "" {
		return fmt.Errorf("请输入启动命令，例如 /usr/bin/python3 /home/me/app.py")
	}
	if !system && (c.User != "" || c.Group != "") {
		return fmt.Errorf("用户模式不能设置 User / Group；清空这两个字段或按 F2 切换系统模式")
	}
	for _, field := range []struct{ name, value string }{
		{"Description", c.Description}, {"ExecStart", c.ExecStart},
		{"WorkingDirectory", c.WorkingDirectory}, {"Type", c.Type},
		{"Restart", c.Restart}, {"RestartSec", c.RestartSec},
		{"User", c.User}, {"Group", c.Group}, {"Environment", c.Environment},
		{"EnvironmentFile", c.EnvironmentFile}, {"After", c.After},
		{"Wants", c.Wants}, {"Requires", c.Requires}, {"ExecStop", c.ExecStop},
		{"ExecReload", c.ExecReload}, {"TimeoutStartSec", c.TimeoutStartSec},
		{"MemoryMax", c.MemoryMax}, {"CPUQuota", c.CPUQuota}, {"WantedBy", c.WantedBy},
	} {
		if strings.IndexFunc(field.value, unicode.IsControl) >= 0 || strings.HasSuffix(strings.TrimSpace(field.value), "\\") {
			return fmt.Errorf("%s 不允许控制字符或末尾的续行反斜杠", field.name)
		}
	}
	if c.Type != "" && !oneOf(c.Type, "simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle") {
		return fmt.Errorf("Type 请选择 simple / exec / forking / oneshot / dbus / notify / notify-reload / idle")
	}
	if c.Restart != "" && !oneOf(c.Restart, "no", "always", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort") {
		return fmt.Errorf("Restart 请选择 no / always / on-success / on-failure / on-abnormal / on-watchdog / on-abort")
	}
	if c.Type == "oneshot" && oneOf(c.Restart, "always", "on-success") {
		return fmt.Errorf("oneshot 服务不能使用 Restart=always 或 on-success")
	}
	return validateExtra(c.Extra)
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func validateExtra(extra string) error {
	section := false
	for i, line := range strings.Split(extra, "\n") {
		line = strings.TrimSpace(line)
		if strings.IndexFunc(line, unicode.IsControl) >= 0 {
			return fmt.Errorf("自定义指令第 %d 行包含控制字符", i+1)
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if oneOf(line, "[Unit]", "[Service]", "[Install]") {
			section = true
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !section || !ok || !keyPattern.MatchString(strings.TrimSpace(key)) || strings.HasSuffix(line, "\\") {
			return fmt.Errorf("自定义指令第 %d 行：请先写 [Unit] / [Service] / [Install]，随后每行写 Key=Value（不支持续行）", i+1)
		}
	}
	return nil
}

func (c Config) Render(system bool) (string, error) {
	if err := c.Validate(system); err != nil {
		return "", err
	}
	var b strings.Builder
	write := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s=%s\n", key, value)
		}
	}
	b.WriteString("[Unit]\n")
	write("Description", c.Description)
	write("After", c.After)
	write("Wants", c.Wants)
	write("Requires", c.Requires)
	b.WriteString("\n[Service]\n")
	typ := c.Type
	if typ == "" {
		typ = "simple"
	}
	write("Type", typ)
	write("ExecStart", c.ExecStart)
	write("WorkingDirectory", c.WorkingDirectory)
	write("Restart", c.Restart)
	write("RestartSec", c.RestartSec)
	write("User", c.User)
	write("Group", c.Group)
	write("Environment", c.Environment)
	write("EnvironmentFile", c.EnvironmentFile)
	write("ExecStop", c.ExecStop)
	write("ExecReload", c.ExecReload)
	write("TimeoutStartSec", c.TimeoutStartSec)
	write("MemoryMax", c.MemoryMax)
	write("CPUQuota", c.CPUQuota)
	b.WriteString("\n[Install]\n")
	target := c.WantedBy
	if target == "" {
		target = "default.target"
		if system {
			target = "multi-user.target"
		}
	}
	write("WantedBy", target)
	if strings.TrimSpace(c.Extra) != "" {
		b.WriteString("\n# Custom directives\n")
		b.WriteString(strings.TrimSpace(c.Extra))
		b.WriteByte('\n')
	}
	return b.String(), nil
}
