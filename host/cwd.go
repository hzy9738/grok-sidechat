package main

import (
	"net/url"
	"path/filepath"
	"strings"
)

// ResolveTurnCwd decides the project --cwd for one turn.
//
// Policy (no forced directory bind):
//   - empty / whitespace → "" (omit grok --cwd; do not inject process Getwd or page URL)
//   - absolute path → cleaned absolute path
//   - relative path → resolved against base (usually process Getwd only for path math, not as a forced project)
//
// Page URLs are never accepted as cwd. Callers must pass user/explicit cwd only.
func ResolveTurnCwd(explicit string, base string) string {
	cwd := strings.TrimSpace(explicit)
	if cwd == "" {
		return ""
	}
	// Reject URL-shaped values so ordinary browsing cannot bind a workspace.
	if looksLikeURL(cwd) {
		return ""
	}
	if filepath.IsAbs(cwd) {
		return filepath.Clean(cwd)
	}
	if strings.TrimSpace(base) == "" {
		// Relative without base: keep cleaned relative form (BuildGrokArgs still accepts it).
		return filepath.Clean(cwd)
	}
	return filepath.Clean(filepath.Join(base, cwd))
}

// LocalPathHintFromURL returns an optional path-like hint for localhost debug pages only.
// It is context text, never a forced --cwd.
func LocalPathHintFromURL(pageURL string) string {
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return ""
	}
	// file-ish path on localhost (e.g. http://localhost:5173/src/App.tsx) — hint only.
	p := strings.TrimSpace(u.Path)
	if p == "" || p == "/" {
		return ""
	}
	return p
}

func looksLikeURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") ||
		strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "file://") ||
		strings.HasPrefix(low, "chrome://") ||
		strings.HasPrefix(low, "chrome-extension://")
}
