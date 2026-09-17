package main

import (
	"fmt"
	"strings"
)

// TurnOptions configures one headless Grok turn.
type TurnOptions struct {
	Cmd             string
	Cwd             string
	PromptFile      string
	ResumeID        string
	Model           string
	ReasoningEffort string
	MaxTurns        int
	ExtraArgs       []string
}

// BuildGrokArgs builds the native headless CLI argv (without the binary name).
// Deliberately non-ACP: streaming-messages-json + --prompt-file + optional --resume.
// Does not include any xAI HTTP chat endpoint configuration.
func BuildGrokArgs(opts TurnOptions) []string {
	args := append([]string(nil), opts.ExtraArgs...)
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(args,
		"--output-format", "streaming-messages-json",
		"--include-partial-messages",
		"--tools", "",
		"--disable-web-search",
		"--no-subagents",
		"--no-ask-user",
		"--no-auto-update",
		"--permission-mode", "default",
	)
	if id := strings.TrimSpace(opts.ResumeID); id != "" {
		args = append(args, "--resume", id)
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := strings.TrimSpace(opts.ReasoningEffort); effort != "" {
		args = append(args, "--reasoning-effort", effort)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	args = append(args, "--prompt-file", opts.PromptFile)
	return args
}

// CommandLine returns binary + args for logging/dry-run.
func CommandLine(opts TurnOptions) (string, []string) {
	cmd := strings.TrimSpace(opts.Cmd)
	if cmd == "" {
		cmd = "grok"
	}
	return cmd, BuildGrokArgs(opts)
}
