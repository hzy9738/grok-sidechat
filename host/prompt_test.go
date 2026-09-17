package main

import (
	"strings"
	"testing"
)

func TestAssemblePrompt_IncludesTitleURLSelectionTabs(t *testing.T) {
	ctx := BrowserContext{
		Title:       "Grok Build Docs",
		URL:         "https://x.ai/cli",
		Selection:   "headless streaming-messages-json",
		PageText:    "Install the CLI and run grok login.",
		IncludeTabs: true,
		Tabs: []TabInfo{
			{Title: "Grok Build Docs", URL: "https://x.ai/cli"},
			{Title: "Example", URL: "https://example.com/"},
		},
	}
	got := AssemblePrompt(ctx, "总结这个页面的安装步骤", 1000)

	// Real shipped assembler must embed browser identity + selection + tabs + user text.
	for _, want := range []string{
		"[Browser context]",
		"Title: Grok Build Docs",
		"URL: https://x.ai/cli",
		"Selection:",
		"headless streaming-messages-json",
		"Page extract:",
		"Install the CLI and run grok login.",
		"Open tabs:",
		"Grok Build Docs | https://x.ai/cli",
		"Example | https://example.com/",
		"[User]",
		"总结这个页面的安装步骤",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("assembled prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}
}

func TestAssemblePrompt_OmitsTabsUnlessRequested(t *testing.T) {
	ctx := BrowserContext{
		Title:       "Only Title",
		URL:         "https://example.com",
		IncludeTabs: false,
		Tabs:        []TabInfo{{Title: "Hidden", URL: "https://hidden.example"}},
	}
	got := AssemblePrompt(ctx, "hi", 100)
	if strings.Contains(got, "Open tabs:") {
		t.Fatalf("tabs should be omitted when IncludeTabs=false:\n%s", got)
	}
	if strings.Contains(got, "Hidden") {
		t.Fatalf("tab metadata leaked:\n%s", got)
	}
	if !strings.Contains(got, "Title: Only Title") || !strings.Contains(got, "URL: https://example.com") {
		t.Fatalf("title/url missing:\n%s", got)
	}
}

func TestAssemblePrompt_TruncatesPageText(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := AssemblePrompt(BrowserContext{PageText: long}, "q", 50)
	if !strings.Contains(got, "…[truncated]") {
		t.Fatalf("expected truncation marker:\n%s", got)
	}
	// user text still present after browser block
	if !strings.Contains(got, "[User]\nq") {
		t.Fatalf("user text missing:\n%s", got)
	}
}

func TestAssemblePrompt_UserOnly(t *testing.T) {
	got := AssemblePrompt(BrowserContext{}, "just chat", 100)
	if strings.Contains(got, "[Browser context]") {
		t.Fatalf("empty browser should not emit context block:\n%s", got)
	}
	if got != "[User]\njust chat\n" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestAssemblePrompt_IgnoresLegacyBrowserControlFlag(t *testing.T) {
	got := AssemblePrompt(BrowserContext{EnableBrowserControl: true}, "click the sign-in button", 100)
	for _, forbidden := range []string{"[Browser control]", "MCP", "grok_sidechat_chrome", "tool"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("tool-free prompt contains %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "[User]\nclick the sign-in button") {
		t.Fatalf("user text missing:\n%s", got)
	}
}
