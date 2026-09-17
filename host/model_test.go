package main

import (
	"context"
	"strings"
	"testing"
)

func TestHandleSendDefaultsModel(t *testing.T) {
	runner := &Runner{DryRun: true, ForceFixture: true}
	var completed HostMessage
	err := handleSend(context.Background(), runner, "", ClientMessage{
		Op:        "send",
		RequestID: "model-default",
		Text:      "hello",
		DryRun:    true,
	}, func(message HostMessage) error {
		if message.Op == "send_done" {
			completed = message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := flagValue(completed.Argv, "--model"); got != defaultChatModel {
		t.Fatalf("default model = %q, want %q; argv=%v", got, defaultChatModel, completed.Argv)
	}
	if completed.Argv[0] != "openai-compat" {
		t.Fatalf("backend = %v", completed.Argv)
	}
}

func TestHandleSendPreservesExplicitModel(t *testing.T) {
	runner := &Runner{DryRun: true, ForceFixture: true}
	var completed HostMessage
	err := handleSend(context.Background(), runner, "", ClientMessage{
		Op:        "send",
		RequestID: "model-explicit",
		Text:      "hello",
		Model:     "custom-model",
		APIBase:   "https://api.example.test/v1",
		DryRun:    true,
	}, func(message HostMessage) error {
		if message.Op == "send_done" {
			completed = message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := flagValue(completed.Argv, "--model"); got != "custom-model" {
		t.Fatalf("explicit model = %q, want custom-model; argv=%v", got, completed.Argv)
	}
	if !strings.Contains(strings.Join(completed.Argv, " "), "https://api.example.test/v1/chat/completions") {
		t.Fatalf("api base missing: %v", completed.Argv)
	}
}

func TestHandleSendForcesReasoningAndDisablesLegacyToolControls(t *testing.T) {
	runner := &Runner{DryRun: true, ForceFixture: true}
	var completed HostMessage
	err := handleSend(context.Background(), runner, "", ClientMessage{
		Op:              "send",
		RequestID:       "tool-free-defaults",
		Text:            "think without tools",
		Mode:            "yolo",
		MaxTurns:        99,
		AlwaysApprove:   true,
		ReasoningEffort: "",
		Browser: BrowserContext{
			EnableBrowserControl: true,
		},
		DryRun: true,
	}, func(message HostMessage) error {
		if message.Op == "send_done" {
			completed = message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(completed.Argv, " ")
	if strings.Contains(joined, "--always-approve") || strings.Contains(joined, "--tools") {
		t.Fatalf("legacy grok flags leaked: %v", completed.Argv)
	}
	if completed.Argv[0] != "openai-compat" {
		t.Fatalf("want openai-compat argv: %v", completed.Argv)
	}
	if completed.Prompt != "[User]\nthink without tools\n" {
		t.Fatalf("legacy browser control changed prompt: %q", completed.Prompt)
	}
}
