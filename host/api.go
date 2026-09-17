package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultChatModel = "gpt-4o-mini"
	defaultAPIBase   = "https://api.openai.com/v1"
	toolFreeSystem   = "你是浏览器侧栏助手。只能根据用户消息和只读页面上下文回答，不能操作网页、执行命令、搜索或调用任何工具。"
)

// ChatAPIConfig 是一份 OpenAI 兼容 Chat Completions 配置。
type ChatAPIConfig struct {
	BaseURL string `json:"apiBase"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ResolveChatAPI 按 请求 > 环境变量 > ~/.sidechat/config.json 解析通用 API。
func ResolveChatAPI(req TurnRequest) ChatAPIConfig {
	file := loadSidechatFileConfig()
	return ChatAPIConfig{
		BaseURL: firstNonEmpty(strings.TrimSpace(req.APIBase), strings.TrimSpace(os.Getenv("SIDECHAT_API_BASE")), file.BaseURL),
		APIKey:  firstNonEmpty(strings.TrimSpace(req.APIKey), strings.TrimSpace(os.Getenv("SIDECHAT_API_KEY")), file.APIKey),
		Model:   firstNonEmpty(strings.TrimSpace(req.Model), strings.TrimSpace(os.Getenv("SIDECHAT_MODEL")), file.Model, defaultChatModel),
	}
}

func loadSidechatFileConfig() ChatAPIConfig {
	home := SidechatHome()
	if home == "" {
		return ChatAPIConfig{}
	}
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		return ChatAPIConfig{}
	}
	var cfg ChatAPIConfig
	if json.Unmarshal(data, &cfg) != nil {
		return ChatAPIConfig{}
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	return cfg
}

// ChatCompletionsURL 接受 /v1 或已经带 /chat/completions 的基址。
func ChatCompletionsURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("需要 API 地址（设置、SIDECHAT_API_BASE 或 ~/.sidechat/config.json）")
	}
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base, nil
	}
	return base + "/chat/completions", nil
}

func applyDryRunAPIDefaults(cfg *ChatAPIConfig) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultAPIBase
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = defaultChatModel
	}
}

func diagnosticArgv(cfg ChatAPIConfig, sessionID, cwd string) []string {
	endpoint, err := ChatCompletionsURL(cfg.BaseURL)
	if err != nil {
		endpoint = strings.TrimSpace(cfg.BaseURL)
	}
	argv := []string{"openai-compat", endpoint, "--model", cfg.Model}
	if sessionID != "" {
		argv = append(argv, "--session", sessionID)
	}
	if cwd != "" {
		argv = append(argv, "--cwd", cwd)
	}
	return argv
}

// BuildChatMessages 把历史轮次、当前页快照和本轮用户原文编成 Chat Completions messages。
func BuildChatMessages(browser BrowserContext, userText string, history []SessionMessage, maxPageChars int) []chatMessage {
	sys := toolFreeSystem
	if page := AssembleBrowserContext(browser, maxPageChars); page != "" {
		sys += "\n\n" + page
	}
	out := []chatMessage{{Role: "system", Content: sys}}
	for _, m := range history {
		role := strings.TrimSpace(m.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		out = append(out, chatMessage{Role: role, Content: m.Text})
	}
	out = append(out, chatMessage{Role: "user", Content: strings.TrimSpace(userText)})
	return out
}

func newChatCompletionRequest(ctx context.Context, cfg ChatAPIConfig, messages []chatMessage) (*http.Request, error) {
	endpoint, err := ChatCompletionsURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("需要模型名")
	}
	body, err := json.Marshal(chatCompletionRequest{
		Model:    cfg.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	return req, nil
}

type openaiStreamChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
		} `json:"delta"`
		Message *struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

// parseOpenAIStreamLine 解析一行 SSE 或裸 JSON，抽出 thinking / text。
func parseOpenAIStreamLine(line string) (thinking string, text string, done bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return "", "", false, nil
	}
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	}
	if line == "" {
		return "", "", false, nil
	}
	if line == "[DONE]" {
		return "", "", true, nil
	}
	var chunk openaiStreamChunk
	if json.Unmarshal([]byte(line), &chunk) != nil {
		return "", "", false, nil
	}
	if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
		return "", "", false, fmt.Errorf("%s", chunk.Error.Message)
	}
	if len(chunk.Choices) == 0 {
		return "", "", false, nil
	}
	delta := chunk.Choices[0].Delta
	thinking = firstNonEmpty(delta.ReasoningContent, delta.Reasoning)
	text = delta.Content
	if chunk.Choices[0].Message != nil {
		if text == "" {
			text = chunk.Choices[0].Message.Content
		}
		if thinking == "" {
			thinking = chunk.Choices[0].Message.ReasoningContent
		}
	}
	return thinking, text, false, nil
}

func consumeOpenAIStream(r io.Reader, onEvent EventHandler) (assistant string, thinking string, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		th, tx, done, perr := parseOpenAIStreamLine(scanner.Text())
		if perr != nil {
			return assistant, thinking, perr
		}
		if th != "" {
			thinking += th
			if onEvent != nil {
				onEvent(StreamEvent{Type: EventThinking, Text: th, RawType: "openai.reasoning"})
			}
		}
		if tx != "" {
			assistant += tx
			if onEvent != nil {
				onEvent(StreamEvent{Type: EventPartial, Text: tx, RawType: "openai.delta"})
			}
		}
		if done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return assistant, thinking, err
	}
	// 增量已经用 EventPartial 推过；再发整段 EventText 会让侧栏把字数算两遍。
	return assistant, thinking, nil
}
