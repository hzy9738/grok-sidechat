package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTurnCwd_EmptyMeansNoBind(t *testing.T) {
	if got := ResolveTurnCwd("", "/some/base"); got != "" {
		t.Fatalf("empty explicit must stay empty, got %q", got)
	}
	if got := ResolveTurnCwd("   ", ""); got != "" {
		t.Fatalf("whitespace must stay empty, got %q", got)
	}
}

func TestResolveTurnCwd_ExplicitAbsolute(t *testing.T) {
	dir := t.TempDir()
	got := ResolveTurnCwd(dir, "")
	if got != filepath.Clean(dir) {
		t.Fatalf("got %q want %q", got, dir)
	}
}

func TestResolveTurnCwd_RelativeUsesBase(t *testing.T) {
	base := t.TempDir()
	got := ResolveTurnCwd("proj", base)
	want := filepath.Join(base, "proj")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveTurnCwd_RejectsURL(t *testing.T) {
	for _, u := range []string{
		"https://example.com/app",
		"http://localhost:3000/",
		"file:///Users/me/proj",
		"chrome://extensions",
	} {
		if got := ResolveTurnCwd(u, t.TempDir()); got != "" {
			t.Fatalf("url %q must not become cwd, got %q", u, got)
		}
	}
}

func TestLocalPathHintFromURL_OnlyLocalhost(t *testing.T) {
	if h := LocalPathHintFromURL("https://example.com/src/App.tsx"); h != "" {
		t.Fatalf("non-local must not hint: %q", h)
	}
	if h := LocalPathHintFromURL("http://localhost:5173/src/App.tsx"); h != "/src/App.tsx" {
		t.Fatalf("localhost path hint: got %q", h)
	}
	if h := LocalPathHintFromURL("http://127.0.0.1:8080/"); h != "" {
		t.Fatalf("root path should not hint: %q", h)
	}
}

func TestRunner_EmptyCwdOmitsFlagAndDoesNotUsePageURL(t *testing.T) {
	r := &Runner{DryRun: true, ForceFixture: true}
	res := r.RunTurn(context.Background(), TurnRequest{
		UserText: "browse freely",
		Browser: BrowserContext{
			Title: "Docs",
			URL:   "https://example.com/docs",
		},
		Cwd:  "", // no bind
		Mode: "default",
	}, func(StreamEvent) {})
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	joined := strings.Join(res.Argv, " ")
	if strings.Contains(joined, "--cwd") {
		t.Fatalf("empty cwd must not pass --cwd: %v", res.Argv)
	}
	if strings.Contains(joined, "example.com") {
		t.Fatalf("page URL must not appear as --cwd: %v", res.Argv)
	}
	// Prompt still has page context for the model.
	if !strings.Contains(res.Prompt, "https://example.com/docs") {
		t.Fatalf("prompt should still include page URL as context:\n%s", res.Prompt)
	}
}

func TestRunner_ExplicitCwdStillPasses(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{DryRun: true, ForceFixture: true}
	res := r.RunTurn(context.Background(), TurnRequest{
		UserText: "work here",
		Cwd:      dir,
	}, func(StreamEvent) {})
	if flagValue(res.Argv, "--cwd") != dir {
		t.Fatalf("explicit cwd missing: %v", res.Argv)
	}
}

func TestBuildGrokArgs_EmptyCwdOmitsPair(t *testing.T) {
	args := BuildGrokArgs(TurnOptions{PromptFile: "p.txt", Cwd: ""})
	if containsToken(args, "--cwd") {
		t.Fatalf("empty cwd should omit --cwd: %v", args)
	}
}

func TestAssemblePrompt_LocalPathHintNotAsWorkspace(t *testing.T) {
	got := AssemblePrompt(BrowserContext{
		URL:           "http://localhost:3000/app",
		LocalPathHint: "/app",
	}, "debug me", 100)
	if !strings.Contains(got, "Local path hint") || !strings.Contains(got, "/app") {
		t.Fatalf("hint missing:\n%s", got)
	}
	if strings.Contains(got, "--cwd") {
		t.Fatalf("prompt must not invent --cwd:\n%s", got)
	}
}

func TestAssemblePrompt_Mentions(t *testing.T) {
	got := AssemblePrompt(BrowserContext{
		Mentions: []ContextMention{
			{Kind: "tab", Title: "Example", URL: "https://example.com/"},
			{Kind: "selection", Text: "highlighted bit"},
		},
	}, "summarize", 100)
	for _, want := range []string{"Attached mentions:", "@tab", "Example", "https://example.com/", "@selection", "highlighted bit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
}
