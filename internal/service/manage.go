package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nirier/systemd-conf-tui/internal/unit"
)

type Unit struct {
	Name        string
	State       string
	Active      string
	Description string
}

type Action string

const (
	Disable Action = "disable"
	Mask    Action = "mask"
	Delete  Action = "delete"
)

func managerctl(run Run, system bool, args ...string) (string, error) {
	prefix := []string{"--no-pager", "--no-ask-password"}
	if !system {
		prefix = append(prefix, "--user")
	}
	return run("systemctl", append(prefix, args...)...)
}

// ListUnits includes both installed files and transient/loaded units of every type.
func ListUnits(system bool, run Run) ([]Unit, error) {
	files, err := managerctl(run, system, "list-unit-files", "--all", "--output=json")
	if err != nil {
		return nil, fmt.Errorf("读取 unit 文件失败: %w", err)
	}
	var installed []struct {
		Name  string `json:"unit_file"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(files), &installed); err != nil {
		return nil, fmt.Errorf("无法解析 unit 文件列表: %w", err)
	}
	if installed == nil {
		return nil, fmt.Errorf("unit 文件列表不是 JSON 数组")
	}
	loaded, err := managerctl(run, system, "list-units", "--all", "--output=json")
	if err != nil {
		return nil, fmt.Errorf("读取已加载 unit 失败: %w", err)
	}
	var running []struct {
		Name        string `json:"unit"`
		Active      string `json:"active"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(loaded), &running); err != nil {
		return nil, fmt.Errorf("无法解析已加载 unit 列表: %w", err)
	}
	if running == nil {
		return nil, fmt.Errorf("已加载 unit 列表不是 JSON 数组")
	}
	byName := map[string]Unit{}
	for _, f := range installed {
		if err := unit.ValidateName(f.Name); err != nil {
			return nil, err
		}
		byName[f.Name] = Unit{Name: f.Name, State: f.State, Active: "未加载"}
	}
	for _, r := range running {
		if err := unit.ValidateName(r.Name); err != nil {
			return nil, err
		}
		u := byName[r.Name]
		u.Name, u.Active, u.Description = r.Name, r.Active, r.Description
		byName[r.Name] = u
	}
	units := make([]Unit, 0, len(byName))
	for _, u := range byName {
		units = append(units, u)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	return units, nil
}

type inspection struct {
	properties map[string]string
	content    string
}

const properties = "Id,Names,LoadState,ActiveState,Job,UnitFileState,FragmentPath,SourcePath,DropInPaths,NeedDaemonReload,Requires,Requisite,Wants,BindsTo,Upholds,PartOf,OnFailure,OnSuccess,Triggers,PropagatesStopTo,PropagatesReloadTo,JoinsNamespaceOf,RequiredBy,RequisiteOf,WantedBy,BoundBy,UpheldBy,ConsistsOf,TriggeredBy"

func showUnits(system bool, run Run, names []string) ([]map[string]string, error) {
	args := []string{"show", "--property=" + properties, "--"}
	out, err := managerctl(run, system, append(args, names...)...)
	if err != nil {
		return nil, fmt.Errorf("读取 unit 依赖失败: %w", err)
	}
	var result []map[string]string
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		p := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				return nil, fmt.Errorf("无法解析 unit 属性")
			}
			p[key] = value
		}
		if p["Id"] == "" {
			return nil, fmt.Errorf("unit 属性缺少 Id，已拒绝操作")
		}
		result = append(result, p)
	}
	if len(result) != len(names) {
		return nil, fmt.Errorf("unit 属性不完整，已拒绝操作")
	}
	return result, nil
}

func controlLabel(system bool) string {
	if system {
		return "sudo systemctl"
	}
	return "systemctl --user"
}

// Templates cannot be loaded with `systemctl show`; inspect their files without
// creating a synthetic instance in the user's manager.
func inspectTemplate(name string, system bool, run Run) (inspection, error) {
	units, err := ListUnits(system, run)
	if err != nil {
		return inspection{}, err
	}
	state := ""
	for _, u := range units {
		if u.Name == name {
			state = u.State
			break
		}
	}
	if state == "" {
		return inspection{}, fmt.Errorf("模板 unit 未找到")
	}
	content, err := managerctl(run, system, "cat", "--", name)
	if err != nil {
		return inspection{}, err
	}
	path := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# /") {
			path = strings.TrimPrefix(line, "# ")
			break
		}
	}
	if path == "" {
		return inspection{}, fmt.Errorf("无法确定模板配置路径")
	}
	return inspection{map[string]string{"Id": name, "Names": name, "LoadState": "loaded", "ActiveState": "inactive", "UnitFileState": state, "FragmentPath": path, "NeedDaemonReload": "no"}, content}, nil
}

// Removing a local override must not silently expose another unit definition.
func checkShadowed(name, dir string, system bool, run Run) error {
	args := []string{"unit-paths"}
	if !system {
		args = append([]string{"--user"}, args...)
	}
	out, err := run("systemd-analyze", args...)
	if err != nil {
		return fmt.Errorf("无法检查 unit 搜索路径: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("unit 搜索路径为空，已拒绝删除")
	}
	candidateNames := []string{name}
	if !unit.IsTemplate(name) && strings.Contains(name, "@") {
		prefix, _, _ := strings.Cut(name, "@")
		candidateNames = append(candidateNames, prefix+"@"+filepath.Ext(name))
	}
	foundCurrent := false
	for _, search := range strings.Split(strings.TrimSpace(out), "\n") {
		if !filepath.IsAbs(search) {
			return fmt.Errorf("unit 搜索路径不是绝对路径")
		}
		local := filepath.Clean(search) == filepath.Clean(dir)
		if local {
			foundCurrent = true
		}
		for _, candidateName := range candidateNames {
			if local && candidateName == name {
				continue
			}
			candidate := filepath.Join(search, candidateName)
			if _, err := os.Lstat(candidate); err == nil {
				return fmt.Errorf("删除会暴露或影响其他配置 %s，请使用 disable / mask", candidate)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	if !foundCurrent {
		return fmt.Errorf("本地配置目录不在 unit 搜索路径中，已拒绝删除")
	}
	return nil
}

func inspect(name string, system bool, run Run) (inspection, error) {
	if err := unit.ValidateName(name); err != nil {
		return inspection{}, err
	}
	if unit.IsTemplate(name) {
		return inspectTemplate(name, system, run)
	}
	ps, err := showUnits(system, run, []string{name})
	if err != nil {
		return inspection{}, err
	}
	p := ps[0]
	if p["LoadState"] != "loaded" && p["LoadState"] != "masked" {
		return inspection{}, fmt.Errorf("unit 未找到或加载失败")
	}
	var content string
	if p["FragmentPath"] == "" && p["LoadState"] == "loaded" {
		content = "# 此 unit 没有持久配置文件（临时 unit 或设备等）；文件操作可能不适用。"
	} else if p["LoadState"] == "masked" {
		content = "# 此 unit 已 mask；配置可能被 /dev/null 链接遮蔽。"
	} else {
		content, err = managerctl(run, system, "cat", "--", name)
		if err != nil {
			return inspection{}, fmt.Errorf("无法读取完整配置: %w", err)
		}
	}
	return inspection{p, content}, nil
}

func PreviewUnit(name string, system bool, run Run) (string, error) {
	in, err := inspect(name, system, run)
	if err != nil {
		return "", err
	}
	return in.content, nil
}

type ManagementPlan struct {
	Name    string
	Action  Action
	System  bool
	Preview string
	Paths   []string
	// File identities and contents are rechecked immediately before unlinking.
	files     []plannedFile
	dropInDir bool
}

type plannedFile struct {
	relative string
	info     os.FileInfo
	data     string
	link     string
}

type ManagementResult struct {
	Completed []string
	Removed   []string
}

func (r ManagementResult) String() string {
	if len(r.Completed) == 0 && len(r.Removed) == 0 {
		return "尚未确认任何更改已完成。"
	}
	text := strings.Join(r.Completed, "\n")
	for _, p := range r.Removed {
		text += "\n已删除：" + p
	}
	return strings.TrimSpace(text)
}

// PrepareManagement is read-only. Delete is limited to local configuration;
// packaged/global units remain available for disable and mask.
func PrepareManagement(name string, action Action, system bool, run Run) (ManagementPlan, error) {
	plan := ManagementPlan{Name: name, Action: action, System: system}
	if action != Disable && action != Mask && action != Delete {
		return plan, fmt.Errorf("不支持的操作")
	}
	in, err := inspect(name, system, run)
	if err != nil {
		return plan, err
	}
	p := in.properties
	mode := "用户"
	if system {
		mode = "系统"
	}
	plan.Preview = fmt.Sprintf(mode+" unit：%s\n操作：%s\n状态：%s / %s\n配置：%s\n\n%s", name, action, p["ActiveState"], p["UnitFileState"], p["FragmentPath"], in.content)
	if action != Delete {
		plan.Preview += "\n\n此操作不会停止正在运行的 unit。"
		if action == Disable {
			plan.Preview += "\ndisable 会按 [Install] 清理关联的启用链接（包括 Also=）。"
		}
		if action == Mask {
			plan.Preview += "\nmask 会阻止后续启动；已有同名本地配置时 systemctl 可能拒绝操作。"
		}
		return plan, nil
	}
	if p["Job"] != "" && p["Job"] != "0" {
		return plan, fmt.Errorf("unit 有待处理任务，请稍后重试")
	}
	if p["ActiveState"] != "inactive" && p["ActiveState"] != "failed" {
		return plan, fmt.Errorf("请先停止 %s（当前 %s），不会自动停止依赖", name, p["ActiveState"])
	}
	switch p["UnitFileState"] {
	case "disabled", "static", "indirect", "masked", "masked-runtime":
	default:
		return plan, fmt.Errorf("请先 disable %s（当前 %s）；不会自动删除启用链接", name, p["UnitFileState"])
	}
	if err := checkReferences(name, p, system, run); err != nil {
		return plan, err
	}
	dir, err := Directory(system)
	if err != nil {
		return plan, err
	}
	if err = checkShadowed(name, dir, system, run); err != nil {
		return plan, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return plan, err
	}
	defer root.Close()
	if p["SourcePath"] != "" {
		return plan, fmt.Errorf("生成的 unit 不允许直接删除，请修改其来源：%s", p["SourcePath"])
	}
	path := filepath.Join(dir, name)
	// A mask's FragmentPath is /dev/null, but only its local link may be removed.
	if p["FragmentPath"] != path && p["LoadState"] != "masked" {
		return plan, fmt.Errorf("仅可删除当前模式的本地配置目录中的同名 unit；共享配置、别名和实例请使用 disable / mask")
	}
	addFile := func(relative string, mask bool) error {
		info, err := root.Lstat(relative)
		if err != nil {
			return err
		}
		f := plannedFile{relative: relative, info: info}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := root.Readlink(relative)
			if err != nil {
				return err
			}
			if !mask || target != "/dev/null" {
				return fmt.Errorf("拒绝删除符号链接：%s", relative)
			}
			f.link = target
		} else {
			if mask {
				return fmt.Errorf("unit 被其他路径 mask，无法完整预览本地配置；请先 unmask 后重试")
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("拒绝删除非普通文件：%s", relative)
			}
			data, err := root.ReadFile(relative)
			if err != nil {
				return err
			}
			f.data = string(data)
		}
		plan.files = append(plan.files, f)
		plan.Paths = append(plan.Paths, filepath.Join(dir, relative))
		return nil
	}
	if err = addFile(name, p["LoadState"] == "masked"); err != nil {
		return plan, err
	}
	dropDir := name + ".d"
	if info, err := root.Lstat(dropDir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return plan, fmt.Errorf("drop-in 目录不可为符号链接")
		}
		plan.dropInDir = true
		f, err := root.Open(dropDir)
		if err != nil {
			return plan, err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return plan, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".conf") {
				return plan, fmt.Errorf("drop-in 目录存在非配置文件 %s，请先手动整理", entry.Name())
			}
			if err = addFile(filepath.Join(dropDir, entry.Name()), false); err != nil {
				return plan, err
			}
		}
	} else if !os.IsNotExist(err) {
		return plan, err
	}
	// Shared/type-wide drop-ins are previewed but never removed.
	plan.Preview += "\n\n依赖检查通过。将删除以下本地配置（包括该 unit 专属 drop-in）：\n" + strings.Join(plan.Paths, "\n") + "\n不会删除依赖项或其他 unit。共享 drop-in 会保留。"
	if p["LoadState"] == "masked" {
		for _, file := range plan.files {
			if file.link == "" {
				plan.Preview += "\n\n# " + filepath.Join(dir, file.relative) + "\n" + file.data
			}
		}
	}
	plan.Preview = plan.deletionCommandPreview(dir) + "\n\n" + plan.Preview
	return plan, nil
}

func (plan ManagementPlan) deletionCommandPreview(dir string) string {
	// These quotes are for display only; execution always uses literal arguments.
	quote := func(path string) string {
		return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
	}
	var b strings.Builder
	b.WriteString("命令预览（省略只读检查命令）：\n")
	prefix := ""
	if plan.System {
		b.WriteString("sudo -v\n")
		prefix = "sudo -n "
	}
	b.WriteString("# 配置与依赖复检通过后：\n")
	for _, path := range plan.Paths {
		fmt.Fprintf(&b, "%srm -- %s\n", prefix, quote(path))
	}
	if plan.dropInDir {
		fmt.Fprintf(&b, "%srmdir -- %s\n", prefix, quote(filepath.Join(dir, plan.Name+".d")))
	}
	b.WriteString(prefix + "systemctl --no-pager --no-ask-password")
	if !plan.System {
		b.WriteString(" --user")
	}
	b.WriteString(" daemon-reload")
	if !plan.System {
		b.WriteString("\n用户模式的 rm / rmdir 为等效命令，文件操作由程序直接完成。")
	}
	return b.String()
}

func checkReferences(name string, target map[string]string, system bool, run Run) error {
	units, err := ListUnits(system, run)
	if err != nil {
		return err
	}
	names := strings.Fields(target["Names"])
	names = append(names, name)
	if target["NeedDaemonReload"] != "no" {
		return fmt.Errorf("配置尚未加载，请先执行 %s daemon-reload", controlLabel(system))
	}
	referenced := func(value string) bool {
		for _, v := range strings.Fields(value) {
			for _, n := range names {
				if unit.ReferencesName(v, n) {
					return true
				}
			}
		}
		return false
	}
	var blockers []string
	// Include manager-generated dependencies (e.g. timer/socket activation).
	for _, key := range []string{"RequiredBy", "RequisiteOf", "WantedBy", "BoundBy", "UpheldBy", "ConsistsOf", "TriggeredBy"} {
		for _, n := range strings.Fields(target[key]) {
			if n != name {
				blockers = append(blockers, n+" ("+key+")")
			}
		}
	}
	// Loading installed but inactive units is essential: reverse dependencies alone
	// only describe units already loaded by the manager.
	for start := 0; start < len(units); start += 64 {
		var batch []string
		for _, u := range units[start:min(start+64, len(units))] {
			if u.Name != name && u.State != "" && !strings.HasPrefix(u.State, "masked") {
				content, err := managerctl(run, system, "cat", "--", u.Name)
				if err != nil {
					return fmt.Errorf("无法检查 unit 配置 %s: %w", u.Name, err)
				}
				if unit.References(content, names) || unit.ImplicitReferences(u.Name, names) {
					blockers = append(blockers, u.Name+" (配置引用)")
				}
			}
			if unit.IsTemplate(u.Name) {
				continue
			}
			batch = append(batch, u.Name)
		}
		if len(batch) == 0 {
			continue
		}
		ps, err := showUnits(system, run, batch)
		if err != nil {
			return err
		}
		for i, p := range ps {
			if p["NeedDaemonReload"] != "no" {
				return fmt.Errorf("%s 配置可能尚未加载，请先执行 %s daemon-reload 后重试", p["Id"], controlLabel(system))
			}
			if p["Id"] == target["Id"] {
				if batch[i] != name {
					blockers = append(blockers, batch[i]+" (别名共用配置)")
				} else if p["ActiveState"] != "inactive" && p["ActiveState"] != "failed" || p["UnitFileState"] != target["UnitFileState"] || p["Job"] != target["Job"] {
					return fmt.Errorf("检查期间 unit 状态已变化，请刷新后重试")
				}
				continue
			}
			if p["LoadState"] != "loaded" && p["LoadState"] != "masked" && p["LoadState"] != "not-found" {
				return fmt.Errorf("%s 加载失败，无法确认依赖", p["Id"])
			}
			if target["FragmentPath"] != "" && target["FragmentPath"] != "/dev/null" && p["FragmentPath"] == target["FragmentPath"] {
				blockers = append(blockers, p["Id"]+" (共用配置)")
			}
			for _, key := range []string{"Requires", "Requisite", "Wants", "BindsTo", "Upholds", "PartOf", "OnFailure", "OnSuccess", "Triggers", "PropagatesStopTo", "PropagatesReloadTo", "JoinsNamespaceOf"} {
				if referenced(p[key]) {
					blockers = append(blockers, p["Id"]+" ("+key+")")
				}
			}
		}
	}
	if len(blockers) > 0 {
		sort.Strings(blockers)
		return fmt.Errorf("仍有 unit 依赖或触发此 unit，请先处理这些引用：\n%s", strings.Join(blockers, "\n"))
	}
	return nil
}

func mutateManager(run Run, system bool, args ...string) (string, error) {
	if !system {
		return managerctl(run, false, args...)
	}
	return run("sudo", append([]string{"-n", "systemctl", "--no-pager", "--no-ask-password"}, args...)...)
}

// ExecuteManagement rechecks a confirmed plan. The caller must release the
// terminal and authenticate sudo before executing a system plan.
func ExecuteManagement(plan ManagementPlan, run Run) (result ManagementResult, err error) {
	system := plan.System

	// Rebuild the plan after confirmation to detect dependency/configuration changes.
	fresh, err := PrepareManagement(plan.Name, plan.Action, system, run)
	if err != nil {
		return result, err
	}
	if fresh.Preview != plan.Preview || !sameFiles(plan.files, fresh.files) {
		return result, fmt.Errorf("配置或状态已变化，请重新预览并确认")
	}
	if plan.Action != Delete {
		if _, err = mutateManager(run, system, string(plan.Action), "--", plan.Name); err != nil {
			return result, fmt.Errorf("%s 未确认完成，可能已有部分链接发生变化；请刷新检查：%w", plan.Action, err)
		}
		result.Completed = append(result.Completed, string(plan.Action)+" 已完成："+plan.Name)
		return result, nil
	}
	dir, err := Directory(system)
	if err != nil {
		return result, err
	}
	return deleteConfiguration(fresh, dir, run)
}

func deleteConfiguration(plan ManagementPlan, dir string, run Run) (result ManagementResult, err error) {
	system := plan.System
	root, err := os.OpenRoot(dir)
	if err != nil {
		return result, err
	}
	defer root.Close()
	for _, f := range plan.files {
		info, e := root.Lstat(f.relative)
		if e != nil || !os.SameFile(info, f.info) {
			return result, fmt.Errorf("文件已变化，已停止删除：%s", f.relative)
		}
		if system {
			_, err = run("sudo", "-n", "rm", "--", filepath.Join(dir, f.relative))
		} else {
			err = root.Remove(f.relative)
		}
		if err != nil {
			return result, fmt.Errorf("删除 %s 失败：%w", f.relative, err)
		}
		result.Removed = append(result.Removed, filepath.Join(dir, f.relative))
	}
	// Only remove an empty directory; never recursively remove newly added files.
	if _, statErr := root.Lstat(plan.Name + ".d"); statErr == nil {
		if system {
			_, err = run("sudo", "-n", "rmdir", "--", filepath.Join(dir, plan.Name+".d"))
		} else {
			err = root.Remove(plan.Name + ".d")
		}
	} else if !os.IsNotExist(statErr) {
		err = statErr
	}
	if err != nil {
		return result, fmt.Errorf("unit 文件已删除，但 drop-in 目录清理失败：%w；请执行 %s daemon-reload", err, controlLabel(system))
	}
	if _, err = mutateManager(run, system, "daemon-reload"); err != nil {
		return result, fmt.Errorf("配置已删除，但 daemon-reload 失败，请手动重试：%w", err)
	}
	result.Completed = append(result.Completed, "daemon-reload 已完成")
	return result, nil
}

func sameFiles(a, b []plannedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].relative != b[i].relative || a[i].data != b[i].data || a[i].link != b[i].link || !os.SameFile(a[i].info, b[i].info) {
			return false
		}
	}
	return true
}
