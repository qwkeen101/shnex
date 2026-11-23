package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineAllFiveCompletionSources(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()

	// 1. Setup fake .git repository with loose ref + packed-refs
	headsDir := filepath.Join(ws, ".git", "refs", "heads", "feature")
	if err := os.MkdirAll(headsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(headsDir, "powerline"), []byte("1111111111111111111111111111111111111111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	packed := "# pack-refs with: peeled fully-peeled sorted\n2222222222222222222222222222222222222222 refs/heads/main\n3333333333333333333333333333333333333333 refs/remotes/origin/release-0.3\n"
	if err := os.WriteFile(filepath.Join(ws, ".git", "packed-refs"), []byte(packed), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Setup package.json scripts
	pkgJSON := `{"name":"demo","scripts":{"dev":"vite","build":"tsc && vite build","lint":"eslint ."}}`
	if err := os.WriteFile(filepath.Join(ws, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Setup SSH config file
	sshCfg := filepath.Join(ws, "ssh_config")
	sshContent := "Host prod-tokyo\n  HostName 203.0.113.10\n  User ubuntu\n\nHost *\n  ServerAliveInterval 30\n"
	if err := os.WriteFile(sshCfg, []byte(sshContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Setup filesystem files including filename with spaces
	if err := os.WriteFile(filepath.Join(ws, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "my secret notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	insp := NewLocalInspector(ws)
	insp.wslOverrideFn = func() []string {
		return []string{"Ubuntu-24.04", "Debian"}
	}

	eng := NewEngine(func() []SSHHostHint {
		return []SSHHostHint{
			{Alias: "staging-eu", Detail: "root@198.51.100.8"},
		}
	})
	eng.sshConfigPath = sshCfg

	// Case 1: Git branch completion
	gitResp := eng.Suggest(ctx, insp, SuggestRequest{Line: "git checkout fe", Cwd: ws})
	if len(gitResp.Candidates) != 1 || gitResp.Candidates[0].Label != "feature/powerline" {
		t.Fatalf("expected feature/powerline branch, got %+v", gitResp.Candidates)
	}
	if gitResp.Candidates[0].GhostSuffix != "ature/powerline " || gitResp.Candidates[0].InsertText != "feature/powerline " {
		t.Fatalf("unexpected git ghost/insert: %+v", gitResp.Candidates[0])
	}
	// Once branch is completed (`git checkout feature/powerline `), should not re-suggest branches
	afterGit := eng.Suggest(ctx, insp, SuggestRequest{Line: "git checkout feature/powerline ", Cwd: ws})
	if len(afterGit.Candidates) != 0 {
		t.Fatalf("expected 0 candidates after branch completed, got %+v", afterGit.Candidates)
	}

	// Case 2: npm script completion
	npmResp := eng.Suggest(ctx, insp, SuggestRequest{Line: "npm run b", Cwd: ws})
	if len(npmResp.Candidates) != 1 || npmResp.Candidates[0].Label != "build" {
		t.Fatalf("expected npm script build, got %+v", npmResp.Candidates)
	}
	if npmResp.Candidates[0].Detail != "tsc && vite build" {
		t.Fatalf("expected script detail, got %q", npmResp.Candidates[0].Detail)
	}

	// Case 3: SSH alias completion (from ssh_config + saved hosts)
	sshResp := eng.Suggest(ctx, insp, SuggestRequest{Line: "ssh pr", Cwd: ws})
	if len(sshResp.Candidates) != 1 || sshResp.Candidates[0].Label != "prod-tokyo" {
		t.Fatalf("expected ssh alias prod-tokyo, got %+v", sshResp.Candidates)
	}
	sshSaved := eng.Suggest(ctx, insp, SuggestRequest{Line: "ssh -p 2222 st", Cwd: ws})
	if len(sshSaved.Candidates) != 1 || sshSaved.Candidates[0].Label != "staging-eu" {
		t.Fatalf("expected saved ssh alias staging-eu, got %+v", sshSaved.Candidates)
	}

	// Case 4: WSL registered distribution completion
	wslResp := eng.Suggest(ctx, insp, SuggestRequest{Line: "wsl -d Ub", Cwd: ws})
	if len(wslResp.Candidates) != 1 || wslResp.Candidates[0].Label != "Ubuntu-24.04" {
		t.Fatalf("expected WSL distro Ubuntu-24.04, got %+v", wslResp.Candidates)
	}

	// Case 5: `cat` reading filename with spaces (both single-word prefix and multi-word unquoted prefix)
	catResp := eng.Suggest(ctx, insp, SuggestRequest{Line: "cat my", Cwd: ws})
	if len(catResp.Candidates) != 1 {
		t.Fatalf("expected 1 space-filename candidate, got %+v", catResp.Candidates)
	}
	c0 := catResp.Candidates[0]
	if c0.InsertText != "'my secret notes.txt' " || c0.ReplaceLen != 2 || c0.GhostSuffix != " secret notes.txt " {
		t.Fatalf("unexpected quoted completion for space filename: %+v", c0)
	}

	catMulti := eng.Suggest(ctx, insp, SuggestRequest{Line: "cat my sec", Cwd: ws})
	if len(catMulti.Candidates) != 1 || catMulti.Candidates[0].InsertText != "'my secret notes.txt' " || catMulti.Candidates[0].ReplaceLen != 6 {
		t.Fatalf("unexpected multi-word space filename completion: %+v", catMulti.Candidates)
	}

	// Case 6: History ONLY sorts discovered candidates and NEVER suggests deleted files
	eng.RecordSelection(KindFile, "my secret notes.txt")
	nvimList := eng.Suggest(ctx, insp, SuggestRequest{Line: "nvim ", Cwd: ws})
	if len(nvimList.Candidates) < 2 || nvimList.Candidates[0].Label != "my secret notes.txt" {
		t.Fatalf("expected history-boosted 'my secret notes.txt' first among existing files, got %+v", nvimList.Candidates)
	}

	// Delete 'my secret notes.txt' and verify it disappears immediately despite history score
	if err := os.Remove(filepath.Join(ws, "my secret notes.txt")); err != nil {
		t.Fatal(err)
	}
	afterDel := eng.Suggest(ctx, insp, SuggestRequest{Line: "nvim ", Cwd: ws})
	for _, cand := range afterDel.Candidates {
		if cand.Label == "my secret notes.txt" {
			t.Fatalf("deleted file must never be suggested from history: %+v", afterDel.Candidates)
		}
	}
}

func TestCdDirectoryCompletionWithDotDirsAndNormalDirs(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(nil)

	// 1. Fresh Ubuntu cloud server home directory with ONLY dot-directories (.ssh, .cache, .claude)
	freshHome := t.TempDir()
	for _, d := range []string{".ssh", ".cache", ".claude"} {
		if err := os.MkdirAll(filepath.Join(freshHome, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(freshHome, ".bashrc"), []byte("# bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	freshInsp := NewLocalInspector(freshHome)
	cdFresh := eng.Suggest(ctx, freshInsp, SuggestRequest{Line: "cd ", Cwd: freshHome})
	if len(cdFresh.Candidates) != 3 {
		t.Fatalf("expected 3 hidden dirs (.claude/, .cache/, .ssh/) when no normal dirs exist, got %+v", cdFresh.Candidates)
	}
	if cdFresh.Candidates[0].Label != ".cache/" || cdFresh.Candidates[1].Label != ".claude/" || cdFresh.Candidates[2].Label != ".ssh/" {
		t.Fatalf("unexpected dot-dir order: %+v", cdFresh.Candidates)
	}

	// 2. Once normal directories exist, normal directories always rank ABOVE dot-directories on `cd `
	for _, d := range []string{"workspace", "app"} {
		if err := os.MkdirAll(filepath.Join(freshHome, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cdMixed := eng.Suggest(ctx, freshInsp, SuggestRequest{Line: "cd ", Cwd: freshHome})
	if len(cdMixed.Candidates) != 5 {
		t.Fatalf("expected 5 total dirs (2 normal + 3 dot), got %+v", cdMixed.Candidates)
	}
	if cdMixed.Candidates[0].Label != "app/" || cdMixed.Candidates[1].Label != "workspace/" {
		t.Fatalf("expected normal dirs first (app/, workspace/), got %+v", cdMixed.Candidates)
	}

	// 3. Empty leaf directory still offers `../` on `cd `
	emptyLeaf := filepath.Join(freshHome, "workspace", "empty")
	if err := os.MkdirAll(emptyLeaf, 0o755); err != nil {
		t.Fatal(err)
	}
	cdEmpty := eng.Suggest(ctx, freshInsp, SuggestRequest{Line: "cd ", Cwd: emptyLeaf})
	if len(cdEmpty.Candidates) != 1 || cdEmpty.Candidates[0].Label != "../" {
		t.Fatalf("expected ../ fallback in empty leaf directory, got %+v", cdEmpty.Candidates)
	}
}

func TestIsFirstWordFilesystemPath(t *testing.T) {
	cases := map[string]bool{
		"/":              false,
		"/model":         false,
		"/help":          false,
		"/m":             false,
		"git":            false,
		"./":             true,
		"./script":       true,
		"../run":         true,
		".venv/bin/act":  true,
		"scripts/run.sh": true,
		"~/bin/tool":     true,
		"/usr/":          true,
		"/usr/bin/py":    true,
		`C:\tools`:       true,
	}
	for tok, want := range cases {
		if got := isFirstWordFilesystemPath(tok); got != want {
			t.Errorf("isFirstWordFilesystemPath(%q) = %v, want %v", tok, got, want)
		}
	}
}

func TestFirstWordSlashCommandsSkipRootPathCompletion(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	insp := NewLocalInspector(ws)
	eng := NewEngine(nil)

	// AI CLI slash commands typed as the first word must never fall into root-directory completion.
	for _, line := range []string{"/", "/clear", "/compact", "/help", "/model", "/m", "/resume"} {
		resp := eng.Suggest(ctx, insp, SuggestRequest{Line: line, Cwd: ws})
		if len(resp.Candidates) != 0 {
			t.Fatalf("expected no candidates for slash command %q, got %+v", line, resp.Candidates)
		}
	}

	hasSrc := func(cands []Candidate) bool {
		for _, c := range cands {
			if strings.Contains(c.Label, "src") {
				return true
			}
		}
		return false
	}

	// Relative first-word paths keep completing.
	if resp := eng.Suggest(ctx, insp, SuggestRequest{Line: "./", Cwd: ws}); !hasSrc(resp.Candidates) {
		t.Fatalf("expected ./ to complete src/, got %+v", resp.Candidates)
	}

	// Forward slashes keep the absolute temp path free of shell escapes on Windows;
	// a temp dir with spaces (e.g. a Windows user name) would split into several words.
	absPrefix := filepath.ToSlash(ws) + "/"
	if strings.Contains(absPrefix, " ") {
		return
	}

	// Multi-segment absolute first-word paths keep completing.
	if resp := eng.Suggest(ctx, insp, SuggestRequest{Line: absPrefix, Cwd: ws}); !hasSrc(resp.Candidates) {
		t.Fatalf("expected %q to complete src/, got %+v", absPrefix, resp.Candidates)
	}

	// Argument-position paths (`cd /...`) are unaffected by the first-word rule.
	if resp := eng.Suggest(ctx, insp, SuggestRequest{Line: "cd " + absPrefix, Cwd: ws}); !hasSrc(resp.Candidates) {
		t.Fatalf("expected cd %q to complete src/, got %+v", absPrefix, resp.Candidates)
	}
}
