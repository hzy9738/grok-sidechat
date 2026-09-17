package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type toolFreeGrokRuntime struct {
	Home    string
	cleanup func()
}

func (r toolFreeGrokRuntime) Close() {
	if r.cleanup != nil {
		r.cleanup()
	}
}

func createToolFreeGrokRuntime(sourceHome string) (toolFreeGrokRuntime, error) {
	tempHome, err := os.MkdirTemp("", "grok-sidechat-home-*")
	if err != nil {
		return toolFreeGrokRuntime{}, fmt.Errorf("create isolated Grok home: %w", err)
	}
	runtime := toolFreeGrokRuntime{Home: tempHome, cleanup: func() { _ = os.RemoveAll(tempHome) }}
	fail := func(err error) (toolFreeGrokRuntime, error) {
		runtime.Close()
		return toolFreeGrokRuntime{}, err
	}
	if err := os.Chmod(tempHome, 0o700); err != nil {
		return fail(fmt.Errorf("secure isolated Grok home: %w", err))
	}

	if sourceHome = strings.TrimSpace(sourceHome); sourceHome != "" {
		authSource := filepath.Join(sourceHome, "auth.json")
		if _, err := os.Stat(authSource); err == nil {
			if err := copyPrivateFile(authSource, filepath.Join(tempHome, "auth.json")); err != nil {
				return fail(fmt.Errorf("copy Grok authentication: %w", err))
			}
		} else if !os.IsNotExist(err) {
			return fail(fmt.Errorf("inspect Grok authentication: %w", err))
		}

		sourceSessions := filepath.Join(sourceHome, "sessions")
		if err := os.MkdirAll(sourceSessions, 0o700); err != nil {
			return fail(fmt.Errorf("prepare Grok sessions: %w", err))
		}
		if err := os.Symlink(sourceSessions, filepath.Join(tempHome, "sessions")); err != nil {
			return fail(fmt.Errorf("link Grok sessions into isolated runtime: %w", err))
		}
	}

	config := toolFreeGrokConfig()
	if err := os.WriteFile(filepath.Join(tempHome, "config.toml"), []byte(config), 0o600); err != nil {
		return fail(fmt.Errorf("write isolated Grok config: %w", err))
	}
	return runtime, nil
}

func toolFreeGrokConfig() string {
	return `[compat.cursor]
skills = false
rules = false
agents = false
mcps = false
hooks = false
sessions = false

[compat.claude]
skills = false
rules = false
agents = false
mcps = false
hooks = false
sessions = false

[compat.codex]
sessions = false
`
}

func copyPrivateFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func setEnvironmentValue(environment []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environment)+1)
	replaced := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}
