package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// EventHandler receives typed stream events during a turn.
type EventHandler func(StreamEvent)

// Runner 执行一轮 OpenAI 兼容 Chat Completions（或 dry-run / fixture）。
type Runner struct {
	DryRun        bool
	FixtureStream []string
	ForceFixture  bool
	StoreDir      string
	HTTPClient    *http.Client
	// DoHTTP 覆盖真实 HTTP，供测试注入。
	DoHTTP func(*http.Request) (*http.Response, error)

	turnMu sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
}

// TurnRequest is one send from the extension/host protocol.
type TurnRequest struct {
	RequestID       string
	SessionID       string
	Cwd             string
	UserText        string
	Browser         BrowserContext
	Model           string
	APIBase         string
	APIKey          string
	Mode            string
	ReasoningEffort string
	MaxTurns        int
	AlwaysApprove   bool
	MaxPageChars    int
}

// TurnResult summarizes a completed turn.
type TurnResult struct {
	SessionID  string
	Argv       []string
	Cmd        string
	Prompt     string
	PromptFile string
	DryRun     bool
	Err        error
}

func (r *Runner) httpDo(req *http.Request) (*http.Response, error) {
	if r.DoHTTP != nil {
		return r.DoHTTP(req)
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// RunTurn 组装页面上下文，调用通用 Chat Completions，并把增量转成现有 event 协议。
func (r *Runner) RunTurn(parent context.Context, req TurnRequest, onEvent EventHandler) TurnResult {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()

	if onEvent == nil {
		onEvent = func(StreamEvent) {}
	}

	cwd := ResolveTurnCwd(req.Cwd, "")
	prompt := AssemblePrompt(req.Browser, req.UserText, req.MaxPageChars)
	cfg := ResolveChatAPI(req)
	if r.DryRun || r.ForceFixture {
		applyDryRunAPIDefaults(&cfg)
	}

	sessionID := strings.TrimSpace(req.SessionID)
	result := TurnResult{
		SessionID: sessionID,
		Argv:      diagnosticArgv(cfg, sessionID, cwd),
		Cmd:       "openai-compat",
		Prompt:    prompt,
		DryRun:    r.DryRun || r.ForceFixture,
	}
	onEvent(StreamEvent{
		Type:    EventPartial,
		Text:    fmt.Sprintf("argv: %v", result.Argv),
		RawType: "host.argv",
	})

	if r.DryRun || r.ForceFixture {
		return r.runFixtureTurn(req, sessionID, result, onEvent)
	}

	if _, err := ChatCompletionsURL(cfg.BaseURL); err != nil {
		result.Err = err
		onEvent(StreamEvent{Type: EventError, Text: err.Error()})
		return result
	}

	root := r.sessionRoot()
	var history []SessionMessage
	if sessionID != "" {
		if msgs, _, err := LoadStoredMessages(root, sessionID, 0); err == nil {
			history = msgs
		}
	}

	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
	}()

	httpReq, err := newChatCompletionRequest(ctx, cfg, BuildChatMessages(req.Browser, req.UserText, history, req.MaxPageChars))
	if err != nil {
		result.Err = err
		onEvent(StreamEvent{Type: EventError, Text: err.Error()})
		return result
	}

	resp, err := r.httpDo(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			onEvent(StreamEvent{Type: EventDone, RawType: "host.cancelled"})
			return result
		}
		result.Err = err
		onEvent(StreamEvent{Type: EventError, Text: err.Error()})
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		result.Err = fmt.Errorf("chat api %s: %s", resp.Status, strings.TrimSpace(string(body)))
		onEvent(StreamEvent{Type: EventError, Text: result.Err.Error()})
		return result
	}

	assistant, thinking, err := consumeOpenAIStream(resp.Body, onEvent)
	if err != nil {
		if ctx.Err() != nil {
			onEvent(StreamEvent{Type: EventDone, RawType: "host.cancelled"})
			return result
		}
		result.Err = err
		onEvent(StreamEvent{Type: EventError, Text: err.Error()})
		return result
	}

	if sessionID == "" {
		sessionID = newSessionID()
	}
	result.SessionID = sessionID
	onEvent(StreamEvent{Type: EventSession, SessionID: sessionID, RawType: "host.session"})

	if root != "" {
		sess, err := loadStoredSession(root, sessionID)
		if err != nil {
			sess = storedSession{Info: SessionInfo{ID: sessionID}}
		}
		appendTurn(&sess, cwd, cfg.Model, strings.TrimSpace(req.UserText), assistant, thinking)
		if err := saveStoredSession(root, sess); err != nil {
			onEvent(StreamEvent{Type: EventError, Text: "保存会话失败: " + err.Error()})
		}
	}

	if ctx.Err() != nil {
		onEvent(StreamEvent{Type: EventDone, RawType: "host.cancelled"})
		return result
	}
	onEvent(StreamEvent{Type: EventDone, RawType: "host.api_done"})
	return result
}

func (r *Runner) runFixtureTurn(req TurnRequest, sessionID string, result TurnResult, onEvent EventHandler) TurnResult {
	lines := r.FixtureStream
	if len(lines) == 0 {
		sid := firstNonEmpty(sessionID, "dry-session-1")
		lines = []string{
			`{"type":"system","subtype":"init","session_id":"` + sid + `","model":"` + defaultChatModel + `"}`,
			`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_0","role":"assistant","content":[]}},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"dry-run thinking… "}},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"dry-run ok"}},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"content_block_stop","index":1},"session_id":"` + sid + `"}`,
			`{"type":"stream_event","event":{"type":"message_stop"},"session_id":"` + sid + `"}`,
			`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"dry-run thinking… "},{"type":"text","text":"dry-run ok"}]},"session_id":"` + sid + `"}`,
			`{"type":"result","session_id":"` + sid + `","subtype":"success"}`,
		}
	}
	ts := NewTurnStream()
	for _, line := range lines {
		events, perr := ts.FeedLine(line)
		if perr != nil {
			onEvent(StreamEvent{Type: EventError, Text: perr.Error()})
			continue
		}
		for _, ev := range events {
			if ev.Type == EventSession && ev.SessionID != "" {
				result.SessionID = ev.SessionID
			}
			onEvent(ev)
		}
	}
	for _, ev := range ts.Flush() {
		if ev.Type == EventSession && ev.SessionID != "" {
			result.SessionID = ev.SessionID
		}
		onEvent(ev)
	}
	onEvent(StreamEvent{Type: EventDone, RawType: "host.dry_run"})
	return result
}

func (r *Runner) Cancel() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

func (r *Runner) LastPID() int {
	return 0
}
