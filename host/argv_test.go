package main

import (
	"strings"
	"testing"
)

func TestBuildGrokArgs_HeadlessNonACP(t *testing.T) {
	args := BuildGrokArgs(TurnOptions{
		Cwd:        "/tmp/work",
		PromptFile: "/tmp/prompt.txt",
		ResumeID:   "sess-1",
		Model:      "grok-4.6",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--cwd", "/tmp/work",
		"--output-format", "streaming-messages-json",
		"--include-partial-messages",
		"--no-ask-user",
		"--no-auto-update",
		"--permission-mode", "default",
		"--resume", "sess-1",
		"--model", "grok-4.6",
		"--prompt-file", "/tmp/prompt.txt",
	} {
		if !containsArgSequence(args, want) && !strings.Contains(joined, want) {
			// sequence check for multi-token flags
		}
	}
	// Stronger: verify pairs
	mustPair(t, args, "--cwd", "/tmp/work")
	mustPair(t, args, "--output-format", "streaming-messages-json")
	mustPair(t, args, "--resume", "sess-1")
	mustPair(t, args, "--prompt-file", "/tmp/prompt.txt")
	mustPair(t, args, "--permission-mode", "default")
	mustPair(t, args, "--tools", "")
	for _, required := range []string{"--disable-web-search", "--no-subagents"} {
		if !containsToken(args, required) {
			t.Fatalf("tool-free argv missing %s: %v", required, args)
		}
	}
	if containsToken(args, "--always-approve") {
		t.Fatal("default mode must not pass --always-approve")
	}
	// Must not look like ACP server mode or HTTP API base URL wiring
	for _, bad := range []string{"acp", "api.x.ai", "chat/completions", "--acp"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("argv must not contain %q: %v", bad, args)
		}
	}
}

func TestBuildGrokArgs_ToolsCannotBeEnabledByExtraArgs(t *testing.T) {
	args := BuildGrokArgs(TurnOptions{
		PromptFile: "p.txt",
		ExtraArgs:  []string{"--tools", "shell"},
	})
	if got := lastFlagValue(args, "--tools"); got != "" {
		t.Fatalf("final tools allowlist must be empty, got %q in %v", got, args)
	}
}

func TestBuildGrokArgs_EmptyCwdNoFlag(t *testing.T) {
	args := BuildGrokArgs(TurnOptions{PromptFile: "p.txt", Cwd: ""})
	if containsToken(args, "--cwd") {
		t.Fatalf("empty cwd must omit --cwd: %v", args)
	}
	mustPair(t, args, "--output-format", "streaming-messages-json")
}

func mustPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("missing pair %s %s in %v", flag, value, args)
}

func containsToken(args []string, tok string) bool {
	for _, a := range args {
		if a == tok {
			return true
		}
	}
	return false
}

func containsArgSequence(args []string, tok string) bool {
	return containsToken(args, tok)
}

func lastFlagValue(args []string, flag string) string {
	value := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			value = args[i+1]
		}
	}
	return value
}
