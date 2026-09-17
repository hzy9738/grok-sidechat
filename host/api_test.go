package main

import (
	"strings"
	"testing"
)

func TestChatCompletionsURL(t *testing.T) {
	got, err := ChatCompletionsURL("https://api.openai.com/v1")
	if err != nil || got != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("base /v1: %q %v", got, err)
	}
	got, err = ChatCompletionsURL("https://api.x.ai/v1/chat/completions")
	if err != nil || got != "https://api.x.ai/v1/chat/completions" {
		t.Fatalf("already complete: %q %v", got, err)
	}
	if _, err := ChatCompletionsURL(""); err == nil {
		t.Fatal("empty base must fail")
	}
}

func TestParseOpenAIStreamLine(t *testing.T) {
	th, tx, done, err := parseOpenAIStreamLine(`data: {"choices":[{"delta":{"content":"hello"}}]}`)
	if err != nil || done || th != "" || tx != "hello" {
		t.Fatalf("content delta: th=%q tx=%q done=%v err=%v", th, tx, done, err)
	}
	th, tx, done, err = parseOpenAIStreamLine(`data: {"choices":[{"delta":{"reasoning_content":"think"}}]}`)
	if err != nil || done || th != "think" || tx != "" {
		t.Fatalf("reasoning: th=%q tx=%q done=%v err=%v", th, tx, done, err)
	}
	_, _, done, err = parseOpenAIStreamLine("data: [DONE]")
	if err != nil || !done {
		t.Fatalf("done: done=%v err=%v", done, err)
	}
	_, _, _, err = parseOpenAIStreamLine(`data: {"error":{"message":"nope"}}`)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error chunk: %v", err)
	}
}

func TestBuildChatMessages_SystemThenHistory(t *testing.T) {
	msgs := BuildChatMessages(BrowserContext{Title: "Demo", URL: "https://demo.test/"}, "now", []SessionMessage{
		{Role: "user", Text: "before"},
		{Role: "thinking", Text: "hidden"},
		{Role: "assistant", Text: "old"},
	}, 100)
	if len(msgs) != 4 {
		t.Fatalf("len=%d %#v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "Title: Demo") {
		t.Fatalf("system: %#v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "before" {
		t.Fatalf("hist user: %#v", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[3].Content != "now" {
		t.Fatalf("tail: %#v", msgs[2:])
	}
}

func TestResolveChatAPI_RequestWins(t *testing.T) {
	t.Setenv("SIDECHAT_API_BASE", "https://env.example/v1")
	t.Setenv("SIDECHAT_API_KEY", "env-key")
	t.Setenv("SIDECHAT_MODEL", "env-model")
	cfg := ResolveChatAPI(TurnRequest{APIBase: "https://req.example/v1", APIKey: "req-key", Model: "req-model"})
	if cfg.BaseURL != "https://req.example/v1" || cfg.APIKey != "req-key" || cfg.Model != "req-model" {
		t.Fatalf("%+v", cfg)
	}
}

func TestResolveChatAPI_DefaultModel(t *testing.T) {
	t.Setenv("SIDECHAT_API_BASE", "")
	t.Setenv("SIDECHAT_API_KEY", "")
	t.Setenv("SIDECHAT_MODEL", "")
	t.Setenv("SIDECHAT_HOME", t.TempDir())
	cfg := ResolveChatAPI(TurnRequest{})
	if cfg.Model != defaultChatModel {
		t.Fatalf("model=%q", cfg.Model)
	}
}
