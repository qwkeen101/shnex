package completion

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
)

// LocalInspector inspects the host machine's filesystem, .git refs, package.json, and WSL registry directly.
type LocalInspector struct {
	defaultCwd    string
	wslOverrideFn func() []string // optional test hook for non-Windows CI or deterministic tests
}

func NewLocalInspector(defaultCwd string) *LocalInspector {
	if strings.TrimSpace(defaultCwd) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			defaultCwd = home
		}
	}
	return &LocalInspector{defaultCwd: defaultCwd}
}

func (l *LocalInspector) IsWindows() bool    { return runtime.GOOS == "windows" }
func (l *LocalInspector) DefaultCwd() string { return l.defaultCwd }

func (l *LocalInspector) resolveLocalDir(dir string) string {
	d := strings.TrimSpace(dir)
	if d == "" {
		d = l.defaultCwd
	}
	// Normalize OSC 7 URI path on Windows ("/E:/2027code/shnex" -> "E:\2027code\shnex")
	if runtime.GOOS == "windows" && len(d) >= 3 && d[0] == '/' && d[2] == ':' {
		d = filepath.FromSlash(d[1:])
	}
	if strings.HasPrefix(d, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			d = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(d, "~/"), `~\`))
		}
	}
	return d
}

func (l *LocalInspector) ListDir(_ context.Context, dir string) ([]DirItem, error) {
	resolved := l.resolveLocalDir(dir)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	items := make([]DirItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, DirItem{
			Name:  e.Name(),
			IsDir: e.IsDir(),
		})
	}
	return items, nil
}

func (l *LocalInspector) ReadSmallFile(_ context.Context, filePath string, maxBytes int64) ([]byte, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(raw)) > maxBytes {
		raw = raw[:maxBytes]
	}
	return raw, nil
}

func (l *LocalInspector) GitBranches(_ context.Context, cwd string) ([]string, error) {
	gitDir := findLocalGitDir(l.resolveLocalDir(cwd))
	if gitDir == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var branches []string
	addRef := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == "HEAD" || strings.HasSuffix(name, "/HEAD") || seen[name] {
			return
		}
		seen[name] = true
		branches = append(branches, name)
	}

	// 1. Loose refs under refs/heads and refs/remotes
	for _, sub := range []string{"heads", "remotes"} {
		root := filepath.Join(gitDir, "refs", sub)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, rErr := filepath.Rel(root, p)
			if rErr == nil {
				addRef(filepath.ToSlash(rel))
			}
			return nil
		})
	}

	// 2. Packed refs (.git/packed-refs)
	if packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(packed), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				ref := fields[1]
				if strings.HasPrefix(ref, "refs/heads/") {
					addRef(strings.TrimPrefix(ref, "refs/heads/"))
				} else if strings.HasPrefix(ref, "refs/remotes/") {
					addRef(strings.TrimPrefix(ref, "refs/remotes/"))
				}
			}
		}
	}

	sort.Strings(branches)
	return branches, nil
}

func findLocalGitDir(startDir string) string {
	cur := filepath.Clean(startDir)
	for depth := 0; depth < 8 && cur != "" && cur != "."; depth++ {
		dotGit := filepath.Join(cur, ".git")
		st, err := os.Stat(dotGit)
		if err == nil {
			if st.IsDir() {
				return dotGit
			}
			// Git worktree / submodule pointer file: "gitdir: /path/to/gitdir"
			if raw, rErr := os.ReadFile(dotGit); rErr == nil {
				line := strings.TrimSpace(string(raw))
				if strings.HasPrefix(line, "gitdir:") {
					target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
					if !filepath.IsAbs(target) {
						target = filepath.Join(cur, target)
					}
					return target
				}
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func (l *LocalInspector) PackageScripts(_ context.Context, cwd string) ([]ScriptEntry, error) {
	cur := filepath.Clean(l.resolveLocalDir(cwd))
	for depth := 0; depth < 6 && cur != "" && cur != "."; depth++ {
		pkgFile := filepath.Join(cur, "package.json")
		if raw, err := os.ReadFile(pkgFile); err == nil {
			return parsePackageJSONScripts(raw), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil, nil
}

func (l *LocalInspector) WSLDistros(_ context.Context) ([]string, error) {
	if l.wslOverrideFn != nil {
		return l.wslOverrideFn(), nil
	}
	return readRegisteredWSLDistros(), nil
}

func parsePackageJSONScripts(raw []byte) []ScriptEntry {
	var parsed struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Scripts) == 0 {
		return nil
	}
	out := make([]ScriptEntry, 0, len(parsed.Scripts))
	for k, v := range parsed.Scripts {
		out = append(out, ScriptEntry{Name: k, Command: v})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// --- Remote SSH Inspector (Cached SFTP + Multiplexed Exec) ---

type cachedEntry[T any] struct {
	val       T
	expiresAt time.Time
}

type RemoteSSHInspector struct {
	defaultCwd string
	sftpProv   transport.SFTPProvider
	execProv   transport.CommandExecutor

	mu           sync.Mutex
	dirCache     map[string]cachedEntry[[]DirItem]
	gitCache     map[string]cachedEntry[[]string]
	scriptsCache map[string]cachedEntry[[]ScriptEntry]
}

func NewRemoteSSHInspector(defaultCwd string, sess transport.Session) *RemoteSSHInspector {
	sp, _ := sess.(transport.SFTPProvider)
	ep, _ := sess.(transport.CommandExecutor)
	return &RemoteSSHInspector{
		defaultCwd:   defaultCwd,
		sftpProv:     sp,
		execProv:     ep,
		dirCache:     make(map[string]cachedEntry[[]DirItem]),
		gitCache:     make(map[string]cachedEntry[[]string]),
		scriptsCache: make(map[string]cachedEntry[[]ScriptEntry]),
	}
}

func (r *RemoteSSHInspector) IsWindows() bool    { return false }
func (r *RemoteSSHInspector) DefaultCwd() string { return r.defaultCwd }

func (r *RemoteSSHInspector) ListDir(ctx context.Context, dir string) ([]DirItem, error) {
	if r.sftpProv == nil {
		return nil, nil
	}
	target := strings.TrimSpace(dir)
	if target == "" {
		target = r.defaultCwd
	}

	r.mu.Lock()
	if hit, ok := r.dirCache[target]; ok && time.Now().Before(hit.expiresAt) {
		r.mu.Unlock()
		return hit.val, nil
	}
	r.mu.Unlock()

	cCtx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
	defer cancel()
	_, entries, err := r.sftpProv.ListDir(cCtx, target)
	if err != nil {
		return nil, err
	}
	items := make([]DirItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, DirItem{
			Name:  e.Name,
			IsDir: e.IsDir,
		})
	}

	r.mu.Lock()
	r.dirCache[target] = cachedEntry[[]DirItem]{val: items, expiresAt: time.Now().Add(2500 * time.Millisecond)}
	r.mu.Unlock()
	return items, nil
}

func (r *RemoteSSHInspector) ReadSmallFile(ctx context.Context, filePath string, maxBytes int64) ([]byte, error) {
	if r.sftpProv == nil {
		return nil, nil
	}
	return r.sftpProv.ReadFile(ctx, filePath, maxBytes)
}

func (r *RemoteSSHInspector) GitBranches(ctx context.Context, cwd string) ([]string, error) {
	if r.execProv == nil {
		return nil, nil
	}
	dir := strings.TrimSpace(cwd)
	if dir == "" {
		dir = "."
	}

	r.mu.Lock()
	if hit, ok := r.gitCache[dir]; ok && time.Now().Before(hit.expiresAt) {
		r.mu.Unlock()
		return hit.val, nil
	}
	r.mu.Unlock()

	cCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cmd := "cd " + relay.PosixQuote(dir) + " 2>/dev/null && git for-each-ref --format='%(refname:short)' refs/heads refs/remotes 2>/dev/null || true"
	res, err := r.execProv.ExecCommand(cCtx, cmd)
	if err != nil {
		return nil, err
	}
	var branches []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(res.Stdout, "\n") {
		b := strings.TrimSpace(line)
		if b != "" && !strings.HasSuffix(b, "/HEAD") && !seen[b] {
			seen[b] = true
			branches = append(branches, b)
		}
	}
	sort.Strings(branches)

	r.mu.Lock()
	r.gitCache[dir] = cachedEntry[[]string]{val: branches, expiresAt: time.Now().Add(4 * time.Second)}
	r.mu.Unlock()
	return branches, nil
}

func (r *RemoteSSHInspector) PackageScripts(ctx context.Context, cwd string) ([]ScriptEntry, error) {
	if r.sftpProv == nil {
		return nil, nil
	}
	dir := strings.TrimSpace(cwd)
	if dir == "" {
		dir = "."
	}

	r.mu.Lock()
	if hit, ok := r.scriptsCache[dir]; ok && time.Now().Before(hit.expiresAt) {
		r.mu.Unlock()
		return hit.val, nil
	}
	r.mu.Unlock()

	pkgPath := path.Join(dir, "package.json")
	cCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	raw, err := r.sftpProv.ReadFile(cCtx, pkgPath, 256*1024)
	if err != nil {
		return nil, nil
	}
	scripts := parsePackageJSONScripts(raw)

	r.mu.Lock()
	r.scriptsCache[dir] = cachedEntry[[]ScriptEntry]{val: scripts, expiresAt: time.Now().Add(4 * time.Second)}
	r.mu.Unlock()
	return scripts, nil
}

func (r *RemoteSSHInspector) WSLDistros(_ context.Context) ([]string, error) {
	return nil, nil
}
