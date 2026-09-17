package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunner_DryRunBuildsArgvAndParsesStream(t *testing.T) {
	r := &Runner{
		DryRun:       true,
		ForceFixture: true,
		FixtureStream: []string{
			`{"type":"assistant","content":[{"type":"text","text":"fixture hello"}]}`,
			`{"type":"result","session_id":"fixture-sess","subtype":"success"}`,
		},
	}

	var events []StreamEvent
	res := r.RunTurn(context.Background(), TurnRequest{
		UserText: "explain this page",
		Browser: BrowserContext{
			Title:       "Demo",
			URL:         "https://demo.test/",
			Selection:   "important bit",
			IncludeTabs: true,
			Tabs:        []TabInfo{{Title: "Demo", URL: "https://demo.test/"}},
		},
		Cwd:     t.TempDir(),
		Mode:    "default",
		APIBase: "https://api.openai.com/v1",
		Model:   defaultChatModel,
	}, func(ev StreamEvent) {
		events = append(events, ev)
	})

	if res.Err != nil {
		t.Fatalf("dry-run error: %v", res.Err)
	}
	if res.SessionID != "fixture-sess" {
		t.Fatalf("session id: got %q", res.SessionID)
	}
	joinedArgv := strings.Join(res.Argv, " ")
	if res.Argv[0] != "openai-compat" {
		t.Fatalf("argv backend: %v", res.Argv)
	}
	if !strings.Contains(joinedArgv, "/chat/completions") {
		t.Fatalf("argv missing completions url: %v", res.Argv)
	}
	if flagValue(res.Argv, "--model") != defaultChatModel {
		t.Fatalf("model: %v", res.Argv)
	}
	if !strings.Contains(res.Prompt, "Title: Demo") || !strings.Contains(res.Prompt, "important bit") {
		t.Fatalf("prompt incomplete: %s", res.Prompt)
	}
	if !hasEvent(events, EventText, "fixture hello") {
		t.Fatalf("text event missing: %#v", events)
	}
}

func TestRunner_DryRunTwiceConsistent(t *testing.T) {
	r := &Runner{DryRun: true, ForceFixture: true}
	req := TurnRequest{
		UserText: "ping",
		Browser:  BrowserContext{Title: "T", URL: "https://t.test"},
		Cwd:      t.TempDir(),
		APIBase:  "https://api.openai.com/v1",
		Model:    defaultChatModel,
	}
	res1 := r.RunTurn(context.Background(), req, func(StreamEvent) {})
	res2 := r.RunTurn(context.Background(), req, func(StreamEvent) {})
	if res1.Err != nil || res2.Err != nil {
		t.Fatalf("errors: %v %v", res1.Err, res2.Err)
	}
	if flagValue(res1.Argv, "--model") != flagValue(res2.Argv, "--model") {
		t.Fatalf("model inconsistent: %v %v", res1.Argv, res2.Argv)
	}
	if !strings.Contains(res1.Prompt, "Title: T") || !strings.Contains(res2.Prompt, "Title: T") {
		t.Fatal("prompt not consistent")
	}
}

func TestRunner_ResumeInArgv(t *testing.T) {
	r := &Runner{DryRun: true, ForceFixture: true}
	res := r.RunTurn(context.Background(), TurnRequest{
		SessionID: "keep-me",
		UserText:  "continue",
		Cwd:       t.TempDir(),
	}, func(StreamEvent) {})
	if flagValue(res.Argv, "--session") != "keep-me" {
		t.Fatalf("session missing: %v", res.Argv)
	}
}

func TestRunner_HTTPStreamPersistsSession(t *testing.T) {
	root := t.TempDir()
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm \"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	r := &Runner{StoreDir: root}
	var events []StreamEvent
	res := r.RunTurn(context.Background(), TurnRequest{
		UserText: "ping",
		APIBase:  srv.URL + "/v1",
		APIKey:   "secret",
		Model:    "demo-model",
		Browser:  BrowserContext{Title: "T", URL: "https://t.test"},
	}, func(ev StreamEvent) { events = append(events, ev) })
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.SessionID == "" {
		t.Fatal("missing session id")
	}
	if !hasEvent(events, EventThinking, "hmm ") || !hasEvent(events, EventPartial, "pong") {
		t.Fatalf("events=%#v", events)
	}
	if !strings.Contains(string(gotBody), `"demo-model"`) || strings.Contains(string(gotBody), "secret") {
		t.Fatalf("body=%s", gotBody)
	}
	msgs, info, err := LoadStoredMessages(root, res.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "demo-model" || len(msgs) < 2 {
		t.Fatalf("stored info=%+v msgs=%#v", info, msgs)
	}
}

func flagValue(argv []string, flag string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}
