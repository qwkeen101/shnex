package completion

import "context"

// CandidateKind identifies the semantic type of a completion item for UI icons and sorting.
type CandidateKind string

const (
	KindCommand    CandidateKind = "command"
	KindSubcommand CandidateKind = "subcommand"
	KindGitBranch  CandidateKind = "git_branch"
	KindNpmScript  CandidateKind = "npm_script"
	KindSSHHost    CandidateKind = "ssh_host"
	KindWSLDistro  CandidateKind = "wsl_distro"
	KindDir        CandidateKind = "dir"
	KindFile       CandidateKind = "file"
	KindFlag       CandidateKind = "flag"
)

// Candidate represents a single native autocomplete suggestion.
type Candidate struct {
	Label       string        `json:"label"`
	InsertText  string        `json:"insert_text"`
	GhostSuffix string        `json:"ghost_suffix"`
	ReplaceLen  int           `json:"replace_len"` // number of runes before cursor to replace when accepting
	Kind        CandidateKind `json:"kind"`
	Source      string        `json:"source"`
	Detail      string        `json:"detail,omitempty"`
}

// SuggestRequest describes the live command line before the cursor in a terminal pane.
type SuggestRequest struct {
	Line string `json:"line"`
	Cwd  string `json:"cwd,omitempty"`
}

// SuggestResponse returns ranked candidates and replacement metadata.
type SuggestResponse struct {
	TokenPrefix string      `json:"token_prefix"`
	ReplaceLen  int         `json:"replace_len"`
	Candidates  []Candidate `json:"candidates"`
}

// DirItem represents a file or directory entry returned by a WorkspaceInspector.
type DirItem struct {
	Name  string
	IsDir bool
}

// WorkspaceInspector abstracts local filesystem/registry vs remote SSH SFTP/exec inspection.
type WorkspaceInspector interface {
	IsWindows() bool
	DefaultCwd() string
	ListDir(ctx context.Context, dir string) ([]DirItem, error)
	ReadSmallFile(ctx context.Context, filePath string, maxBytes int64) ([]byte, error)
	GitBranches(ctx context.Context, cwd string) ([]string, error)
	PackageScripts(ctx context.Context, cwd string) ([]ScriptEntry, error)
	WSLDistros(ctx context.Context) ([]string, error)
}

// ScriptEntry represents a discovered script in package.json or Makefile.
type ScriptEntry struct {
	Name    string
	Command string
}
