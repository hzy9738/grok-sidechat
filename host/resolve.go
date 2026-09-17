package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveGrokExecutable finds the grok CLI binary for Chrome Native Messaging hosts.
// Chrome launches hosts with a minimal PATH (often without ~/.grok/bin), so bare
// LookPath("grok") fails even when the user has a working shell install.
//
// Order:
//  1. absolute/relative path that exists
//  2. GROK_SIDECHAT_CMD / GROK_BIN env
//  3. common install locations under $HOME and package managers
//  4. LookPath with an augmented PATH
func ResolveGrokExecutable(cmdName string) (string, error) {
	name := strings.TrimSpace(cmdName)
	if name == "" {
		name = "grok"
	}

	// Explicit absolute/relative file path.
	if strings.Contains(name, string(os.PathSeparator)) || filepath.IsAbs(name) {
		if st, err := os.Stat(name); err == nil && !st.IsDir() {
			return name, nil
		}
		return "", fmt.Errorf("grok CLI path not found: %s", name)
	}

	// Env overrides (useful for host wrapper / launchd).
	for _, key := range []string{"GROK_SIDECHAT_CMD", "GROK_BIN", "GROK_CLI"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if st, err := os.Stat(v); err == nil && !st.IsDir() {
				return v, nil
			}
			// allow env to be a bare name → fall through with LookPath later using that name
			if !strings.Contains(v, string(os.PathSeparator)) {
				name = v
			}
		}
	}

	// Well-known locations (Grok Build default install + Homebrew).
	home, _ := os.UserHomeDir()
	candidates := []string{}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".grok", "bin", "grok"),
			filepath.Join(home, ".local", "bin", "grok"),
		)
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/opt/homebrew/bin/grok",
			"/usr/local/bin/grok",
		)
	} else if runtime.GOOS == "linux" {
		candidates = append(candidates,
			"/usr/local/bin/grok",
			"/usr/bin/grok",
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && isExecutable(c, st) {
			return c, nil
		}
	}

	// LookPath with PATH augmented for GUI-launched native hosts.
	pathEnv := augmentPATH(os.Getenv("PATH"), home)
	if p, err := lookPathWithPATH(name, pathEnv); err == nil {
		return p, nil
	}

	return "", fmt.Errorf(
		"grok CLI not found (looked in PATH and common locations like ~/.grok/bin); install from https://x.ai/cli or set GROK_SIDECHAT_CMD to the absolute path",
	)
}

// AugmentProcessEnv returns env for spawning grok: ensures ~/.grok/bin etc. are on PATH
// so the CLI and any tools it shells out to can resolve.
func AugmentProcessEnv(base []string, home string) []string {
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+1)
	found := false
	for _, e := range base {
		if strings.HasPrefix(e, "PATH=") {
			out = append(out, "PATH="+augmentPATH(strings.TrimPrefix(e, "PATH="), home))
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, "PATH="+augmentPATH("", home))
	}
	return out
}

func augmentPATH(current, home string) string {
	parts := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		parts = append(parts, p)
	}
	if home != "" {
		add(filepath.Join(home, ".grok", "bin"))
		add(filepath.Join(home, ".local", "bin"))
	}
	if runtime.GOOS == "darwin" {
		add("/opt/homebrew/bin")
		add("/usr/local/bin")
	}
	for _, p := range strings.Split(current, string(os.PathListSeparator)) {
		add(p)
	}
	// Keep system paths as fallback
	add("/usr/bin")
	add("/bin")
	return strings.Join(parts, string(os.PathListSeparator))
}

func lookPathWithPATH(file, pathEnv string) (string, error) {
	// Minimal reimplementation of exec.LookPath using a custom PATH.
	if filepath.IsAbs(file) {
		return file, nil
	}
	for _, dir := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, file)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && isExecutable(p, st) {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func isExecutable(path string, st os.FileInfo) bool {
	// Symlinks to binaries are fine (Grok Build uses ~/.grok/bin/grok -> downloads/...)
	mode := st.Mode()
	if mode&0o111 != 0 {
		return true
	}
	// On some volumes executable bit may be missing; still try if regular file.
	return mode.IsRegular()
}
