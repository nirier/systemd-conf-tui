package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/nirier/systemd-conf-tui/internal/i18n"
	"github.com/nirier/systemd-conf-tui/internal/unit"
)

func TestEnglishManagementPreviewRecheckAndPartialResult(t *testing.T) {
	f := newManagementFixture(t)
	// Unit contents and descriptions are literal data even when they match UI text.
	f.content["demo.service"] = "[Unit]\nDescription=服务名称\n[Service]\nExecStart=/usr/bin/true\n"
	plan, err := PrepareManagementInLanguage("demo.service", Delete, false, f.run, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Command preview", "Action: delete", "Description=服务名称", filepath.Join(f.dir, "demo.service")} {
		if !strings.Contains(plan.Preview, want) {
			t.Fatal("preview lost translated message or literal unit data", plan.Preview)
		}
	}
	f.fail = "daemon-reload"
	result, err := ExecuteManagement(plan, f.run) // Temporary files and mocked commands only.
	if err == nil || len(result.Removed) != 1 || !strings.Contains(i18n.English.Error(err), "Configuration deleted, but daemon-reload failed") {
		t.Fatal("localized recheck or partial result failed", result, err)
	}
	if !strings.Contains(result.LocalizedString(i18n.English), "Removed: ") {
		t.Fatal("removed files missing from English result")
	}
}

type managementFixture struct {
	t         *testing.T
	dir       string
	props     map[string]map[string]string
	content   map[string]string
	installed []map[string]string
	loaded    []map[string]string
	calls     [][]string
	paths     []string
	fail      string
}

func newManagementFixture(t *testing.T) *managementFixture {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := Directory(false)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f := &managementFixture{t: t, dir: dir, props: map[string]map[string]string{}, content: map[string]string{}, installed: []map[string]string{}, loaded: []map[string]string{}, paths: []string{dir}}
	f.add("demo.service", "disabled")
	if err = os.WriteFile(filepath.Join(dir, "demo.service"), []byte("[Service]\nExecStart=/usr/bin/true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *managementFixture) add(name, state string) map[string]string {
	p := map[string]string{"Id": name, "Names": name, "LoadState": "loaded", "ActiveState": "inactive", "UnitFileState": state, "FragmentPath": filepath.Join(f.dir, name), "NeedDaemonReload": "no"}
	f.props[name] = p
	f.installed = append(f.installed, map[string]string{"unit_file": name, "state": state})
	return p
}
func (f *managementFixture) run(name string, args ...string) (string, error) {
	f.t.Helper()
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if name == "systemd-analyze" {
		return strings.Join(f.paths, "\n"), nil
	}
	if name != "systemctl" && name != "sudo" {
		f.t.Fatalf("unexpected command %v", call)
	}
	verbs := map[string]bool{"list-unit-files": true, "list-units": true, "show": true, "cat": true, "disable": true, "mask": true, "daemon-reload": true}
	verb := ""
	var rest []string
	for i, arg := range args {
		if verbs[arg] {
			verb = arg
			rest = args[i+1:]
			break
		}
	}
	if verb == "" {
		f.t.Fatalf("unknown command %v", call)
	}
	if verb == f.fail {
		return "", errors.New("fake " + verb + " failure")
	}
	switch verb {
	case "list-unit-files":
		b, _ := json.Marshal(f.installed)
		return string(b), nil
	case "list-units":
		b, _ := json.Marshal(f.loaded)
		return string(b), nil
	case "show":
		var blocks []string
		for _, n := range rest {
			if strings.HasPrefix(n, "-") {
				continue
			}
			if unit.IsTemplate(n) {
				return "", errors.New("templates cannot be loaded")
			}
			p, ok := f.props[n]
			if !ok {
				return "", errors.New("unknown unit " + n)
			}
			var keys []string
			for key := range p {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			var lines []string
			for _, key := range keys {
				lines = append(lines, key+"="+p[key])
			}
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
		return strings.Join(blocks, "\n\n"), nil
	case "cat":
		n := rest[len(rest)-1]
		if content, ok := f.content[n]; ok {
			return content, nil
		}
		p := f.props[n]
		data, err := os.ReadFile(p["FragmentPath"])
		if err != nil {
			data = []byte("[Unit]\nDescription=fixture\n")
		}
		return "# " + p["FragmentPath"] + "\n" + string(data), nil
	}
	return "", nil
}
func (f *managementFixture) mutations() [][]string {
	var result [][]string
	for _, call := range f.calls {
		for _, arg := range call {
			if arg == "disable" || arg == "mask" || arg == "daemon-reload" || arg == "rm" || arg == "rmdir" {
				result = append(result, call)
				break
			}
		}
	}
	return result
}

func TestListManagedUnits(t *testing.T) {
	for _, system := range []bool{false, true} {
		t.Run(map[bool]string{false: "user", true: "system"}[system], func(t *testing.T) {
			f := newManagementFixture(t)
			for _, name := range []string{"-.mount", "-.slice", "wake.timer", "listen.socket", "watch.path", "batch@.service", `dev-disk-by\x2duuid.device`} {
				f.add(name, "static")
			}
			f.loaded = []map[string]string{{"unit": "demo.service", "active": "active", "description": "示例"}, {"unit": "session.scope", "active": "active"}}
			units, err := ListUnits(system, f.run)
			if err != nil {
				t.Fatal(err)
			}
			if len(units) != 9 {
				t.Fatal(units)
			}
			for i, u := range units {
				if i > 0 && units[i-1].Name >= u.Name {
					t.Fatal("not sorted", units)
				}
				if u.Name == "demo.service" && (u.State != "disabled" || u.Active != "active" || u.Description != "示例") {
					t.Fatal(u)
				}
			}
			for _, call := range f.calls {
				if call[0] != "systemctl" || contains(call, "--user") == system {
					t.Fatalf("wrong query privilege/scope %v", call)
				}
			}
		})
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func TestManagementRejectsIncompleteLists(t *testing.T) {
	for _, input := range []string{"null", "{}", "invalid", `[{"unit_file":"../evil.service"}]`} {
		_, err := ListUnits(false, func(string, ...string) (string, error) { return input, nil })
		if err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestDisableAndMaskPrivilegesAndConfirmation(t *testing.T) {
	for _, system := range []bool{false, true} {
		for _, action := range []Action{Disable, Mask} {
			t.Run(string(action)+map[bool]string{false: "/user", true: "/system"}[system], func(t *testing.T) {
				f := newManagementFixture(t)
				plan, err := PrepareManagement("demo.service", action, system, f.run)
				if err != nil {
					t.Fatal(err)
				}
				if len(f.mutations()) != 0 || !strings.Contains(plan.Preview, "ExecStart=/usr/bin/true") {
					t.Fatal("preview must be complete and read-only", plan, f.calls)
				}
				result, err := ExecuteManagement(plan, f.run)
				if err != nil || len(result.Completed) != 1 {
					t.Fatal(result, err)
				}
				mutations := f.mutations()
				if len(mutations) != 1 {
					t.Fatal(mutations)
				}
				want := []string{"systemctl", "--no-pager", "--no-ask-password", "--user", string(action), "--", "demo.service"}
				if system {
					want = []string{"sudo", "-n", "systemctl", "--no-pager", "--no-ask-password", string(action), "--", "demo.service"}
				}
				if !reflect.DeepEqual(mutations[0], want) {
					t.Fatalf("got %v want %v", mutations[0], want)
				}
				for _, call := range f.calls {
					if call[0] == "sudo" && !contains(call, string(action)) {
						t.Fatal("query elevated", call)
					}
				}
			})
		}
	}
}

func TestInvalidManagementNameNeverExecutes(t *testing.T) {
	for _, name := range []string{"../demo.service", "demo.service;touch /tmp/nope", "*.service", "demo\n.service", "demo.service/other", "$(touch x).service"} {
		called := false
		_, err := PrepareManagement(name, Delete, false, func(string, ...string) (string, error) { called = true; return "", nil })
		if err == nil || called {
			t.Fatalf("invalid name %q reached runner", name)
		}
	}
}

func TestDeleteUnitAndDedicatedDropIns(t *testing.T) {
	f := newManagementFixture(t)
	drop := filepath.Join(f.dir, "demo.service.d")
	if err := os.Mkdir(drop, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(drop, "override.conf"), []byte("[Service]\nRestart=no\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Shared type-wide drop-ins must survive.
	shared := filepath.Join(f.dir, "service.d")
	if err := os.Mkdir(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "shared.conf"), []byte("[Service]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareManagement("demo.service", Delete, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) != 2 || len(f.mutations()) != 0 {
		t.Fatal(plan, f.calls)
	}
	for _, command := range []string{
		"rm -- '" + filepath.Join(f.dir, "demo.service") + "'",
		"rm -- '" + filepath.Join(drop, "override.conf") + "'",
		"rmdir -- '" + drop + "'",
		"systemctl --no-pager --no-ask-password --user daemon-reload",
	} {
		if !strings.Contains(plan.Preview, command) {
			t.Fatalf("missing deletion command %q in preview:\n%s", command, plan.Preview)
		}
	}
	if !strings.HasPrefix(plan.Preview, "命令预览") || !strings.Contains(plan.Preview, "等效命令") {
		t.Fatal("preview must lead with commands and explain user file operations", plan.Preview)
	}
	result, err := ExecuteManagement(plan, f.run)
	if err != nil {
		t.Fatal(result, err)
	}
	if len(result.Removed) != 2 || len(result.Completed) != 1 {
		t.Fatal(result)
	}
	if _, err = os.Lstat(filepath.Join(f.dir, "demo.service")); !os.IsNotExist(err) {
		t.Fatal("unit not removed", err)
	}
	if _, err = os.Lstat(drop); !os.IsNotExist(err) {
		t.Fatal("drop-in directory not removed", err)
	}
	if _, err = os.Stat(filepath.Join(shared, "shared.conf")); err != nil {
		t.Fatal("shared drop-in changed", err)
	}
	if got := f.mutations(); len(got) != 1 || !contains(got[0], "daemon-reload") {
		t.Fatal(got)
	}
}

func TestDeletionCommandPreviewQuotesPathsAndShowsSystemScope(t *testing.T) {
	dir := "/etc/config space'$(id)/systemd/system"
	plan := ManagementPlan{
		Name: "demo.service", System: true,
		Paths: []string{filepath.Join(dir, "demo.service")},
	}
	preview := plan.deletionCommandPreview(dir)
	want := "命令预览（省略只读检查命令）：\nsudo -v\n# 配置与依赖复检通过后：\n" +
		"sudo -n rm -- '/etc/config space'\\''$(id)/systemd/system/demo.service'\n" +
		"sudo -n systemctl --no-pager --no-ask-password daemon-reload"
	if preview != want {
		t.Fatalf("got:\n%s\nwant:\n%s", preview, want)
	}
	plan.dropInDir = true
	preview = plan.deletionCommandPreview(dir)
	if !strings.Contains(preview, "sudo -n rmdir -- '/etc/config space'\\''$(id)/systemd/system/demo.service.d'") {
		t.Fatal("missing quoted drop-in directory command", preview)
	}
}

func TestDeleteDependencyGuards(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*managementFixture)
	}{
		{"active", "请先停止", func(f *managementFixture) { f.props["demo.service"]["ActiveState"] = "active" }},
		{"enabled", "请先 disable", func(f *managementFixture) { f.props["demo.service"]["UnitFileState"] = "enabled" }},
		{"pending job", "待处理任务", func(f *managementFixture) { f.props["demo.service"]["Job"] = "42" }},
		{"loaded reverse", "wake.timer", func(f *managementFixture) { f.props["demo.service"]["TriggeredBy"] = "wake.timer" }},
		{"unloaded timer", "wake.timer", func(f *managementFixture) { f.add("wake.timer", "disabled")["Triggers"] = "demo.service" }},
		{"alias name dependency", "client.service", func(f *managementFixture) {
			f.props["demo.service"]["Names"] = "demo.service alias.service"
			f.add("client.service", "disabled")["Requires"] = "alias.service"
		}},
		{"alias file", "alias.service", func(f *managementFixture) {
			p := f.add("alias.service", "alias")
			p["Id"] = "demo.service"
			p["FragmentPath"] = f.props["demo.service"]["FragmentPath"]
		}},
		{"inactive instance", "demo@one.service", func(f *managementFixture) {
			p := f.add("demo@one.service", "disabled")
			p["FragmentPath"] = f.props["demo.service"]["FragmentPath"]
		}},
		{"uninstantiated template", "worker@.service", func(f *managementFixture) {
			f.add("worker@.service", "disabled")
			f.content["worker@.service"] = "[Unit]\nRequires=demo.service\n"
		}},
		{"specifier template", "worker@.service", func(f *managementFixture) {
			f.add("worker@.service", "disabled")
			f.content["worker@.service"] = "[Unit]\nWants=%i.service\n"
		}},
		{"stale target", "daemon-reload", func(f *managementFixture) { f.props["demo.service"]["NeedDaemonReload"] = "yes" }},
		{"stale dependent", "daemon-reload", func(f *managementFixture) { f.add("client.service", "disabled")["NeedDaemonReload"] = "yes" }},
		{"broken dependent", "加载失败", func(f *managementFixture) { f.add("client.service", "disabled")["LoadState"] = "bad-setting" }},
		{"query failure", "fake list-unit-files", func(f *managementFixture) { f.fail = "list-unit-files" }},
		{"missing properties", "daemon-reload", func(f *managementFixture) { delete(f.props["demo.service"], "NeedDaemonReload") }},
	}
	for _, relation := range []string{"Requires", "Wants", "Requisite", "BindsTo", "Upholds", "PartOf", "OnFailure", "OnSuccess", "PropagatesStopTo", "PropagatesReloadTo", "JoinsNamespaceOf"} {
		cases = append(cases, struct {
			name, want string
			change     func(*managementFixture)
		}{relation, relation, func(f *managementFixture) { f.add("client.service", "disabled")[relation] = "demo.service" }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newManagementFixture(t)
			tc.change(f)
			_, err := PrepareManagement("demo.service", Delete, false, f.run)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
			if len(f.mutations()) != 0 {
				t.Fatal("guard mutated state", f.calls)
			}
			if _, err = os.Stat(filepath.Join(f.dir, "demo.service")); err != nil {
				t.Fatal("guard deleted unit", err)
			}
		})
	}
}

func TestDeleteFileGuards(t *testing.T) {
	cases := []struct {
		name   string
		change func(*managementFixture)
	}{
		{"vendor unit", func(f *managementFixture) {
			f.props["demo.service"]["FragmentPath"] = "/usr/lib/systemd/user/demo.service"
		}},
		{"generated unit", func(f *managementFixture) { f.props["demo.service"]["SourcePath"] = "/etc/example.conf" }},
		{"main symlink", func(f *managementFixture) {
			path := filepath.Join(f.dir, "demo.service")
			os.Remove(path)
			if err := os.Symlink("elsewhere.service", path); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"drop-in directory symlink", func(f *managementFixture) {
			if err := os.Symlink(f.t.TempDir(), filepath.Join(f.dir, "demo.service.d")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"drop-in file symlink", func(f *managementFixture) {
			dir := filepath.Join(f.dir, "demo.service.d")
			os.Mkdir(dir, 0755)
			if err := os.Symlink("/etc/passwd", filepath.Join(dir, "override.conf")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"unrelated file in drop-in directory", func(f *managementFixture) {
			dir := filepath.Join(f.dir, "demo.service.d")
			os.Mkdir(dir, 0755)
			if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0644); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"underlying vendor config", func(f *managementFixture) {
			dir := f.t.TempDir()
			f.paths = append(f.paths, dir)
			if err := os.WriteFile(filepath.Join(dir, "demo.service"), []byte("vendor"), 0644); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"runtime mask hides local file", func(f *managementFixture) {
			f.props["demo.service"]["LoadState"] = "masked"
			f.props["demo.service"]["FragmentPath"] = "/dev/null"
			f.props["demo.service"]["UnitFileState"] = "masked-runtime"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newManagementFixture(t)
			tc.change(f)
			if _, err := PrepareManagement("demo.service", Delete, false, f.run); err == nil {
				t.Fatal("unsafe file accepted")
			}
			if len(f.mutations()) != 0 {
				t.Fatal(f.calls)
			}
		})
	}
}

func TestDeleteRechecksAfterConfirmation(t *testing.T) {
	for _, change := range []string{"dependency", "file", "drop-in", "state"} {
		t.Run(change, func(t *testing.T) {
			f := newManagementFixture(t)
			plan, err := PrepareManagement("demo.service", Delete, false, f.run)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "dependency":
				f.add("new.timer", "disabled")["Triggers"] = "demo.service"
			case "file":
				if err = os.WriteFile(filepath.Join(f.dir, "demo.service"), []byte("modified"), 0644); err != nil {
					t.Fatal(err)
				}
			case "drop-in":
				dir := filepath.Join(f.dir, "demo.service.d")
				os.Mkdir(dir, 0755)
				if err = os.WriteFile(filepath.Join(dir, "new.conf"), []byte("[Service]"), 0644); err != nil {
					t.Fatal(err)
				}
			case "state":
				f.props["demo.service"]["ActiveState"] = "active"
			}
			if _, err = ExecuteManagement(plan, f.run); err == nil {
				t.Fatal("changed plan executed")
			}
			if len(f.mutations()) != 0 {
				t.Fatal(f.calls)
			}
			if _, err = os.Stat(filepath.Join(f.dir, "demo.service")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteReportsReloadFailure(t *testing.T) {
	f := newManagementFixture(t)
	plan, err := PrepareManagement("demo.service", Delete, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	f.fail = "daemon-reload"
	result, err := ExecuteManagement(plan, f.run)
	if err == nil || len(result.Removed) != 1 || len(result.Completed) != 0 || !strings.Contains(err.Error(), "配置已删除") {
		t.Fatal(result, err)
	}
	if !strings.Contains(result.String(), "已删除") {
		t.Fatal(result)
	}
}

func TestSystemDeletionUsesLiteralSudoCommandsAndReportsPartialFailure(t *testing.T) {
	for _, fail := range []string{"", "rm-second", "daemon-reload"} {
		t.Run(fail, func(t *testing.T) {
			f := newManagementFixture(t)
			drop := filepath.Join(f.dir, "demo.service.d")
			os.Mkdir(drop, 0755)
			os.WriteFile(filepath.Join(drop, "override.conf"), []byte("[Service]\n"), 0644)
			plan, err := PrepareManagement("demo.service", Delete, false, f.run)
			if err != nil {
				t.Fatal(err)
			}
			plan.System = true
			var calls [][]string
			run := func(name string, args ...string) (string, error) {
				calls = append(calls, append([]string{name}, args...))
				if name != "sudo" {
					t.Fatalf("system mutation without sudo: %v", calls)
				}
				if len(args) == 0 || args[0] != "-n" {
					t.Fatal("mutation may prompt after dependency checks", args)
				}
				args = args[1:]
				switch args[0] {
				case "rm":
					if len(args) != 3 || args[1] != "--" {
						t.Fatal(args)
					}
					if fail == "rm-second" && strings.HasSuffix(args[2], "override.conf") {
						return "", errors.New("permission denied")
					}
					return "", os.Remove(args[2])
				case "rmdir":
					return "", os.Remove(args[len(args)-1])
				case "systemctl":
					if fail == "daemon-reload" {
						return "", errors.New("reload failed")
					}
					return "", nil
				default:
					t.Fatalf("unexpected privileged command %v", args)
				}
				return "", nil
			}
			result, err := deleteConfiguration(plan, f.dir, run)
			if (err != nil) != (fail != "") {
				t.Fatal(result, err)
			}
			wantRemoved := 2
			if fail == "rm-second" {
				wantRemoved = 1
			}
			if len(result.Removed) != wantRemoved {
				t.Fatal(result, err)
			}
			if fail == "rm-second" && contains(calls[len(calls)-1], "daemon-reload") {
				t.Fatal("continued after partial deletion", calls)
			}
		})
	}
}

func TestTemplateInspectionAndImplicitTimerDependencies(t *testing.T) {
	f := newManagementFixture(t)
	f.add("worker@.service", "disabled")
	template := filepath.Join(f.dir, "worker@.service")
	if err := os.WriteFile(template, []byte("[Service]\nExecStart=/usr/bin/true %i\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareManagement("worker@.service", Delete, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) != 1 || plan.Paths[0] != template {
		t.Fatal(plan)
	}
	f.add("worker@.timer", "disabled")
	if _, err = ExecuteManagement(plan, f.run); err == nil || !strings.Contains(err.Error(), "worker@.timer") {
		t.Fatal("missed implicit template timer", err)
	}
	if _, err = os.Stat(template); err != nil {
		t.Fatal(err)
	}
}

func TestMaskFailureDoesNotClaimNoChanges(t *testing.T) {
	f := newManagementFixture(t)
	plan, err := PrepareManagement("demo.service", Mask, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	f.fail = "mask"
	result, err := ExecuteManagement(plan, f.run)
	if err == nil || !strings.Contains(err.Error(), "可能已有部分") || len(result.Completed) != 0 {
		t.Fatal(result, err)
	}
	if !strings.Contains(result.String(), "尚未确认") {
		t.Fatal(result.String())
	}
}

func TestDeleteInstanceCannotExposeTemplate(t *testing.T) {
	f := newManagementFixture(t)
	p := f.add("worker@production.service", "disabled")
	if err := os.WriteFile(p["FragmentPath"], []byte("[Service]\nExecStart=/usr/bin/true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A lower-priority template can replace an instance even without an exact
	// same-name file elsewhere in the search path.
	dir := t.TempDir()
	f.paths = append(f.paths, dir)
	if err := os.WriteFile(filepath.Join(dir, "worker@.service"), []byte("vendor template"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareManagement("worker@production.service", Delete, false, f.run); err == nil || !strings.Contains(err.Error(), "worker@.service") {
		t.Fatal("template fallback not protected", err)
	}
}

func TestDeleteOwnMaskWithoutUnderlyingDefinition(t *testing.T) {
	f := newManagementFixture(t)
	path := filepath.Join(f.dir, "demo.service")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	p := f.props["demo.service"]
	p["LoadState"] = "masked"
	p["FragmentPath"] = "/dev/null"
	p["UnitFileState"] = "masked"
	plan, err := PrepareManagement("demo.service", Delete, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ExecuteManagement(plan, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("mask was not removed", err)
	}
}

func TestDeleteDetectsActivationDuringDependencyScan(t *testing.T) {
	f := newManagementFixture(t)
	f.add("client.service", "disabled")
	run := func(name string, args ...string) (string, error) {
		out, err := f.run(name, args...)
		if contains(args, "cat") && contains(args, "client.service") {
			f.props["demo.service"]["ActiveState"] = "active"
		}
		return out, err
	}
	if _, err := PrepareManagement("demo.service", Delete, false, run); err == nil || !strings.Contains(err.Error(), "状态已变化") {
		t.Fatal("activation during scan was missed", err)
	}
	if len(f.mutations()) != 0 {
		t.Fatal(f.calls)
	}
}
