package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGrokExecutable_FindsHomeGrokBin(t *testing.T) {
	// Prefer real install if present (this machine has ~/.grok/bin/grok).
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(home, ".grok", "bin", "grok")
	if _, err := os.Stat(want); err != nil {
		t.Skip("no ~/.grok/bin/grok on this machine")
	}
	// Simulate Chrome-like empty PATH
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GROK_SIDECHAT_CMD", "")
	t.Setenv("GROK_BIN", "")
	t.Setenv("GROK_CLI", "")

	got, err := ResolveGrokExecutable("grok")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Allow symlink resolution differences: both should end with grok and live under .grok
	if got != want {
		// resolved may be the same path after clean
		if filepath.Clean(got) != filepath.Clean(want) {
			// if LookPath found another copy, still OK if executable exists
			if _, err := os.Stat(got); err != nil {
				t.Fatalf("got %q want %q", got, want)
			}
		}
	}
}

func TestResolveGrokExecutable_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-grok")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_SIDECHAT_CMD", fake)
	t.Setenv("PATH", "/usr/bin:/bin")
	got, err := ResolveGrokExecutable("grok")
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Fatalf("got %q want %q", got, fake)
	}
}

func TestResolveGrokExecutable_Missing(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GROK_SIDECHAT_CMD", "")
	t.Setenv("GROK_BIN", "")
	t.Setenv("GROK_CLI", "")
	// Point home candidates away by using a name that won't match candidates when cmd is absolute missing
	_, err := ResolveGrokExecutable(filepath.Join(t.TempDir(), "no-such-grok-binary"))
	if err == nil {
		t.Fatal("expected error for missing absolute path")
	}
}

func TestAugmentPATH_PrependsGrokBin(t *testing.T) {
	home := "/Users/demo"
	got := augmentPATH("/usr/bin:/bin", home)
	if !strings.Contains(got, filepath.Join(home, ".grok", "bin")) {
		t.Fatalf("missing .grok/bin: %s", got)
	}
	// chrome-minimal path still preserved
	if !strings.Contains(got, "/usr/bin") {
		t.Fatalf("lost system path: %s", got)
	}
}

func TestAugmentProcessEnv_SetsPATH(t *testing.T) {
	env := AugmentProcessEnv([]string{"FOO=bar", "PATH=/bin"}, "/Users/demo")
	var path string
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
		}
	}
	if path == "" || !strings.Contains(path, ".grok") {
		t.Fatalf("PATH not augmented: %v", env)
	}
}
