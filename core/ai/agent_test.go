package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/settings"
	"github.com/shnex/shnex/core/transport"
)

type stubExecutor struct {
	runs atomic.Int32
}

func (s *stubExecutor) ExecCommand(_ context.Context, command string) (*transport.CommandResult, error) {
	s.runs.Add(1)
	return &transport.CommandResult{
		Command:    command,
		Stdout:     "MemTotal: 16384 MB\nAPI_KEY=sk-1234567890abcdefghijklmnop\n",
		ExitCode:   0,
		DurationMs: 12,
	}, nil
}

func TestRedactSecrets(t *testing.T) {
	raw := "export API_KEY=sk-proj1234567890abcdefghijkl\n-----BEGIN OPENSSH PRIVATE KEY-----\nabcdef\n-----END OPENSSH PRIVATE KEY-----"
	clean := RedactSecrets(raw)
	if strings.Contains(clean, "sk-proj") || strings.Contains(clean, "BEGIN OPENSSH") {
		t.Fatalf("failed to redact secrets: %s", clean)
	}
}

func TestAIServiceAutoReviewAndAskPolicies(t *testing.T) {
	var step atomic.Int32
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch n {
		case 1:
			// Turn 1 tool call: free -m
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]any{
										"name":      "run_ssh_command",
										"arguments": `{"command":"free -m"}`,
									},
								},
							},
						},
					},
				},
			})
		case 2:
			// Safety reviewer verdict: 96% safe (>= 90% standard threshold)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"safe":true,"confidence":96,"reason":"只读查询内存状态"}`,
						},
					},
				},
			})
		case 3:
			// Final assistant table summary
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "| 项目 | 数值 |\n| --- | --- |\n| 总内存 | 16 GiB |",
						},
					},
				},
			})
		case 4:
			// Turn 2 risky command: rm -rf /tmp/data
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_2",
									"type": "function",
									"function": map[string]any{
										"name":      "run_ssh_command",
										"arguments": `{"command":"rm -rf /tmp/data"}`,
									},
								},
							},
						},
					},
				},
			})
		case 5:
			// Safety reviewer flags low confidence / unsafe -> must pause for user approval!
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"safe":false,"confidence":40,"reason":"递归删除目录具有破坏性"}`,
						},
					},
				},
			})
		}
	}))
	defer mockLLM.Close()

	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	setStore, err := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"), bridge)
	if err != nil {
		t.Fatal(err)
	}
	_, err = setStore.UpsertAIProvider(ctx, settings.UpsertProviderInput{
		Name:      "openai",
		Protocol:  "openai",
		BaseURL:   mockLLM.URL,
		Model:     "gpt-mock",
		APIKey:    "sk-test-secret-key",
		IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewService(filepath.Join(t.TempDir(), "ai.json"), bridge, setStore)
	exec := &stubExecutor{}

	// 1. Safe read-only command passes AutoReview (96% >= 90%) with "已批准 · 审核模型" AND redacts stdout secrets!
	res1, err := svc.RunTurn(ctx, "aws", exec, ChatRequest{
		Messages: []Message{{Role: "user", Content: "查询内存"}},
		Policy:   PolicyAutoReview,
	})
	if err != nil {
		t.Fatalf("RunTurn 1: %v", err)
	}
	if exec.runs.Load() != 1 || len(res1.Steps) != 1 || res1.Steps[0].ApprovalNote != "已批准 · 审核模型" {
		t.Fatalf("unexpected auto_review step: %+v", res1.Steps)
	}
	if strings.Contains(res1.Steps[0].Stdout, "sk-12345") {
		t.Fatalf("expected secret in stdout to be redacted, got: %s", res1.Steps[0].Stdout)
	}

	// 2. Risky command fails AutoReview (40% < 90%) -> pauses and returns PendingApproval without executing!
	res2, err := svc.RunTurn(ctx, "aws", exec, ChatRequest{
		Messages: []Message{{Role: "user", Content: "清理目录"}},
		Policy:   PolicyAutoReview,
	})
	if err != nil {
		t.Fatalf("RunTurn 2: %v", err)
	}
	if res2.PendingApproval == nil || exec.runs.Load() != 1 {
		t.Fatalf("expected risky command to require user approval without running: %+v", res2)
	}
}
