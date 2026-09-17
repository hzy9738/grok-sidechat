package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type storedSession struct {
	Info     SessionInfo      `json:"info"`
	Messages []SessionMessage `json:"messages"`
}

// SidechatHome 解析 SIDECHAT_HOME 或 ~/.sidechat，与 GROK_HOME 隔离。
func SidechatHome() string {
	if h := strings.TrimSpace(os.Getenv("SIDECHAT_HOME")); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".sidechat")
}

func defaultSessionRoot() string {
	home := SidechatHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "sessions")
}

func (r *Runner) sessionRoot() string {
	if r != nil && strings.TrimSpace(r.StoreDir) != "" {
		return r.StoreDir
	}
	return defaultSessionRoot()
}

func newSessionID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sc-%d", time.Now().UnixMilli())
	}
	return fmt.Sprintf("sc-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func sessionFile(root, id string) string {
	return filepath.Join(root, id, "session.json")
}

func loadStoredSession(root, id string) (storedSession, error) {
	id = strings.TrimSpace(id)
	if root == "" || id == "" {
		return storedSession{}, fmt.Errorf("session not found")
	}
	data, err := os.ReadFile(sessionFile(root, id))
	if err != nil {
		return storedSession{}, err
	}
	var sess storedSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return storedSession{}, err
	}
	if sess.Info.ID == "" {
		sess.Info.ID = id
	}
	return sess, nil
}

func saveStoredSession(root string, sess storedSession) error {
	if root == "" || strings.TrimSpace(sess.Info.ID) == "" {
		return fmt.Errorf("invalid session")
	}
	dir := filepath.Join(root, sess.Info.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFile(root, sess.Info.ID), data, 0o600)
}

func titleFromUser(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return "新对话"
	}
	const max = 40
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max]) + "…"
}

func appendTurn(sess *storedSession, cwd, model, userText, assistant, thinking string) {
	now := time.Now().UTC().Format(time.RFC3339)
	if sess.Info.CreatedAt == "" {
		sess.Info.CreatedAt = now
	}
	sess.Info.UpdatedAt = now
	if parsed, err := time.Parse(time.RFC3339, now); err == nil {
		sess.Info.UpdatedUnix = parsed.Unix()
	}
	if cwd != "" {
		sess.Info.Cwd = cwd
	}
	if model != "" {
		sess.Info.Model = model
	}
	if sess.Info.Title == "" || sess.Info.Title == "新对话" {
		sess.Info.Title = titleFromUser(userText)
	}
	if strings.TrimSpace(userText) != "" {
		sess.Messages = append(sess.Messages, SessionMessage{Role: "user", Text: userText})
	}
	if strings.TrimSpace(thinking) != "" {
		sess.Messages = append(sess.Messages, SessionMessage{Role: "thinking", Text: thinking})
	}
	if strings.TrimSpace(assistant) != "" {
		sess.Messages = append(sess.Messages, SessionMessage{Role: "assistant", Text: assistant})
	}
	sess.Info.NumMessages = len(sess.Messages)
	sess.Info.Summary = strings.TrimSpace(assistant)
	if sess.Info.Summary == "" {
		sess.Info.Summary = strings.TrimSpace(userText)
	}
}

func ListStoredSessions(root, query string, limit int) ([]SessionInfo, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []SessionInfo
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		sess, err := loadStoredSession(root, ent.Name())
		if err != nil {
			continue
		}
		info := sess.Info
		if query != "" {
			blob := strings.ToLower(info.Title + " " + info.Summary + " " + info.ID)
			if !strings.Contains(blob, query) {
				continue
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func LoadStoredMessages(root, sessionID string, maxMsgs int) ([]SessionMessage, SessionInfo, error) {
	sess, err := loadStoredSession(root, sessionID)
	if err != nil {
		return nil, SessionInfo{}, err
	}
	msgs := sess.Messages
	if maxMsgs > 0 && len(msgs) > maxMsgs {
		msgs = msgs[len(msgs)-maxMsgs:]
	}
	return msgs, sess.Info, nil
}
