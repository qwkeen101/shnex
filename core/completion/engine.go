package completion

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SSHHostHint describes an SSH alias or saved server surfaced to ssh/scp/sftp completion.
type SSHHostHint struct {
	Alias  string
	Detail string
}

// Engine provides zero-plugin, zero-AI native shell completion across Git, npm/pnpm/yarn,
// ~/.ssh/config + saved hosts, Windows WSL registry distros, and pane filesystem paths.
type Engine struct {
	savedHostsFn  func() []SSHHostHint
	sshConfigPath string

	mu         sync.RWMutex
	usageScore map[string]int // used ONLY to sort discovered candidates, never to invent stale files
}

func NewEngine(savedHostsFn func() []SSHHostHint) *Engine {
	return &Engine{
		savedHostsFn: savedHostsFn,
		usageScore:   make(map[string]int),
	}
}

// RecordSelection increments the ranking score for a chosen candidate so history sorts discovered items.
func (e *Engine) RecordSelection(kind CandidateKind, label string) {
	key := string(kind) + ":" + strings.ToLower(strings.TrimSpace(label))
	e.mu.Lock()
	e.usageScore[key]++
	e.mu.Unlock()
}

func (e *Engine) scoreOf(kind CandidateKind, label string) int {
	key := string(kind) + ":" + strings.ToLower(strings.TrimSpace(label))
	e.mu.RLock()
	sc := e.usageScore[key]
	e.mu.RUnlock()
	return sc
}

var builtinTopCommands = []struct {
	cmd    string
	detail string
}{
	{"git", "Git 版本控制"},
	{"npm", "Node 包与脚本管理"},
	{"pnpm", "高性能 Node 包管理"},
	{"yarn", "Yarn 脚本管理"},
	{"ssh", "远程安全登录"},
	{"scp", "SSH 远程文件拷贝"},
	{"sftp", "交互式文件传输"},
	{"wsl", "Windows Linux 子系统"},
	{"cat", "查看文件内容"},
	{"nvim", "Neovim 编辑器"},
	{"code", "VS Code 打开文件/目录"},
	{"vim", "Vim 编辑器"},
	{"cd", "切换工作目录"},
	{"ls", "列出目录内容"},
	{"mkdir", "创建目录"},
	{"rm", "删除文件或目录"},
	{"cp", "复制文件"},
	{"mv", "移动或重命名"},
	{"docker", "容器引擎"},
	{"kubectl", "Kubernetes 命令行"},
	{"claude", "Claude Code AI 终端"},
	{"python", "Python 解释器"},
	{"go", "Go 工具链"},
	{"cargo", "Rust 工具链"},
}

var gitSubcommands = []struct {
	sub        string
	detail     string
	needsRef   bool
}{
	{"checkout", "切换分支或恢复工作区文件", true},
	{"switch", "切换到指定分支", true},
	{"merge", "合并指定分支到当前分支", true},
	{"rebase", "变基到指定分支", true},
	{"branch", "查看、创建或删除分支", true},
	{"cherry-pick", "拣选提交或分支", true},
	{"reset", "重置当前 HEAD 状态", true},
	{"diff", "查看工作区或分支差异", true},
	{"log", "查看提交历史", true},
	{"status", "查看工作区状态", false},
	{"add", "添加文件到暂存区", false},
	{"commit", "记录暂存区变更", false},
	{"pull", "拉取并合并远端分支", false},
	{"push", "推送提交到远端仓库", false},
	{"fetch", "下载远端对象与引用", false},
	{"stash", "暂存工作区修改", false},
	{"clone", "克隆远程仓库", false},
}

var fileArgCommands = map[string]bool{
	"cat": true, "nvim": true, "vim": true, "vi": true, "nano": true,
	"code": true, "cursor": true, "less": true, "more": true, "head": true,
	"tail": true, "bat": true, "type": true, "get-content": true, "gc": true,
	"ls": true, "dir": true, "rm": true, "cp": true, "mv": true, "chmod": true,
	"python": true, "python3": true, "node": true, "bun": true, "sh": true, "bash": true,
}

// Suggest computes ranked native completion candidates for req.Line in inspector's context.
func (e *Engine) Suggest(ctx context.Context, inspector WorkspaceInspector, req SuggestRequest) SuggestResponse {
	if inspector == nil || strings.TrimSpace(req.Line) == "" {
		return SuggestResponse{}
	}

	pl := parseActiveCommandSegment(req.Line)
	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		cwd = inspector.DefaultCwd()
	}

	// Case 1: Typing the first command word itself (e.g. `gi`, `np`, `./script`)
	if len(pl.args) == 0 && !pl.hasTrailingSp {
		if strings.ContainsAny(pl.activeUnquoted, `/\`) || strings.HasPrefix(pl.activeUnquoted, ".") {
			cands := e.completeFilesystem(ctx, inspector, cwd, pl, false)
			return buildResponse(pl, cands)
		}
		var cands []Candidate
		prefixLower := strings.ToLower(pl.activeUnquoted)
		for _, bc := range builtinTopCommands {
			if strings.HasPrefix(bc.cmd, prefixLower) && bc.cmd != prefixLower {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, bc.cmd, true, false)
				cands = append(cands, Candidate{
					Label:       bc.cmd,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindCommand,
					Source:      "内置命令",
					Detail:      bc.detail,
				})
			}
		}
		e.sortCandidates(pl.activeUnquoted, cands)
		return buildResponse(pl, cands)
	}

	if len(pl.args) == 0 {
		return SuggestResponse{}
	}

	cmd := strings.ToLower(filepath.Base(pl.args[0]))
	cmd = strings.TrimSuffix(cmd, ".exe")

	var cands []Candidate
	switch cmd {
	case "git":
		cands = e.completeGit(ctx, inspector, cwd, pl)
	case "npm", "pnpm", "yarn", "bun":
		cands = e.completePackageManager(ctx, inspector, cwd, cmd, pl)
	case "ssh", "scp", "sftp":
		cands = e.completeSSH(pl)
	case "wsl":
		cands = e.completeWSL(ctx, inspector, pl)
	case "cd", "pushd", "set-location", "sl":
		cands = e.completeFilesystem(ctx, inspector, cwd, pl, true)
	default:
		if fileArgCommands[cmd] || strings.ContainsAny(pl.activeUnquoted, `/\`) || strings.HasPrefix(pl.activeUnquoted, ".") {
			cands = e.completeFilesystem(ctx, inspector, cwd, pl, false)
		}
	}

	e.sortCandidates(pl.activeUnquoted, cands)
	if len(cands) > 24 {
		cands = cands[:24]
	}
	return buildResponse(pl, cands)
}

func buildResponse(pl parsedLine, cands []Candidate) SuggestResponse {
	if pl.activeRaw != "" && len(cands) > 0 {
		filtered := make([]Candidate, 0, len(cands))
		for _, c := range cands {
			if strings.TrimRight(c.InsertText, " ") == pl.activeRaw {
				continue
			}
			filtered = append(filtered, c)
		}
		cands = filtered
	}
	return SuggestResponse{
		TokenPrefix: pl.activeRaw,
		ReplaceLen:  len([]rune(pl.activeRaw)),
		Candidates:  cands,
	}
}

func (e *Engine) completeGit(ctx context.Context, inspector WorkspaceInspector, cwd string, pl parsedLine) []Candidate {
	// `git <subcommand>`
	if len(pl.args) == 1 {
		var out []Candidate
		pref := strings.ToLower(pl.activeUnquoted)
		for _, sc := range gitSubcommands {
			if strings.HasPrefix(sc.sub, pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, sc.sub, true, false)
				out = append(out, Candidate{
					Label:       sc.sub,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindSubcommand,
					Source:      "Git 命令",
					Detail:      sc.detail,
				})
			}
		}
		return out
	}

	sub := strings.ToLower(pl.args[1])
	nonFlagAfterSub := 0
	for _, a := range pl.args[2:] {
		if !strings.HasPrefix(a, "-") {
			nonFlagAfterSub++
		}
	}

	switch sub {
	case "checkout", "switch", "merge", "rebase", "branch", "cherry-pick", "reset", "diff", "log", "push", "pull":
		if nonFlagAfterSub >= 1 {
			if pl.activeRaw != "" && (sub == "checkout" || sub == "diff" || sub == "log" || sub == "reset") {
				return e.completeFilesystem(ctx, inspector, cwd, pl, false)
			}
			return nil
		}
		branches, _ := inspector.GitBranches(ctx, cwd)
		pref := strings.ToLower(pl.activeUnquoted)
		var out []Candidate
		for _, br := range branches {
			if strings.HasPrefix(strings.ToLower(br), pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, br, true, false)
				out = append(out, Candidate{
					Label:       br,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindGitBranch,
					Source:      "Git 分支",
				})
			}
		}
		if len(out) > 0 {
			return out
		}
		if pl.activeRaw != "" {
			return e.completeFilesystem(ctx, inspector, cwd, pl, false)
		}
		return nil
	case "add", "rm", "restore":
		return e.completeFilesystem(ctx, inspector, cwd, pl, false)
	default:
		return nil
	}
}

func (e *Engine) completePackageManager(ctx context.Context, inspector WorkspaceInspector, cwd string, pm string, pl parsedLine) []Candidate {
	scripts, _ := inspector.PackageScripts(ctx, cwd)
	pref := strings.ToLower(pl.activeUnquoted)
	var out []Candidate

	// `npm <subcommand>` vs `npm run <script>` (and `pnpm <script>` / `yarn <script>` shorthand!)
	if len(pl.args) == 1 {
		npmSubs := []struct{ s, d string }{
			{"run", "运行 package.json 脚本"},
			{"install", "安装依赖包"},
			{"test", "运行测试脚本"},
			{"start", "运行 start 脚本"},
			{"build", "构建项目"},
		}
		for _, sub := range npmSubs {
			if strings.HasPrefix(sub.s, pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, sub.s, true, false)
				out = append(out, Candidate{
					Label:       sub.s,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindSubcommand,
					Source:      pm + " 命令",
					Detail:      sub.d,
				})
			}
		}
		// pnpm / yarn / bun allow running scripts directly (`pnpm build`)
		if pm != "npm" {
			for _, sc := range scripts {
				if strings.HasPrefix(strings.ToLower(sc.Name), pref) {
					ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, sc.Name, true, false)
					out = append(out, Candidate{
						Label:       sc.Name,
						InsertText:  ins,
						GhostSuffix: ghost,
						ReplaceLen:  repLen,
						Kind:        KindNpmScript,
						Source:      "项目脚本",
						Detail:      sc.Command,
					})
				}
			}
		}
		return out
	}

	if len(pl.args) == 2 && strings.EqualFold(pl.args[1], "run") {
		for _, sc := range scripts {
			if strings.HasPrefix(strings.ToLower(sc.Name), pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, sc.Name, true, false)
				out = append(out, Candidate{
					Label:       sc.Name,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindNpmScript,
					Source:      "npm 脚本",
					Detail:      sc.Command,
				})
			}
		}
	}
	return out
}

func (e *Engine) completeSSH(pl parsedLine) []Candidate {
	// Skip option flags like `ssh -p 2222 <alias>` until user is typing the host arg,
	// and stop once the target host positional argument has already been entered.
	positional := 0
	for i := 1; i < len(pl.args); i++ {
		arg := pl.args[i]
		if arg == "-p" || arg == "-i" || arg == "-F" || arg == "-J" || arg == "-l" || arg == "-o" || arg == "-L" || arg == "-R" || arg == "-D" {
			if i == len(pl.args)-1 {
				return nil
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		positional++
	}
	if positional >= 1 {
		return nil
	}

	seen := make(map[string]bool)
	var hints []SSHHostHint

	for _, h := range e.readSSHConfigAliases() {
		key := strings.ToLower(h.Alias)
		if !seen[key] {
			seen[key] = true
			hints = append(hints, h)
		}
	}
	if e.savedHostsFn != nil {
		for _, h := range e.savedHostsFn() {
			key := strings.ToLower(h.Alias)
			if h.Alias != "" && !seen[key] {
				seen[key] = true
				hints = append(hints, h)
			}
		}
	}

	pref := strings.ToLower(pl.activeUnquoted)
	var out []Candidate
	for _, h := range hints {
		if strings.HasPrefix(strings.ToLower(h.Alias), pref) {
			ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, h.Alias, true, false)
			out = append(out, Candidate{
				Label:       h.Alias,
				InsertText:  ins,
				GhostSuffix: ghost,
				ReplaceLen:  repLen,
				Kind:        KindSSHHost,
				Source:      "SSH 配置",
				Detail:      h.Detail,
			})
		}
	}
	return out
}

func (e *Engine) completeWSL(ctx context.Context, inspector WorkspaceInspector, pl parsedLine) []Candidate {
	distros, _ := inspector.WSLDistros(ctx)
	pref := strings.ToLower(pl.activeUnquoted)
	var out []Candidate

	prevArg := ""
	if len(pl.args) >= 2 {
		prevArg = strings.ToLower(pl.args[len(pl.args)-1])
	}
	expectingDistro := prevArg == "-d" || prevArg == "--distribution" || prevArg == "-t" || prevArg == "--terminate" || prevArg == "-s" || prevArg == "--set-default"

	if expectingDistro || (len(pl.args) == 1 && !strings.HasPrefix(pref, "-")) {
		for _, d := range distros {
			if strings.HasPrefix(strings.ToLower(d), pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, d, true, false)
				out = append(out, Candidate{
					Label:       d,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindWSLDistro,
					Source:      "WSL 发行版",
				})
			}
		}
	}

	if !expectingDistro && len(pl.args) == 1 {
		flags := []struct{ f, desc string }{
			{"-d", "指定要运行的 WSL 发行版"},
			{"--list", "列出已注册的 Linux 发行版"},
			{"--status", "查看 WSL 系统状态"},
			{"--shutdown", "立即终止所有运行中的发行版"},
		}
		for _, fl := range flags {
			if strings.HasPrefix(fl.f, pref) {
				ins, ghost, repLen := buildShellSafeCompletion(pl.activeRaw, pl.activeUnquoted, pl.quoteChar, fl.f, true, false)
				out = append(out, Candidate{
					Label:       fl.f,
					InsertText:  ins,
					GhostSuffix: ghost,
					ReplaceLen:  repLen,
					Kind:        KindFlag,
					Source:      "WSL 选项",
					Detail:      fl.desc,
				})
			}
		}
	}
	return out
}

func (e *Engine) completeFilesystem(ctx context.Context, inspector WorkspaceInspector, cwd string, pl parsedLine, dirsOnly bool) []Candidate {
	var nonFlagArgs []string
	if len(pl.args) > 1 {
		for _, a := range pl.args[1:] {
			if !strings.HasPrefix(a, "-") {
				nonFlagArgs = append(nonFlagArgs, a)
			}
		}
	}
	if dirsOnly && len(nonFlagArgs) >= 1 {
		return nil
	}
	if !dirsOnly && len(nonFlagArgs) >= 1 && pl.activeRaw == "" {
		return nil
	}

	out := e.matchFilesystemPrefix(ctx, inspector, cwd, pl.activeRaw, pl.activeUnquoted, pl.quoteChar, dirsOnly)
	// Support unquoted multi-word prefix for space-containing filenames (e.g. `cat my sec` -> `'my secret notes.txt' `)
	if len(out) == 0 && pl.quoteChar == 0 && len(nonFlagArgs) == 1 && pl.activeRaw != "" && !strings.ContainsAny(nonFlagArgs[0], `/\`) {
		combined := nonFlagArgs[0] + " " + pl.activeRaw
		out = e.matchFilesystemPrefix(ctx, inspector, cwd, combined, combined, 0, dirsOnly)
	}
	return out
}

func (e *Engine) matchFilesystemPrefix(ctx context.Context, inspector WorkspaceInspector, cwd string, activeRaw, rawPath string, quoteChar byte, dirsOnly bool) []Candidate {
	dirPrefix := ""
	basePrefix := rawPath
	searchDir := cwd

	lastSlash := strings.LastIndexAny(rawPath, `/\`)
	if lastSlash >= 0 {
		dirPrefix = rawPath[:lastSlash+1]
		basePrefix = rawPath[lastSlash+1:]
		sub := rawPath[:lastSlash]
		if sub == "" && strings.HasPrefix(rawPath, "/") {
			searchDir = "/"
		} else if inspector.IsWindows() {
			if filepath.IsAbs(sub) || strings.HasPrefix(sub, "~") {
				searchDir = sub
			} else {
				searchDir = filepath.Join(cwd, filepath.FromSlash(sub))
			}
		} else {
			if strings.HasPrefix(sub, "/") || strings.HasPrefix(sub, "~") {
				searchDir = sub
			} else {
				searchDir = path.Join(cwd, sub)
			}
		}
	}

	items, err := inspector.ListDir(ctx, searchDir)
	if err != nil {
		return nil
	}

	baseLower := strings.ToLower(basePrefix)
	showDotFiles := strings.HasPrefix(basePrefix, ".")

	var normalOut []Candidate
	var dotOut []Candidate
	for _, it := range items {
		if it.Name == "." || it.Name == ".." || it.Name == "" {
			continue
		}
		if dirsOnly && !it.IsDir {
			continue
		}
		isDot := strings.HasPrefix(it.Name, ".")
		if isDot && !showDotFiles && basePrefix != "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(it.Name), baseLower) {
			continue
		}

		fullTarget := dirPrefix + it.Name
		ins, ghost, repLen := buildShellSafeCompletion(activeRaw, rawPath, quoteChar, fullTarget, !it.IsDir, it.IsDir)
		kind := KindFile
		detail := ""
		lbl := it.Name
		if it.IsDir {
			kind = KindDir
			if isDot {
				detail = "隐藏目录"
			} else {
				detail = "目录"
			}
			lbl = it.Name + "/"
		} else if isDot {
			detail = "隐藏文件"
		}
		cand := Candidate{
			Label:       lbl,
			InsertText:  ins,
			GhostSuffix: ghost,
			ReplaceLen:  repLen,
			Kind:        kind,
			Source:      "当前目录",
			Detail:      detail,
		}
		if isDot {
			dotOut = append(dotOut, cand)
		} else {
			normalOut = append(normalOut, cand)
		}
	}

	var out []Candidate
	if showDotFiles {
		out = dotOut
	} else {
		out = normalOut
		// When browsing a directory with empty basePrefix (e.g. `cd `, `cd sub/`, `ls `, `cat `),
		// include hidden dot-directories/files after normal items so fresh cloud servers
		// (where `~` often only has `.ssh/`, `.claude/`, `.config/`) still present candidates!
		if basePrefix == "" && len(dotOut) > 0 {
			out = append(out, dotOut...)
		}
	}

	// If `cd ` is invoked in an empty leaf directory, offer `../` so the user can still navigate up
	if dirsOnly && len(out) == 0 && basePrefix == "" && dirPrefix == "" && searchDir != "/" {
		ins, ghost, repLen := buildShellSafeCompletion(activeRaw, rawPath, quoteChar, "..", false, true)
		out = append(out, Candidate{
			Label:       "../",
			InsertText:  ins,
			GhostSuffix: ghost,
			ReplaceLen:  repLen,
			Kind:        KindDir,
			Source:      "上级目录",
			Detail:      "返回上一级",
		})
	}
	return out
}

func (e *Engine) sortCandidates(typedUnquoted string, cands []Candidate) {
	typedDot := strings.HasPrefix(typedUnquoted, ".")
	sort.SliceStable(cands, func(i, j int) bool {
		// 1. History boost strictly among discovered candidates ("历史只给已发现的候选排序")
		si := e.scoreOf(cands[i].Kind, cands[i].Label)
		sj := e.scoreOf(cands[j].Kind, cands[j].Label)
		if si != sj {
			return si > sj
		}
		// 2. Non-hidden entries always precede dot-entries unless user typed leading `.`
		if !typedDot {
			di := strings.HasPrefix(path.Base(strings.TrimSuffix(cands[i].Label, "/")), ".")
			dj := strings.HasPrefix(path.Base(strings.TrimSuffix(cands[j].Label, "/")), ".")
			if di != dj {
				return !di
			}
		}
		// 3. Exact case-sensitive prefix match beats case-insensitive match
		if typedUnquoted != "" {
			ci := strings.HasPrefix(cands[i].Label, typedUnquoted)
			cj := strings.HasPrefix(cands[j].Label, typedUnquoted)
			if ci != cj {
				return ci
			}
		}
		// 4. Directories before files when browsing paths, otherwise alphabetical
		if cands[i].Kind != cands[j].Kind {
			if cands[i].Kind == KindDir && cands[j].Kind == KindFile {
				return true
			}
			if cands[i].Kind == KindFile && cands[j].Kind == KindDir {
				return false
			}
		}
		return strings.ToLower(cands[i].Label) < strings.ToLower(cands[j].Label)
	})
}

func (e *Engine) readSSHConfigAliases() []SSHHostHint {
	cfgPath := e.sshConfigPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil
		}
		cfgPath = filepath.Join(home, ".ssh", "config")
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil
	}

	var out []SSHHostHint
	var curAliases []string
	var curHostName, curUser string

	flush := func() {
		detail := curHostName
		if curUser != "" && curHostName != "" {
			detail = curUser + "@" + curHostName
		}
		for _, a := range curAliases {
			if a != "" && !strings.ContainsAny(a, "*?!") {
				out = append(out, SSHHostHint{Alias: a, Detail: detail})
			}
		}
		curAliases = nil
		curHostName = ""
		curUser = ""
	}

	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		val := strings.Join(fields[1:], " ")
		switch key {
		case "host":
			flush()
			curAliases = fields[1:]
		case "hostname":
			curHostName = val
		case "user":
			curUser = val
		}
	}
	flush()
	return out
}
