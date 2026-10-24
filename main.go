package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/nirier/systemd-conf-tui/internal/tui"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

var version = "0.1.0"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("systemd-conf-tui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	system := flags.Bool("system", false, "以系统模式打开界面，或生成系统服务（默认用户模式）")
	render := flags.String("render", "", "从 JSON 文件生成 unit 到标准输出；- 表示标准输入，不安装服务")
	showVersion := flags.Bool("version", false, "显示版本")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), `systemd-conf-tui — 用几步创建 systemd 服务

用法：
  systemd-conf-tui                    打开用户服务创建向导
  systemd-conf-tui --system           打开系统服务向导
  systemd-conf-tui --render app.json  生成 unit 到标准输出，不修改系统
  systemd-conf-tui --system --render app.json

界面按键：F2 切换用户/系统，F3 高级配置，Ctrl+C 退出。
系统安装时才调用 sudo；请以普通用户运行本程序。

选项：`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "不支持位置参数；使用 --help 查看用法。")
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "systemd-conf-tui "+version)
		return 0
	}
	if *render != "" {
		var input io.Reader = os.Stdin
		if *render != "-" {
			f, err := os.Open(*render)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			defer f.Close()
			input = f
		}
		c, err := decodeConfig(input)
		if err == nil {
			var content string
			content, err = c.Render(*system)
			if err == nil {
				_, err = io.WriteString(stdout, content)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "服务安装界面仅支持 Linux；可使用 --render 生成 unit。")
		return 1
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(stderr, "交互界面需要终端。非交互用法：systemd-conf-tui --render app.json")
		return 1
	}
	final, err := tea.NewProgram(tui.New(*system), tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if m, ok := final.(tui.Model); ok {
		if summary := m.Summary(); summary != "" {
			fmt.Fprintln(stdout, summary)
		}
		if m.Failed() {
			return 1
		}
	}
	return 0
}

func decodeConfig(input io.Reader) (unit.Config, error) {
	c := unit.Default()
	// Bound configuration reads, including standard input.
	data, err := io.ReadAll(io.LimitReader(input, 1024*1024+1))
	if err != nil {
		return c, err
	}
	if len(data) > 1024*1024 {
		return c, fmt.Errorf("JSON 配置不能超过 1 MiB")
	}
	// Decode into a struct to reject misspelled directive names.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("JSON 配置无效: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("JSON 配置必须只包含一个对象")
	}
	return c, nil
}
