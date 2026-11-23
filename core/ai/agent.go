package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/settings"
	"github.com/shnex/shnex/core/transport"
)

const legacyAISecretKeyRef = "shnex:ai:api_key"

// ApprovalPolicy controls how AI-proposed SSH commands are gated.
type ApprovalPolicy string

const (
	PolicyAsk        ApprovalPolicy = "ask"         // 询问：每次均需人工确认
	PolicyAutoReview ApprovalPolicy = "auto_review" // 自动审核：交由审核模型按严格度阈值判定
	PolicyAllowAll   ApprovalPolicy = "allow_all"   // 完全允许：直接执行
	// Backwards-compatible aliases
	PolicyAuto   ApprovalPolicy = "auto"
	PolicyManual ApprovalPolicy = "manual"
)

// Config holds legacy single-provider view for backwards compatibility.
type Config struct {
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"has_api_key"`
}

type UpdateConfigInput struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
}

// Message represents a chat message in the Built-in AI Ops panel.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ExecutedStep describes a shell command proposed, reviewed, or executed during an AI turn.
type ExecutedStep struct {
	CallID       string `json:"call_id"`
	Command      string `json:"command"`
	Status       string `json:"status"`        // "executed", "pending_approval", "failed", "rejected"
	ApprovalNote string `json:"approval_note"` // e.g. "已批准 · 审核模型" or "已批准 · 人工确认"
	ReviewReason string `json:"review_reason,omitempty"`
	Confidence   int    `json:"confidence,omitempty"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	ExitCode     int    `json:"exit_code"`
	DurationMs   int64  `json:"duration_ms"`
}

// ChatRequest carries conversation history, provider selection, terminal context, and approval policy.
type ChatRequest struct {
	Messages        []Message      `json:"messages"`
	Policy          ApprovalPolicy `json:"policy"`
	ProviderID      string         `json:"provider_id,omitempty"`
	Model           string         `json:"model,omitempty"`
	TerminalContext string         `json:"terminal_context,omitempty"`
}

// ChatResponse returns updated messages, executed/pending command cards, and assistant reply.
type ChatResponse struct {
	Reply           string         `json:"reply"`
	Steps           []ExecutedStep `json:"steps,omitempty"`
	PendingApproval *ExecutedStep  `json:"pending_approval,omitempty"`
	History         []Message      `json:"history"`
}

// Service manages AI provider resolution, secret redaction, safety review, and SSH tool turns.
type Service struct {
	cfgPath       string
	bridge        platform.PlatformBridge
	settingsStore *settings.Store
	client        *http.Client

	mu  sync.RWMutex
	cfg Config
}

func NewService(cfgPath string, bridge platform.PlatformBridge, stores ...*settings.Store) *Service {
	if strings.TrimSpace(cfgPath) == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		cfgPath = filepath.Join(dir, "shnex", "ai_config.json")
	}
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)

	var st *settings.Store
	if len(stores) > 0 {
		st = stores[0]
	}

	s := &Service{
		cfgPath:       cfgPath,
		bridge:        bridge,
		settingsStore: st,
		client:        &http.Client{Timeout: 60 * time.Second},
		cfg: Config{
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}
	s.loadLegacy()
	return s
}

func (s *Service) GetConfig(ctx context.Context) Config {
	if s.settingsStore != nil {
		snap := s.settingsStore.Get()
		for _, p := range snap.AI.Providers {
			if p.IsDefault {
				return Config{BaseURL: p.BaseURL, Model: p.Model, HasAPIKey: p.HasAPIKey}
			}
		}
		if len(snap.AI.Providers) > 0 {
			p := snap.AI.Providers[0]
			return Config{BaseURL: p.BaseURL, Model: p.Model, HasAPIKey: p.HasAPIKey}
		}
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if s.bridge != nil {
		if k, err := s.bridge.GetSecret(ctx, legacyAISecretKeyRef); err == nil && strings.TrimSpace(k) != "" {
			cfg.HasAPIKey = true
		}
	}
	return cfg
}

func (s *Service) UpdateConfig(ctx context.Context, in UpdateConfigInput) (Config, error) {
	if s.settingsStore != nil {
		snap := s.settingsStore.Get()
		existingID := ""
		for _, p := range snap.AI.Providers {
			if p.IsDefault {
				existingID = p.ID
				break
			}
		}
		_, err := s.settingsStore.UpsertAIProvider(ctx, settings.UpsertProviderInput{
			ID:        existingID,
			Name:      "default",
			Protocol:  "openai",
			BaseURL:   in.BaseURL,
			Model:     in.Model,
			APIKey:    in.APIKey,
			IsDefault: true,
		})
		if err != nil {
			return Config{}, err
		}
		return s.GetConfig(ctx), nil
	}

	s.mu.Lock()
	if strings.TrimSpace(in.BaseURL) != "" {
		s.cfg.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	}
	if strings.TrimSpace(in.Model) != "" {
		s.cfg.Model = strings.TrimSpace(in.Model)
	}
	raw, _ := json.MarshalIndent(s.cfg, "", "  ")
	_ = os.WriteFile(s.cfgPath, raw, 0o600)
	s.mu.Unlock()

	if strings.TrimSpace(in.APIKey) != "" {
		if s.bridge == nil {
			return Config{}, errors.New("ai: PlatformBridge unavailable to store API key")
		}
		if err := s.bridge.SetSecret(ctx, legacyAISecretKeyRef, strings.TrimSpace(in.APIKey)); err != nil {
			return Config{}, err
		}
	}
	return s.GetConfig(ctx), nil
}

func (s *Service) resolveEndpointAndKey(ctx context.Context, providerID, modelOverride string) (baseURL, apiKey, model string, err error) {
	if s.settingsStore != nil {
		prov, key, rErr := s.settingsStore.ResolveProviderSecret(ctx, providerID)
		if rErr == nil {
			m := prov.Model
			if strings.TrimSpace(modelOverride) != "" {
				m = strings.TrimSpace(modelOverride)
			}
			return prov.BaseURL, key, m, nil
		}
		// Fallback to legacy key if configured
		if s.bridge != nil {
			if legacyKey, lErr := s.bridge.GetSecret(ctx, legacyAISecretKeyRef); lErr == nil && strings.TrimSpace(legacyKey) != "" {
				cfg := s.GetConfig(ctx)
				m := cfg.Model
				if strings.TrimSpace(modelOverride) != "" {
					m = strings.TrimSpace(modelOverride)
				}
				return cfg.BaseURL, strings.TrimSpace(legacyKey), m, nil
			}
		}
		return "", "", "", rErr
	}

	if s.bridge == nil {
		return "", "", "", errors.New("ai: PlatformBridge not configured")
	}
	key, gErr := s.bridge.GetSecret(ctx, legacyAISecretKeyRef)
	if gErr != nil || strings.TrimSpace(key) == "" {
		return "", "", "", errors.New("尚未配置 AI 模型服务商与 API Key（请打开右上角「设置 -> AI 助理」添加提供商）")
	}
	cfg := s.GetConfig(ctx)
	m := cfg.Model
	if strings.TrimSpace(modelOverride) != "" {
		m = strings.TrimSpace(modelOverride)
	}
	return cfg.BaseURL, strings.TrimSpace(key), m, nil
}

// RunTurn executes an OpenAI-compatible tool-calling loop with optional secret redaction
// and LLM guardrail auto-review (80% / 90% / 97% confidence threshold).
func (s *Service) RunTurn(ctx context.Context, targetHost string, exec transport.CommandExecutor, req ChatRequest) (*ChatResponse, error) {
	baseURL, apiKey, model, err := s.resolveEndpointAndKey(ctx, req.ProviderID, req.Model)
	if err != nil {
		return nil, err
	}

	redactEnabled := true
	strictness := "standard"
	reviewProvID := req.ProviderID
	defaultPerm := ApprovalPolicy(PolicyAutoReview)

	if s.settingsStore != nil {
		aiSet := s.settingsStore.Get().AI
		redactEnabled = aiSet.RedactSecrets
		if aiSet.ReviewStrictness != "" {
			strictness = aiSet.ReviewStrictness
		}
		if aiSet.ReviewProviderID != "" {
			reviewProvID = aiSet.ReviewProviderID
		}
		if aiSet.DefaultPermission != "" {
			defaultPerm = ApprovalPolicy(aiSet.DefaultPermission)
		}
	}

	policy := normalizePolicy(req.Policy, defaultPerm)

	termCtxBlock := ""
	if strings.TrimSpace(req.TerminalContext) != "" {
		ctxText := req.TerminalContext
		if redactEnabled {
			ctxText = RedactSecrets(ctxText)
		}
		termCtxBlock = "\n\n当前终端最近输出上下文：\n```\n" + ctxText + "\n```"
	}

	sysPrompt := fmt.Sprintf(
		"你是 Shnex 内置的远程服务器 AI 运维与开发助手，当前已连接主机 %s。"+
			"当需要查询系统状态、日志、文件或执行用户要求的操作时，请调用 run_ssh_command 工具执行精简的非交互式 Linux 命令。"+
			"请用简洁清晰的中文与 Markdown（如表格、列表、代码块）汇报结果。%s",
		targetHost,
		termCtxBlock,
	)

	history := make([]Message, 0, len(req.Messages)+4)
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		history = append(history, Message{Role: "system", Content: sysPrompt})
	}
	for _, m := range req.Messages {
		if redactEnabled && m.Content != "" {
			m.Content = RedactSecrets(m.Content)
		}
		history = append(history, m)
	}

	var steps []ExecutedStep

	for round := 0; round < 4; round++ {
		assistantMsg, cErr := s.callCompletion(ctx, baseURL, apiKey, model, history, true)
		if cErr != nil {
			return nil, cErr
		}
		history = append(history, assistantMsg)

		if len(assistantMsg.ToolCalls) == 0 {
			return &ChatResponse{
				Reply:   assistantMsg.Content,
				Steps:   steps,
				History: stripSystem(history),
			}, nil
		}

		for _, tc := range assistantMsg.ToolCalls {
			if tc.Function.Name != "run_ssh_command" {
				continue
			}
			var args struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			cmdStr := strings.TrimSpace(args.Command)
			if cmdStr == "" {
				continue
			}

			approvalNote := "已批准 · 完全允许"
			var confidence int
			var reviewReason string

			switch policy {
			case PolicyAsk:
				pending := ExecutedStep{
					CallID:       tc.ID,
					Command:      cmdStr,
					Status:       "pending_approval",
					ApprovalNote: "等待人工审批",
				}
				steps = append(steps, pending)
				return &ChatResponse{
					Reply:           assistantMsg.Content,
					Steps:           steps,
					PendingApproval: &pending,
					History:         stripSystem(history),
				}, nil

			case PolicyAutoReview:
				approved, conf, reason := s.reviewCommandSafety(ctx, reviewProvID, baseURL, apiKey, model, cmdStr, strictness)
				confidence = conf
				reviewReason = reason
				if !approved {
					pending := ExecutedStep{
						CallID:       tc.ID,
						Command:      cmdStr,
						Status:       "pending_approval",
						ApprovalNote: fmt.Sprintf("审核模型建议人工确认 (%d%%)", conf),
						ReviewReason: reason,
						Confidence:   conf,
					}
					steps = append(steps, pending)
					return &ChatResponse{
						Reply:           assistantMsg.Content,
						Steps:           steps,
						PendingApproval: &pending,
						History:         stripSystem(history),
					}, nil
				}
				approvalNote = "已批准 · 审核模型"
			}

			cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			res, execErr := exec.ExecCommand(cmdCtx, cmdStr)
			cancel()

			step := ExecutedStep{
				CallID:       tc.ID,
				Command:      cmdStr,
				Status:       "executed",
				ApprovalNote: approvalNote,
				Confidence:   confidence,
				ReviewReason: reviewReason,
			}
			var toolOutput string
			if execErr != nil {
				step.Status = "failed"
				step.Stderr = execErr.Error()
				toolOutput = "ERROR: " + execErr.Error()
			} else {
				outStr := res.Stdout
				errStr := res.Stderr
				if redactEnabled {
					outStr = RedactSecrets(outStr)
					errStr = RedactSecrets(errStr)
				}
				step.Stdout = outStr
				step.Stderr = errStr
				step.ExitCode = res.ExitCode
				step.DurationMs = res.DurationMs
				if res.ExitCode != 0 {
					step.Status = "failed"
				}
				toolOutput = fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", res.ExitCode, outStr, errStr)
			}
			steps = append(steps, step)
			history = append(history, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolOutput,
			})
		}
	}

	return &ChatResponse{
		Reply:   "已达到单轮工具执行上限。",
		Steps:   steps,
		History: stripSystem(history),
	}, nil
}

func (s *Service) reviewCommandSafety(
	ctx context.Context,
	reviewProvID string,
	fallbackURL, fallbackKey, fallbackModel string,
	command string,
	strictness string,
) (approved bool, confidence int, reason string) {
	threshold := 90
	switch strictness {
	case "loose":
		threshold = 80
	case "strict":
		threshold = 97
	}

	rURL, rKey, rModel := fallbackURL, fallbackKey, fallbackModel
	if reviewProvID != "" && s.settingsStore != nil {
		if prov, key, err := s.settingsStore.ResolveProviderSecret(ctx, reviewProvID); err == nil {
			rURL, rKey, rModel = prov.BaseURL, key, prov.Model
		}
	}

	prompt := fmt.Sprintf(
		`评估以下即将在 Linux 服务器上运行的 Shell 命令是否安全（不会造成数据损毁、提权破坏、格式化、恶意网络外发或服务中断）。
仅返回单行合法 JSON，不要包含 Markdown 代码块：
{"safe": true或false, "confidence": 0到100整数把握度, "reason": "简短中文原因"}
待审核命令：%s`,
		command,
	)

	revCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	msg, err := s.callCompletion(revCtx, rURL, rKey, rModel, []Message{
		{Role: "user", Content: prompt},
	}, false)
	if err != nil {
		return false, 0, "审核模型调用失败，转为人工确认"
	}

	raw := strings.TrimSpace(msg.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var verdict struct {
		Safe       bool   `json:"safe"`
		Confidence int    `json:"confidence"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return false, 0, "审核模型返回格式不可解析，要求人工确认"
	}
	if !verdict.Safe || verdict.Confidence < threshold {
		return false, verdict.Confidence, verdict.Reason
	}
	return true, verdict.Confidence, verdict.Reason
}

func normalizePolicy(p ApprovalPolicy, fallback ApprovalPolicy) ApprovalPolicy {
	switch p {
	case PolicyAsk, PolicyManual:
		return PolicyAsk
	case PolicyAllowAll, PolicyAuto:
		return PolicyAllowAll
	case PolicyAutoReview:
		return PolicyAutoReview
	default:
		if fallback == PolicyAsk || fallback == PolicyAllowAll || fallback == PolicyAutoReview {
			return fallback
		}
		return PolicyAutoReview
	}
}

func (s *Service) callCompletion(ctx context.Context, baseURL, apiKey, model string, msgs []Message, withTools bool) (Message, error) {
	bodyMap := map[string]any{
		"model":    model,
		"messages": msgs,
	}
	if withTools {
		bodyMap["tools"] = []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "run_ssh_command",
					"description": "在当前 SSH 远程主机上执行单条非交互式 Shell 命令并返回 stdout/stderr。",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]any{
								"type":        "string",
								"description": "非交互式 Linux/POSIX 命令，例如 free -m 或 ls -la /tmp",
							},
						},
						"required": []string{"command"},
					},
				},
			},
		}
	}

	payload, _ := json.Marshal(bodyMap)
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("请求模型接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 300 {
		return Message{}, fmt.Errorf("模型接口返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Choices) == 0 {
		return Message{}, errors.New("解析模型响应失败")
	}
	return parsed.Choices[0].Message, nil
}

func stripSystem(msgs []Message) []Message {
	if len(msgs) > 0 && msgs[0].Role == "system" {
		return msgs[1:]
	}
	return msgs
}

func (s *Service) loadLegacy() {
	raw, err := os.ReadFile(s.cfgPath)
	if err != nil {
		return
	}
	var c Config
	if json.Unmarshal(raw, &c) == nil {
		if c.BaseURL != "" {
			s.cfg.BaseURL = c.BaseURL
		}
		if c.Model != "" {
			s.cfg.Model = c.Model
		}
	}
}
