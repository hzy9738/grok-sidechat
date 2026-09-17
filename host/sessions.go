package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo is one Grok session for the side-panel history list.
type SessionInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Model       string `json:"model,omitempty"`
	NumMessages int    `json:"numMessages,omitempty"`
	// UpdatedUnix is for sorting only (not required by clients).
	UpdatedUnix int64 `json:"-"`
}

// SessionMessage is a simplified transcript row for the side panel feed.
type SessionMessage struct {
	Role string `json:"role"` // user | assistant | thinking
	Text string `json:"text"`
}

type summaryFile struct {
	Info struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"info"`
	SessionSummary  string `json:"session_summary"`
	GeneratedTitle  string `json:"generated_title"` // cc-connect store.go
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	LastActiveAt    string `json:"last_active_at"`
	NumMessages     int    `json:"num_messages"`
	NumChatMessages int    `json:"num_chat_messages"`
	CurrentModelID  string `json:"current_model_id"`
}

// GrokHome resolves GROK_HOME or ~/.grok.
func GrokHome() string {
	if h := strings.TrimSpace(os.Getenv("GROK_HOME")); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok")
}

// ListGrokSessions scans ~/.grok/sessions/**/summary.json (real Grok history).
// query filters title/summary/cwd/id (case-insensitive). limit<=0 defaults to 40.
func ListGrokSessions(grokHome, query string, limit int) ([]SessionInfo, error) {
	if grokHome == "" {
		grokHome = GrokHome()
	}
	root := filepath.Join(grokHome, "sessions")
	if limit <= 0 {
		limit = 40
	}
	q := strings.ToLower(strings.TrimSpace(query))

	var out []SessionInfo
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// skip sqlite / non-session trees
			name := d.Name()
			if name == "session_search.sqlite" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "summary.json" {
			return nil
		}
		info, ok := readSummary(path)
		if !ok {
			return nil
		}
		if q != "" {
			blob := strings.ToLower(strings.Join([]string{
				info.ID, info.Title, info.Summary, info.Cwd,
			}, " "))
			if !strings.Contains(blob, q) {
				return nil
			}
		}
		out = append(out, info)
		return nil
	})

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedUnix != out[j].UpdatedUnix {
			return out[i].UpdatedUnix > out[j].UpdatedUnix
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func readSummary(path string) (SessionInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionInfo{}, false
	}
	var s summaryFile
	if err := json.Unmarshal(data, &s); err != nil {
		return SessionInfo{}, false
	}
	id := strings.TrimSpace(s.Info.ID)
	if id == "" {
		// fallback: parent dir name
		id = filepath.Base(filepath.Dir(path))
	}
	if id == "" || id == "." || id == "sessions" {
		return SessionInfo{}, false
	}
	// Align with cc-connect store.go: session_summary then generated_title
	title := strings.TrimSpace(s.SessionSummary)
	if title == "" {
		title = strings.TrimSpace(s.GeneratedTitle)
	}
	if title == "" {
		title = "（无标题）"
	}
	// Truncate long titles for list UI (same idea as store.go rune limit)
	if runes := []rune(title); len(runes) > 60 {
		title = string(runes[:60]) + "…"
	}
	n := s.NumChatMessages
	if n == 0 {
		n = s.NumMessages
	}
	updatedAt := s.UpdatedAt
	if strings.TrimSpace(updatedAt) == "" {
		updatedAt = s.LastActiveAt
	}
	updatedUnix := parseTimeUnix(updatedAt)
	if updatedUnix == 0 {
		updatedUnix = parseTimeUnix(s.CreatedAt)
	}
	if updatedUnix == 0 {
		if st, err := os.Stat(path); err == nil {
			updatedUnix = st.ModTime().Unix()
		}
	}
	return SessionInfo{
		ID:          id,
		Title:       title,
		Summary:     strings.TrimSpace(firstNonEmpty(s.SessionSummary, s.GeneratedTitle)),
		Cwd:         strings.TrimSpace(s.Info.Cwd),
		UpdatedAt:   updatedAt,
		CreatedAt:   s.CreatedAt,
		Model:       s.CurrentModelID,
		NumMessages: n,
		UpdatedUnix: updatedUnix,
	}, true
}

func parseTimeUnix(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// RFC3339 / RFC3339Nano
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// LoadSessionMessages reads a compact user/assistant transcript for the side panel.
// Prefers chat_history.jsonl; falls back to updates.jsonl ACP chunks.
// maxMsgs <=0 → default 80 (most recent).
func LoadSessionMessages(grokHome, sessionID string, maxMsgs int) ([]SessionMessage, SessionInfo, error) {
	if grokHome == "" {
		grokHome = GrokHome()
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, SessionInfo{}, nil
	}
	if maxMsgs <= 0 {
		maxMsgs = 80
	}

	dir, meta, err := findSessionDir(grokHome, sessionID)
	if err != nil || dir == "" {
		return nil, meta, err
	}

	// chat_history.jsonl — cleaner for UI
	if msgs := loadChatHistory(filepath.Join(dir, "chat_history.jsonl"), maxMsgs); len(msgs) > 0 {
		return msgs, meta, nil
	}
	// fallback: export-like from updates
	if msgs := loadUpdatesTranscript(filepath.Join(dir, "updates.jsonl"), maxMsgs); len(msgs) > 0 {
		return msgs, meta, nil
	}
	return nil, meta, nil
}

func findSessionDir(grokHome, sessionID string) (string, SessionInfo, error) {
	root := filepath.Join(grokHome, "sessions")
	var found string
	var meta SessionInfo
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() != sessionID {
			return nil
		}
		// verify summary
		sum := filepath.Join(path, "summary.json")
		if info, ok := readSummary(sum); ok {
			found = path
			meta = info
			return filepath.SkipAll
		}
		// accept dir even without parseable summary
		if _, err := os.Stat(filepath.Join(path, "chat_history.jsonl")); err == nil {
			found = path
			meta = SessionInfo{ID: sessionID}
			return filepath.SkipAll
		}
		return nil
	})
	return found, meta, nil
}

func loadChatHistory(path string, maxMsgs int) []SessionMessage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all []SessionMessage
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		typ := strings.ToLower(stringValue(raw["type"]))
		switch typ {
		case "user":
			if t := contentToText(raw["content"]); t != "" {
				// strip heavy system wrappers for display
				t = trimUserDisplay(t)
				if t != "" {
					all = append(all, SessionMessage{Role: "user", Text: t})
				}
			}
		case "assistant":
			if t := contentToText(raw["content"]); t != "" {
				all = append(all, SessionMessage{Role: "assistant", Text: t})
			}
		case "reasoning":
			// optional short thinking summary
			if sum, ok := raw["summary"].([]any); ok {
				var b strings.Builder
				for _, item := range sum {
					m, ok := item.(map[string]any)
					if !ok {
						continue
					}
					if stringValue(m["type"]) == "summary_text" {
						if tx := stringValue(m["text"]); tx != "" {
							if b.Len() > 0 {
								b.WriteByte('\n')
							}
							b.WriteString(tx)
						}
					}
				}
				if b.Len() > 0 {
					all = append(all, SessionMessage{Role: "thinking", Text: b.String()})
				}
			}
		}
	}
	if len(all) > maxMsgs {
		all = all[len(all)-maxMsgs:]
	}
	return all
}

func loadUpdatesTranscript(path string, maxMsgs int) []SessionMessage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Aggregate chunks into messages
	var all []SessionMessage
	var curRole string
	var cur strings.Builder
	flush := func() {
		if curRole == "" || cur.Len() == 0 {
			cur.Reset()
			curRole = ""
			return
		}
		all = append(all, SessionMessage{Role: curRole, Text: cur.String()})
		cur.Reset()
		curRole = ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		params, _ := raw["params"].(map[string]any)
		if params == nil {
			continue
		}
		update, _ := params["update"].(map[string]any)
		if update == nil {
			continue
		}
		su := stringValue(update["sessionUpdate"])
		content, _ := update["content"].(map[string]any)
		text := ""
		if content != nil {
			text = stringValue(content["text"])
		}
		if text == "" {
			continue
		}
		role := ""
		switch su {
		case "user_message_chunk":
			role = "user"
		case "agent_message_chunk":
			role = "assistant"
		case "agent_thought_chunk":
			role = "thinking"
		default:
			continue
		}
		if curRole != "" && curRole != role {
			flush()
		}
		curRole = role
		cur.WriteString(text)
	}
	flush()
	if len(all) > maxMsgs {
		all = all[len(all)-maxMsgs:]
	}
	// trim user display noise
	for i := range all {
		if all[i].Role == "user" {
			all[i].Text = trimUserDisplay(all[i].Text)
		}
	}
	return all
}

func contentToText(v any) string {
	switch c := v.(type) {
	case string:
		return strings.TrimSpace(c)
	case []any:
		var b strings.Builder
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t := stringValue(m["text"]); t != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(t)
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

func trimUserDisplay(t string) string {
	t = strings.TrimSpace(t)
	// Prefer <user_query> body if present
	if i := strings.Index(t, "<user_query>"); i >= 0 {
		rest := t[i+len("<user_query>"):]
		if j := strings.Index(rest, "</user_query>"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	// Drop huge system-reminder prefixes for side panel readability
	if len(t) > 1200 && (strings.Contains(t, "<system-reminder>") || strings.Contains(t, "<user_info>")) {
		// keep last 800 chars as fallback
		if len(t) > 800 {
			return "…" + t[len(t)-800:]
		}
	}
	if len(t) > 4000 {
		return t[:4000] + "…"
	}
	return t
}
