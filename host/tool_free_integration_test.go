package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrokToolFreeRuntimeHasNoMCPServers(t *testing.T) {
	if os.Getenv("GROK_SIDECHAT_INTEGRATION") != "1" {
		t.Skip("set GROK_SIDECHAT_INTEGRATION=1 to test the installed Grok CLI")
	}
	runtime, err := createToolFreeGrokRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	grok, err := exec.LookPath("grok")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(grok, "mcp", "list", "--json")
	cmd.Env = setEnvironmentValue(os.Environ(), "GROK_HOME", runtime.Home)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("grok mcp list failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "grok_sidechat_chrome") {
		t.Fatalf("tool-free runtime registered a side-chat MCP server: %s", output)
	}
}

func TestGrokRealTurnThinksWithoutTools(t *testing.T) {
	if os.Getenv("SIDECHAT_REAL_TURN") != "1" && os.Getenv("GROK_SIDECHAT_REAL_TURN") != "1" {
		t.Skip("set SIDECHAT_REAL_TURN=1 and SIDECHAT_API_BASE/KEY to run a real chat turn")
	}
	realHome := GrokHome()
	isolatedSourceHome := t.TempDir()
	if err := copyPrivateFile(filepath.Join(realHome, "auth.json"), filepath.Join(isolatedSourceHome, "auth.json")); err != nil {
		t.Fatalf("copy local Grok auth for isolated integration turn: %v", err)
	}
	t.Setenv("GROK_HOME", isolatedSourceHome)

	runner := &Runner{StoreDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var thinking, assistant strings.Builder
	toolEvents := 0
	result := runner.RunTurn(ctx, TurnRequest{
		RequestID:       "tool-free-real-turn",
		UserText:        "Think through 17 * 19, then answer with only the number. Do not use tools.",
		Model:           firstNonEmpty(os.Getenv("SIDECHAT_MODEL"), defaultChatModel),
		ReasoningEffort: "high",
		Browser: BrowserContext{
			Title:                "Tool-free integration page",
			URL:                  "https://integration.example/",
			EnableBrowserControl: true, // legacy opt-in must be ignored
		},
	}, func(event StreamEvent) {
		switch event.Type {
		case EventThinking:
			thinking.WriteString(event.Text)
		case EventText:
			assistant.WriteString(event.Text)
		case EventToolUse, EventToolResult:
			toolEvents++
		}
	})
	if result.Err != nil {
		t.Fatalf("real Grok turn failed: %v", result.Err)
	}
	if toolEvents != 0 {
		t.Fatalf("tool-free turn emitted %d tool events", toolEvents)
	}
	if strings.TrimSpace(thinking.String()) == "" {
		t.Fatal("real turn emitted no thinking")
	}
	if !strings.Contains(assistant.String(), "323") {
		t.Fatalf("unexpected answer: %q", assistant.String())
	}
}
