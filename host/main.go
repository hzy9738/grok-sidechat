package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const (
	hostVersion = "0.4.0"
)

func main() {
	mode := flag.String("mode", "native", "native | stdio | once")
	dryRun := flag.Bool("dry-run", false, "do not call the chat API; still build diagnostic argv and parse fixture stream")
	fixture := flag.String("fixture", "", "path to NDJSON fixture stream (optional)")
	onceJSON := flag.String("once", "", "with -mode once: path to a ClientMessage JSON file to process once")
	cwd := flag.String("cwd", "", "default working directory stored on the session")
	_ = flag.String("cmd", "", "deprecated; ignored after switching to OpenAI-compatible HTTP")
	flag.Parse()

	runner := &Runner{
		DryRun: *dryRun,
	}
	if *fixture != "" {
		lines, err := readFixtureLines(*fixture)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
			os.Exit(2)
		}
		runner.FixtureStream = lines
		if *dryRun {
			runner.ForceFixture = true
		}
	}

	// Empty default: do not force process Getwd as a project bind for turns.
	// Explicit -cwd flag still applies when the extension omits per-request cwd.
	defaultCwd := strings.TrimSpace(*cwd)
	switch *mode {
	case "once":
		if err := runOnce(runner, defaultCwd, *onceJSON); err != nil {
			fmt.Fprintf(os.Stderr, "once: %v\n", err)
			os.Exit(1)
		}
	case "stdio":
		if err := serveLoop(os.Stdin, os.Stdout, runner, defaultCwd, true); err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "stdio: %v\n", err)
			os.Exit(1)
		}
	default: // native
		if err := serveLoop(os.Stdin, os.Stdout, runner, defaultCwd, false); err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "native: %v\n", err)
			os.Exit(1)
		}
	}
}

func readFixtureLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func runOnce(runner *Runner, defaultCwd, oncePath string) error {
	var msg ClientMessage
	if oncePath == "" {
		// Default dry demonstration message for launch checks.
		msg = ClientMessage{
			Op:        "send",
			RequestID: "once-1",
			Text:      "hello from once mode",
			Browser: BrowserContext{
				Title:       "Example Page",
				URL:         "https://example.com/",
				Selection:   "selected snippet",
				IncludeTabs: true,
				Tabs: []TabInfo{
					{Title: "Example Page", URL: "https://example.com/"},
					{Title: "Docs", URL: "https://docs.example.com/"},
				},
			},
			// Empty unless -cwd provided: once dry-run must not invent a project bind.
			Cwd:    defaultCwd,
			DryRun: runner.DryRun,
			Mode:   "default",
		}
	} else {
		data, err := os.ReadFile(oncePath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
	}
	if msg.DryRun {
		runner.DryRun = true
		runner.ForceFixture = true
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return handleMessage(context.Background(), runner, defaultCwd, msg, func(out HostMessage) error {
		return enc.Encode(out)
	})
}

func serveLoop(in io.Reader, out io.Writer, runner *Runner, defaultCwd string, lineJSON bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var writeMu sync.Mutex
	write := func(msg HostMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if lineJSON {
			return WriteJSONLine(out, msg)
		}
		return WriteNativeMessage(out, msg)
	}

	// Hello so extension can confirm host is alive.
	_ = write(HostMessage{Op: "hello", Version: hostVersion, OK: true})

	// Track in-flight sends so process exit can wait for them on shutdown.
	var sends sync.WaitGroup
	defer sends.Wait()

	for {
		select {
		case <-ctx.Done():
			_ = runner.Cancel()
			return nil
		default:
		}

		var msg ClientMessage
		var err error
		if lineJSON {
			err = ReadJSONLine(in, &msg)
		} else {
			err = ReadNativeMessage(in, &msg)
		}
		if err != nil {
			_ = runner.Cancel()
			return err
		}

		op := strings.ToLower(strings.TrimSpace(msg.Op))
		// send runs asynchronously so the read loop stays free for cancel/ping
		// while a long headless turn is in progress (criterion: cancel mid-turn).
		if op == "send" {
			sends.Add(1)
			go func(m ClientMessage) {
				defer sends.Done()
				if err := handleSend(ctx, runner, defaultCwd, m, write); err != nil {
					_ = write(HostMessage{
						Op:        "error",
						RequestID: m.RequestID,
						Error:     err.Error(),
						OK:        false,
					})
				}
			}(msg)
			continue
		}

		if err := handleMessage(ctx, runner, defaultCwd, msg, write); err != nil {
			_ = write(HostMessage{
				Op:        "error",
				RequestID: msg.RequestID,
				Error:     err.Error(),
				OK:        false,
			})
		}
	}
}

func handleMessage(ctx context.Context, runner *Runner, defaultCwd string, msg ClientMessage, write func(HostMessage) error) error {
	switch strings.ToLower(strings.TrimSpace(msg.Op)) {
	case "ping", "hello":
		return write(HostMessage{Op: "pong", RequestID: msg.RequestID, Version: hostVersion, OK: true})
	case "cancel":
		// Must run on the read loop (not blocked behind send) so the extension
		// can abort a live turn without waiting for the CLI to finish.
		err := runner.Cancel()
		out := HostMessage{Op: "cancelled", RequestID: msg.RequestID, OK: true}
		if err != nil {
			// Process may already be dead; still report cancelled.
			out.Error = err.Error()
		}
		return write(out)
	case "list_sessions":
		list, err := ListStoredSessions(runner.sessionRoot(), msg.Query, msg.Limit)
		if err != nil {
			return write(HostMessage{Op: "list_sessions", RequestID: msg.RequestID, OK: false, Error: err.Error()})
		}
		return write(HostMessage{Op: "list_sessions", RequestID: msg.RequestID, OK: true, Sessions: list})
	case "get_session":
		msgs, meta, err := LoadStoredMessages(runner.sessionRoot(), msg.SessionID, msg.Limit)
		if err != nil {
			return write(HostMessage{Op: "get_session", RequestID: msg.RequestID, OK: false, Error: err.Error()})
		}
		s := meta
		return write(HostMessage{
			Op:        "get_session",
			RequestID: msg.RequestID,
			OK:        true,
			SessionID: meta.ID,
			Session:   &s,
			Messages:  msgs,
		})
	case "send":
		// Caller (serveLoop) dispatches send on a goroutine; once-mode uses this path.
		return handleSend(ctx, runner, defaultCwd, msg, write)
	default:
		return write(HostMessage{
			Op:        "error",
			RequestID: msg.RequestID,
			Error:     fmt.Sprintf("unknown op %q", msg.Op),
			OK:        false,
		})
	}
}

func handleSend(ctx context.Context, runner *Runner, defaultCwd string, msg ClientMessage, write func(HostMessage) error) error {
	if strings.TrimSpace(msg.Text) == "" {
		return write(HostMessage{Op: "error", RequestID: msg.RequestID, Error: "text is required", OK: false})
	}
	// Per-request dry-run (extension tests / first paint without spawning).
	prevDry := runner.DryRun
	prevForce := runner.ForceFixture
	if msg.DryRun {
		runner.DryRun = true
		runner.ForceFixture = true
	}
	defer func() {
		runner.DryRun = prevDry
		runner.ForceFixture = prevForce
	}()

	// Prefer per-request cwd; fall back to host -cwd flag only. Never invent from page URL / Getwd.
	explicit := strings.TrimSpace(msg.Cwd)
	if explicit == "" {
		explicit = strings.TrimSpace(defaultCwd)
	}
	base := ""
	if explicit != "" && !filepath.IsAbs(explicit) {
		// Relative paths resolve against process Getwd for path math only.
		base, _ = os.Getwd()
	}
	cwd := ResolveTurnCwd(explicit, base)

	// Optional localhost path hint as browser context text only (not --cwd).
	if hint := LocalPathHintFromURL(msg.Browser.URL); hint != "" && strings.TrimSpace(msg.Browser.LocalPathHint) == "" {
		msg.Browser.LocalPathHint = hint
	}

	req := TurnRequest{
		RequestID:       msg.RequestID,
		SessionID:       msg.SessionID,
		Cwd:             cwd,
		UserText:        msg.Text,
		Browser:         msg.Browser,
		Model:           firstNonEmpty(strings.TrimSpace(msg.Model), defaultChatModel),
		APIBase:         strings.TrimSpace(msg.APIBase),
		APIKey:          strings.TrimSpace(msg.APIKey),
		Mode:            "default",
		ReasoningEffort: firstNonEmpty(strings.TrimSpace(msg.ReasoningEffort), "high"),
		MaxTurns:        1,
		AlwaysApprove:   false,
	}
	req.Browser.EnableBrowserControl = false

	var sessionID string
	var argv []string
	var prompt string

	result := runner.RunTurn(ctx, req, func(ev StreamEvent) {
		if ev.Type == EventSession && ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		// Extract argv line for structured response
		if ev.RawType == "host.argv" && strings.HasPrefix(ev.Text, "argv: ") {
			// kept in result; still forward partial to UI
		}
		_ = write(HostMessage{
			Op:        "event",
			RequestID: msg.RequestID,
			Event:     &ev,
			SessionID: sessionID,
		})
	})
	sessionID = firstNonEmpty(result.SessionID, sessionID)
	argv = result.Argv
	prompt = result.Prompt

	out := HostMessage{
		Op:        "send_done",
		RequestID: msg.RequestID,
		SessionID: sessionID,
		Argv:      argv,
		Prompt:    prompt,
		OK:        result.Err == nil,
	}
	if result.Err != nil {
		out.Error = result.Err.Error()
	}
	if result.DryRun {
		out.Info = map[string]any{"dryRun": true}
	}
	return write(out)
}
