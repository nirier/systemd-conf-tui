package unit

import (
	"strings"
	"testing"
)

func validConfig() Config {
	c := Default()
	c.Name, c.ExecStart = "demo", "/usr/bin/true"
	return c
}

func TestRenderScopesAndDirectives(t *testing.T) {
	for _, system := range []bool{false, true} {
		c := validConfig()
		c.Description = "测试服务"
		c.Environment = `"GREETING=hello world" "PORT=8080"`
		c.Extra = "[Service]\nNoNewPrivileges=yes\n\n[Unit]\nStartLimitBurst=5"
		if system {
			c.User = "nobody"
		}
		got, err := c.Render(system)
		if err != nil {
			t.Fatal(err)
		}
		target := "default.target"
		if system {
			target = "multi-user.target"
		}
		for _, want := range []string{"[Unit]\n", "Description=测试服务\n", "[Service]\nType=simple\nExecStart=/usr/bin/true\n", "Environment=" + c.Environment + "\n", "WantedBy=" + target + "\n", "NoNewPrivileges=yes\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("system=%t: missing %q in %s", system, want, got)
			}
		}
	}
}

func TestRejectUnsafeOrInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"traversal", func(c *Config) { c.Name = "../evil" }},
		{"option name", func(c *Config) { c.Name = "--system" }},
		{"instance template", func(c *Config) { c.Name = "demo@" }},
		{"empty exec", func(c *Config) { c.ExecStart = " " }},
		{"newline injection", func(c *Config) { c.Description = "demo\nExecStart=/bin/false" }},
		{"terminal escape", func(c *Config) { c.ExecStart = "/bin/true\x1b[2J" }},
		{"continuation", func(c *Config) { c.ExecStart = "/bin/true\\" }},
		{"padded continuation", func(c *Config) { c.Description = "demo\\  " }},
		{"user identity", func(c *Config) { c.User = "root" }},
		{"unknown type", func(c *Config) { c.Type = "typo" }},
		{"oneshot restart", func(c *Config) { c.Type, c.Restart = "oneshot", "always" }},
		{"extra missing section", func(c *Config) { c.Extra = "ExecStart=/bin/false" }},
		{"extra invalid section", func(c *Config) { c.Extra = "[Socket]\nListenStream=80" }},
		{"extra continuation", func(c *Config) { c.Extra = "[Service]\nExecStart=/bin/true\\" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.change(&c)
			if _, err := c.Render(false); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNativeSyntaxAndDefaults(t *testing.T) {
	c := Config{Name: "demo.service", ExecStart: `/bin/sh -c 'echo "$HOME" | cat'`, WantedBy: "custom.target"}
	c.Extra = "[Service]\nExecStart=\nExecStart=/usr/bin/true\nRemainAfterExit=yes"
	got, err := c.Render(false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Filename() != "demo.service" || !strings.Contains(got, "WantedBy=custom.target") || !strings.Contains(got, "ExecStart="+c.ExecStart) {
		t.Fatal(got)
	}
}
