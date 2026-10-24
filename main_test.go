package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--help"}, 0, "--render app.json"},
		{[]string{"--version"}, 0, "systemd-conf-tui " + version},
		{[]string{"--unknown"}, 2, "flag provided but not defined"},
		{[]string{"unexpected"}, 2, "不支持位置参数"},
		{[]string{"--render", "examples/app.json"}, 0, "WantedBy=default.target"},
		{[]string{"--system", "--render", "examples/app.json"}, 0, "WantedBy=multi-user.target"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(tc.args, &out, &errOut); code != tc.code {
				t.Fatalf("exit=%d, stderr=%s", code, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), tc.want) {
				t.Fatalf("stdout=%s stderr=%s", out.String(), errOut.String())
			}
		})
	}
}

func TestDecodeConfig(t *testing.T) {
	for _, input := range []string{
		`{"name":"demo","exec_start":"/usr/bin/true","typo":true}`,
		`{} {}`,
		`null`,
		`[]`,
		strings.Repeat(" ", 1024*1024+1),
	} {
		c, err := decodeConfig(strings.NewReader(input))
		if err == nil {
			_, err = c.Render(false)
		}
		if err == nil {
			t.Fatalf("accepted invalid config: %.80s", input)
		}
	}
	c, err := decodeConfig(strings.NewReader(`{"name":"demo","exec_start":"/usr/bin/true"}`))
	if err != nil || c.Restart != "on-failure" || c.RestartSec != "3s" {
		t.Fatalf("%+v %v", c, err)
	}
}
