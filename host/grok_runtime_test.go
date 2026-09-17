package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateToolFreeGrokRuntimeIsolatedConfigAndSharedSessions(t *testing.T) {
	sourceHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceHome, "auth.json"), []byte(`{"token":"test-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := createToolFreeGrokRuntime(sourceHome)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	authInfo, err := os.Stat(filepath.Join(runtime.Home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if authInfo.Mode().Perm() != 0o600 {
		t.Fatalf("auth mode: %o", authInfo.Mode().Perm())
	}
	sessionsTarget, err := os.Readlink(filepath.Join(runtime.Home, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if sessionsTarget != filepath.Join(sourceHome, "sessions") {
		t.Fatalf("sessions target: %q", sessionsTarget)
	}
	configBytes, err := os.ReadFile(filepath.Join(runtime.Home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configBytes)
	for _, required := range []string{"skills = false", "agents = false", "mcps = false"} {
		if !strings.Contains(config, required) {
			t.Fatalf("config missing %q:\n%s", required, config)
		}
	}
	for _, forbidden := range []string{"[mcp_servers.", "MCPTool(", "bridge-address", "bridge-token"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("tool-free config contains %q:\n%s", forbidden, config)
		}
	}
}

func TestSetEnvironmentValueReplacesDuplicates(t *testing.T) {
	got := setEnvironmentValue([]string{"A=1", "GROK_HOME=old", "GROK_HOME=older"}, "GROK_HOME", "/tmp/new")
	if strings.Join(got, "|") != "A=1|GROK_HOME=/tmp/new" {
		t.Fatalf("environment: %#v", got)
	}
}
